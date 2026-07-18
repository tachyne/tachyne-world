package server

import (
	"sync"
	"sync/atomic"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// skinProperty is the game-profile textures blob (skins), carried into the
// PlayerInfo event so other clients render the skin.
type skinProperty struct {
	Name, Value, Signature string
}

// player is the per-connection game state for one client. It collects what used
// to live as locals threaded through the play loop (loaded chunks, position) and
// adds the creative inventory needed for block placement.
//
// Concurrency: the connection's own goroutine owns every field here. The hub
// never reads them — it keeps its own copy of position (see hub.tracked) fed by
// move events — so there are no shared-field races. The one exception is `out`,
// a channel, which the hub writes to and the writer goroutine drains.
type player struct {
	eid  int32 // server-assigned entity ID (unique per session)
	name string
	uuid [16]byte

	x, y, z    float64        // current position (this goroutine's copy, for streaming)
	yaw, pitch float32        // current look angles
	dim        int            // 0 overworld, 1 nether (connection-owned)
	props      []skinProperty // Mojang textures etc. (online mode)
	pendingDim atomic.Int32   // hub-requested dimension switch (-1 = none)
	// Written by the hub before pendingDim.Store, read by the connection after
	// Load (the atomic pair orders them). pendingFrom is the departure portal;
	// pendingDest, when OK, is the remembered destination portal base.
	pendingFrom   dimPos
	pendingDest   blockPos
	pendingDestOK bool
	onGround      bool
	sprinting     bool // from Entity Action start/stop-sprint (for hunger exhaustion)
	sneaking      bool // from Entity Action start/stop-sneak (place against usable blocks)

	hmu    sync.Mutex // guards hotbar (the hub mirrors the survival inventory in)
	hotbar [9]int32   // item id per hotbar slot (0 = empty)
	// hotbarPaint carries the painting/variant component of a creative-menu
	// painting preset per hotbar slot ("" = plain painting → random fit).
	hotbarPaint [9]string
	held        int  // selected hotbar slot, 0..8 (connection-owned)
	hudOn       bool // action-bar HUD preference (this goroutine's copy)

	hubX, hubZ atomic.Uint64 // hub-VALIDATED position (float bits) — chunk-stream gate
	digStartAt uint64        // tick the current survival dig began
	digPos     blockPos      // …and the block it's digging

	// viewDist: the client's requested view distance (client_information),
	// clamped to [2, viewRadius]. Vanilla streams min(client, server) chunks;
	// atomic because config/play readers and streamChunks may differ.
	viewDist atomic.Int32

	out      chan outPkt   // outbound packets; the writer goroutine owns the socket
	quit     chan struct{} // closed when the connection is tearing down
	quitOnce sync.Once     // quit closes exactly once (session teardown OR /kick)

	// crit is the reliable overflow for one-shot lifecycle frames (entity/player
	// add + remove). These MUST NOT be dropped — a lost removal leaves a frozen
	// "ghost" the client renders forever; a lost add leaves an invisible entity.
	// When `out` is full we park them here instead of dropping; the pump drains
	// crit directly (see decodeLoop), never through the bounded `out` channel, so
	// delivery never blocks the hub goroutine.
	crit     []outPkt
	critMu   sync.Mutex
	critWake chan struct{} // buffered(1): nudges the pump to drain crit
}

// outPkt is one typed domain event queued for the session pump (remote.go's
// decodeLoop serializes it as an attach frame).
type outPkt struct {
	ev any
}

func newPlayer(eid int32, name string, uuid [16]byte) *player {
	p := &player{
		eid:      eid,
		name:     name,
		uuid:     uuid,
		hudOn:    true,
		out:      make(chan outPkt, 256),
		quit:     make(chan struct{}),
		critWake: make(chan struct{}, 1),
	}
	p.pendingDim.Store(-1)
	p.viewDist.Store(viewRadius)
	return p
}

// radius is the effective chunk-stream radius: min(client request, server cap).
func (p *player) radius() int32 {
	r := p.viewDist.Load()
	if r < 2 {
		r = 2
	}
	if r > viewRadius {
		r = viewRadius
	}
	return r
}

// setViewDist records the client's requested view distance (from
// client_information in either the config or play state).
func (p *player) setViewDist(d int32) { p.viewDist.Store(d) }

// critCap bounds the reliable overflow. A client this far behind on lifecycle
// frames is hopelessly stalled (vanilla disconnects a slow client too); we drop
// the session rather than grow crit without bound.
const critCap = 8192

// isLifecycleFrame reports whether ev is a one-shot entity/player add-or-remove
// frame that must be delivered reliably and in order. Dropping one desyncs the
// client permanently: a lost removal leaves a frozen "ghost" the client renders
// forever, a lost add leaves an invisible entity. Position/metadata frames are
// deliberately NOT in this set — they're safe to drop, since the next absolute
// update re-syncs against what the viewer actually rendered.
func isLifecycleFrame(ev any) bool {
	switch e := ev.(type) {
	case attachproto.EntityAdd, attachproto.EntityRemove,
		attachproto.PlayerInfo, attachproto.PlayerGone:
		return true
	case attachproto.Chat:
		// A chat LINE must not be dropped — a lost message is gone forever
		// (unlike entity moves, which self-heal on the next absolute resync). The
		// HUD action-bar overlay (ActionBar) may still drop: it re-sends every
		// tick. Without this, a back-pressured session (e.g. a heavy admin view)
		// silently loses ALL room chat while reliable command replies get through.
		return !e.ActionBar
	}
	return false
}

// sendEv queues a domain event, BLOCKING until there is room. Called from a
// player's own handler paths, so back-pressure only ever stalls that one
// session, never the hub. Lifecycle frames divert to the reliable overflow so
// they neither block nor drop even when this runs on the hub goroutine.
func (p *player) sendEv(ev any) {
	if isLifecycleFrame(ev) {
		p.sendEvReliable(ev)
		return
	}
	select {
	case p.out <- outPkt{ev: ev}:
	case <-p.quit:
	}
}

// trySendEv queues a domain event without blocking, dropping on a full queue
// like trySend. Dropping stays safe for entity movement BY CONSTRUCTION here:
// the viewer-side renderer computes relative moves against what it actually
// rendered, so a dropped move event never desyncs — the next one just carries
// a bigger delta (or resyncs absolutely if it grew past the i16 range).
// Lifecycle frames are the exception (see isLifecycleFrame): they must not drop,
// so they divert to the reliable overflow.
func (p *player) trySendEv(ev any) {
	if isLifecycleFrame(ev) {
		p.sendEvReliable(ev)
		return
	}
	select {
	case p.out <- outPkt{ev: ev}:
	default:
	}
}

// sendEvReliable queues a lifecycle frame for guaranteed, in-order delivery.
// While the overflow is empty it uses the normal `out` channel (so a spawn stays
// FIFO with the metadata frames that follow it); once `out` fills, the frame —
// and every later lifecycle frame until the overflow clears — parks in `crit`,
// which the pump drains at lower priority than `out` (so temporal order across
// the two streams is preserved). Never blocks the caller, so it is safe on the
// hub goroutine. A session past critCap is hopelessly behind and is disconnected
// rather than growing crit without bound.
func (p *player) sendEvReliable(ev any) {
	p.critMu.Lock()
	if len(p.crit) > 0 { // overflow already active — stay in crit to keep FIFO
		if len(p.crit) >= critCap {
			p.critMu.Unlock()
			p.disconnect()
			return
		}
		p.crit = append(p.crit, outPkt{ev: ev})
		p.critMu.Unlock()
		p.wakeCrit()
		return
	}
	p.critMu.Unlock()
	select { // fast path: overflow empty, ride the normal queue
	case p.out <- outPkt{ev: ev}:
		return
	default:
	}
	p.critMu.Lock() // out is full — begin the overflow
	if len(p.crit) >= critCap {
		p.critMu.Unlock()
		p.disconnect()
		return
	}
	p.crit = append(p.crit, outPkt{ev: ev})
	p.critMu.Unlock()
	p.wakeCrit()
}

// wakeCrit nudges the pump to drain the overflow (buffered, so it never blocks).
func (p *player) wakeCrit() {
	select {
	case p.critWake <- struct{}{}:
	default:
	}
}

// takeCrit atomically drains the reliable overflow for the pump.
func (p *player) takeCrit() []outPkt {
	p.critMu.Lock()
	batch := p.crit
	p.crit = nil
	p.critMu.Unlock()
	return batch
}

// heldItem returns the item id in the player's selected hotbar slot.
func (p *player) heldItem() int32 {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	return p.hotbar[p.held]
}

// heldSlot returns the selected hotbar index (0-8).
func (p *player) heldSlot() int {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	return p.held
}

// setHotbarSlot records a hotbar slot's item — from the creative client (Set
// Creative Mode Slot) or the hub mirroring the survival inventory.
func (p *player) setHotbarSlot(slot int, item int32) {
	if slot < 0 || slot > 8 {
		return
	}
	p.hmu.Lock()
	if p.hotbar[slot] != item {
		// a DIFFERENT item clears a creative painting preset; same-item
		// writes keep it — the hub's inventory mirror re-asserts the slot
		// right after the creative set and must not wipe the preset
		p.hotbarPaint[slot] = ""
	}
	p.hotbar[slot] = item
	p.hmu.Unlock()
}

// setHotbarPaint records the painting preset carried by a creative slot set.
func (p *player) setHotbarPaint(slot int, variant string) {
	if slot < 0 || slot > 8 {
		return
	}
	p.hmu.Lock()
	p.hotbarPaint[slot] = variant
	p.hmu.Unlock()
}

// heldPaintVariant is the preset on the selected hotbar slot ("" = none).
func (p *player) heldPaintVariant() string {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	return p.hotbarPaint[p.held]
}

// disconnect tears the session down (used by /kick); safe alongside the
// normal leave path — quit closes exactly once.
func (p *player) disconnect() { p.quitOnce.Do(func() { close(p.quit) }) }
