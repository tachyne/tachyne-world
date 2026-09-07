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
func (h *hub) scheduleSnifferEgg(dim, x, y, z int) {
	total := snifferHatchTicks
	if h.snifferHatchBoost(dim, x, y, z) {
		total = snifferHatchBoosted
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
		h.scheduleSnifferEgg(dim, x, y, z)
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
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.SetProperty(info, state, "hatch", next))
		h.playSoundDim(players, dim, "minecraft:block.sniffer_egg.crack", sndBlock, cx, cy, cz, 0.7, 1)
		h.scheduleSnifferEgg(dim, x, y, z)
		return true
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	h.playSoundDim(players, dim, "minecraft:block.sniffer_egg.hatch", sndBlock, cx, cy, cz, 0.7, 1)
	if m := h.spawnSpecies(players, entitySniffer, dim, cx, float64(y), cz); m != nil {
		m.baby, m.growLeft = true, growUpTicks
		h.toNearbyEv(players, dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	}
	return true
}

// Digging (vanilla Sniffer + SnifferAi): a grown sniffer that is not busy
// picks a scent — a reachable patch of diggable ground within ten to
// eighteen blocks it has not dug before — walks to it, digs for 160-180
// ticks with its nose down, drops a torchflower seed or a pitcher pod two
// seconds in, and then leaves the spot alone for eight minutes (and never
// digs the same block twice, remembering its last twenty).

const (
	snifferDigMin     = 160
	snifferDigJitter  = 20
	snifferSeedAt     = 120  // DIGGING_DROP_SEED_OFFSET_TICKS
	snifferSniffCD    = 9600 // SNIFFING_COOLDOWN_TICKS
	snifferExploredN  = 20
	snifferSearchOdds = 60 // ticks between scent checks, on average (Sniffing/Scenting 40-80)
	snifferReach      = 1.5
	poseSniffing      = 12
	poseDigging       = 14
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

// snifferStep is the dig state machine. Returns true while it owns the
// sniffer's movement.
func (h *hub) snifferStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if m.sniffCD > 0 {
		m.sniffCD -= mobMoveInterval
		return false
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	switch m.sniffState {
	case 0:
		if m.baby || m.panic > 0 || m.loveTicks > 0 || h.rng.Intn(snifferSearchOdds/mobMoveInterval) != 0 {
			return false
		}
		// Sniffer.calculateDigPosition: five tries at growing radii.
		for n := 0; n < 5; n++ {
			r := 10 + 2*n
			x := int(math.Floor(m.x)) + h.rng.Intn(2*r+1) - r
			z := int(math.Floor(m.z)) + h.rng.Intn(2*r+1) - r
			y := w.MobFeet(x, z)
			if math.Abs(float64(y)-m.y) > 3 {
				continue
			}
			floor := blockPos{x, y - 1, z}
			if !inRanges(snifferDiggable, w.At(floor.x, floor.y, floor.z)) || m.sniffExploredHas(floor) {
				continue
			}
			m.sniffTarget, m.sniffState = floor, 1
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseSniffing)))
			return true
		}
		return false
	case 1:
		tx, tz := float64(m.sniffTarget.x)+0.5, float64(m.sniffTarget.z)+0.5
		dx, dz := tx-m.x, tz-m.z
		if hd := math.Hypot(dx, dz); hd > snifferReach {
			sp := m.moveSpeed()
			m.vx, m.vz = dx/hd*sp, dz/hd*sp
			m.rest = 0
			return true
		}
		if !inRanges(snifferDiggable, w.At(m.sniffTarget.x, m.sniffTarget.y, m.sniffTarget.z)) {
			m.sniffState = 0 // the ground changed under it
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
			return false
		}
		m.sniffState, m.sniffStart = 2, now
		m.sniffUntil = now + snifferDigMin + uint64(h.rng.Intn(snifferDigJitter+1))
		m.vx, m.vz = 0, 0
		h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.digging", sndNeutral, m.x, m.y, m.z, 1, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseDigging)))
		return true
	default:
		m.vx, m.vz = 0, 0
		if m.sniffStart != 0 && now >= m.sniffStart+snifferSeedAt {
			m.sniffStart = 0
			if h.rules.DoMobLoot {
				seed := snifferSeeds[h.rng.Intn(len(snifferSeeds))]
				h.spawnItemIn(players, m.dim, seed, 1, float64(m.sniffTarget.x)+0.5, float64(m.sniffTarget.y)+1, float64(m.sniffTarget.z)+0.5)
			}
		}
		if now >= m.sniffUntil {
			h.playSoundDim(players, m.dim, "minecraft:entity.sniffer.digging_stop", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
			m.sniffExplored = append(m.sniffExplored, m.sniffTarget)
			if len(m.sniffExplored) > snifferExploredN {
				m.sniffExplored = m.sniffExplored[len(m.sniffExplored)-snifferExploredN:]
			}
			m.sniffState, m.sniffCD = 0, snifferSniffCD
		}
		return true
	}
}
