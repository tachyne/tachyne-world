package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Zombies in water, spiders on walls, drowned in the deep.

// waterHub floods a column so a mob can be submerged in it.
func waterHub(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h, players := pushWorld(t)
	water, _ := worldgen.BlockRange("water")
	for y := 70; y <= 76; y++ {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				h.world.SetBlock(x, y, z, water)
			}
		}
	}
	return h, players
}

// Zombie.tick: 600 ticks with its eyes under before the conversion even
// STARTS, then 300 more while it shakes. We used to turn at the first timer.
func TestConversionTakesBothVanillaPhases(t *testing.T) {
	h, players := waterHub(t)
	z := putMob(t, h, players, entityZombie, 0.5, 71, 0.5)
	eid := z.eid

	for i := 0; i < drownConvertSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[eid]; !still {
		t.Fatal("the zombie converted the moment the first timer ran out")
	}
	if z.convertIn != drownShakeSecs {
		t.Errorf("shake timer %d, want %d after the first phase", z.convertIn, drownShakeSecs)
	}

	for i := 0; i < drownShakeSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[eid]; still {
		t.Error("the zombie never finished converting")
	}
	var drowned bool
	for _, m := range h.mobs {
		if m.etype == entityDrowned {
			drowned = true
		}
	}
	if !drowned {
		t.Error("no drowned came out of the conversion")
	}
}

// Once it has started, hauling the zombie onto dry land does not save it:
// Zombie.tick decrements the countdown before it looks at the water at all.
func TestAStartedConversionFinishesOutOfWater(t *testing.T) {
	h, players := waterHub(t)
	z := putMob(t, h, players, entityZombie, 0.5, 71, 0.5)
	for i := 0; i < drownConvertSecs; i++ {
		h.mobEnvironment(players)
	}
	if z.convertIn == 0 {
		t.Fatal("the conversion never started")
	}
	z.x, z.y, z.z = 40, 71, 40 // dragged out onto dry land

	for i := 0; i < drownShakeSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[z.eid]; still {
		t.Error("leaving the water cancelled a conversion already under way")
	}
}

// …but leaving before it starts resets the clock.
func TestLeavingEarlyResetsTheClock(t *testing.T) {
	h, players := waterHub(t)
	z := putMob(t, h, players, entityZombie, 0.5, 71, 0.5)
	for i := 0; i < drownConvertSecs-2; i++ {
		h.mobEnvironment(players)
	}
	z.x, z.y, z.z = 40, 71, 40
	h.mobEnvironment(players)
	if z.submerged != 0 {
		t.Errorf("submersion clock %d after leaving the water, want it reset", z.submerged)
	}
	if z.convertIn != 0 {
		t.Error("a conversion started after the zombie had already got out")
	}
}

// A husk turns into a zombie, not straight into a drowned.
func TestAHuskBecomesAZombieFirst(t *testing.T) {
	h, players := waterHub(t)
	husk := putMob(t, h, players, entityHusk, 0.5, 71, 0.5)
	for i := 0; i < drownConvertSecs+drownShakeSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[husk.eid]; still {
		t.Fatal("the husk never converted")
	}
	for _, m := range h.mobs {
		if m.etype == entityDrowned {
			t.Error("a husk turned straight into a drowned; vanilla goes via a zombie")
		}
	}
}

// Spider.tick: climbing is exactly "walked into something", and it is synced
// so the client can render the spider clinging.
func TestASpiderClimbsWhatItWalksInto(t *testing.T) {
	h, players := pushWorld(t)
	s := putMob(t, h, players, entitySpider, 0.5, 70, 0.5)
	if !climbsWalls(s.etype) {
		t.Fatal("spiders are not climbers")
	}
	h.setClimbing(players, s, true)
	if !s.climbing {
		t.Error("the climbing flag did not stick")
	}
	h.setClimbing(players, s, false)
	if s.climbing {
		t.Error("the flag did not clear")
	}
}

// A drowned swims in water and walks out of it — the amphibious pair that
// neither m.swims (water-bound) nor a plain walker covers.
func TestADrownedIsAmphibious(t *testing.T) {
	h, players := waterHub(t)
	d := h.spawnHostileY(players, entityDrowned, 0.5, 71, 0.5)
	if d == nil {
		t.Fatal("no drowned")
	}
	if !isAmphibious(d.etype) {
		t.Error("a drowned is not amphibious, so it walks the seabed")
	}
	if d.swims {
		t.Error("a drowned is marked water-BOUND; it must be able to come ashore")
	}
}

// Ordinary zombies neither climb nor swim.
func TestOrdinaryZombiesDoNeither(t *testing.T) {
	h, players := pushWorld(t)
	z := h.spawnHostileY(players, entityZombie, 0.5, 70, 0.5)
	if z == nil {
		t.Fatal("no zombie")
	}
	if climbsWalls(z.etype) || isAmphibious(z.etype) {
		t.Errorf("a zombie climbs=%v amphibious=%v, want neither",
			climbsWalls(z.etype), isAmphibious(z.etype))
	}
}
