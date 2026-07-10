package server

import (
	"math"
	"tachyne/internal/worldgen"
	"testing"

	"tachyne/internal/world"
)

func TestHostileChasesTarget(t *testing.T) {
	h := newHub(world.New(1))
	m := &mob{speed: speedFor(entityZombie), hasTarget: true, x: 0, z: 0, tx: 10, tz: 0}
	vx, vz := hostileBehavior{}.steer(h, m)
	if vx <= 0 {
		t.Fatalf("hostile should steer toward its target (+x), got vx=%v", vx)
	}
	if vz != 0 {
		t.Fatalf("target due east should give no z steering, got vz=%v", vz)
	}
}

func TestZombieBitesPlayer(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[1] = pl
	m := &mob{eid: 2, etype: entityZombie, hostile: true, speed: speedFor(entityZombie), x: 0.5, y: 70, z: 1.2}

	h.mobMelee(players, m)
	if want := float32(maxHealth - zombieDamage); pl.health != want {
		t.Fatalf("zombie in reach should bite for %d: health=%v want %v", zombieDamage, pl.health, want)
	}
	if m.attackCD != attackCooldown {
		t.Fatalf("bite should start the attack cooldown, got %d", m.attackCD)
	}
	h.mobMelee(players, m) // still cooling down → no second bite
	if want := float32(maxHealth - zombieDamage); pl.health != want {
		t.Fatalf("on cooldown the zombie should not bite again: health=%v", pl.health)
	}
}

func TestZombieOutOfReachDoesNotBite(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[1] = pl
	m := &mob{eid: 2, etype: entityZombie, hostile: true, x: 0.5, y: 70, z: 6} // ~5 blocks away
	h.mobMelee(players, m)
	if pl.health != maxHealth {
		t.Fatalf("player out of reach should be unharmed: %v", pl.health)
	}
}

func TestHostileDoesNotFleeWhenHit(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.z = 0, 0
	players[1] = pl
	m := &mob{eid: 2, etype: entityZombie, hostile: true, health: zombieHealth, x: 1, z: 0}
	h.mobs[2] = m

	h.attackMob(players, 1, 2) // player 1 hits zombie 2
	if m.panic != 0 {
		t.Fatalf("a hostile mob should keep hunting when hit, not flee (panic=%d)", m.panic)
	}
	if want := zombieHealth - fistDamage; m.health != want {
		t.Fatalf("zombie should take the hit: health=%d want %d", m.health, want)
	}
}

func TestDaylightBurnsHostiles(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	players[1] = pl
	lx, lz := h.findLand(0, 0) // open-sky surface column
	m := h.spawnZombie(players, lx, lz)
	m.health = burnDamagePerSec // one second of burn is lethal
	m.burnDelay = 0             // skip the dawn-ramp stagger for the test
	h.dayTime.Store(1000)       // daytime

	h.updateHostiles(players) // relights the afterburn clock
	h.mobEnvironment(players) // renders the flame + deals the burn damage
	if !m.burning {
		t.Fatal("daylight burn must set the visible fire flag")
	}
	if m.dying == 0 {
		t.Fatal("a sky-exposed hostile should start dying (burn) in daylight")
	}
	for h.mobs[m.eid] != nil { // death animation plays out → despawn
		h.updateMobs(players)
	}
}

func TestBurnFlagClearsUnderCoverAndAtNight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	lx, lz := h.findLand(0, 0)
	m := h.spawnZombie(players, lx, lz)
	m.burnDelay = 0
	m.health = 100        // survive long enough to observe the afterburn
	h.dayTime.Store(1000) // daytime
	h.updateHostiles(players)
	h.mobEnvironment(players)
	if !m.burning {
		t.Fatal("should be burning in the open at day")
	}
	// A roof overhead stops re-ignition; the afterburn then burns down and out
	// (vanilla setSecondsOnFire keeps a mob alight ~8 s after reaching cover).
	roofY := int(m.y) + 3
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(int(m.x)+dx, roofY, int(m.z)+dz, 1)
		}
	}
	for i := 0; i < fireAfterburn+2 && m.burning; i++ {
		h.updateHostiles(players) // no re-ignite under cover
		h.mobEnvironment(players) // ticks the afterburn down
	}
	if m.burning {
		t.Fatal("afterburn should expire under cover")
	}
	// Once out, cover keeps it out (no more damage).
	before := m.health
	h.updateHostiles(players)
	h.mobEnvironment(players)
	if m.health != before {
		t.Fatal("a cool mob under cover should take no burn damage")
	}
	// Night puts out any straggler still flagged as burning.
	m.burning, m.fireSecs = true, 0
	h.dayTime.Store(nightStart + 100)
	h.mobEnvironment(players)
	if m.burning {
		t.Fatal("nightfall should clear the fire flag")
	}
}

func TestFireMetadataShape(t *testing.T) {
	b := fireMetadata(7, true)
	want := []byte{7, 0, 0, 0x01, 0xff} // eid, index 0, type byte, flags, end
	if len(b) != len(want) {
		t.Fatalf("fireMetadata = %v, want %v", b, want)
	}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("fireMetadata[%d] = %d, want %d", i, b[i], want[i])
		}
	}
	if off := fireMetadata(7, false); off[3] != 0 {
		t.Fatalf("fire-off flags = %d, want 0", off[3])
	}
}

// TestClosedDoorBlocksZombie proves a zombie can neither open nor walk through a
// door: a door is two solid collision cells, so entering its column is a 2-block
// step-up the mob refuses. Ring one in with doors and it must stay penned.
func TestClosedDoorBlocksZombie(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	cx, cz := h.findLand(0, 0)
	// Genuinely CLOSED oak_door states (open=false; the fixture previously
	// used 4686, which is open=true — the old collision-only logic blocked
	// mobs even through open doors, hiding the mistake).
	oakDoorClosedUpper := worldgen.BlockBase("oak_door") + 3  // north/upper/left/open=false/powered=false
	oakDoorClosedLower := worldgen.BlockBase("oak_door") + 11 // north/lower/left/open=false/powered=false
	// Fence the 3x3 pen with two-tall doors seated on each ring column's surface.
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			fx, fz := cx+dx, cz+dz
			base := w.SurfaceFeet(fx, fz)
			w.SetBlock(fx, base, fz, oakDoorClosedLower)
			w.SetBlock(fx, base+1, fz, oakDoorClosedUpper)
		}
	}
	m := h.spawnZombie(players, cx, cz)
	pl.x, pl.y, pl.z = float64(cx+5), m.y, float64(cz) // player outside the pen → zombie drives at the door
	for i := 0; i < 2000; i++ {
		h.updateMobs(players)
		if int(math.Floor(m.x)) != cx || int(math.Floor(m.z)) != cz {
			t.Fatalf("zombie passed a closed door to (%v,%v) at step %d", m.x, m.z, i)
		}
	}
}

func TestNightSpawnsHostiles(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	players[1] = pl

	// Daytime: the spawner must never produce hostiles.
	h.dayTime.Store(2000)
	for i := 0; i < 50; i++ {
		h.updateHostiles(players)
	}
	for _, m := range h.mobs {
		if m.hostile {
			t.Fatal("no hostiles should spawn during the day")
		}
	}

	// Night: a zombie should appear within a reasonable number of attempts.
	h.dayTime.Store(15000)
	spawned := false
	for i := 0; i < 200 && !spawned; i++ {
		h.updateHostiles(players)
		for _, m := range h.mobs {
			if m.hostile {
				spawned = true
			}
		}
	}
	if !spawned {
		t.Fatal("night spawner produced no hostiles after 200 attempts")
	}
}

func TestHostileStandoff(t *testing.T) {
	h := newHub(world.New(1))
	// Target within standoff distance: the mob should hold, not keep closing.
	m := &mob{speed: speedFor(entityZombie), hasTarget: true, x: 0, z: 0, tx: 0.5, tz: 0}
	if vx, vz := (hostileBehavior{}).steer(h, m); vx != 0 || vz != 0 {
		t.Fatalf("within standoff the hostile should hold position, got (%v,%v)", vx, vz)
	}
}
