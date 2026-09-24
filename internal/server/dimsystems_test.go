package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Sculk, bees, villager doors and the creaking heart used to run only in the
// overworld. A player can build every one of them in the Nether, so each must
// work in the world it stands in, and stay apart from the overworld block at
// the same coordinates.

// netherPad builds a hub with a Nether world, a stone floor at y-1 and open
// air above it over a small square, and returns the pad's corner.
func netherPad(t *testing.T) (*hub, *world.World, map[int32]*tracked, int, int, int) {
	t.Helper()
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	x, y, z := 0, 80, 0
	nw.ForceLoad(x, z, 2)
	h.world.ForceLoad(x, z, 2)
	for dx := -6; dx <= 10; dx++ {
		for dz := -6; dz <= 10; dz++ {
			nw.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy < 5; dy++ {
				nw.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	players := map[int32]*tracked{}
	h.playersRef = players
	return h, nw, players, x, y, z
}

// A sensor placed in the Nether hears a block placed in the Nether and turns
// active there; the overworld sensor at the same coordinates stays quiet.
func TestNetherSculkSensorHearsNetherBlocks(t *testing.T) {
	h, nw, players, x, y, z := netherPad(t)
	pl := survPlayer(h)
	pl.dim = dimNether
	players[pl.p.eid] = pl
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	nw.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimNether, x: x, y: y, z: z, state: sensor, by: pl.p.eid})
	// The overworld twin: a sensor at the same x/y/z, in open air on stone.
	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	h.world.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimOverworld, x: x, y: y, z: z, state: sensor})
	stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2) // each heard its own placement

	nw.SetBlock(x+3, y, z, worldgen.Stone)
	h.onBlock(players, evBlock{dim: dimNether, x: x + 3, y: y, z: z, state: worldgen.Stone, by: pl.p.eid})
	stepSculk(h, players, 5)
	if s := nw.At(x, y, z); !isAnySensor(s) || sensorPhase(s) != sculkPhaseActive {
		t.Fatalf("the Nether sensor did not hear a Nether block placed three blocks away (phase %d)", sensorPhase(s))
	}
	if f := h.sculkFreq[simPos{dim: dimNether, blockPos: blockPos{x, y, z}}]; f != freqBlockPlace {
		t.Errorf("Nether sensor comparator frequency = %d, want %d", f, freqBlockPlace)
	}
	if s := h.world.At(x, y, z); sensorPhase(s) != sculkPhaseInactive {
		t.Error("a Nether placement set off the overworld sensor at the same coordinates")
	}
}

// A mob killed by a player near a Nether catalyst blooms sculk into the
// Nether floor, not the overworld's.
func TestNetherCatalystBloomsOnDeath(t *testing.T) {
	h, nw, players, x, y, z := netherPad(t)
	cat := worldgen.BlockBase("sculk_catalyst") + 1
	nw.SetBlock(x, y, z, cat)
	h.onBlock(players, evBlock{dim: dimNether, x: x, y: y, z: z, state: cat})
	m := h.spawnMobIn(players, entityBlaze, dimNether, float64(x)+2.5, float64(y), float64(z)+0.5)
	if m == nil {
		t.Fatal("no blaze")
	}
	m.hitByPlayer = true
	h.killMob(players, m)
	for i := 0; i < deathAnimTicks+2 && h.mobs[m.eid] != nil; i++ {
		h.tick.Add(1)
		h.updateMobs(players)
	}
	if h.mobs[m.eid] != nil {
		t.Fatal("the blaze never finished dying")
	}
	sculk := worldgen.BlockBase("sculk")
	converted := 0
	for dx := -4; dx <= 6; dx++ {
		for dz := -4; dz <= 4; dz++ {
			if nw.At(x+dx, y-1, z+dz) == sculk {
				converted++
			}
			if h.world.At(x+dx, y-1, z+dz) == sculk {
				t.Fatal("the Nether death bloomed sculk into the overworld")
			}
		}
	}
	if converted == 0 {
		t.Fatal("a Nether catalyst should bloom sculk into the Nether floor")
	}
}

// A shrieker in the Nether on its last warning calls its Warden up in the
// Nether, beside the shrieker.
func TestNetherShriekerSummonsWardenInNether(t *testing.T) {
	h, nw, players, x, y, z := netherPad(t)
	pl := survPlayer(h)
	pl.dim = dimNether
	pl.x, pl.y, pl.z = float64(x)+1.5, float64(y), float64(z)+0.5
	pl.wardenWarn = wardenWarnMax - 1
	players[pl.p.eid] = pl
	shrieker := shriekerWith(worldgen.BlockBase("sculk_shrieker"), false)
	nw.SetBlock(x, y, z, shrieker)
	h.onBlock(players, evBlock{dim: dimNether, x: x, y: y, z: z, state: shrieker})
	nw.SetBlock(x+3, y, z, worldgen.Stone)
	h.onBlock(players, evBlock{dim: dimNether, x: x + 3, y: y, z: z, state: worldgen.Stone, by: pl.p.eid})
	stepSculk(h, players, 5+shriekingTicks+1)
	var warden *mob
	for _, m := range h.mobs {
		if m.etype == entityWarden {
			warden = m
		}
	}
	if warden == nil {
		t.Fatal("the last warning should summon a Warden")
	}
	if warden.dim != dimNether {
		t.Fatalf("the Warden rose in dimension %d, want the Nether", warden.dim)
	}
}

// A bee in the Nether goes home to its Nether hive, is stored against the
// Nether position, and — with no night there to keep it in — comes out and
// fills that hive with honey while the overworld sleeps.
func TestNetherBeeHiveWorks(t *testing.T) {
	h, nw, players, x, y, z := netherPad(t)
	h.hivestore = newHiveStore("")
	h.hivesLoad()
	h.dayTime.Store(15000) // overworld night
	nest := blockPos{x, y + 1, z}
	nestState := worldgen.BlockBase("bee_nest") + 6
	nw.SetBlock(nest.x, nest.y, nest.z, nestState)
	h.world.SetBlock(nest.x, nest.y, nest.z, nestState) // the overworld twin
	m := h.spawnMobIn(players, entityBee, dimNether, float64(nest.x)+0.5, float64(nest.y)+0.5, float64(nest.z)+1.5)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeHome, m.beeHasHome, m.beeNectar = nest, true, true
	for i := 0; i < 30 && h.mobs[m.eid] != nil; i++ {
		h.updateBees(players)
	}
	if h.mobs[m.eid] != nil {
		t.Fatal("the Nether bee never entered its Nether hive")
	}
	nk := simPos{dim: dimNether, blockPos: nest}
	if occ := h.hives[nk]; len(occ) != 1 || !occ[0].Nectar {
		t.Fatalf("Nether hive occupants %v, want one with nectar", occ)
	}
	if len(h.hives[simPos{blockPos: nest}]) != 0 {
		t.Fatal("the bee went into the overworld hive at the same coordinates")
	}
	for i := 0; i < beeOccupySecs+5 && len(h.hives[nk]) > 0; i++ {
		h.updateBees(players)
	}
	if len(h.hives[nk]) != 0 {
		t.Fatal("the occupant stayed in: the Nether has no night to keep it")
	}
	if lvl := honeyLevel(nw.At(nest.x, nest.y, nest.z)); lvl != 1 {
		t.Fatalf("Nether hive honey %d, want 1", lvl)
	}
	if lvl := honeyLevel(h.world.At(nest.x, nest.y, nest.z)); lvl != 0 {
		t.Fatalf("the overworld hive gained honey (%d) from a Nether bee", lvl)
	}
	var out *mob
	for _, b := range h.mobs {
		if b.etype == entityBee {
			out = b
		}
	}
	if out == nil || out.dim != dimNether {
		t.Fatal("the bee should come back out in the Nether")
	}
}

// hives.json written before dimensions were known loads as the overworld's,
// and a Nether hive's occupants survive a save and a reload under their own
// dimension.
func TestHiveStoreCarriesDimension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hives.json")
	if err := os.WriteFile(path, []byte(`{"5,70,-3":[{"secs":40,"nectar":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHub(world.New(1))
	h.hivestore = newHiveStore(path)
	h.hivesLoad()
	if occ := h.hives[simPos{blockPos: blockPos{5, 70, -3}}]; len(occ) != 1 || occ[0].SecsLeft != 40 {
		t.Fatalf("an old save's hive did not load as the overworld's: %v", h.hives)
	}
	h.hives[simPos{dim: dimNether, blockPos: blockPos{5, 70, -3}}] = []hiveOccupant{{SecsLeft: 7}}
	h.hivesMark()
	h.hivestore.save()

	h2 := newHub(world.New(1))
	h2.hivestore = newHiveStore(path)
	h2.hivesLoad()
	if occ := h2.hives[simPos{dim: dimNether, blockPos: blockPos{5, 70, -3}}]; len(occ) != 1 || occ[0].SecsLeft != 7 {
		t.Fatalf("the Nether hive did not survive a reload: %v", h2.hives)
	}
	if occ := h2.hives[simPos{blockPos: blockPos{5, 70, -3}}]; len(occ) != 1 || occ[0].SecsLeft != 40 {
		t.Fatalf("the overworld hive changed across the reload: %v", h2.hives)
	}
}

// A villager in the Nether opens the Nether door beside it (not the
// overworld's) and shuts it again once it has gone.
func TestNetherVillagerOpensNetherDoor(t *testing.T) {
	h, nw, players, x, y, z := netherPad(t)
	lower, upper := worldgen.BlockBase("oak_door")+27, worldgen.BlockBase("oak_door")+19
	door := blockPos{x + 1, y, z}
	nw.SetBlock(door.x, door.y, door.z, lower)
	nw.SetBlock(door.x, door.y+1, door.z, upper)
	h.world.SetBlock(door.x, door.y, door.z, lower) // the overworld twin
	h.world.SetBlock(door.x, door.y+1, door.z, upper)
	m := h.spawnMobIn(players, entityVillager, dimNether, float64(x)+0.5, float64(y), float64(z)+0.5)
	if m == nil {
		t.Fatal("no villager")
	}
	m.usesDoors = true
	m.behavior = villagerBehavior{}
	m.home = blockPos{x, y, z}
	for i := 0; i < mobMoveInterval*2; i++ {
		h.tick.Add(1)
		h.updateMobs(players)
	}
	if worldgen.IsClosedDoor(nw.At(door.x, door.y, door.z)) {
		t.Fatal("the Nether villager did not open the Nether door beside it")
	}
	if !worldgen.IsClosedDoor(h.world.At(door.x, door.y, door.z)) {
		t.Fatal("a Nether villager opened the overworld door at the same coordinates")
	}
	h.removeMob(players, m)
	h.tick.Add(doorCloseGrace + 1)
	h.updateOpenDoors(players)
	if !worldgen.IsClosedDoor(nw.At(door.x, door.y, door.z)) {
		t.Fatal("the Nether door was not shut after the villager left")
	}
}

// heartTrunk stands a pale oak trunk with a heart in it along the given axis
// delta and places the heart through the block-change entry point.
func heartTrunk(h *hub, players map[int32]*tracked, w *world.World, dim int, pos blockPos, dx, dy, dz int, heart uint32) {
	for i := -2; i <= 2; i++ {
		w.SetBlock(pos.x+dx*i, pos.y+dy*i, pos.z+dz*i, worldgen.PaleOakLog)
	}
	w.SetBlock(pos.x, pos.y, pos.z, heart)
	h.onBlock(players, evBlock{dim: dim, x: pos.x, y: pos.y, z: pos.z, state: heart})
}

// A heart built in the Nether registers and ticks there, but CREAKING_ACTIVE
// follows the overworld's night: it sleeps and never sends out a creaking.
// The overworld heart at the same coordinates is a heart of its own.
func TestNetherCreakingHeartSleeps(t *testing.T) {
	h, nw, players, x, y, z := netherPad(t)
	nightHub(h)
	pl := survPlayer(h)
	pl.dim = dimNether
	pl.x, pl.y, pl.z = float64(x)+4, float64(y), float64(z)+4
	players[pl.p.eid] = pl
	pos := blockPos{x, y + 1, z}
	heartTrunk(h, players, nw, dimNether, pos, 0, 1, 0, worldgen.CreakingHeartUproot)
	if h.hearts[simPos{dim: dimNether, blockPos: pos}] == nil {
		t.Fatal("a heart placed in the Nether was never registered")
	}
	for i := 0; i < 10; i++ {
		for _, l := range h.hearts {
			l.nextAt = 0
		}
		h.updateHearts(players)
	}
	if got := nw.At(pos.x, pos.y, pos.z); got != worldgen.CreakingHeartDormant {
		t.Fatalf("a Nether heart with its logs should be dormant, state %d", got)
	}
	for _, m := range h.mobs {
		if m.etype == entityCreaking {
			t.Fatal("a Nether heart sent out a creaking")
		}
	}
	if h.hearts[simPos{blockPos: pos}] != nil {
		t.Fatal("the Nether heart registered against the overworld")
	}
}

// A heart a player lays on its side in the overworld wakes at night keeping
// its axis, and reads its logs along that axis.
func TestBuiltSidewaysHeartWakes(t *testing.T) {
	h, _, players, x, _, z := netherPad(t)
	nightHub(h)
	w := h.world
	pos := blockPos{x, 150, z}
	sideways := worldgen.CreakingHeartBase + 0*6 + heartUprooted*2 + 1 // axis x, uprooted, not natural
	heartTrunk(h, players, w, dimOverworld, pos, 1, 0, 0, sideways)
	for _, l := range h.hearts {
		l.nextAt = 0
	}
	h.updateHearts(players)
	want := worldgen.CreakingHeartBase + 0*6 + heartAwake*2 + 1
	if got := w.At(pos.x, pos.y, pos.z); got != want {
		t.Fatalf("a built sideways heart at night = state %d, want %d (axis x, awake, not natural)", got, want)
	}
}
