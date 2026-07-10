package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
)

// awarenessRadiusChunks bounds which neighbours a pod keeps warm links to: the
// chunk view radius, so any neighbour whose region a player could see (and thus
// hand entities to, incl. diagonal corners via NeighboursWithin) has a link.
const awarenessRadiusChunks = 8

// onPeerFrame handles a frame arriving from a neighbour over the peer mesh. It
// runs on a peer reader goroutine, so anything touching hub state must go through
// h.post (the hub's event channel) — never mutate hub state here directly.
//
// It runs on a peer reader goroutine, so it hands off to the hub goroutine via
// h.post — handlePeerFrame (handoff.go) does the actual work under the single
// writer. The payload is a fresh slice from ReadFrame, safe to keep.
func (h *hub) onPeerFrame(from int32, typ byte, payload []byte) {
	h.post(evPeerFrame{from: from, typ: typ, payload: payload})
}

// peerMesh manages this pod's warm world↔world links to its neighbour shards.
// Handover and shadow frames travel these DIRECT connections (never a broker),
// so they are pre-established at boot: an entity crossing a seam is one small
// message + ack over an already-open socket (invariant #1 — instant handover).
//
// To avoid a double link per pair, the LOWER sid dials the higher; every pod
// also listens for inbound links from lower-sid neighbours. A dropped link is
// re-dialed (by the lower sid) with backoff. Topology + a shared token are
// asserted at handshake, so a stale or unauthorized peer is rejected at connect.
type peerMesh struct {
	sid      int32
	token    string
	topoHash string
	addrOf   func(sid int32) string // neighbour sid -> "host:port"
	onFrame  func(from int32, typ byte, payload []byte)

	mu    sync.Mutex
	conns map[int32]*peerConn
	quit  chan struct{}
}

type peerConn struct {
	c   net.Conn
	wmu sync.Mutex // serialises frame writes on this link
}

func newPeerMesh(sid int32, topoHash, token string, addrOf func(int32) string, onFrame func(int32, byte, []byte)) *peerMesh {
	return &peerMesh{
		sid: sid, token: token, topoHash: topoHash, addrOf: addrOf, onFrame: onFrame,
		conns: map[int32]*peerConn{}, quit: make(chan struct{}),
	}
}

// serve accepts inbound peer links until the listener closes.
func (m *peerMesh) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed (shutdown)
		}
		go func() {
			peer, err := m.handshake(c, false)
			if err != nil {
				log.Printf("peer: inbound handshake: %v", err)
				c.Close()
				return
			}
			m.readLoop(peer, c)
		}()
	}
}

// dial opens (and keeps re-opening) links to the neighbours this pod initiates
// to — those with a higher sid than ours.
func (m *peerMesh) dial(neighbours []int32) {
	for _, n := range neighbours {
		if n > m.sid { // lower sid dials; equal/lower neighbours dial us
			go m.dialLoop(n)
		}
	}
}

func (m *peerMesh) dialLoop(peer int32) {
	backoff := 250 * time.Millisecond
	for {
		if m.closed() {
			return
		}
		if c, err := net.DialTimeout("tcp", m.addrOf(peer), 5*time.Second); err == nil {
			if _, herr := m.handshake(c, true); herr == nil {
				backoff = 250 * time.Millisecond
				m.readLoop(peer, c) // blocks until the link drops
			} else {
				log.Printf("peer: dial handshake %d: %v", peer, herr)
				c.Close()
			}
		}
		if m.sleep(backoff) {
			return
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

// readLoop registers the link, pumps inbound frames to onFrame, and deregisters
// on any error. Blocks for the life of the connection.
func (m *peerMesh) readLoop(peer int32, c net.Conn) {
	pc := &peerConn{c: c}
	m.mu.Lock()
	if old := m.conns[peer]; old != nil {
		old.c.Close() // replace a stale link (e.g. after a peer restart)
	}
	m.conns[peer] = pc
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.conns[peer] == pc {
			delete(m.conns, peer)
		}
		m.mu.Unlock()
		c.Close()
	}()

	br := bufio.NewReader(c)
	for {
		typ, payload, err := attachproto.ReadFrame(br)
		if err != nil {
			return
		}
		if typ == handover.MsgPeerHello {
			continue // stray post-handshake hello — ignore
		}
		if m.onFrame != nil {
			m.onFrame(peer, typ, payload)
		}
	}
}

// send delivers a JSON frame to a neighbour over its warm link. Returns an error
// (never blocks on a dead link) if the neighbour is not currently connected.
func (m *peerMesh) send(peer int32, typ byte, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	pc := m.conns[peer]
	m.mu.Unlock()
	if pc == nil {
		return fmt.Errorf("peer %d not connected", peer)
	}
	pc.wmu.Lock()
	defer pc.wmu.Unlock()
	return attachproto.WriteFrame(pc.c, typ, payload)
}

// connected reports whether the link to a neighbour is currently up.
func (m *peerMesh) connected(peer int32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conns[peer] != nil
}

func (m *peerMesh) close() {
	select {
	case <-m.quit:
	default:
		close(m.quit)
	}
	m.mu.Lock()
	for _, pc := range m.conns {
		pc.c.Close()
	}
	m.mu.Unlock()
}

// handshake exchanges PeerHello, authenticates the token, and asserts topology
// agreement. The dialer sends first; the acceptor replies. Returns the peer sid.
func (m *peerMesh) handshake(c net.Conn, dial bool) (int32, error) {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	defer c.SetDeadline(time.Time{})

	send := func() error {
		b, _ := json.Marshal(handover.PeerHello{SID: m.sid, Token: m.token, Topo: m.topoHash})
		return attachproto.WriteFrame(c, handover.MsgPeerHello, b)
	}
	recv := func() (handover.PeerHello, error) {
		typ, payload, err := attachproto.ReadFrame(c)
		if err != nil {
			return handover.PeerHello{}, err
		}
		if typ != handover.MsgPeerHello {
			return handover.PeerHello{}, fmt.Errorf("expected PeerHello, got %#x", typ)
		}
		var ph handover.PeerHello
		return ph, json.Unmarshal(payload, &ph)
	}

	var peer handover.PeerHello
	var err error
	if dial {
		if err = send(); err != nil {
			return 0, err
		}
		peer, err = recv()
	} else {
		if peer, err = recv(); err == nil {
			err = send()
		}
	}
	if err != nil {
		return 0, err
	}
	if m.token == "" || peer.Token != m.token {
		return 0, fmt.Errorf("peer %d unauthorized", peer.SID)
	}
	if peer.Topo != m.topoHash {
		return 0, fmt.Errorf("peer %d topology mismatch", peer.SID)
	}
	return peer.SID, nil
}

func (m *peerMesh) closed() bool {
	select {
	case <-m.quit:
		return true
	default:
		return false
	}
}

// sleep waits d or until close; returns true if the mesh is shutting down.
func (m *peerMesh) sleep(d time.Duration) bool {
	select {
	case <-m.quit:
		return true
	case <-time.After(d):
		return false
	}
}
