package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The trial spawner. Unlike a dungeon spawner, which trickles mobs forever,
// this one runs a FIGHT: it wakes when you walk in, sends waves scaled to how
// many of you turned up, and when the last one dies it opens and pays out —
// then sleeps for half an hour.
//
// Ported from TrialSpawner + TrialSpawnerState. The states are vanilla's, and
// so is the arithmetic; what differs is that tachyne finds its spawners from
// worldgen (chambers are a pure function of the seed) rather than from block
// entities loaded with the chunk.

// trialState is TrialSpawnerState.
type trialState uint8

const (
	trialInactive        trialState = iota // nothing nearby; the dark, quiet state
	trialWaitingPlayers                    // lit, watching for someone to come in
	trialActive                            // the fight: spawning and counting
	trialWaitingEjection                   // the last mob died; the shutter is opening
	trialEjecting                          // paying out, one reward per player who fought
	trialCooldown                          // spent; nothing until the timer runs out
)

const (
	// TrialSpawnerConfig defaults + trialChamberBase(): the normal-mode numbers.
	trialTotalMobs      = 6.0  // totalMobs
	trialTotalPerPlayer = 2.0  // totalMobsAddedPerPlayer
	trialSimultaneous   = 3.0  // simultaneousMobs
	trialSimulPerPlayer = 0.5  // simultaneousMobsAddedPerPlayer
	trialTicksBetween   = 20   // ticksBetweenSpawn
	trialSpawnRange     = 4    // spawnRange, in blocks
	trialPlayerRange    = 14.0 // FullConfig.requiredPlayerRange
	trialCooldownTicks  = 36000
	trialEjectDelay     = 40 // DELAY_BEFORE_EJECT_AFTER_KILLING_LAST_MOB
	trialEjectEvery     = 30 // TIME_BETWEEN_EACH_EJECTION
)

// trialSpawner is one spawner's live state.
type trialSpawner struct {
	pos     blockPos
	dim     int
	kind    string // which mob, from the chamber piece
	ominous bool

	state trialState
	// Players who triggered it (the reward roll), by UUID like vanilla's
	// detectedPlayers — an eid would not survive the relog this set is meant
	// to outlast, let alone the restart it is persisted across.
	detected  map[[16]byte]bool
	current   map[int32]bool // mob eids alive from this spawner
	spawned   int            // totalMobsSpawned this round
	nextSpawn uint64         // tick the next mob may appear
	nextEject uint64         // tick the next reward may be ejected
	cooldown  uint64         // tick the cooldown ends
	shown     bool           // its block has been corrected from the template's base state
}

// trialSpawnerMobs maps a chamber piece's mob name to the entity type it
// spawns. The names come straight from the template paths.
var trialSpawnerMobs = map[string]int{
	"zombie":          entityZombie,
	"husk":            entityHusk,
	"spider":          entitySpider,
	"baby_zombie":     entityZombie, // spawned as a baby below
	"cave_spider":     entityCaveSpider,
	"silverfish":      entitySilverfish,
	"slime":           entitySlime,
	"skeleton":        entitySkeleton,
	"stray":           entityStray,
	"poison_skeleton": entityBogged, // the poison archer is the bogged
	"breeze":          entityBreeze,
}

var trialSpawnerMin, trialSpawnerMax = worldgen.BlockRange("trial_spawner")

// trialStateNames are the block's own trial_spawner_state values, in the order
// of our states — the block shows the fight, so a player sees it light up,
// flame while it spawns, open its shutter and go dark.
var trialStateNames = map[trialState]string{
	trialInactive:        "inactive",
	trialWaitingPlayers:  "waiting_for_players",
	trialActive:          "active",
	trialWaitingEjection: "waiting_for_reward_ejection",
	trialEjecting:        "ejecting_reward",
	trialCooldown:        "cooldown",
}

// trialStateOrder is the block's own trial_spawner_state values in registry
// order. The 12 states are ominous(true,false) x these six, with the state
// varying fastest — so id = min + ominousIndex*6 + stateIndex.
//
// Computed here rather than through InfoForState, which only knows ORIENTABLE
// blocks: a trial spawner has properties but no facing, so it is absent from
// that table and every lookup silently returns false.
var trialStateOrder = []trialState{
	trialInactive, trialWaitingPlayers, trialActive,
	trialWaitingEjection, trialEjecting, trialCooldown,
}

// trialSpawnerBlock is the state id for one spawner appearance.
func trialSpawnerBlock(ominous bool, st trialState) uint32 {
	idx := 0
	for i, s := range trialStateOrder {
		if s == st {
			idx = i
			break
		}
	}
	off := uint32(0) // ominous=true comes FIRST in the registry
	if !ominous {
		off = uint32(len(trialStateOrder))
	}
	return trialSpawnerMin + off + uint32(idx)
}

// showTrialState writes the state onto the block so clients render it — the
// spawner lights, flames while it spawns, opens its shutter and goes dark.
func (h *hub) showTrialState(players map[int32]*tracked, ts *trialSpawner) {
	next := trialSpawnerBlock(ts.ominous, ts.state)
	if cur := h.world.At(ts.pos.x, ts.pos.y, ts.pos.z); next != cur {
		h.setBlockAt(players, 0, ts.pos, next)
	}
}

// isTrialSpawner reports whether a state is a trial spawner.
func isTrialSpawner(s uint32) bool { return s >= trialSpawnerMin && s <= trialSpawnerMax }

// updateTrialSpawners finds the chambers near players and runs each spawner
// still standing in them. Chambers are a pure function of the seed, so this
// needs no block scan — the same trick updateSpawners uses for dungeons.
func (h *hub) updateTrialSpawners(players map[int32]*tracked) {
	if h.rules.Difficulty == diffPeaceful {
		return
	}
	gen := h.world.Gen()
	seen := map[blockPos]bool{}
	for _, t := range players {
		if t.dim != 0 || t.dead {
			continue
		}
		tc := gen.TrialChamberIn(int(t.x), int(t.z))
		if !tc.Exists {
			continue
		}
		for _, f := range gen.TrialChamberSpawners(tc) {
			pos := blockPos{f.X, f.Y, f.Z}
			if seen[pos] {
				continue
			}
			seen[pos] = true
			if !h.ownedBlock(f.X, f.Z) {
				continue
			}
			if !isTrialSpawner(h.world.At(f.X, f.Y, f.Z)) {
				delete(h.trials, pos) // mined out
				continue
			}
			was := h.trialAt(pos, f)
			// Templates stamp these at the block's BASE state, which is the
			// OMINOUS one — resolveState ignores properties for blocks with no
			// facing. Correct the appearance from the piece the spawner came
			// from the first time we see it.
			if !was.shown {
				was.shown = true
				h.showTrialState(players, was)
			}
			before := was.state
			h.tickTrialSpawner(players, was)
			if was.state != before {
				h.showTrialState(players, was) // the block itself shows the fight
			}
		}
	}
}

// trialAt returns the live state for a spawner, created on first sight.
func (h *hub) trialAt(pos blockPos, f worldgen.TrialChamberFeature) *trialSpawner {
	if h.trials == nil {
		h.trials = map[blockPos]*trialSpawner{}
	}
	ts := h.trials[pos]
	if ts == nil {
		ts = &trialSpawner{pos: pos, detected: map[[16]byte]bool{}, current: map[int32]bool{}, ominous: f.Ominous}
		h.trials[pos] = ts
	}
	// Which mob comes from worldgen, never from the save file: a restored
	// record carries progress — and whether an omen turned it ominous.
	ts.dim, ts.kind = 0, f.Kind
	return ts
}

// tickTrialSpawner is TrialSpawnerState.tickAndGetNext.
func (h *hub) tickTrialSpawner(players map[int32]*tracked, ts *trialSpawner) {
	now := h.tick.Load()
	ts.forgetDeadMobs(h)

	switch ts.state {
	case trialInactive:
		ts.state = trialWaitingPlayers

	case trialWaitingPlayers:
		if ts.detect(h, players) > 0 {
			ts.state = trialActive
		}
		h.trialOmenCheck(players, ts)

	case trialActive:
		h.trialOmenCheck(players, ts)
		extra := ts.detect(h, players) - 1
		if extra < 0 {
			extra = 0
		}
		if ts.spawned >= ts.targetTotal(extra) {
			if len(ts.current) == 0 {
				// The last one died: the shutter opens after a beat.
				ts.cooldown = now + trialCooldownTicks
				ts.nextEject = now + trialEjectDelay
				ts.spawned, ts.nextSpawn = 0, 0
				ts.state = trialWaitingEjection
				h.playSound(players, "minecraft:block.trial_spawner.open_shutter", sndBlock,
					ts.fx(), ts.fy(), ts.fz(), 1, 1)
			}
			return
		}
		if now >= ts.nextSpawn && len(ts.current) < ts.targetSimultaneous(extra) {
			if m := h.spawnTrialMob(players, ts); m != nil {
				ts.current[m.eid] = true
				ts.spawned++
				ts.nextSpawn = now + trialTicksBetween
			}
		}

	case trialWaitingEjection:
		if now >= ts.nextEject {
			ts.state = trialEjecting
		}

	case trialEjecting:
		if now < ts.nextEject {
			return
		}
		if len(ts.detected) == 0 {
			h.playSound(players, "minecraft:block.trial_spawner.close_shutter", sndBlock,
				ts.fx(), ts.fy(), ts.fz(), 1, 1)
			ts.state = trialCooldown
			return
		}
		// One reward per player who fought, one at a time.
		for u := range ts.detected {
			delete(ts.detected, u)
			break
		}
		h.ejectTrialReward(players, ts)
		ts.nextEject = now + trialEjectEvery

	case trialCooldown:
		// The cooldown always ends in waiting_for_players (vanilla's COOLDOWN
		// tick), the omen dropped with it; players still in the room are
		// picked up again on the next tick.
		if now >= ts.cooldown {
			ts.reset()
			if ts.ominous { // removeOminous: the omen wears off with the cooldown
				ts.ominous = false
				h.showTrialState(players, ts)
			}
		}
	}
}

// detect refreshes the set of players in range and returns its size. Vanilla
// keeps the set across the fight: everyone who showed up gets paid, even if
// they wander off before the last mob dies.
func (ts *trialSpawner) detect(h *hub, players map[int32]*tracked) int {
	for _, t := range players {
		if t.dim != ts.dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if dist3(t.x, t.y, t.z, ts.fx(), ts.fy(), ts.fz()) <= trialPlayerRange {
			ts.detected[t.p.uuid] = true
		}
	}
	return len(ts.detected)
}

// forgetDeadMobs drops mobs this spawner made that are gone — the fight ends
// when the last of them dies, so a stale eid would hang it open forever.
func (ts *trialSpawner) forgetDeadMobs(h *hub) {
	for eid := range ts.current {
		if m := h.mobs[eid]; m == nil || m.dying > 0 {
			delete(ts.current, eid)
		}
	}
}

func (ts *trialSpawner) targetTotal(extra int) int {
	if ts.ominous && ts.kind == "breeze" {
		return int(trialOminousBreeze + trialTotalPerPlayer*float64(extra))
	}
	return int(trialTotalMobs + trialTotalPerPlayer*float64(extra))
}

func (ts *trialSpawner) targetSimultaneous(extra int) int {
	return int(trialSimultaneous + trialSimulPerPlayer*float64(extra))
}

// reset returns a spent spawner to its sleeping state.
func (ts *trialSpawner) reset() {
	ts.state = trialWaitingPlayers
	ts.detected = map[[16]byte]bool{}
	ts.current = map[int32]bool{}
	ts.spawned, ts.nextSpawn, ts.nextEject = 0, 0, 0
}

func (ts *trialSpawner) fx() float64 { return float64(ts.pos.x) + 0.5 }
func (ts *trialSpawner) fy() float64 { return float64(ts.pos.y) }
func (ts *trialSpawner) fz() float64 { return float64(ts.pos.z) + 0.5 }

// spawnTrialMob puts one of the spawner's mobs into the room around it.
func (h *hub) spawnTrialMob(players map[int32]*tracked, ts *trialSpawner) *mob {
	etype, ok := trialSpawnerMobs[ts.kind]
	if !ok {
		return nil
	}
	for try := 0; try < 8; try++ {
		x := ts.fx() + float64(h.rng.Intn(2*trialSpawnRange+1)-trialSpawnRange)
		z := ts.fz() + float64(h.rng.Intn(2*trialSpawnRange+1)-trialSpawnRange)
		y := ts.pos.y
		if !h.spawnableAt(int(math.Floor(x)), y, int(math.Floor(z))) {
			continue
		}
		m := h.spawnHostileY(players, etype, x, float64(y), z)
		if m == nil {
			return nil // plugin-cancelled
		}
		if ts.kind == "baby_zombie" {
			m.baby = true
			m.refreshBabySpeed()
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
		}
		if ts.ominous {
			h.equipTrialMob(players, m, ts.kind) // the ominous configs' equipment tables
		}
		h.playSound(players, "minecraft:block.trial_spawner.spawn_mob", sndBlock,
			ts.fx(), ts.fy(), ts.fz(), 1, 1)
		return m
	}
	return nil
}

// spawnableAt is a cheap room check: air to stand in, air above, something
// solid underfoot.
func (h *hub) spawnableAt(x, y, z int) bool {
	w := h.world
	return w.At(x, y, z) == worldgen.Air && w.At(x, y+1, z) == worldgen.Air &&
		worldgen.IsSolidFull(w.At(x, y-1, z))
}

// ejectTrialReward pays out one player's share: vanilla rolls the key table and
// the consumables table, and the key is the only way one enters the world.
func (h *hub) ejectTrialReward(players map[int32]*tracked, ts *trialSpawner) {
	// One table per ejection, drawn by weight (lootTablesToEject.getRandom).
	for _, name := range []string{h.trialRewardTable(ts)} {
		tbl, ok := lootForChest(name)
		if !ok {
			continue // table not baked — the other still pays out
		}
		ctx := &lootCtx{rng: h.rng.Intn, randf: h.rng.Float64}
		for _, st := range h.evalChestStacks(tbl, ctx, 0) {
			if st.item == 0 || st.count <= 0 {
				continue
			}
			h.spawnItem(players, st.item, st.count, ts.fx(), ts.fy()+1, ts.fz())
		}
	}
	h.playSound(players, "minecraft:block.trial_spawner.eject_item", sndBlock,
		ts.fx(), ts.fy(), ts.fz(), 1, 1)
}
