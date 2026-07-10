package server

import (
	"math"
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func TestMobStaysOnLand(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	lx, lz := h.findLand(0, 0)
	h.herds = append(h.herds, &herd{x: float64(lx), z: float64(lz)})
	m := h.spawnMob(players, entityCow, float64(lx), float64(h.world.GroundY(lx, lz)), float64(lz))

	prevY := m.y
	for i := 0; i < 400; i++ {
		h.updateMobs(players)
		if d := math.Abs(m.y - prevY); d > 1 {
			t.Fatalf("mob stepped off a ledge: y jumped %v in one step", d)
		}
		prevY = m.y
	}
	fx, fz := int(math.Floor(m.x)), int(math.Floor(m.z))
	// Walkable = the mob's floor is dry and treeless. (Strict IsLand was
	// wrong here: DRY ground at exactly sea level — beaches — is legal
	// footing in vanilla; only water columns are off-limits.)
	if !h.world.Walkable(fx, fz) {
		t.Errorf("land mob wandered into water/tree at (%d,%d)", fx, fz)
	}
	if want := float64(h.world.MobFeet(fx, fz)); m.y != want {
		t.Errorf("mob y=%v not standing on ground (%v)", m.y, want)
	}
}

// TestMobPennedByFence builds a fence ring around a mob and checks it can never
// escape — a land mob must not climb or jump over a fence (1.5-block collision).
func TestMobPennedByFence(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	cx, cz := h.findLand(0, 0)
	g := h.world.GroundY(cx, cz)
	oakFence := worldgen.BlockBase("oak_fence") + 31 // default state
	// Fence the 3x3 pen: a ring one block out from the centre column, seated on
	// the surface of each column so it acts as a wall the mob can't cross.
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			fx, fz := cx+dx, cz+dz
			h.world.SetBlock(fx, h.world.SurfaceFeet(fx, fz), fz, oakFence)
		}
	}
	m := h.spawnMob(players, entityCow, float64(cx), float64(g), float64(cz))
	m.behavior = wanderBehavior{}
	for i := 0; i < 2000; i++ {
		h.updateMobs(players)
		if int(math.Floor(m.x)) != cx || int(math.Floor(m.z)) != cz {
			t.Fatalf("mob escaped its fenced pen to (%v,%v) at step %d", m.x, m.z, i)
		}
	}
}

// TestFenceAboveMobDoesNotTeleport places a fence in a mob's own column and checks
// the per-tick re-seat never lifts it onto the fence (where it would be stranded).
func TestFenceAboveMobDoesNotTeleport(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	cx, cz := h.findLand(0, 0)
	feet := w.MobFeet(cx, cz)
	m := h.spawnMob(players, entityCow, float64(cx), float64(feet), float64(cz))
	oakFence := worldgen.BlockBase("oak_fence") + 31
	w.SetBlock(cx, feet, cz, oakFence) // fence dropped right where the cow stands

	h.updateMobs(players)
	if int(math.Floor(m.y)) > feet {
		t.Fatalf("mob was teleported up onto the fence: y=%v (feet was %d)", m.y, feet)
	}
}

// TestNoSpawnOnFence proves the spawn-site check rejects a fenced column (a mob
// must not spawn on or inside a fence), while a plain land column is fine.
func TestNoSpawnOnFence(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	cx, cz := h.findLand(0, 0)
	if !w.Spawnable(cx, cz) {
		t.Fatalf("plain land column (%d,%d) should be spawnable", cx, cz)
	}
	oakFence := worldgen.BlockBase("oak_fence") + 31
	w.SetBlock(cx, w.MobFeet(cx, cz), cz, oakFence)
	if w.Spawnable(cx, cz) {
		t.Fatal("a fenced column must not be spawnable")
	}
}

func TestHerdCohesion(t *testing.T) {
	h := newHub(world.New(1))
	h.herds = append(h.herds, &herd{x: 0, z: 0}) // herd goal at origin
	m := &mob{eid: 1, etype: entityCow, behavior: herdBehavior{}, x: 12, z: 0}
	h.mobs[1] = m
	vx, _ := m.behavior.steer(h, m)
	if vx >= 0 {
		t.Errorf("cohesion should pull the mob toward the herd goal (vx<0), got vx=%v", vx)
	}
}

// TestNoAnimalSpawnInsideBuildings: a roofed interior is not animal-spawnable;
// open grass is.
func TestNoAnimalSpawnInsideBuildings(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	// Find an open GRASS column (findLand may return beach sand, which is
	// correctly not animal ground).
	cx, cz := -1000, -1000
	for x := -64; x <= 64 && cx == -1000; x++ {
		for z := -64; z <= 64; z++ {
			if h.spawnableAnimal(x, z) {
				cx, cz = x, z
				break
			}
		}
	}
	if cx == -1000 {
		t.Fatal("no animal-spawnable grass column within 64 blocks of spawn")
	}
	// Build a roof 3 blocks over the ground: now it's "inside".
	roofY := w.MobFeet(cx, cz) + 3
	w.SetBlock(cx, roofY, cz, worldgen.Stone)
	if h.spawnableAnimal(cx, cz) {
		t.Fatal("a roofed column must not be animal-spawnable")
	}
	// Stone floor (player-built) is also not natural spawning ground.
	cx2, cz2 := h.findLand(40, 40)
	w.SetBlock(cx2, w.MobFeet(cx2, cz2)-1, cz2, worldgen.Stone)
	if h.spawnableAnimal(cx2, cz2) {
		t.Fatal("a stone floor must not be animal-spawnable")
	}
}
