package server

import (
	"math/rand"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Ominous trials — the omen chain vanilla 1.21 built around the trial
// chambers. A slain raid captain drops an ominous bottle (levels I–V);
// drinking it gives Bad Omen; a trial spawner that sees a player with Bad
// Omen turns it into Trial Omen and goes ominous itself: its fight restarts
// harder and its rewards come from the ominous tables (TrialSpawnerStateData
// tryDetectPlayers / transformBadOmenIntoTrialOmen / resetAfterBecomingOminous).

var itemOminousBottle = itemByName["ominous_bottle"]

const (
	badOmenBottleSecs     = 6000 // Bad Omen from a bottle: 120000 ticks
	trialOmenSecsPerLevel = 900  // TRIAL_OMEN_PER_BAD_OMEN_LEVEL 18000 ticks
	worldEventTrialOmen   = 3020 // level event at the player's eyes as the omen turns
	worldEventTrialMobOut = 3012 // a spawner's mob discarded (flame particles)
	trialOminousBreeze    = 4    // ominous breeze config: totalMobs 4 (normal 2)
)

// dropOminousBottle is the captain's death drop (entities/pillager loot:
// one bottle with set_ominous_bottle_amplifier uniform 0–4). The stack's
// potion field carries amplifier+1.
func (h *hub) dropOminousBottle(players map[int32]*tracked, m *mob) {
	if itemOminousBottle == 0 {
		return
	}
	if it := h.spawnItemIn(players, m.dim, itemOminousBottle, 1, m.x, m.y+0.5, m.z); it != nil {
		it.potion = int8(h.rng.Intn(5) + 1)
		h.refreshItemMeta(players, it)
	}
}

// ominousBottleLevel reads a bottle's level (1–5) from its stack.
func ominousBottleLevel(st invStack) int {
	if st.potion < 1 {
		return 1
	}
	return min(5, int(st.potion))
}

// drinkOminousBottle applies the bottle's Bad Omen (amplifier = level-1)
// and spends it.
func (h *hub) drinkOminousBottle(players map[int32]*tracked, t *tracked, slot int) {
	s := &t.inv.slots[slot]
	if s.item != itemOminousBottle || s.count == 0 {
		return
	}
	level := ominousBottleLevel(*s)
	h.advance(players, t, "consume_item", advMatch{item: s.item})
	s.count--
	if s.count == 0 {
		*s = invStack{}
	}
	h.sendSlot(t, slot)
	h.applyEffect(players, t, effBadOmen, level-1, badOmenBottleSecs)
}

// trialOmenCheck is the ominous half of tryDetectPlayers: a normal spawner
// that sees a player with Trial Omen, or with Bad Omen (which it turns into
// Trial Omen, 15 minutes a level), becomes ominous.
func (h *hub) trialOmenCheck(players map[int32]*tracked, ts *trialSpawner) {
	if ts.ominous || (ts.state == trialCooldown) {
		return
	}
	var omen *tracked
	var bad *tracked
	for _, t := range players {
		if t.dim != ts.dim || t.dead || t.gamemode == gmSpectator ||
			dist3(t.x, t.y, t.z, ts.fx(), ts.fy(), ts.fz()) > trialPlayerRange {
			continue
		}
		if t.hasEffect(effTrialOmen) > 0 {
			omen = t
			break
		}
		if bad == nil && t.hasEffect(effBadOmen) > 0 {
			bad = t
		}
	}
	if omen == nil && bad == nil {
		return
	}
	if omen == nil {
		omen = bad
		level := omen.hasEffect(effBadOmen) // amplifier+1
		h.removeEffect(omen, effBadOmen)
		h.applyEffect(players, omen, effTrialOmen, 0, trialOmenSecsPerLevel*level)
	}
	h.toNearbyEv(players, omen.dim, omen.x, omen.z, attachproto.WorldFX{Event: worldEventTrialOmen,
		X: floorInt(omen.x), Y: floorInt(omen.y + 1.62), Z: floorInt(omen.z)})
	h.becomeOminous(players, ts)
}

// becomeOminous is applyOminous + resetAfterBecomingOminous: the current
// mobs vanish in a puff, the round restarts on the ominous config, and the
// block shows it.
func (h *hub) becomeOminous(players map[int32]*tracked, ts *trialSpawner) {
	for eid := range ts.current {
		if m := h.mobs[eid]; m != nil {
			h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.WorldFX{Event: worldEventTrialMobOut,
				X: floorInt(m.x), Y: floorInt(m.y), Z: floorInt(m.z)})
			h.removeMob(players, m)
		}
	}
	ts.current = map[int32]bool{}
	ts.spawned = 0
	ts.nextSpawn = h.tick.Load() + trialTicksBetween
	ts.ominous = true
	if ts.state == trialWaitingPlayers || ts.state == trialInactive {
		ts.state = trialActive
	}
	h.showTrialState(players, ts)
	h.playSound(players, "minecraft:block.trial_spawner.ominous_activate", sndBlock, ts.fx(), ts.fy(), ts.fz(), 1, 1)
}

// trialRewardTable picks the one table an ejection pays from
// (lootTablesToEject.getRandom): normal 1:1 consumables:key, ominous 7:3.
func (h *hub) trialRewardTable(ts *trialSpawner) string {
	if ts.ominous {
		if h.rng.Intn(10) < 3 {
			return "spawners/ominous/trial_chamber/key"
		}
		return "spawners/ominous/trial_chamber/consumables"
	}
	if h.rng.Intn(2) == 0 {
		return "spawners/trial_chamber/key"
	}
	return "spawners/trial_chamber/consumables"
}

// trialEquipmentTable is the ominous config's equipment table for a mob
// kind (spawnDataWithEquipment): the melee set for zombies and husks, the
// ranged set for the archers; the rest fight bare.
func trialEquipmentTable(kind string) string {
	switch kind {
	case "zombie", "husk", "baby_zombie":
		return "equipment/trial_chamber_melee"
	case "skeleton", "stray", "poison_skeleton":
		return "equipment/trial_chamber_ranged"
	}
	return ""
}

// equipTrialMob dresses an ominous trial mob from its equipment table —
// trimmed, enchanted chainmail at even odds per piece and an enchanted
// weapon — as gear that never drops.
func (h *hub) equipTrialMob(players map[int32]*tracked, m *mob, kind string) {
	name := trialEquipmentTable(kind)
	if name == "" {
		return
	}
	tbl, ok := lootForChest(name)
	if !ok {
		return
	}
	r := rand.New(rand.NewSource(h.rng.Int63()))
	ctx := &lootCtx{rng: r.Intn, randf: r.Float64}
	worn := false
	for _, st := range h.evalChestStacks(tbl, ctx, 0) {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		if ap, ok := armorInfo[st.item]; ok {
			st.count = 1
			m.gear[ap.Slot] = st
			worn = true
			continue
		}
		m.held = st.item
		worn = true
	}
	if !worn {
		return
	}
	m.spawnGear = true
	m.refreshGearArmor()
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: b2i(m.held != 0)}, invStack{}, m.gear))
}
