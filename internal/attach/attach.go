// Package attach exposes the world to tachyne gateways over the domain
// attach protocol (tachyne-common/attach): sessions arrive here with
// gateway-stamped identity claims, and the world streams itself back as raw
// block-state/light arrays — no Minecraft wire format ("worlds are
// versionless"). v1 scope: welcome + chunk streaming + movement intake +
// keepalive + time; hub/entity integration (visibility to TCP players) is
// the next milestone.
package attach

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"sync"
	"time"

	"tachyne/internal/world"

	proto "github.com/tachyne/tachyne-common/attach"
)

// Config wires a listener to a world.
type Config struct {
	World *world.World
	Time  func() int64 // world day-time in ticks
	Token string       // shared secret gateways must present; "" = refuse all
	Spawn proto.Pos    // fallback spawn (used when Join is nil — solo mode)

	// Worlds picks the world for a dimension (0 overworld, 1 nether, 2 end);
	// nil = World only (solo/test mode).
	Worlds func(dim int32) *world.World

	// BlockEntities renders a chunk's block-entity section in canonical wire
	// form (nil = none) — chests/signs/beds render for gateway players.
	BlockEntities func(w *world.World, cx, cz int32) []byte

	// Join gives the session hub presence (multiplayer): emit receives domain
	// frames (entities/chat) to forward to the gateway. nil = solo walk-around.
	Join func(name string, uuid [16]byte, emit func(typ byte, payload []byte)) (Remote, error)

	// Owned reports whether this pod owns a chunk in a sharded world; the Want
	// handler serves only owned chunks, so the world ends at the pod's region
	// boundary. nil = own everything (unsharded/solo/test).
	Owned func(dim, cx, cz int32) bool

	// Resume binds a session to a player migrated here from a neighbour shard,
	// when Hello.Purpose == "resume" (token = Hello.ResumeToken). nil = resume
	// unsupported (falls back to a normal Join).
	Resume func(name string, uuid [16]byte, token string, emit func(typ byte, payload []byte)) (Remote, error)
}

// Remote is a hub-attached player, as the attach layer sees it.
type Remote interface {
	EID() int32
	Spawn() (x, y, z float64)
	Gamemode() int32
	Move(x, y, z float64, yaw, pitch float32, onGround bool)
	Chat(text string)
	Command(cmd string)
	// Action receives any typed serverbound action frame (stage 6b): a value
	// of one of the attach action types (UseItem, WindowClick, …).
	Action(v any)
	Dig(proto.Dig)
	Place(proto.Place)
	HeldSlot(slot int16)
	Leave()
}

// timeInterval is how often sessions get a Time frame (the client clock
// interpolates between them).
const timeInterval = 5 * time.Second

// maxRadius clamps a Want radius. Raised 8→32 for earth mode: real-terrain
// vistas (Table Mountain from the city bowl) need distance; 65² chunks stream
// progressively through the 4-worker build pool, and the LRU + Valkey caches
// absorb the churn. This is the HARD ceiling — the gateways' honored render
// distance (gwsession Config.ViewCap, default 12, env TACHYNE_VIEW_CAP)
// stays at or below it; raise a deployment's cap only where the vistas are
// worth ~(2r+1)² chunks per client.
const maxRadius = 32

// Serve accepts gateway sessions until ln closes.
func Serve(ln net.Listener, cfg Config) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("attach: accept: %v", err)
			}
			return
		}
		go session(c, cfg)
	}
}

// frame renders one message to raw bytes for the writer goroutine.
func frame(typ byte, payload []byte) []byte {
	var buf bytes.Buffer
	proto.WriteFrame(&buf, typ, payload)
	return buf.Bytes()
}

func frameJSON(typ byte, v any) []byte {
	var buf bytes.Buffer
	proto.WriteJSON(&buf, typ, v)
	return buf.Bytes()
}

func session(c net.Conn, cfg Config) {
	defer c.Close()

	// Hello must arrive promptly and authenticate. A clean close before any
	// bytes is the kubelet's TCP probe (readiness/liveness) — not a log line.
	c.SetReadDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(c)
	if _, err := br.Peek(1); err != nil {
		return
	}
	typ, payload, err := proto.ReadFrame(br)
	if err != nil || typ != proto.MsgHello {
		log.Printf("attach %s: no hello (typ %#x, %v)", c.RemoteAddr(), typ, err)
		return
	}
	var hello proto.Hello
	if err := jsonUnmarshal(payload, &hello); err != nil {
		log.Printf("attach %s: bad hello: %v", c.RemoteAddr(), err)
		return
	}
	if cfg.Token == "" || hello.Token != cfg.Token {
		c.Write(frameJSON(proto.MsgBye, proto.Bye{Reason: "attach: unauthorized"}))
		log.Printf("attach %s: unauthorized hello from %q", c.RemoteAddr(), hello.Gateway)
		return
	}
	c.SetReadDeadline(time.Time{})
	log.Printf("attach %s: session %q (%s) via %s roles=%v",
		c.RemoteAddr(), hello.Name, hello.UUID, hello.Gateway, hello.Roles)

	// Single writer goroutine; everything else queues frames.
	out := make(chan []byte, 256)
	done := make(chan struct{})
	var closeOnce sync.Once
	closeSession := func() { closeOnce.Do(func() { close(done); c.Close() }) }
	defer closeSession()
	go func() {
		for {
			select {
			case b := <-out:
				if _, err := c.Write(b); err != nil {
					closeSession()
					return
				}
			case <-done:
				return
			}
		}
	}()
	send := func(b []byte) bool {
		select {
		case out <- b:
			return true
		case <-done:
			return false
		}
	}

	welcome := proto.Welcome{Spawn: cfg.Spawn, Time: cfg.Time(), MinY: proto.MinY, Sections: proto.Sections}
	var remote Remote
	// Welcome MUST be the session's first frame (gateways refuse otherwise),
	// but Join emits frames synchronously (command tree, abilities) and its
	// decode loop may start emitting immediately — so emissions are held back
	// until Welcome is on the queue, then flushed in arrival order.
	var preMu sync.Mutex
	var pre [][]byte
	welcomed := false
	if cfg.Join != nil || cfg.Resume != nil {
		var uuid [16]byte
		parseUUID(hello.UUID, &uuid)
		emit := func(typ byte, payload []byte) {
			b := frame(typ, payload)
			preMu.Lock()
			if !welcomed {
				pre = append(pre, b)
				preMu.Unlock()
				return
			}
			preMu.Unlock()
			send(b)
		}
		var r Remote
		var err error
		switch {
		case hello.Purpose == "resume" && cfg.Resume != nil:
			r, err = cfg.Resume(hello.Name, uuid, hello.ResumeToken, emit)
		case cfg.Join != nil:
			r, err = cfg.Join(hello.Name, uuid, emit)
		default:
			err = fmt.Errorf("attach: no handler for purpose %q", hello.Purpose)
		}
		if err != nil {
			send(frameJSON(proto.MsgBye, proto.Bye{Reason: "attach: join failed"}))
			return
		}
		remote = r
		defer remote.Leave()
		welcome.EID = remote.EID()
		welcome.Spawn.X, welcome.Spawn.Y, welcome.Spawn.Z = remote.Spawn()
		welcome.Gamemode = remote.Gamemode()
	}
	send(frameJSON(proto.MsgWelcome, welcome))
	preMu.Lock() // flush held frames; concurrent emits block until we're done
	for _, b := range pre {
		send(b)
	}
	pre = nil
	welcomed = true
	preMu.Unlock()

	// Periodic world clock.
	go func() {
		t := time.NewTicker(timeInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if !send(frameJSON(proto.MsgTime, proto.Time{Time: cfg.Time()})) {
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Chunk streaming: Want requests queue coordinates; a small worker pool
	// builds chunk frames (generation + lighting are the expensive bits) and
	// the sent-set ensures each chunk goes once per session.
	// Sized to hold a full max-radius window so one Want never silently drops
	// chunks (the drop-and-retry below stays as a backstop for pathological
	// Want bursts — a dropped chunk is only re-requested on the NEXT Want, so
	// a stationary player would otherwise keep a hole in the world).
	wants := make(chan [3]int32, (2*maxRadius+1)*(2*maxRadius+1)+64) // {dim, cx, cz}
	defer close(wants)
	sent := map[[3]int32]bool{}
	worldFor := func(dim int32) *world.World {
		if cfg.Worlds != nil {
			if w := cfg.Worlds(dim); w != nil {
				return w
			}
		}
		return cfg.World
	}
	for range 4 {
		go func() {
			for cc := range wants {
				var bes []byte
				if cfg.BlockEntities != nil {
					bes = cfg.BlockEntities(worldFor(cc[0]), cc[1], cc[2])
				}
				b, err := buildChunk(worldFor(cc[0]), cc[0], cc[1], cc[2], bes)
				if err != nil {
					log.Printf("attach %s: chunk %d,%d: %v", c.RemoteAddr(), cc[0], cc[1], err)
					continue
				}
				if !send(b) {
					return
				}
			}
		}()
	}

	for {
		typ, payload, err := proto.ReadFrame(br)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("attach %s: read: %v", c.RemoteAddr(), err)
			}
			return
		}
		switch typ {
		case proto.MsgWant:
			var w proto.Want
			if err := jsonUnmarshal(payload, &w); err != nil {
				continue
			}
			if w.Radius > maxRadius {
				w.Radius = maxRadius
			}
			// The client trims chunks outside its view window as it moves (and
			// drops everything on a dimension switch); forget those too so a
			// returning player gets them re-sent.
			for cc := range sent {
				if cc[0] != w.Dim || abs32(cc[1]-w.CX) > w.Radius+2 || abs32(cc[2]-w.CZ) > w.Radius+2 {
					delete(sent, cc)
				}
			}
			// Collect the not-yet-sent window, then enqueue NEAREST-FIRST:
			// the chunk under the player must build before the horizon —
			// the client sits on "Loading terrain" until its own chunk
			// arrives, and outward fill matches how terrain actually
			// becomes visible. (A raster sweep here once queued the player's
			// chunk ~half a 65×65 window deep: tens of seconds of loading
			// screen at earth-mode radii.)
			var queue [][3]int32
			for cx := w.CX - w.Radius; cx <= w.CX+w.Radius; cx++ {
				for cz := w.CZ - w.Radius; cz <= w.CZ+w.Radius; cz++ {
					if cfg.Owned != nil && !cfg.Owned(w.Dim, cx, cz) {
						continue // finite world: this pod doesn't own the chunk, don't serve it
					}
					cc := [3]int32{w.Dim, cx, cz}
					if w.Force || !sent[cc] { // Force re-queues even if already sent (/refresh)
						queue = append(queue, cc)
					}
				}
			}
			nearestFirst(queue, w.CX, w.CZ)
			for _, cc := range queue {
				sent[cc] = true
				select {
				case wants <- cc:
				default: // backlog full; the next Want retries
					delete(sent, cc)
				}
			}
		case proto.MsgMove:
			if remote != nil {
				var m proto.Move
				if jsonUnmarshal(payload, &m) == nil {
					remote.Move(m.X, m.Y, m.Z, m.Yaw, m.Pitch, m.OnGround)
				}
			}
		case proto.MsgChat:
			if remote != nil {
				var ch proto.Chat
				if jsonUnmarshal(payload, &ch) == nil && ch.Text != "" {
					remote.Chat(ch.Text)
				}
			}
		case proto.MsgCommand:
			if remote != nil {
				var cm proto.Command
				if jsonUnmarshal(payload, &cm) == nil && cm.Cmd != "" {
					remote.Command(cm.Cmd)
				}
			}
		case proto.MsgUseItem:
			if remote != nil {
				remote.Action(proto.UseItem{})
			}
		case proto.MsgUseEntity:
			actTo(remote, payload, proto.UseEntity{})
		case proto.MsgSelTrade:
			actTo(remote, payload, proto.SelTrade{})
		case proto.MsgInput:
			actTo(remote, payload, proto.Input{})
		case proto.MsgWindowClick:
			actTo(remote, payload, proto.WindowClick{})
		case proto.MsgCraft:
			actTo(remote, payload, proto.Craft{})
		case proto.MsgWindowClose:
			if remote != nil {
				remote.Action(proto.WindowClose{})
			}
		case proto.MsgNameItem:
			actTo(remote, payload, proto.NameItem{})
		case proto.MsgEnchant:
			actTo(remote, payload, proto.Enchant{})
		case proto.MsgPlayerAction:
			actTo(remote, payload, proto.PlayerAction{})
		case proto.MsgRespawnReq:
			if remote != nil {
				remote.Action(proto.RespawnReq{})
			}
		case proto.MsgCreativeSlot:
			actTo(remote, payload, proto.CreativeSlot{})
		case proto.MsgVehicleMove: // gw→w: the rider steering its vehicle
			actTo(remote, payload, proto.VehicleMove{})
		case proto.MsgDig:
			if remote != nil {
				var d proto.Dig
				if jsonUnmarshal(payload, &d) == nil {
					remote.Dig(d)
				}
			}
		case proto.MsgPlace:
			if remote != nil {
				var pl proto.Place
				if jsonUnmarshal(payload, &pl) == nil {
					remote.Place(pl)
				}
			}
		case proto.MsgHeldSlot:
			if remote != nil {
				var hs proto.HeldSlot
				if jsonUnmarshal(payload, &hs) == nil {
					remote.HeldSlot(hs.Slot)
				}
			}
		case proto.MsgPing:
			if !send(frame(proto.MsgPong, payload)) {
				return
			}
		case proto.MsgBye:
			log.Printf("attach %s: session %q closed by gateway", c.RemoteAddr(), hello.Name)
			return
		default:
			log.Printf("attach %s: unknown frame %#x", c.RemoteAddr(), typ)
		}
	}
}

// buildChunk renders one chunk column into a domain Chunk frame.
func buildChunk(w *world.World, dim, cx, cz int32, bes []byte) ([]byte, error) {
	ch := w.Chunk(cx, cz)
	light := w.Light(cx, cz)

	body := &proto.ChunkBody{
		BlockStates: make([]uint32, proto.BlocksPerCh),
		Heightmap:   make([]int16, 256),
		SkyLight:    make([]uint8, proto.BlocksPerCh),
		BlockLight:  make([]uint8, proto.BlocksPerCh),
	}
	for s := range proto.Sections {
		copy(body.BlockStates[s*4096:], ch.Sections[s][:])
		copy(body.SkyLight[s*4096:], light.Sky[s][:])
		copy(body.BlockLight[s*4096:], light.Block[s][:])
	}
	copy(body.Heightmap, ch.Heightmap[:])

	payload, err := proto.EncodeChunk(proto.ChunkHeader{CX: cx, CZ: cz, Dim: dim, Biomes: ch.Biomes[:], BEs: bes}, body)
	if err != nil {
		return nil, err
	}
	return frame(proto.MsgChunk, payload), nil
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// parseUUID reads a dashed-hex UUID string into raw bytes (best effort).
func parseUUID(s string, out *[16]byte) {
	i := 0
	for j := 0; j+1 < len(s) && i < 16; j++ {
		if s[j] == '-' {
			continue
		}
		hi, ok1 := hexVal(s[j])
		lo, ok2 := hexVal(s[j+1])
		if ok1 && ok2 {
			out[i] = hi<<4 | lo
			i++
			j++
		}
	}
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// nearestFirst orders a chunk-coordinate queue by squared distance from the
// Want center so the build pool serves the player's own chunk first and fills
// outward. Ties keep their raster order (stable sort) for determinism.
func nearestFirst(queue [][3]int32, cx, cz int32) {
	dist2 := func(cc [3]int32) int64 {
		dx, dz := int64(cc[1]-cx), int64(cc[2]-cz)
		return dx*dx + dz*dz
	}
	sort.SliceStable(queue, func(i, j int) bool {
		return dist2(queue[i]) < dist2(queue[j])
	})
}

// actTo decodes an action frame into a fresh value of proto's type and hands
// it to the session's Remote. Generic over the action type via the zero-value
// argument (Go's encoding/json needs a concrete pointer).
func actTo[T any](remote Remote, payload []byte, zero T) {
	if remote == nil {
		return
	}
	if jsonUnmarshal(payload, &zero) == nil {
		remote.Action(zero)
	}
}
