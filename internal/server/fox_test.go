package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// foxArena lays a grass floor at y=179 with clear air above it, r blocks
// each way from the origin, high above the terrain.
func foxArena(h *hub, r int) float64 {
	h.world.ForceLoad(0, 0, 3)
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			h.world.SetBlock(x, 179, z, worldgen.GrassBlock)
			for y := 180; y <= 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	return 180
}

// A fox walks in on a chicken at 1.5 while it is far, crouches inside six
// blocks and stays down until it is fully crouched, then springs at it
// (StalkPreyGoal, FoxPounceGoal).
func TestFoxStalksCrouchesAndPounces(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 20)
	h.dayTime.Store(15000) // night: no sleep, no shelter
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	hen := h.spawnMob(players, entityChicken, 9.5, y, 0.5)
	hen.frozen, hen.health = true, 1000
	h.gridDirty()
	h.updateMobs(players)
	if fox.foxFlags&(foxFlagCrouching|foxFlagInterested) != 0 || fox.vx <= 0 {
		t.Fatalf("far off, the fox walks in upright: vx=%.3f flags=%#x", fox.vx, fox.foxFlags)
	}
	if want := fox.moveSpeed() * foxStalkSpeed; math.Abs(math.Hypot(fox.vx, fox.vz)-want) > 1e-9 {
		t.Errorf("stalking pace %.4f, want %.4f (1.5×)", math.Hypot(fox.vx, fox.vz), want)
	}
	crouched := false
	for i := 0; i < 60 && !crouched; i++ {
		h.gridDirty()
		h.updateMobs(players)
		crouched = fox.foxFlags&foxFlagCrouching != 0
	}
	if !crouched || fox.foxFlags&foxFlagInterested == 0 {
		t.Fatalf("inside six blocks it crouches, interested: flags=%#x", fox.foxFlags)
	}
	if d := math.Hypot(hen.x-fox.x, hen.z-fox.z); d > foxStalkRange+0.5 {
		t.Fatalf("crouched %.2f from the hen", d)
	}
	x0, held := fox.x, 0
	for fox.foxFlags&foxFlagPouncing == 0 && held < 40 {
		h.gridDirty()
		h.updateMobs(players)
		if fox.foxFlags&foxFlagPouncing == 0 {
			held++
			if fox.x != x0 {
				t.Fatalf("a crouched fox holds still: x %.3f→%.3f", x0, fox.x)
			}
		}
	}
	if fox.foxFlags&foxFlagPouncing == 0 {
		t.Fatal("once fully crouched it pounces")
	}
	if held*mobMoveInterval < foxFullCrouch-mobMoveInterval {
		t.Errorf("it pounced after %d ticks crouched; the crouch takes %d", held*mobMoveInterval, foxFullCrouch)
	}
	bitten := false
	for i := 0; i < 80 && !bitten; i++ {
		h.gridDirty()
		h.updateMobs(players)
		bitten = hen.health < 1000
	}
	if !bitten {
		t.Fatal("the pounce (or the bite after it) lands on the hen")
	}
}

// Coming down in snow, a pouncing fox ends up face down in it for forty
// ticks and forgets its quarry (FoxPounceGoal.tick, FaceplantGoal).
func TestFoxFaceplantsInSnow(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 20)
	for x := -20; x <= 20; x++ {
		for z := -20; z <= 20; z++ {
			h.world.SetBlock(x, 180, z, snowLayer1)
		}
	}
	h.dayTime.Store(15000)
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	hen := h.spawnMob(players, entityChicken, 5.5, y, 0.5)
	hen.frozen, hen.health = true, 1000
	h.gridDirty()
	// Put it straight into the pounce, as the crouch would.
	fox.foxPrey = hen.eid
	h.foxPounce(players, fox, hen)
	planted := false
	for i := 0; i < 60 && !planted; i++ {
		h.gridDirty()
		h.updateMobs(players)
		planted = fox.foxFlags&foxFlagFaceplanted != 0
	}
	if !planted {
		t.Fatalf("landing in snow the fox faceplants: flags=%#x y=%.2f", fox.foxFlags, fox.y)
	}
	if fox.foxFlags&foxFlagPouncing != 0 {
		t.Error("the pounce is over")
	}
	x0, z0 := fox.x, fox.z
	for i := 0; i < foxFaceplantTicks/mobMoveInterval-1; i++ {
		h.gridDirty()
		h.updateMobs(players)
		if fox.foxFlags&foxFlagFaceplanted == 0 {
			t.Fatalf("up again after %d ticks, want %d", (i+1)*mobMoveInterval, foxFaceplantTicks)
		}
		if fox.x != x0 || fox.z != z0 {
			t.Fatal("face down in the snow it goes nowhere")
		}
	}
	for i := 0; i < 3; i++ {
		h.gridDirty()
		h.updateMobs(players)
	}
	if fox.foxFlags&foxFlagFaceplanted != 0 {
		t.Fatal("after forty ticks it is up")
	}
}

// A fox picks up a loaf lying beside it and eats it after half a minute.
func TestFoxPicksUpAndEats(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 12)
	h.dayTime.Store(15000)
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	h.gridDirty()
	bread := int32(itemByName["bread"])
	it := h.spawnItem(players, bread, 1, 1.0, y, 0.5)
	it.y, it.noPickupUntil = y, 0
	for i := 0; i < 20 && fox.held != bread; i++ {
		h.gridDirty()
		h.updateMobs(players)
	}
	if fox.held != bread || h.items[it.eid] != nil {
		t.Fatalf("the fox should take the bread into its mouth: held=%d", fox.held)
	}
	for i := 0; i < foxEatAfter/mobMoveInterval+2; i++ {
		h.gridDirty()
		h.updateMobs(players)
	}
	if fox.held != 0 {
		t.Fatal("after 600 ticks the bread is eaten")
	}
}

// A fox sleeps through a quiet day under cover on grass; any creature
// within twelve blocks — a cow, not only a player — keeps it up
// (FoxAlertableEntitiesSelector).
func TestFoxSleepsOnlyWhenQuietAndSheltered(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 14)
	for x := -14; x <= 14; x++ {
		for z := -14; z <= 14; z++ {
			h.world.SetBlock(x, 182, z, worldgen.Stone) // a roof over the whole arena
		}
	}
	h.dayTime.Store(1000)
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	h.gridDirty()
	asleep := false
	for i := 0; i < 120 && !asleep; i++ {
		h.gridDirty()
		h.updateMobs(players)
		asleep = fox.foxFlags&foxFlagSleeping != 0
	}
	if !asleep {
		t.Fatalf("a quiet sheltered day: the fox sleeps (flags=%#x)", fox.foxFlags)
	}
	cow := h.spawnMob(players, entityCow, 6.5, y, 0.5)
	cow.frozen = true
	h.gridDirty()
	h.updateMobs(players)
	if fox.foxFlags&foxFlagSleeping != 0 {
		t.Fatal("a cow wandering up wakes it")
	}
	for i := 0; i < 120; i++ {
		h.gridDirty()
		h.updateMobs(players)
		if fox.foxFlags&foxFlagSleeping != 0 {
			t.Fatal("with a cow about it does not lie down again")
		}
	}
}

// Under the open sky by day a fox makes for a dark spot the sky cannot see
// (SeekShelterGoal at 1.25); in a thunderstorm it goes at once, even at night.
func TestFoxSeeksShelter(t *testing.T) {
	for _, storm := range []bool{false, true} {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		y := foxArena(h, 16)
		// A dark stone hall to the east: floor, walls and roof.
		for x := 3; x <= 15; x++ {
			for z := -15; z <= 15; z++ {
				h.world.SetBlock(x, 179, z, worldgen.Stone)
				for yy := 180; yy <= 182; yy++ {
					if x == 3 || x == 15 || z == -15 || z == 15 || yy == 182 {
						h.world.SetBlock(x, yy, z, worldgen.Stone)
					}
				}
			}
		}
		if storm {
			h.dayTime.Store(15000)
			h.thundering = true
		} else {
			h.dayTime.Store(1000)
		}
		fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
		h.gridDirty()
		found := false
		for i := 0; i < 40 && !found; i++ {
			fox.foxShelterIn = 0 // look every update: the search is what is under test
			fox.x, fox.z = 0.5, 0.5
			h.gridDirty()
			h.updateMobs(players)
			found = fox.hidePos != (blockPos{})
		}
		if !found {
			t.Fatalf("storm=%v: the fox should head for cover", storm)
		}
		p := fox.hidePos
		if p.x < 4 || p.x > 14 || p.z < -14 || p.z > 14 || p.y != 180 {
			t.Errorf("storm=%v: cover at %v is not inside the hall", storm, p)
		}
		if want := fox.moveSpeed() * foxShelterSpeed; math.Abs(math.Hypot(fox.vx, fox.vz)-want) > 1e-9 {
			t.Errorf("storm=%v: it runs for cover at %.4f, want %.4f", storm, math.Hypot(fox.vx, fox.vz), want)
		}
	}
	// At night, with no storm, it stays out.
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 8)
	h.dayTime.Store(15000)
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	for i := 0; i < 20; i++ {
		fox.foxShelterIn = 0
		h.gridDirty()
		h.updateMobs(players)
		if fox.hidePos != (blockPos{}) {
			t.Fatal("a clear night: no need for cover")
		}
	}
}

// A fox goes to a ripe sweet berry bush, waits beside it, and picks it:
// one berry in its mouth, the rest on the ground, the bush back to age one.
// A cave vine in fruit gives up its glow berries the same way.
func TestFoxEatsBerries(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 14)
	h.dayTime.Store(15000)
	h.world.SetBlock(6, 180, 0, berryBase+3)
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	h.gridDirty()
	for i := 0; i < 200 && h.world.At(6, 180, 0) != berryBase+1; i++ {
		h.gridDirty()
		h.updateMobs(players)
	}
	if got := h.world.At(6, 180, 0); got != berryBase+1 {
		t.Fatalf("the bush should be picked back to age one: state %d (age 3 is %d)", got, berryBase+3)
	}
	if fox.held != itemSweetBerries {
		t.Errorf("the fox keeps one berry in its mouth: held=%d", fox.held)
	}
	dropped := 0
	for _, it := range h.items {
		if it.item == itemSweetBerries {
			dropped += it.count
		}
	}
	if dropped < 1 || dropped > 2 {
		t.Errorf("a full bush gives 2-3 berries, one kept: %d on the ground", dropped)
	}

	// Glow berries.
	h2 := newHub(world.New(1))
	y = foxArena(h2, 14)
	h2.dayTime.Store(15000)
	vineBase := worldgen.BlockBase("cave_vines")
	info, _ := worldgen.InfoForState(vineBase)
	vine := worldgen.SetProperty(info, vineBase, "berries", "true")
	h2.world.SetBlock(5, 181, 0, worldgen.Stone)
	h2.world.SetBlock(5, 180, 0, vine)
	fox = h2.spawnMob(players, entityFox, 0.5, y, 0.5)
	h2.gridDirty()
	for i := 0; i < 200 && caveVineHasBerries(h2.world.At(5, 180, 0)); i++ {
		h2.gridDirty()
		h2.updateMobs(players)
	}
	if caveVineHasBerries(h2.world.At(5, 180, 0)) {
		t.Fatal("the fox should pick the glow berries off the vine")
	}
	got := fox.held == itemGlowBerries
	for _, it := range h2.items {
		got = got || it.item == itemGlowBerries
	}
	if !got {
		t.Error("the glow berries should be on the ground (or in its mouth)")
	}
}

// Idle and unalarmed, a fox now and then sits down and looks about, holding
// still until its two to four looks are done (PerchAndSearchGoal).
func TestFoxPerchesAndLooksAround(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := foxArena(h, 14)
	h.dayTime.Store(15000)
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	h.gridDirty()
	sat := false
	for i := 0; i < 3000 && !sat; i++ {
		h.gridDirty()
		h.updateMobs(players)
		sat = fox.foxFlags&foxFlagSitting != 0
	}
	if !sat {
		t.Fatal("an idle fox sits down to look about")
	}
	if fox.foxPerchLooks < 1 || fox.foxPerchLooks > 4 {
		t.Errorf("two to four looks: %d", fox.foxPerchLooks)
	}
	x0, z0 := fox.x, fox.z
	h.gridDirty()
	h.updateMobs(players)
	if fox.foxFlags&foxFlagSitting != 0 && (fox.x != x0 || fox.z != z0) {
		t.Error("a sitting fox holds still")
	}
}

// A fox watches a player from as far as twenty-four blocks
// (FoxLookAtPlayerGoal), where most animals stop at eight.
func TestFoxLooksAtPlayerFromAfar(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	y := foxArena(h, 14)
	h.dayTime.Store(15000)
	pl.x, pl.y, pl.z = 20.5, y, 0.5
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	h.gridDirty()
	watched := false
	for i := 0; i < 2000 && !watched; i++ {
		fox.x, fox.z = 0.5, 0.5 // keep it where it is: only the head is under test
		h.gridDirty()
		h.updateMobs(players)
		watched = fox.lookEID == pl.p.eid
	}
	if !watched {
		t.Fatal("a fox should watch a player twenty blocks off")
	}
}
