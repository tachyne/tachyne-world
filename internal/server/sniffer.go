package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The sniffer egg.
//
// Nothing hatched it: an egg dug out of a suspicious-sand brush sat on the
// ground for ever, which made the whole archaeology-to-sniffer chain a dead
// end. It cracks twice and then opens, and moss underneath halves the wait —
// which is the one piece of husbandry the egg actually asks of you.

const (
	snifferHatchTicks     = 24000 // a full day, split across the three stages
	snifferHatchBoosted   = 12000 // …halved by moss beneath it
	snifferHatchJitter    = 300   // vanilla's random spread, so a clutch is staggered
	snifferMaxHatch       = 2     // the state at which the next tick opens it
	snifferEggHatchStages = 3
)

var (
	snifferEggLo, snifferEggHi, snifferEggOK = worldgen.BlockRangeOK("sniffer_egg")
	mossBlockState                           = worldgen.BlockBase("moss_block")
	paleMossBlockState                       = worldgen.BlockBase("pale_moss_block")
)

func isSnifferEgg(s uint32) bool {
	return snifferEggOK && s >= snifferEggLo && s <= snifferEggHi
}

// snifferHatchBoost is the SNIFFER_EGG_HATCH_BOOST tag: moss under the egg.
func (h *hub) snifferHatchBoost(dim, x, y, z int) bool {
	below := h.worldFor(dim).At(x, y-1, z)
	return below == mossBlockState || below == paleMossBlockState
}

// scheduleSnifferEgg books the next crack. Vanilla schedules a third of the
// hatch time plus a jitter, and re-schedules from onPlace each time the state
// changes — so each of the three stages waits its own third.
func (h *hub) scheduleSnifferEgg(players map[int32]*tracked, dim, x, y, z int) {
	total := snifferHatchTicks
	if h.snifferHatchBoost(dim, x, y, z) {
		total = snifferHatchBoosted
		h.levelEvent(players, dim, worldEventSnifferEggBoost, x, y, z, 0) // SnifferEggBlock.onPlace: the moss's green sparkle
	}
	delay := uint64(total/snifferEggHatchStages + h.rng.Intn(snifferHatchJitter))
	if h.snifferEggs == nil {
		h.snifferEggs = map[simPos]uint64{}
	}
	h.snifferEggs[simPos{dim: dim, blockPos: blockPos{x, y, z}}] = h.tick.Load() + delay
	h.scheduleIn(dim, blockPos{x, y, z}, delay)
}

// tickSnifferEgg cracks the egg, and opens it on the last stage.
//
// processUpdate fires for ANY neighbour change, not only for the tick we
// booked, so the egg keeps its own due time: without that, walking past and
// placing a torch would hatch it on the spot.
func (h *hub) tickSnifferEgg(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isSnifferEgg(state) {
		return false
	}
	key := simPos{dim: dim, blockPos: blockPos{x, y, z}}
	now := h.tick.Load()
	due, known := h.snifferEggs[key]
	if !known {
		// First time we have seen this egg — start its clock. Lazy rather than
		// on-place so eggs already sitting in a loaded world also hatch.
		h.scheduleSnifferEgg(players, dim, x, y, z)
		return true
	}
	if now < due {
		return true // its own tick has not come round yet
	}
	delete(h.snifferEggs, key)
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	if hatch := worldgen.GetProperty(info, state, "hatch"); hatch != "2" {
		next := "1"
		if hatch == "1" {
			next = "2"
		}
		h.playSoundDim(players, dim, "minecraft:block.sniffer_egg.crack", sndBlock, cx, cy, cz, 0.7, 0.9+h.rng.Float32()*0.2)
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.SetProperty(info, state, "hatch", next))
		// The new state's onPlace: a BLOCK_PLACE, and the next crack booked.
		h.vib(dim, freqBlockPlace, x, y, z, 0)
		h.scheduleSnifferEgg(players, dim, x, y, z)
		return true
	}
	// SnifferEggBlock.tick: the hatch sound, then Level.destroyBlock without
	// drops (the break effect and BLOCK_DESTROY), and the baby at the
	// block's centre facing a random way.
	h.playSoundDim(players, dim, "minecraft:block.sniffer_egg.hatch", sndBlock, cx, cy, cz, 0.7, 0.9+h.rng.Float32()*0.2)
	h.toNearbyEv(players, dim, cx, cz, blockBreakEvent(x, y, z, state))
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	h.vib(dim, freqBlockDestroy, x, y, z, 0)
	if m := h.spawnSpecies(players, entitySniffer, dim, cx, cy, cz); m != nil {
		m.baby, m.growLeft = true, growUpTicks
		m.yaw = h.rng.Float32()*360 - 180
		h.toTracking(players, m.eid, dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	}
	return true
}

// The sniffer's day (vanilla Sniffer + SnifferAi). Idle, now and then it
// lifts its nose to the air (SCENTING) or — grown, dry and off its cooldown
// — sniffs (SNIFFING); a sniff that runs its full course finds a scent: a
// reachable patch of diggable ground ten to eighteen blocks off that it has
// not dug before. It walks there (SEARCHING), digs for 160-180 ticks with its
// nose down (DIGGING: a torchflower seed or a pitcher pod two seconds in),
// gets up (RISING, forty ticks), remembers the spot (its last twenty, never
// the same block twice) and is pleased with itself (FEELING_HAPPY, 40-100
// ticks). A dig that ran its course leaves the sniffer eight minutes before
// the next sniff. A panic or a tempting player breaks off whatever it was
// at (resetSniffing); courting or water break off a sniff, a search or a
// dig (canSniff); and only a sniffer idling, scenting or happy will mate.
//
// The client animates these from the sniffer's DATA_STATE, which the
// gateways cannot render yet (it needs the SNIFFER_STATE serializer); the
// states run here with their timing and sounds, and the pose flags the
// search and the dig always sent are unchanged.

const (
	snifferDigMin      = 160
	snifferDigJitter   = 20
	snifferSeedAt      = 120  // DIGGING_DROP_SEED_OFFSET_TICKS
	snifferSniffCD     = 9600 // SNIFFING_COOLDOWN_TICKS
	snifferExploredN   = 20
	snifferReach       = 1.5
	poseSniffing       = 12
	snifferScentSpeed  = 1.25 // SnifferAi: the walk to a scented block
	poseDigging        = 14
	snifferRiseTicks   = 40  // FinishedDigging(40)
	snifferSearchTicks = 600 // Searching's longest run
	// The idle RunOne draws one of eight weights about every nineteen ticks
	// (the mean length of what it draws); scenting and sniffing are one
	// weight each, so each starts about once in 150 ticks: one mob update
	// in 76.
	snifferIdleOdds = 76
)

// Sniffer.State, in the engine's numbering (idle, searching and digging
// kept the values they always had).
const (
	sniffIdling int8 = iota
	sniffSearching
	sniffDigging
	sniffRising
	sniffHappy
	sniffScenting
	sniffSniffing
)

// snifferDiggable is #minecraft:sniffer_diggable_block.
var snifferDiggable = rangesOf([]string{"dirt", "grass_block", "podzol", "coarse_dirt", "rooted_dirt",
	"moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots"})

var snifferSeeds = []int32{int32(itemByName["torchflower_seeds"]), int32(itemByName["pitcher_pod"])}

func (m *mob) sniffExploredHas(p blockPos) bool {
	for _, q := range m.sniffExplored {
		if q == p {
			return true
		}
	}
	return false
}

// snifferFor is a Behavior's run length: min plus a roll up to max.
func (h *hub) snifferFor(lo, hi int) uint64 { return uint64(lo + h.rng.Intn(hi-lo+1)) }

// snifferCanSniff is Sniffer.canSniff: not tempted, panicking, courting, in
// the water or carried.
func (h *hub) snifferCanSniff(m *mob) bool {
	return !m.tempted && m.panic == 0 && m.loveTicks == 0 && m.mount == 0 && m.cart == 0 && m.leash == 0 &&
		!h.inWater(m.dim, m.x, m.y, m.z)
}

// snifferMates is Sniffer.canMate's own half: only a sniffer idling,
// scenting or feeling happy.
func snifferMates(m *mob) bool {
	return m.sniffState == sniffIdling || m.sniffState == sniffScenting || m.sniffState == sniffHappy
}

// snifferInterrupt is resetSniffing where vanilla calls it: AnimalPanic and
// FollowTemptation break off whatever the sniffer was at when they start,
// and a sniff, a search or a dig also end the moment canSniff fails.
func (h *hub) snifferInterrupt(players map[int32]*tracked, m *mob) {
	st := m.sniffState
	if st == sniffIdling {
		return
	}
	stop := m.panic > 0 || m.tempted
	if !stop && (st == sniffSniffing || st == sniffSearching || st == sniffDigging) {
		stop = !h.snifferCanSniff(m)
	}
	if !stop {
		return
	}
	if st == sniffSearching || st == sniffDigging {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
	}
	m.sniffState = sniffIdling
}

// snifferStep runs the states. Returns true while they hold the sniffer
// still or walking.
func (h *hub) snifferStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	m.sniffCD = max(0, m.sniffCD-mobMoveInterval)
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	switch m.sniffState {
	case sniffIdling:
		if m.tempted || m.panic > 0 || m.loveTicks > 0 {
			return false
		}
		switch h.rng.Intn(snifferIdleOdds) {
		case 0: // Scenting(40, 80)
			m.sniffState, m.sniffUntil = sniffScenting, now+h.snifferFor(40, 80)
			pitch := float32(1)
			if m.baby {
				pitch = 1.3
			}
			h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.scenting", sndNeutral, m.x, m.y, m.z, 1, pitch)
			m.vx, m.vz = 0, 0
			return true
		case 1: // Sniffing(40, 80): a grown sniffer off its cooldown
			if m.baby || m.sniffCD > 0 || !h.snifferCanSniff(m) {
				return false
			}
			m.sniffState, m.sniffUntil = sniffSniffing, now+h.snifferFor(40, 80)
			h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.sniffing", sndNeutral, m.x, m.y, m.z, 1, 1)
			m.vx, m.vz = 0, 0
			return true
		}
		return false
	case sniffScenting:
		if now < m.sniffUntil {
			m.vx, m.vz = 0, 0 // RunOne holds its other draws off while it runs
			return true
		}
		m.sniffState = sniffIdling
		return false
	case sniffSniffing:
		if now < m.sniffUntil {
			m.vx, m.vz = 0, 0
			return true
		}
		// Sniffing.stop, run to its end: calculateDigPosition, five tries at
		// growing radii, each within three blocks of its own height
		// (LandRandomPos.getPos(mob, 10+2n, 3)).
		m.sniffState = sniffIdling
		for n := 0; n < 5; n++ {
			r := 10 + 2*n
			x := int(math.Floor(m.x)) + h.rng.Intn(2*r+1) - r
			z := int(math.Floor(m.z)) + h.rng.Intn(2*r+1) - r
			y := w.MobFeetFrom(x, z, int(math.Floor(m.y)))
			if math.Abs(float64(y)-m.y) > 3 {
				continue
			}
			floor := blockPos{x, y - 1, z}
			if !inRanges(snifferDiggable, w.At(floor.x, floor.y, floor.z)) || m.sniffExploredHas(floor) {
				continue
			}
			m.sniffTarget, m.sniffState, m.sniffStart = floor, sniffSearching, now
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseSniffing)))
			return true
		}
		return false
	case sniffSearching:
		if now >= m.sniffStart+snifferSearchTicks {
			m.sniffState = sniffIdling // Searching runs out: the scent is lost
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
			return false
		}
		tx, tz := float64(m.sniffTarget.x)+0.5, float64(m.sniffTarget.z)+0.5
		dx, dz := tx-m.x, tz-m.z
		if hd := math.Hypot(dx, dz); hd > snifferReach {
			sp := m.moveSpeed() * snifferScentSpeed // SPEED_MULTIPLIER_WHEN_SNIFFING
			m.vx, m.vz = dx/hd*sp, dz/hd*sp
			m.rest = 0
			return true
		}
		if !inRanges(snifferDiggable, w.At(m.sniffTarget.x, m.sniffTarget.y, m.sniffTarget.z)) {
			m.sniffState = sniffIdling // the ground changed under it
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
			return false
		}
		m.sniffState, m.sniffStart = sniffDigging, now
		m.sniffUntil = now + h.snifferFor(snifferDigMin, snifferDigMin+snifferDigJitter)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusSnifferDig)) // onDiggingStart
		m.vx, m.vz = 0, 0
		h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.digging", sndNeutral, m.x, m.y, m.z, 1, 1)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseDigging)))
		return true
	case sniffDigging:
		m.vx, m.vz = 0, 0
		if m.sniffStart != 0 && now >= m.sniffStart+snifferSeedAt {
			m.sniffStart = 0
			if h.rules.DoMobLoot {
				seed := snifferSeeds[h.rng.Intn(len(snifferSeeds))]
				h.spawnItemIn(players, m.dim, seed, 1, float64(m.sniffTarget.x)+0.5, float64(m.sniffTarget.y)+1, float64(m.sniffTarget.z)+0.5)
			}
			h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.drop_seed", sndNeutral, m.x, m.y, m.z, 1, 1)
		}
		if now >= m.sniffUntil {
			// Digging ran its course: SNIFF_COOLDOWN, and FinishedDigging
			// gets it up (RISING).
			m.sniffCD = snifferSniffCD
			m.sniffState, m.sniffUntil = sniffRising, now+snifferRiseTicks
			h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.digging_stop", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
		}
		return true
	case sniffRising:
		m.vx, m.vz = 0, 0
		if now < m.sniffUntil {
			return true
		}
		// onDiggingComplete(true): the spot is remembered; then SNIFFER_HAPPY.
		m.sniffExplored = append(m.sniffExplored, m.sniffTarget)
		if len(m.sniffExplored) > snifferExploredN {
			m.sniffExplored = m.sniffExplored[len(m.sniffExplored)-snifferExploredN:]
		}
		m.sniffState, m.sniffUntil = sniffHappy, now+h.snifferFor(40, 100) // FeelingHappy(40, 100)
		h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.happy", sndNeutral, m.x, m.y, m.z, 1, 1)
		return false
	default: // sniffHappy: it goes about its idling meanwhile
		if now >= m.sniffUntil {
			m.sniffState = sniffIdling
		}
		return false
	}
}
