package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Raid follow-ups: per-difficulty bonus spawns, the omen-level bonus wave
// (raid.go), villagers ringing the bell while the raid is on, and the
// gifts they throw the heroes afterwards (raidgifts.go).

const (
	raidOmenMax       = 5    // Raid.getMaxRaidOmenLevel
	raidBellRange     = 3.0  // RingBell.RING_BELL_FROM_DISTANCE
	raidBellChance    = 0.05 // 1 - RingBell.BELL_RING_CHANCE, per villager per second
	giftThrowDistance = 5.0  // GiveGiftToHero.THROW_GIFT_AT_DISTANCE
	giftGapMin        = 600  // MIN_TIME_BETWEEN_GIFTS
	giftGapSpan       = 6001 // …to MAX_TIME_BETWEEN_GIFTS 6600
	giftSeekRange     = 8.0  // how far a villager notices a hero
)

// raidBonusSpawns is Raid.getPotentialBonusSpawns: extra raiders of a kind
// on a wave, by difficulty (a random 0..n).
func (h *hub) raidBonusSpawns(etype, wave int, bonusWave bool) int {
	easy, normal := h.rules.Difficulty == diffEasy, h.rules.Difficulty == diffNormal
	bonus := 0
	switch etype {
	case entityPillager, entityVindicator:
		switch {
		case easy:
			bonus = h.rng.Intn(2)
		case normal:
			bonus = 1
		default:
			bonus = 2
		}
	case entityWitch:
		if easy || wave <= 2 || wave == 4 {
			return 0
		}
		bonus = 1
	case entityRavager:
		if !easy && bonusWave {
			bonus = 1
		}
	default:
		return 0
	}
	if bonus <= 0 {
		return 0
	}
	return h.rng.Intn(bonus + 1)
}

// raidBell is the villagers' RingBell behaviour while a raid is on: once a
// second, a villager within three blocks of the village bell rings it with
// a 5% chance — which lights every raider up (bellRevealRaiders).
func (h *hub) raidBell(players map[int32]*tracked, r *raid) {
	if h.tick.Load()%20 != 0 {
		return
	}
	bell := r.center
	if !isBell(h.world.At(bell.x, bell.y, bell.z)) {
		return
	}
	rang := false
	h.grid().nearby(dimOverworld, float64(bell.x)+0.5, float64(bell.z)+0.5, raidBellRange, func(m *mob) {
		if rang || m.etype != entityVillager || m.dying > 0 {
			return
		}
		if dist3(m.x, m.y, m.z, float64(bell.x)+0.5, float64(bell.y), float64(bell.z)+0.5) > raidBellRange {
			return
		}
		if h.rng.Float64() < raidBellChance {
			rang = h.ringBell(players, dimOverworld, bell, bellDirFor(h.world.At(bell.x, bell.y, bell.z)))
		}
	})
}

// bellDirFor picks the swing direction a rung bell is reported with (the
// bell's facing; the sound and reveal do not depend on it).
func bellDirFor(state uint32) int32 {
	if info, ok := worldgen.InfoForState(state); ok {
		switch worldgen.GetProperty(info, state, "facing") {
		case "north":
			return 2
		case "south":
			return 3
		case "west":
			return 4
		case "east":
			return 5
		}
	}
	return 2
}
