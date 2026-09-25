package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// eggOffspringNames are the species whose spawn egg, used on one of them,
// makes it a baby (Mob.checkAndHandleImportantInteractions →
// SpawnEggItem.spawnOffspringFromSpawnEgg): the AgeableMobs whose
// getBreedOffspring gives one of their own kind. Parrots, wandering
// traders, camel husks and slimes give none, so their egg does nothing to
// them.
var eggOffspringNames = []string{
	"cow", "mooshroom", "pig", "sheep", "chicken", "rabbit", "wolf", "cat", "ocelot",
	"fox", "panda", "polar_bear", "goat", "bee", "turtle", "sniffer", "armadillo",
	"axolotl", "horse", "donkey", "mule", "llama", "trader_llama", "camel", "strider",
	"hoglin", "skeleton_horse", "zombie_horse", "happy_ghast",
	"villager", "squid", "glow_squid", "dolphin",
}

// eggSetBaby are the non-ageable mobs an egg of their own kind still makes
// a baby of: spawnOffspringFromSpawnEgg creates a fresh one and calls
// Mob.setBaby, which only the zombie family, piglins and zoglins honour (a
// piglin brute stays grown, so its egg does nothing to it).
var eggSetBaby = entitySet("zombie", "husk", "drowned", "zombie_villager", "zombified_piglin", "piglin", "zoglin")

var eggOffspring = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range eggOffspringNames {
		if et, ok := entityByName[n]; ok {
			out[et] = true
		}
	}
	return out
}()

// tryEggOffspring is spawnOffspringFromSpawnEgg: an egg of the clicked
// mob's own species puts a baby of it where the mob stands, bred from the
// mob as both parents, and the egg is spent (creative keeps it).
func (h *hub) tryEggOffspring(players map[int32]*tracked, t *tracked, m *mob) bool {
	et, ok := spawnEggEntity[heldStack(t).item]
	if !ok || et != m.etype || m.dying > 0 {
		return false
	}
	if eggSetBaby[m.etype] {
		return h.eggSetBabyOffspring(players, t, m)
	}
	if !eggOffspring[m.etype] {
		return false
	}
	baby := h.spawnSpecies(players, m.etype, m.dim, m.x, m.y, m.z)
	if baby == nil {
		return false // a plugin refused the spawn
	}
	baby.baby, baby.growLeft = true, growUpTicks // setBaby(true): setAge(-24000)
	h.breedHorseAttributes(m, m, baby)
	h.inheritVariant(baby, m, m)
	if baby.etype == entityVillager {
		// Villager.getBreedOffspring with itself as the partner: half the
		// time the biome's type, else the parent's; born unemployed.
		baby.variant, baby.variantSet = int32(h.villagerType(m)), true
		if h.rng.Float64() < 0.5 {
			baby.variantSet = false
			h.villagerType(baby)
		}
		baby.setMoveSpeed(vanillaMoveSpeed[entityVillager] * attrToStep)
		h.initVillagerTrades(baby, profUnemployed)
		h.sendVillagerData(players, baby)
		baby.behavior = villagerBehavior{}
		baby.usesDoors = true
	}
	if baby.etype == entitySheep {
		h.toNearbyEv(players, baby.dim, baby.x, baby.z, metaEv(sheepMeta(baby, false)))
	}
	if vm := variantMeta(baby); vm != nil {
		h.toNearbyEv(players, baby.dim, baby.x, baby.z, metaEv(vm))
	}
	h.toTracking(players, baby.eid, baby.dim, baby.x, baby.z, metaEv(babyMeta(baby.eid, true)))
	if t.gamemode != gmCreative {
		h.consumeHeld(t)
	}
	return true
}

// eggSetBabyOffspring is spawnOffspringFromSpawnEgg's non-ageable branch:
// type.create (no finalizeSpawn roll to undo, so a spawned one that came
// out grown is made a baby) then setBaby — the zombie family and piglins
// take the baby speed bonus, a zoglin just shrinks.
func (h *hub) eggSetBabyOffspring(players map[int32]*tracked, t *tracked, m *mob) bool {
	baby := h.spawnMobIn(players, m.etype, m.dim, m.x, m.y, m.z)
	if baby == nil {
		return false
	}
	if baby.wearsAnything() { // type.create runs no finalizeSpawn: nothing worn or held
		baby.held, baby.heldEnch, baby.heldDmg, baby.gear = 0, enchList{}, 0, [4]invStack{}
		baby.refreshGearArmor()
		h.toTracking(players, baby.eid, baby.dim, baby.x, baby.z, equipEv(baby.eid, invStack{}, invStack{}, baby.gear))
	}
	if !baby.baby {
		baby.baby = true
		if baby.etype != entityZoglin {
			baby.setBabySpeed(true)
		}
		h.toTracking(players, baby.eid, baby.dim, baby.x, baby.z, metaEv(mobBabyMeta(baby, true)))
	}
	if t.gamemode != gmCreative {
		h.consumeHeld(t)
	}
	return true
}

// evSpawnEggLook is a spawn egg used with no block clicked: the client
// reports a use when the crosshair is on a fluid, which is not clickable.
type evSpawnEggLook struct {
	eid  int32
	slot int32
}

func (evSpawnEggLook) isHubEvent() {}

// useSpawnEggOnFluid is SpawnEggItem.use: the look ray, clipped at fluid
// sources, must end in a water or lava source, and the mob is spawned in
// that cell — how a squid or a fish is put straight into the sea.
func (h *hub) useSpawnEggOnFluid(players map[int32]*tracked, t *tracked, slot int32) {
	st := t.handStack(int(slot))
	if st == nil || st.count <= 0 || t.dead {
		return
	}
	et, ok := spawnEggEntity[st.item]
	if !ok {
		return
	}
	pos, found := h.lookRay(t, boatPlaceReach, func(_ blockPos, s uint32) bool {
		return s == worldgen.WaterBase || s == worldgen.LavaBase || rayStopsAt(s)
	})
	if !found {
		return
	}
	if s := h.worldFor(t.dim).At(pos.x, pos.y, pos.z); s != worldgen.WaterBase && s != worldgen.LavaBase {
		return // not a LiquidBlock: PASS
	}
	m := h.spawnMobIn(players, et, t.dim, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
	if m == nil {
		return
	}
	m.persistent = true
	h.vib(t.dim, freqEntityPlace, pos.x, pos.y, pos.z, t.p.eid)
	h.incStat(t, attachproto.StatUsed, st.item, 1)
	if t.gamemode != gmCreative {
		if st.count--; st.count == 0 {
			*st = invStack{}
		}
		h.sendHandSlot(t, int(slot))
	}
}
