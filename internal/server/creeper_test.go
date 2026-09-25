package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestCreeperFusesAndExplodes(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(20, 20)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	m := h.spawnHostile(players, entityCreeper, lx, lz)
	pl.x, pl.y, pl.z = m.x+1.5, m.y, m.z // right next to it

	h.creeperFuse(players, m)
	if m.swellDir != 1 || m.swell != mobMoveInterval {
		t.Fatalf("creeper next to a player must start to swell, dir=%d swell=%d", m.swellDir, m.swell)
	}

	groundY := int(m.y) - 1 // solid under its feet (findLand ⇒ standable)
	if h.world.At(lx, groundY, lz) == worldgen.Air {
		t.Fatal("test setup: expected solid ground under the creeper")
	}
	for i := 0; i < 40; i++ {
		if _, alive := h.mobs[m.eid]; !alive {
			break
		}
		h.creeperFuse(players, m)
	}
	if _, alive := h.mobs[m.eid]; alive {
		t.Fatal("fuse ran out but the creeper did not explode")
	}
	if h.world.At(lx, groundY, lz) != worldgen.Air {
		t.Fatal("explosion must carve a crater (ground block survived)")
	}
	if pl.health >= maxHealth {
		t.Fatalf("point-blank blast must hurt: health=%v", pl.health)
	}
}

// Creeper.tick moves the swell a tick at a time toward the goal's direction:
// a creeper whose target runs off unwinds gradually, and one caught again
// half-swollen picks up where it left off. The engine used to zero the
// fuse on the spot, so a player could step out and back in for a fresh
// 1.5 seconds every time.
func TestCreeperUnwindsAndResumes(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(40, 40)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	m := h.spawnHostile(players, entityCreeper, lx, lz)
	pl.x, pl.y, pl.z = m.x+1.0, m.y, m.z
	for i := 0; i < 5; i++ {
		h.creeperFuse(players, m)
	}
	if m.swell != 5*mobMoveInterval {
		t.Fatalf("five updates of swelling = %d ticks, got %d", 5*mobMoveInterval, m.swell)
	}
	// While it swells it stands its ground (no chase mid-swell).
	if vx, vz := (creeperBehavior{}).steer(h, m); vx != 0 || vz != 0 {
		t.Fatal("a swelling creeper must hold still")
	}

	pl.x = m.x + 30 // sprint away past the cancel range
	h.creeperFuse(players, m)
	h.creeperFuse(players, m)
	if m.swellDir == 1 || m.swell != 3*mobMoveInterval {
		t.Fatalf("it should unwind a tick at a time: dir=%d swell=%d, want %d", m.swellDir, m.swell, 3*mobMoveInterval)
	}
	pl.x = m.x + 1.0 // back again
	updates := 0
	for ; updates < 40 && h.mobs[m.eid] != nil; updates++ {
		h.creeperFuse(players, m)
	}
	if want := (creeperFuseTicks - 3*mobMoveInterval) / mobMoveInterval; updates != want {
		t.Errorf("caught again it went off after %d updates, want %d (it resumes, not restarts)", updates, want)
	}

	// Far off for long enough, it winds all the way down and stays put.
	m2 := h.spawnHostile(players, entityCreeper, lx, lz)
	pl.x = m2.x + 1.0
	h.creeperFuse(players, m2)
	pl.x = m2.x + 30
	for i := 0; i < 60; i++ {
		h.creeperFuse(players, m2)
	}
	if _, alive := h.mobs[m2.eid]; !alive || m2.swell != 0 {
		t.Fatalf("a defused creeper must not explode (alive %v swell %d)", alive, m2.swell)
	}
}

// SwellGoal measures the real distance (distanceToSqr < 9), height
// included: a player 2.5 blocks straight overhead is in range, one 2 up and
// 2.5 across is not. The engine used to take 3 across and 2 up separately.
func TestCreeperIgnitionRangeIsSpherical(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	x, y, z := 50, 70, 50
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy < 5; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	m := h.spawnMob(players, entityCreeper, float64(x)+0.5, float64(y), float64(z)+0.5)
	pl.x, pl.y, pl.z = m.x+2.5, m.y+2, m.z // 3.2 away
	h.creeperFuse(players, m)
	if m.swellDir == 1 {
		t.Fatal("3.2 blocks away (2 up, 2.5 across) is out of range")
	}
	pl.x, pl.y, pl.z = m.x, m.y+2.5, m.z // straight up, 2.5 away
	h.creeperFuse(players, m)
	if m.swellDir != 1 {
		t.Fatal("2.5 blocks straight up is in range")
	}
}

// Flint and steel lights it for good (Creeper.isIgnited): it swells and goes
// off whether or not anybody is about — a creative player lighting one used
// to see it stand down on the next update, with no survival target near.
func TestIgnitedCreeperGoesOffAlone(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(60, 60)
	pl := testTracked()
	pl.gamemode = gmCreative
	players := map[int32]*tracked{1: pl}
	m := h.spawnHostile(players, entityCreeper, lx, lz)
	pl.x, pl.y, pl.z = m.x+1.0, m.y, m.z
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemFlintAndSteel, count: 1}
	if !h.tryIgniteCreeper(players, pl, m) {
		t.Fatal("flint and steel should light it")
	}
	for i := 0; i < 40 && h.mobs[m.eid] != nil; i++ {
		h.creeperFuse(players, m)
	}
	if h.mobs[m.eid] != nil {
		t.Fatalf("a lit creeper must go off, swell=%d", m.swell)
	}
}

// Ducking behind a wall is how you survive a creeper: SwellGoal wants the
// target in SIGHT, and stands the fuse down the moment it loses it. The fuse
// used to watch only the distance.
func TestCreeperStandsDownWhenItLosesSight(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	x, y, z := 50, 70, 50
	for dx := -4; dx <= 4; dx++ { // a floor, and open air above it
		for dz := -4; dz <= 4; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy < 3; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+2.5
	m := h.spawnMob(players, entityCreeper, float64(x)+0.5, float64(y), float64(z)+0.5)
	if m == nil {
		t.Fatal("the creeper should have spawned")
	}
	h.creeperFuse(players, m)
	if m.swellDir != 1 {
		t.Fatal("in the open and in range, the fuse should have lit")
	}
	// Drop a wall between them.
	for dy := 0; dy < 3; dy++ {
		h.world.SetBlock(x, y+dy, z+1, worldgen.Stone)
	}
	h.creeperFuse(players, m)
	if m.swellDir == 1 {
		t.Fatalf("out of sight, the creeper should stand down, swell=%d", m.swell)
	}
}
