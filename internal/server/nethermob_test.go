package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func netherHub(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(7))
	nw, _ := world.NewNether(7, nil)
	h.nether = nw
	pl := testTracked()
	pl.dim = 1
	// Park the player on a real nether floor.
	y := nw.Gen().NetherFloor(40, 40)
	pl.x, pl.y, pl.z = 40.5, float64(y), 40.5
	return h, pl, map[int32]*tracked{1: pl}
}

func TestNetherMobsSpawnInDimOne(t *testing.T) {
	h, _, players := netherHub(t)
	for i := 0; i < 60 && len(h.mobs) == 0; i++ {
		h.updateNetherMobs(players)
	}
	if len(h.mobs) == 0 {
		t.Fatal("no nether mobs spawned in 60 passes")
	}
	for _, m := range h.mobs {
		if m.dim != 1 {
			t.Fatalf("nether mob in dim %d", m.dim)
		}
		if !isNetherMob(m.etype) {
			t.Fatalf("wrong species %d", m.etype)
		}
	}
}

func TestPiglinNeutralUntilHitThenPackAngers(t *testing.T) {
	h, pl, players := netherHub(t)
	a := h.spawnMobIn(players, entityZombifiedPiglin, 1, pl.x+2, pl.y, pl.z)
	h.configureNetherMob(players, a)
	b := h.spawnMobIn(players, entityZombifiedPiglin, 1, pl.x+4, pl.y, pl.z)
	h.configureNetherMob(players, b)
	if !a.neutral || a.anger != 0 {
		t.Fatal("piglins start neutral")
	}
	h.attackMob(players, 1, a.eid)
	if a.anger == 0 || b.anger == 0 {
		t.Fatal("hitting one piglin must anger the pack")
	}
}

func TestBlazeShootsFireballs(t *testing.T) {
	h, pl, players := netherHub(t)
	pl.gamemode = gmSurvival
	m := h.spawnMobIn(players, entityBlaze, 1, pl.x+5, pl.y, pl.z)
	h.configureNetherMob(players, m)
	h.blazeShoot(players, m)
	if len(h.arrows) != 1 {
		t.Fatalf("blaze should launch a fireball, arrows=%d", len(h.arrows))
	}
	for _, a := range h.arrows {
		if a.dim != 1 || !a.fire {
			t.Fatalf("fireball must be nether-bound + fiery: %+v", a)
		}
	}
}

func TestMagmaCubeSplitsInNether(t *testing.T) {
	h, pl, players := netherHub(t)
	m := h.spawnMobIn(players, entityMagmaCube, 1, pl.x+3, pl.y, pl.z)
	h.configureNetherMob(players, m)
	m.size = 4
	h.splitSlime(players, m)
	kids := 0
	for _, o := range h.mobs {
		if o != m && o.etype == entityMagmaCube {
			kids++
			if o.dim != 1 || o.size != 2 {
				t.Fatalf("split cube wrong: dim=%d size=%d", o.dim, o.size)
			}
		}
	}
	if kids < 2 {
		t.Fatalf("cube should split into 2+, got %d", kids)
	}
}

func TestBlazeRodOnlyOnPlayerKill(t *testing.T) {
	h, pl, players := netherHub(t)
	m := h.spawnMobIn(players, entityBlaze, 1, pl.x+2, pl.y, pl.z)
	h.configureNetherMob(players, m)
	m.hitByPlayer = false
	if loot := h.mobLoot(m); loot != nil {
		t.Fatal("no rods without a player kill")
	}
	m.hitByPlayer = true
	found := false
	for i := 0; i < 20; i++ {
		for _, d := range h.mobLoot(m) {
			if d.item == itemBlazeRod {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("player kills should sometimes drop blaze rods")
	}
}

func TestNetherDropsStayInNether(t *testing.T) {
	h, pl, players := netherHub(t)
	it := h.spawnItemIn(players, 1, itemBlazeRod, 1, pl.x, pl.y, pl.z)
	if it == nil || it.dim != 1 {
		t.Fatalf("nether drop should carry dim 1: %+v", it)
	}
	pl.gamemode = gmCreative // sidelined for phase one
	// An overworld player at the same coords must NOT pick it up.
	ow := testTracked()
	ow.p.eid = 2
	ow.gamemode = gmSurvival
	ow.x, ow.y, ow.z = pl.x, pl.y, pl.z
	players[2] = ow
	it.noPickupUntil = 0
	h.pickupItems(players)
	if len(h.items) != 1 {
		t.Fatal("cross-dimension pickup must be impossible")
	}
	// The nether player can.
	pl.gamemode = gmSurvival
	if pl.inv == nil {
		t.Skip("test tracked has no inventory")
	}
	h.pickupItems(players)
	if len(h.items) != 0 {
		t.Fatal("same-dimension pickup should work")
	}
	_ = worldgen.Netherrack
}

func TestNetherSpawnsNeverFloat(t *testing.T) {
	h, _, players := netherHub(t)
	for i := 0; i < 200; i++ {
		h.updateNetherMobs(players)
	}
	for _, m := range h.mobs {
		below := h.nether.At(floorInt(m.x), floorInt(m.y)-1, floorInt(m.z))
		if below == worldgen.Air || worldgen.IsLava(below) {
			t.Fatalf("mob %d floating/in-lava at (%.1f,%.1f,%.1f) over %d", m.etype, m.x, m.y, m.z, below)
		}
		if m.y > worldgen.NetherCeiling {
			t.Fatalf("mob above the nether ceiling at y=%.0f", m.y)
		}
	}
}

func TestMobFeetCappedInCaverns(t *testing.T) {
	nw, _ := world.NewNether(7, nil)
	// A fully solid column: MobFeet must not escape to the void above y=120.
	for x := 0; x < 400; x += 8 {
		if _, ok := nw.Gen().NetherFloorOK(x, 3); !ok {
			if y := nw.MobFeet(x, 3); y > worldgen.NetherCeiling {
				t.Fatalf("MobFeet climbed out of the world: y=%d at x=%d", y, x)
			}
			return
		}
	}
	t.Skip("no solid column found to test")
}

func TestNetherMobsCanActuallyWalk(t *testing.T) {
	nw, _ := world.NewNether(7, nil)
	// A known-good cavern floor must be Walkable (the overworld sea-level rule
	// froze every nether mob below y=63).
	for x := 0; x < 300; x += 4 {
		if y, ok := nw.Gen().NetherFloorOK(x, 40); ok && y < 60 {
			if !nw.Walkable(x, 40) {
				t.Fatalf("standable cavern floor at (%d,%d,40) reported unwalkable", x, y)
			}
			return
		}
	}
	t.Skip("no low cavern floor found")
}

func TestDroppedItemsSurviveRestart(t *testing.T) {
	h, pl, players := netherHub(t)
	h.spawnItemIn(players, 1, itemBlazeRod, 3, pl.x, pl.y, pl.z)
	h.spawnItem(players, 35, 7, 4.5, 70, 4.5)
	snap := h.snapshotItems()
	if len(snap) != 2 {
		t.Fatalf("want 2 snapshotted items, got %d", len(snap))
	}
	// A fresh hub restores them with dims intact.
	h2 := newHub(h.world)
	nw, _ := world.NewNether(7, nil)
	h2.nether = nw
	h2.restoreItems(snap)
	if len(h2.items) != 2 {
		t.Fatalf("want 2 restored, got %d", len(h2.items))
	}
	dims := map[int]int{}
	for _, it := range h2.items {
		dims[it.dim]++
	}
	if dims[1] != 1 || dims[0] != 1 {
		t.Fatalf("dims wrong after restore: %v", dims)
	}
}
