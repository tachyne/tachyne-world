package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// SpawnEggItem.useOn: right-clicking a block with a spawn egg puts the mob on
// the face that was clicked. This did nothing at all — eggs worked only on a
// spawner or out of a dispenser — which a creative player notices at once.
func TestSpawnEggPlacesItsMob(t *testing.T) {
	w := world.New(61)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.gamemode = gmCreative
	pl.dim, pl.x, pl.y, pl.z = 0, 0.5, 180, 0.5

	stone := worldgen.BlockBase("stone")
	w.SetBlock(2, 179, 0, stone)

	count := func(et int) int {
		n := 0
		for _, m := range h.mobs {
			if m.etype == et {
				n++
			}
		}
		return n
	}

	// A cat egg on the top face of a stone block: the cat stands on it.
	pl.inv.slots[0] = invStack{item: itemByName["cat_spawn_egg"], count: 1}
	pl.p.setHotbarSlot(0, int32(itemByName["cat_spawn_egg"]))
	h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: 2, y: 179, z: 0, face: 1})
	if count(entityCat) != 1 {
		t.Fatalf("a cat spawn egg on stone made %d cats, want 1", count(entityCat))
	}
	var cat *mob
	for _, m := range h.mobs {
		if m.etype == entityCat {
			cat = m
		}
	}
	if int(cat.y) != 180 {
		t.Errorf("the cat stands at y=%v, want on top of the block at 180", cat.y)
	}
	if !cat.persistent {
		t.Error("an egg-spawned mob is placed, not natural: it must not despawn")
	}
	// Creative keeps the egg.
	if pl.inv.slots[0].count != 1 {
		t.Errorf("creative must not spend the egg, count %d", pl.inv.slots[0].count)
	}

	// Clicking a replaceable block spawns INSIDE it rather than on its face.
	w.SetBlock(5, 180, 0, worldgen.BlockBase("short_grass"))
	pl.inv.slots[0] = invStack{item: itemByName["pig_spawn_egg"], count: 1}
	pl.p.setHotbarSlot(0, int32(itemByName["pig_spawn_egg"]))
	h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: 5, y: 180, z: 0, face: 1})
	var pig *mob
	for _, m := range h.mobs {
		if m.etype == entityPig {
			pig = m
		}
	}
	if pig == nil {
		t.Fatal("a pig spawn egg on grass made no pig")
	}
	if int(pig.y) != 180 {
		t.Errorf("the pig stands at y=%v, want inside the grass at 180", pig.y)
	}

	// A survival player pays for it.
	pl.gamemode = gmSurvival
	pl.inv.slots[0] = invStack{item: itemByName["cow_spawn_egg"], count: 3}
	pl.p.setHotbarSlot(0, int32(itemByName["cow_spawn_egg"]))
	h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: 2, y: 179, z: 0, face: 1})
	if count(entityCow) != 1 {
		t.Fatalf("a cow spawn egg made %d cows, want 1", count(entityCow))
	}
	if pl.inv.slots[0].count != 2 {
		t.Errorf("survival spends one egg, %d left of 3", pl.inv.slots[0].count)
	}
}

// Mob.checkAndHandleImportantInteractions: an egg of the clicked mob's own
// species makes a baby of it at the mob's feet (spawnOffspringFromSpawnEgg),
// bred from it — a lamb takes its parent's fleece — and the egg is spent;
// an egg of another species does nothing to the mob.
func TestSpawnEggOnItsOwnSpeciesMakesABaby(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 1.5, 180, 0.5
	for x := -2; x <= 3; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	sheep := h.spawnSpecies(players, entitySheep, 0, 0.5, 180, 0.5)
	sheep.color = 11 // blue
	egg := int32(itemByName["sheep_spawn_egg"])
	pl.inv.slots[0] = invStack{item: egg, count: 2}
	pl.p.setHotbarSlot(0, egg)
	before := len(h.mobs)
	if !h.interactMob(players, pl, sheep, false) {
		t.Fatal("a sheep egg on a sheep should be used")
	}
	if len(h.mobs) != before+1 {
		t.Fatalf("want one new mob, have %d → %d", before, len(h.mobs))
	}
	var lamb *mob
	for _, m := range h.mobs {
		if m != sheep {
			lamb = m
		}
	}
	if lamb.etype != entitySheep || !lamb.baby || lamb.growLeft <= 0 {
		t.Fatalf("the egg should make a lamb: type %d baby %v grow %d", lamb.etype, lamb.baby, lamb.growLeft)
	}
	if lamb.color != 11 {
		t.Errorf("the lamb takes its one parent's fleece: colour %d", lamb.color)
	}
	if pl.inv.slots[0].count != 1 {
		t.Errorf("survival spends the egg: %d left", pl.inv.slots[0].count)
	}

	cow := int32(itemByName["cow_spawn_egg"])
	pl.inv.slots[0] = invStack{item: cow, count: 1}
	pl.p.setHotbarSlot(0, cow)
	before = len(h.mobs)
	h.interactMob(players, pl, sheep, false)
	if len(h.mobs) != before || pl.inv.slots[0].count != 1 {
		t.Fatalf("a cow egg on a sheep does nothing: mobs %d → %d, eggs %d", before, len(h.mobs), pl.inv.slots[0].count)
	}
}

// SpawnEggItem.use: an egg used while looking at water (the client sends a
// plain use, a fluid not being clickable) puts the mob in the water.
func TestSpawnEggUsedOnWaterSpawnsInIt(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 0, 60
	for x := -2; x <= 2; x++ {
		for z := -1; z <= 4; z++ {
			h.world.SetBlock(x, 178, z, worldgen.WaterBase)
		}
	}
	egg := int32(itemByName["squid_spawn_egg"])
	pl.offhand = invStack{item: egg, count: 1}
	pl.p.setOffhand(egg)
	r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
	r.Action(attachproto.UseItem{Hand: handOffhand})
	for len(h.events) > 0 {
		if e, ok := (<-h.events).(evSpawnEggLook); ok {
			h.useSpawnEggOnFluid(players, pl, e.slot)
		}
	}
	var squid *mob
	for _, m := range h.mobs {
		if m.etype == entitySquid {
			squid = m
		}
	}
	if squid == nil {
		t.Fatal("a squid egg used on water should put a squid in it")
	}
	if floorInt(squid.y) != 178 {
		t.Errorf("the squid should be in the water cell at y=178, is at %v", squid.y)
	}
	if pl.offhand.count != 0 {
		t.Errorf("the offhand egg is spent: %+v", pl.offhand)
	}
}

// EntityType.spawn runs finalizeSpawn for an egg's mob: an egg zombie is a
// zombie — hostile, able to be geared — and an egg villager has its walk,
// its doors and its (unemployed) trades, as a summoned one does. The egg
// used to make a bare wandering body of either.
func TestSpawnEggMobIsConfiguredLikeItsKind(t *testing.T) {
	w := world.New(61)
	w.ForceLoad(0, 0, 1)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.gamemode = gmCreative
	pl.dim, pl.x, pl.y, pl.z = 0, 0.5, 180, 0.5
	for x := -2; x <= 6; x++ {
		w.SetBlock(x, 179, 0, worldgen.BlockBase("stone"))
	}
	use := func(egg string, x int) *mob {
		t.Helper()
		pl.inv.slots[0] = invStack{item: itemByName[egg], count: 1}
		pl.p.setHotbarSlot(0, int32(itemByName[egg]))
		before := map[int32]bool{}
		for id := range h.mobs {
			before[id] = true
		}
		h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: x, y: 179, z: 0, face: 1})
		for id, m := range h.mobs {
			if !before[id] && m.etype == spawnEggEntity[itemByName[egg]] {
				return m
			}
		}
		t.Fatalf("%s spawned nothing", egg)
		return nil
	}
	if z := use("zombie_spawn_egg", 0); !z.hostile {
		t.Error("an egg zombie must be hostile")
	}
	v := use("villager_spawn_egg", 3)
	if _, ok := v.behavior.(villagerBehavior); !ok || !v.usesDoors {
		t.Errorf("an egg villager must run the villager brain and use doors: %T doors=%v", v.behavior, v.usesDoors)
	}
}
