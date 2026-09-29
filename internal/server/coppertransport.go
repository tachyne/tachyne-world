package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// The copper golem's work (CopperGolemAi + TransportItemsBetweenContainers).
// Empty-handed it looks for the nearest copper chest within 32 blocks
// sideways and 8 up or down; carrying, for the nearest plain or trapped
// chest. It walks there (opening wooden doors on the way), waits its turn
// three blocks off while anyone else has the chest open, and once beside it
// opens the lid, plays its animation and, nine ticks in, its sound. Sixty
// ticks after arriving it closes the lid and does the work: from a copper
// chest it takes up to sixteen of the first stack; into a chest that is
// empty or already holds that item it puts the stack into the first slot
// that takes it. A chest it has tried is remembered for 6000 ticks; after
// ten tries, fifty unreachable chests or none left to try it rests for 140
// ticks (TRANSPORT_ITEMS_COOLDOWN_TICKS), and only then strolls a block or
// two about or stands (the idle RunOne). A chest that would not take its
// load is skipped for the next nearest; a successful pickup or drop clears
// the memories.

const (
	cgSearchH        = 32   // TRANSPORT_ITEM_HORIZONTAL_SEARCH_RADIUS
	cgSearchV        = 8    // TRANSPORT_ITEM_VERTICAL_SEARCH_RADIUS
	cgMaxStack       = 16   // TRANSPORTED_ITEM_MAX_STACK_SIZE
	cgInteractTicks  = 60   // TARGET_INTERACTION_TIME
	cgOpenTick       = 1    // TICK_TO_START_ON_REACHED_INTERACTION
	cgSoundTick      = 9    // TICK_TO_PLAY_ON_REACHED_SOUND
	cgMemoryTicks    = 6000 // VISITED_POSITIONS_MEMORY_TIME
	cgMaxVisited     = 10   // MAX_VISITED_POSITIONS
	cgMaxUnreachable = 50   // MAX_UNREACHABLE_POSITIONS
	cgIdleCooldown   = 140  // IDLE_COOLDOWN
	cgQueueDist      = 3.0  // CLOSE_ENOUGH_TO_START_QUEUING_DISTANCE
	cgStayDist       = 2.0  // CLOSE_ENOUGH_TO_CONTINUE_INTERACTING_WITH_TARGET
	cgPathNodes      = 1500
)

// TransportItemState.
const (
	cgTravelling = iota
	cgQueuing
	cgInteracting
)

// ContainerInteractionState, one up (0 = none).
const (
	cgPickupItem = iota + 1
	cgPickupNoItem
	cgPlaceItem
	cgPlaceNoItem
)

// CopperGolemState ids.
const (
	cgStateIdle = iota
	cgStateGettingItem
	cgStateGettingNoItem
	cgStateDroppingItem
	cgStateDroppingNoItem
)

// cgInteraction is getTargetReachedInteractions: each interaction's golem
// state and the sound it makes nine ticks in.
var cgInteraction = map[int8]struct {
	state int8
	sound string
}{
	cgPickupItem:   {cgStateGettingItem, "minecraft:entity.copper_golem.no_item_get"},
	cgPickupNoItem: {cgStateGettingNoItem, "minecraft:entity.copper_golem.no_item_no_get"},
	cgPlaceItem:    {cgStateDroppingItem, "minecraft:entity.copper_golem.item_drop"},
	cgPlaceNoItem:  {cgStateDroppingNoItem, "minecraft:entity.copper_golem.item_no_drop"},
}

const (
	metaIndexCopperGolemState = 17 // COPPER_GOLEM_STATE, after the weather state; 17 on 26.2 and 26.3 alike
	// copperGolemStateSynced gates the COPPER_GOLEM_STATE metadata. The
	// gateways restore only index 16's serializer (FixCopperGolemMeta); an
	// INT at 17 would disconnect every client that sees the golem, so the
	// state stays server-side until they restore 17 as well.
	copperGolemStateSynced = false
)

// copperGolemStateMeta is the golem's interaction state as a plain INT at
// index 17, the placeholder the gateway turns into COPPER_GOLEM_STATE.
func copperGolemStateMeta(eid int32, state int8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCopperGolemState)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(state))
	return protocol.AppendU8(b, itemMetaEnd)
}

// setCopperGolemState is CopperGolem.setState.
func (h *hub) setCopperGolemState(players map[int32]*tracked, m *mob, state int8) {
	if m.cgState == state {
		return
	}
	m.cgState = state
	if copperGolemStateSynced {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(copperGolemStateMeta(m.eid, state)))
	}
}

// copperGolemStep runs the golem's IDLE activity for one update: the
// transport while its cooldown is absent, else the RunOne of a short stroll
// or a pause. It reports whether it took the golem's move.
func (h *hub) copperGolemStep(players map[int32]*tracked, m *mob) bool {
	if m.dying > 0 {
		return false
	}
	if m.transportCD > 0 { // CountDownCooldownTicks
		m.transportCD = max(0, m.transportCD-mobMoveInterval)
	}
	if m.panic > 0 || m.panicHasT || m.leash != 0 {
		// canStillUse: not while panicking or on a lead.
		h.copperGolemStopTransport(players, m)
		return false
	}
	if m.transportCD > 0 {
		if h.idleWalkStep(m) {
			return true
		}
		h.runOne([]int{1, 1}, func(i int) bool {
			if i == 0 {
				h.randomStroll(m, 1, 2, 2) // RandomStroll.stroll(1.0F, 2, 2)
			} else {
				h.doNothing(m)
			}
			return true
		})
		if !h.idleWalkStep(m) {
			m.vx, m.vz = 0, 0
		}
		return true
	}
	m.idleWalk = nil
	h.copperGolemTransport(players, m)
	return true
}

// copperGolemStopTransport is the behaviour's stop: back to travelling, the
// state idle and the chest let go.
func (h *hub) copperGolemStopTransport(players map[int32]*tracked, m *mob) {
	if !m.cgHasTarget && m.cgState == cgStateIdle && h.golemChests[m.eid] == [2]simPos{} {
		return
	}
	h.cgStartTravelling(players, m)
}

// copperGolemTransport is TransportItemsBetweenContainers.tick.
func (h *hub) copperGolemTransport(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	if m.cgMemUntil != 0 && now >= m.cgMemUntil { // setMemoryWithExpiry lapses
		m.cgVisited, m.cgUnreach, m.cgMemUntil = nil, nil, 0
	}
	if !h.cgHasValidTarget(m) {
		h.cgStopTargeting(m)
		if p, ok := h.cgFindTarget(m); ok {
			m.cgTarget, m.cgHasTarget, m.cgPathOK = p, true, false
			h.cgStartTravelling(players, m)
			h.cgSetVisited(m, p)
		} else {
			h.cgEnterCooldown(m)
		}
		return
	}
	switch m.cgPhase {
	case cgQueuing:
		if !h.cgAnotherAtTarget(m) {
			m.cgPhase = cgTravelling
			h.cgWalk(m)
		}
	case cgTravelling:
		fx, fy, fz := m.x, m.y+m.box().h/2, m.z
		switch {
		case h.cgWithin(m, cgQueueDist, fx, fy, fz) && h.cgAnotherAtTarget(m):
			m.vx, m.vz = 0, 0
			m.cgPhase = cgQueuing
		case h.cgWithin(m, h.cgInteractionRange(m), fx, fy, fz):
			m.cgInteract = h.cgInteractionFor(m)
			m.cgPhase = cgInteracting
			m.vx, m.vz = 0, 0
		default:
			h.cgWalk(m)
		}
	case cgInteracting:
		h.cgOnReachedTarget(players, m)
	}
}

// cgOnReachedTarget is onReachedTarget: beside the chest, the interaction
// runs its sixty ticks — the lid opened on the first, the sound on the
// ninth, the lid shut on the sixtieth, and then the work.
func (h *hub) cgOnReachedTarget(players map[int32]*tracked, m *mob) {
	if !h.cgWithin(m, cgStayDist, m.x, m.y+m.box().h/2, m.z) {
		h.cgStartTravelling(players, m)
		return
	}
	prev := m.cgTicks
	m.cgTicks += mobMoveInterval
	passed := func(k int) bool { return prev < k && m.cgTicks >= k }
	// onTargetInteraction: look at the chest and stand still.
	tx, tz := float64(m.cgTarget.x)+0.5, float64(m.cgTarget.z)+0.5
	m.headYaw = yawToward(m.x, m.z, tx, tz)
	m.vx, m.vz = 0, 0
	if act, ok := cgInteraction[m.cgInteract]; ok {
		if passed(cgOpenTick) {
			h.golemOpenChest(players, m, m.cgTarget)
			h.setCopperGolemState(players, m, act.state)
		}
		if passed(cgSoundTick) {
			h.playSoundDim(players, m.dim, act.sound, sndNeutral, m.x, m.y, m.z, 1, 1)
		}
		if passed(cgInteractTicks) {
			h.golemCloseChest(players, m)
		}
	}
	if m.cgTicks < cgInteractTicks {
		return
	}
	c := h.cgContainer(m.dim, m.cgTarget)
	switch {
	case m.held == 0 && !cgEmpty(c):
		h.cgPickUp(players, m, c)
	case m.held == 0:
		h.cgStopTargeting(m)
	case cgEmpty(c) || cgHasItem(c, m.heldStack()):
		h.cgPutDown(players, m, c)
	default:
		h.cgStopTargeting(m)
	}
	h.cgStartTravelling(players, m)
}

// cgStartTravelling is onStartTravelling: the golem state back to idle, the
// chest let go, the interaction forgotten.
func (h *hub) cgStartTravelling(players map[int32]*tracked, m *mob) {
	h.golemCloseChest(players, m)
	h.setCopperGolemState(players, m, cgStateIdle)
	m.cgPhase, m.cgInteract, m.cgTicks = cgTravelling, 0, 0
}

// cgStopTargeting is stopTargetingCurrentTarget.
func (h *hub) cgStopTargeting(m *mob) {
	m.cgTicks, m.cgHasTarget, m.cgPathOK = 0, false, false
	m.vx, m.vz = 0, 0
}

// cgClearMemories is clearMemoriesAfterMatchingTargetFound.
func (h *hub) cgClearMemories(m *mob) {
	h.cgStopTargeting(m)
	m.cgVisited, m.cgUnreach, m.cgMemUntil = nil, nil, 0
}

// cgEnterCooldown is enterCooldownAfterNoMatchingTargetFound.
func (h *hub) cgEnterCooldown(m *mob) {
	h.cgClearMemories(m)
	m.transportCD = cgIdleCooldown
}

// cgSetVisited is setVisitedBlockPos: the eleventh chest ends the round.
func (h *hub) cgSetVisited(m *mob, p blockPos) {
	if !containsPos(m.cgVisited, p) {
		m.cgVisited = append(m.cgVisited, p)
	}
	if len(m.cgVisited) > cgMaxVisited {
		h.cgEnterCooldown(m)
		return
	}
	m.cgMemUntil = h.tick.Load() + cgMemoryTicks
}

// cgMarkUnreachable is markVisitedBlockPosAsUnreachable.
func (h *hub) cgMarkUnreachable(m *mob, p blockPos) {
	for i, v := range m.cgVisited {
		if v == p {
			m.cgVisited = append(m.cgVisited[:i:i], m.cgVisited[i+1:]...)
			break
		}
	}
	if !containsPos(m.cgUnreach, p) {
		m.cgUnreach = append(m.cgUnreach, p)
	}
	if len(m.cgUnreach) > cgMaxUnreachable {
		h.cgEnterCooldown(m)
		return
	}
	m.cgMemUntil = h.tick.Load() + cgMemoryTicks
}

func containsPos(ps []blockPos, p blockPos) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}

// cgWanted is isWantedBlock: a copper chest (any weathering, waxed or not)
// for an empty hand; a plain or trapped chest for a full one.
func cgWanted(m *mob, s uint32) bool {
	if m.held == 0 {
		return isCopperChest(s)
	}
	return isChestBlock(s) && !isCopperChest(s)
}

// cgHalves is getConnectedTargets: the chest and, for half of a large
// chest, its partner.
func (h *hub) cgHalves(dim int, p blockPos) []blockPos {
	s := h.worldFor(dim).At(p.x, p.y, p.z)
	if l, r, paired := h.chestPairPositions(dim, p.x, p.y, p.z, s); paired {
		return []blockPos{l, r}
	}
	return []blockPos{p}
}

// cgHasValidTarget is hasValidTarget: still the wanted chest, not blocked,
// and — while travelling — still reachable, else marked unreachable.
func (h *hub) cgHasValidTarget(m *mob) bool {
	if !m.cgHasTarget {
		return false
	}
	w := h.worldFor(m.dim)
	if w == nil || !cgWanted(m, w.At(m.cgTarget.x, m.cgTarget.y, m.cgTarget.z)) || h.chestBlockedAt(m.dim, m.cgTarget) {
		return false
	}
	if m.cgPhase != cgTravelling || m.cgPathOK {
		return true
	}
	if h.cgReachable(m, m.cgTarget) {
		m.cgPathOK = true
		return true
	}
	h.cgMarkUnreachable(m, m.cgTarget)
	return false
}

// cgReachable is hasValidTravellingPath: a path whose end is within the
// interaction range of the chest and can see one of its faces.
func (h *hub) cgReachable(m *mob, p blockPos) bool {
	w := h.worldFor(m.dim)
	start := blockPos{floorInt(m.x), floorInt(m.y + 0.01), floorInt(m.z)}
	cells, _ := search3D(w, true, start, p, 1, cgSearchH+2, cgPathNodes)
	ex, ey, ez := m.x, m.y, m.z
	if len(cells) > 0 {
		e := cells[len(cells)-1]
		ex, ey, ez = float64(e.x)+0.5, float64(e.y), float64(e.z)+0.5
	}
	fy := ey + m.box().h/2
	if !h.cgWithin(m, 1.0, ex, fy, ez) {
		return false
	}
	// canSeeAnyTargetSide: a clear line from there to the middle of a face.
	cx, cy, cz := float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5
	for _, d := range [6][3]float64{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if hit, ok := h.clipOutline(m.dim, ex, fy, ez, cx+0.5*d[0], cy+0.5*d[1], cz+0.5*d[2]); ok && hit == p {
			return true
		}
	}
	return false
}

// cgWithin is isWithinTargetDistance: the golem's box, centred on (fx, fy,
// fz), meets the chest's shape inflated d sideways and half a block up and
// down.
func (h *hub) cgWithin(m *mob, d, fx, fy, fz float64) bool {
	b := m.box()
	hw, hh := b.w/2, b.h/2
	t := m.cgTarget
	const lo, hi, top = 1.0 / 16, 15.0 / 16, 14.0 / 16 // ChestBlock.SHAPE
	return fx+hw > float64(t.x)+lo-d && fx-hw < float64(t.x)+hi+d &&
		fz+hw > float64(t.z)+lo-d && fz-hw < float64(t.z)+hi+d &&
		fy+hh > float64(t.y)-0.5 && fy-hh < float64(t.y)+top+0.5
}

// cgInteractionRange is getInteractionRange: a block once its path is done,
// half a block before.
func (h *hub) cgInteractionRange(m *mob) float64 {
	if m.pathGoal == [2]int{m.cgTarget.x, m.cgTarget.z} && m.pathIdx >= len(m.path) {
		return 1.0
	}
	return 0.5
}

// cgWalk is walkTowardsTarget: BehaviorUtils.setWalkAndLookTargetMemories at
// the behaviour's speed of 1.0.
func (h *hub) cgWalk(m *mob) {
	m.vx, m.vz = h.pathSteerTo(m, m.cgTarget, 1)
	m.headYaw = yawToward(m.x, m.z, float64(m.cgTarget.x)+0.5, float64(m.cgTarget.z)+0.5)
	m.rest = 0
}

// cgAnotherAtTarget is isAnotherMobInteractingWithTarget: anyone — a player
// or another golem — with either half open.
func (h *hub) cgAnotherAtTarget(m *mob) bool {
	for _, p := range h.cgHalves(m.dim, m.cgTarget) {
		if h.chestViewers(simPos{dim: m.dim, blockPos: p}, nil) > 0 {
			return true
		}
	}
	return false
}

// cgFindTarget is getTransportTarget: the nearest wanted chest (by distance
// to its centre) in the search box whose halves it has neither tried nor
// found unreachable. Chests are found among the stored ones and the placed
// ones in the chunks about the golem.
func (h *hub) cgFindTarget(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return blockPos{}, false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	best, found := math.MaxFloat64, blockPos{}
	ok := false
	seen := map[blockPos]bool{}
	consider := func(p blockPos) {
		if seen[p] {
			return
		}
		seen[p] = true
		if abs(p.x-bx) > cgSearchH || abs(p.z-bz) > cgSearchH || abs(p.y-by) > cgSearchV {
			return
		}
		d := sq(float64(p.x)+0.5-m.x) + sq(float64(p.y)+0.5-m.y) + sq(float64(p.z)+0.5-m.z)
		if d >= best || !cgWanted(m, w.At(p.x, p.y, p.z)) {
			return
		}
		for _, q := range h.cgHalves(m.dim, p) {
			if containsPos(m.cgVisited, q) || containsPos(m.cgUnreach, q) {
				return
			}
		}
		best, found, ok = d, p, true
	}
	for pos := range h.chests {
		if pos.dim == m.dim {
			consider(pos.blockPos)
		}
	}
	r := cgSearchH/16 + 1 // ChunkPos.rangeClosed(…, floorDiv(32, 16) + 1)
	for cx := (bx >> 4) - r; cx <= (bx>>4)+r; cx++ {
		for cz := (bz >> 4) - r; cz <= (bz>>4)+r; cz++ {
			if !w.Loaded(int32(cx), int32(cz)) {
				continue
			}
			var placed []blockPos
			w.ForEachEditIn(int32(cx), int32(cz), func(x, y, z int, s uint32) {
				if isChestBlock(s) {
					placed = append(placed, blockPos{x, y, z})
				}
			})
			for _, p := range placed {
				consider(p)
			}
		}
	}
	return found, ok
}

// cgContainer is the target's Container: the chest, or for half of a large
// chest the pair, each half's storage made if it had none (a structure
// chest fills from its loot table).
func (h *hub) cgContainer(dim int, p blockPos) []*chest {
	var out []*chest
	for _, q := range h.cgHalves(dim, p) {
		sp := simPos{dim: dim, blockPos: q}
		c := h.chests[sp]
		if c == nil {
			c = &chest{}
			h.fillStructureChestIn(dim, q, c)
			h.chests[sp] = c
		}
		out = append(out, c)
	}
	return out
}

func cgEmpty(cs []*chest) bool {
	for _, c := range cs {
		for _, st := range c.slots {
			if st.item != 0 && st.count > 0 {
				return false
			}
		}
	}
	return true
}

// cgHasItem is hasItemMatchingHandItem: ItemStack.isSameItem, the item alone.
func cgHasItem(cs []*chest, held invStack) bool {
	for _, c := range cs {
		for _, st := range c.slots {
			if st.count > 0 && st.item == held.item {
				return true
			}
		}
	}
	return false
}

// cgPickUp is pickUpItems: up to sixteen of the first stack, into the hand
// as a sure drop.
func (h *hub) cgPickUp(players map[int32]*tracked, m *mob, cs []*chest) {
	for _, c := range cs {
		for i := range c.slots {
			st := &c.slots[i]
			if st.item == 0 || st.count <= 0 {
				continue
			}
			n := min(st.count, cgMaxStack)
			took := *st
			took.count = n
			if st.count -= n; st.count == 0 {
				*st = invStack{}
			}
			m.setHeld(took)
			m.gearSure[gearSlotHand] = true // setGuaranteedDrop
			h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
			h.cgClearMemories(m)
			return
		}
	}
}

// cgPutDown is putDownItem: the stack goes into the first slot that takes
// it — an empty one takes the lot, a matching stack as much as it has room
// for — and what is left stays in the hand.
func (h *hub) cgPutDown(players map[int32]*tracked, m *mob, cs []*chest) {
	st := m.heldStack()
	left := st
put:
	for _, c := range cs {
		for i := range c.slots {
			d := &c.slots[i]
			if d.item == 0 || d.count <= 0 {
				*d = left
				left = invStack{}
				break put
			}
			if sameItemComponents(*d, left) && d.count < stackCap(d.item) {
				n := min(stackCap(d.item)-d.count, left.count)
				d.count += n
				if left.count -= n; left.count <= 0 {
					left = invStack{}
					break put
				}
			}
		}
	}
	if left.item == 0 {
		m.setHeld(invStack{})
		m.gearSure[gearSlotHand] = false
		h.cgClearMemories(m)
	} else {
		m.setHeld(left)
		h.cgStopTargeting(m)
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
}

// cgInteractionFor is doReachedTargetInteraction's choice of state as the
// golem arrives.
func (h *hub) cgInteractionFor(m *mob) int8 {
	c := h.cgContainer(m.dim, m.cgTarget)
	switch {
	case m.held == 0 && !cgEmpty(c):
		return cgPickupItem
	case m.held == 0:
		return cgPickupNoItem
	case cgEmpty(c) || cgHasItem(c, m.heldStack()):
		return cgPlaceItem
	}
	return cgPlaceNoItem
}

// golemOpeners counts the copper golems with the chest at pos open
// (CopperGolem.hasContainerOpen: the chest it opened, or that chest's
// partner in a large chest).
func (h *hub) golemOpeners(pos simPos) int {
	n := 0
	for _, halves := range h.golemChests {
		if halves[0] == pos || (halves[1] != simPos{} && halves[1] == pos) {
			n++
		}
	}
	return n
}

// golemOpenChest is container.startOpen for a golem: each half's opener
// count goes up, and a chest nobody had open plays its sound and sends its
// CONTAINER_OPEN; the lids rise.
func (h *hub) golemOpenChest(players map[int32]*tracked, m *mob, p blockPos) {
	h.golemCloseChest(players, m)
	halves := h.cgHalves(m.dim, p)
	var set [2]simPos
	first := false
	for i, q := range halves {
		sp := simPos{dim: m.dim, blockPos: q}
		set[i] = sp
		if h.chestViewers(sp, nil) == 0 {
			first = true
			h.vib(m.dim, freqContainerOpen, q.x, q.y, q.z, m.eid)
		}
	}
	h.golemChests[m.eid] = set
	if first {
		if len(halves) == 2 {
			h.pairSoundAt(players, set[0], set[1], true)
		} else {
			h.containerSoundAt(players, set[0], true)
		}
	}
	for i := range halves {
		h.lidEvent(players, set[i])
		h.trappedChestChanged(m.dim, set[i].blockPos)
	}
}

// golemCloseChest is the golem letting its chest go (stopOpen, or the
// openers counter's recheck once clearOpenedChestPos has run): the last
// opener's leaving closes the lid with its sound and CONTAINER_CLOSE.
func (h *hub) golemCloseChest(players map[int32]*tracked, m *mob) {
	h.golemReleaseChest(players, m.eid, m.dim)
}

func (h *hub) golemReleaseChest(players map[int32]*tracked, eid int32, dim int) {
	set, ok := h.golemChests[eid]
	if !ok {
		return
	}
	delete(h.golemChests, eid)
	halves := []simPos{set[0]}
	if set[1] != (simPos{}) {
		halves = append(halves, set[1])
	}
	last := false
	for _, sp := range halves {
		if h.chestViewers(sp, nil) == 0 {
			last = true
			h.vib(sp.dim, freqContainerClose, sp.x, sp.y, sp.z, eid)
		}
	}
	if last && h.worldFor(dim) != nil {
		if len(halves) == 2 {
			h.pairSoundAt(players, halves[0], halves[1], false)
		} else {
			h.containerSoundAt(players, halves[0], false)
		}
	}
	for _, sp := range halves {
		h.lidEvent(players, sp)
		h.trappedChestChanged(sp.dim, sp.blockPos)
	}
}

// golemChestSweep is ContainerOpenersCounter.recheckOpeners for golems that
// are gone: a golem that died or was removed with a chest open leaves it.
func (h *hub) golemChestSweep(players map[int32]*tracked) {
	for eid, set := range h.golemChests {
		if m := h.mobs[eid]; m == nil || m.dying > 0 {
			h.golemReleaseChest(players, eid, set[0].dim)
		}
	}
}

// copperGolemTakeItem is CopperGolem.mobInteract with an empty hand: the
// golem throws what it is carrying to the player.
func (h *hub) copperGolemTakeItem(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityCopperGolem || m.dying > 0 || m.held == 0 || usedStack(t).item != 0 {
		return false
	}
	st := m.heldStack()
	m.setHeld(invStack{})
	m.gearSure[gearSlotHand] = false
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
	// BehaviorUtils.throwItem: from the hand (eye less 0.3) toward the
	// player at 0.3 a tick on each axis of the direction.
	dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
	if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d > 1e-9 {
		dx, dy, dz = dx/d*0.3, dy/d*0.3, dz/d*0.3
	}
	if it := h.spawnItemAt(players, m.dim, st.item, st.count, m.x, m.y+copperGolemEye-0.3, m.z, dx, dy, dz); it != nil {
		it.setFrom(st)
		h.refreshItemMeta(players, it)
	}
	return true
}

const copperGolemEye = 0.8125 // EntityType COPPER_GOLEM eyeHeight
