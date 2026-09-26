package server

import (
	"math"
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

	bedrock bool   // joined through the Bedrock gateway (Identity.Edition)
	ip      string // the client's address, from the gateway (for /ban-ip)

	x, y, z    float64        // current position (this goroutine's copy, for streaming)
	yaw, pitch float32        // current look angles
	dim        int            // 0 overworld, 1 nether (connection-owned)
	props      []skinProperty // Mojang textures etc. (online mode)
	pendingDim atomic.Int32   // hub-requested dimension switch (-1 = none)
	// Written by the hub before pendingDim.Store, read by the connection after
	// Load (the atomic pair orders them). pendingDest, when OK, is a known
	// landing cell (a bed, the spawn, a teleport); pendingAt is a nether
	// portal's exact arrival spot and heading, worked out by the hub.
	pendingDest   blockPos
	pendingDestOK bool
	pendingAt     bool
	pendingPos    [3]float64
	pendingYaw    float32
	onGround      bool
	sprinting     bool // from Entity Action start/stop-sprint (for hunger exhaustion)
	sneaking      bool // from Entity Action start/stop-sneak (place against usable blocks)

	// ackSeq is the highest block-prediction sequence this player has sent,
	// stored +1 so the zero value means "nothing to acknowledge". The
	// connection goroutine records it as actions arrive; the hub drains it
	// once a tick and sends one BlockAck, which is vanilla's cadence —
	// ServerGamePacketListenerImpl keeps the max and flushes it at the top of
	// the next tick, after the changes the action produced.
	latency atomic.Int32 // ms, from the gateway's keep-alive round trips (the tab list shows it)
	ackSeq  atomic.Int32

	// lastAction is ServerPlayer.lastActionTime (unix millis): the last time
	// the player did something — moved, pressed a key, clicked, typed. The
	// idle timeout (/setidletimeout) measures from it.
	lastAction atomic.Int64

	// loadedOnly is set by the hub while this player is a spectator and
	// spectators_generate_chunks is off: the chunk stream then sends only
	// chunks that are already loaded (ChunkMap.skipPlayer).
	loadedOnly atomic.Bool

	digBonusMirror atomic.Uint64 // MINING_EFFICIENCY, as float64 bits (hub -> session)
	digMultMirror  atomic.Uint64 // the dig-speed multipliers (Haste, BLOCK_BREAK_SPEED), float64 bits
	offhandMirror  atomic.Int32  // the offhand's item id (setOffhand), for the use-item dispatch
	leading        atomic.Int32  // mobs on this player's leads (hub → session): a fence click ties them
	hmu            sync.Mutex    // guards hotbar (the hub mirrors the survival inventory in)
	hotbar         [9]int32      // item id per hotbar slot (0 = empty)
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

	// view is the client's chunk view as its attach session reports it
	// (attach.ChunkViewer): the window centre and render distance from the
	// latest Want, and the chunks actually sent within it. Entity tracking
	// reads it on the hub goroutine; the session writes it from its reader and
	// build-pool goroutines. A player with no session (tests, the local path)
	// never sets known and is treated as holding every chunk.
	view struct {
		sync.Mutex
		known          bool
		dim, cx, cz, r int32
		sent           map[[3]int32]bool
	}

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

// key is the player's store key: its UUID (playerkeys.go).
func (p *player) key() string { return uuidString(p.uuid) }

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
	p.touch() // ServerPlayer: lastActionTime starts at creation
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
		attachproto.PlayerInfo, attachproto.PlayerInfoMode, attachproto.PlayerGone:
		return true
	case attachproto.BlockAck:
		// A dropped acknowledgement leaves the client showing its own guess
		// at that position for good, ignoring everything the world says
		// about it — and the hub sends this one, so it must neither block
		// nor drop.
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

// offhandItem mirrors the offhand's item id onto the SESSION side. The hub
// owns the inventory, but the use-item dispatch runs on the session
// goroutine and has to know what the offhand holds — a shield lives there.
func (p *player) setOffhand(item int32) { p.offhandMirror.Store(item) }
func (p *player) offhandItem() int32    { return p.offhandMirror.Load() }

// digBonus and digMult mirror the player's dig model onto the SESSION side:
// the MINING_EFFICIENCY addend (Efficiency, or whatever else raised it) and
// the multipliers Player.getDestroySpeed applies after it (Haste or Conduit
// Power, and BLOCK_BREAK_SPEED). The hub owns effects and attributes, but the
// fast-break check runs on the session goroutine and would otherwise have to
// guess — and guessing cannot work: Efficiency V on a wooden pickaxe is a
// fourteen-fold speed-up, and /effect can give Haste 255.
func (p *player) setDigModel(bonus, mult float64) {
	p.digBonusMirror.Store(math.Float64bits(bonus))
	p.digMultMirror.Store(math.Float64bits(mult))
}
func (p *player) digBonus() float64 { return math.Float64frombits(p.digBonusMirror.Load()) }
func (p *player) digMult() float64 {
	if b := p.digMultMirror.Load(); b != 0 {
		return math.Float64frombits(b)
	}
	return 1 // not yet mirrored
}

// handItem is the item in the hand a packet named (InteractionHand): the
// selected hotbar slot's, or the offhand's.
func (p *player) handItem(off bool) int32 {
	if off {
		return p.offhandItem()
	}
	return p.heldItem()
}

// handSlot is where that hand's stack lives, as the hub's hand events take
// it: the selected hotbar index, or offhandSlot.
func (p *player) handSlot(off bool) int32 {
	if off {
		return offhandSlot
	}
	return int32(p.held)
}

// handPaintVariant is the painting preset the hand carries. Only hotbar
// presets are mirrored, so an offhand painting hangs a random variant.
func (p *player) handPaintVariant(off bool) string {
	if off {
		return ""
	}
	return p.heldPaintVariant()
}

// heldSlot returns the selected hotbar index (0-8).
func (p *player) heldSlot() int {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	return p.held
}

// setHeldSlot is a server-side hotbar selection (a middle-click pick);
// the caller tells the client with HeldSync.
func (p *player) setHeldSlot(slot int) {
	if slot < 0 || slot > 8 {
		return
	}
	p.hmu.Lock()
	p.held = slot
	p.hmu.Unlock()
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

// noteAck records a block-prediction sequence to acknowledge. Vanilla keeps
// the maximum, so a sequence older than one already pending is ignored.
func (p *player) noteAck(seq int32) {
	if seq < 0 {
		return
	}
	for {
		cur := p.ackSeq.Load()
		if cur >= seq+1 {
			return
		}
		if p.ackSeq.CompareAndSwap(cur, seq+1) {
			return
		}
	}
}

// takeAck drains the pending sequence, if any.
func (p *player) takeAck() (int32, bool) {
	v := p.ackSeq.Swap(0)
	return v - 1, v != 0
}

// viewWindow records a Want: the window's centre and radius, and forgets the
// chunks the client has dropped — outside the window's +2 margin or in
// another dimension, the same rule the attach session uses to re-send.
func (p *player) viewWindow(dim, cx, cz, r int32) {
	p.view.Lock()
	defer p.view.Unlock()
	p.view.known = true
	p.view.dim, p.view.cx, p.view.cz, p.view.r = dim, cx, cz, r
	if p.view.sent == nil {
		p.view.sent = map[[3]int32]bool{}
	}
	for cc := range p.view.sent {
		if cc[0] != dim || abs32(cc[1]-cx) > r+2 || abs32(cc[2]-cz) > r+2 {
			delete(p.view.sent, cc)
		}
	}
}

// chunkSent records a chunk the attach session has written to the client.
func (p *player) chunkSent(dim, cx, cz int32) {
	p.view.Lock()
	if p.view.sent == nil {
		p.view.sent = map[[3]int32]bool{}
	}
	p.view.sent[[3]int32{dim, cx, cz}] = true
	p.view.Unlock()
}

// chunkView is the view as one tracking pass reads it, under the lock taken
// once per viewer so the pass does not lock per entity.
type chunkView struct {
	known          bool
	dim, cx, cz, r int32
	sent           map[[3]int32]bool
}

// lockView takes the view's lock and returns it as a chunkView (the sent set
// by reference: the pass only reads it, and holds the lock the writers take
// until unlockView).
func (p *player) lockView() chunkView {
	p.view.Lock()
	return chunkView{p.view.known, p.view.dim, p.view.cx, p.view.cz, p.view.r, p.view.sent}
}

func (p *player) unlockView() { p.view.Unlock() }

// trackingDistance is ChunkMap.getPlayerViewDistance: the client's render
// distance clamped to [2, the server's] — the gateway has already applied
// that clamp to the radius it asks for, so the Want radius is the answer.
// Before any Want it is the default interest radius.
func (v chunkView) trackingDistance() int32 {
	if !v.known {
		return viewRadius
	}
	return max(v.r, 2)
}

// holds is ChunkMap.isChunkTracked: the chunk is inside the player's chunk
// tracking view (ChunkTrackingView.contains, neighbours included — a circle
// of the render distance with a two-chunk buffer) and is not still pending,
// i.e. it has been sent.
func (v chunkView) holds(dim, cx, cz int32) bool {
	if !v.known {
		return true
	}
	if dim != v.dim || !chunkWithinDistance(v.cx, v.cz, v.r, cx, cz, true) {
		return false
	}
	return v.sent[[3]int32{dim, cx, cz}]
}

// chunkWithinDistance is ChunkTrackingView.isWithinDistance.
func chunkWithinDistance(centerX, centerZ, viewDistance, chunkX, chunkZ int32, includeNeighbors bool) bool {
	buffer := int64(1)
	if includeNeighbors {
		buffer = 2
	}
	dx := max(0, absI64(int64(chunkX-centerX))-buffer)
	dz := max(0, absI64(int64(chunkZ-centerZ))-buffer)
	return dx*dx+dz*dz < int64(viewDistance)*int64(viewDistance)
}

func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
