package server

import "testing"

// snowHub buries a small column in powder snow so a mob can stand in a drift.
func snowHub(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h, players := pushWorld(t)
	for y := 70; y <= 72; y++ {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				h.world.SetBlock(x, y, z, powderSnowBlock)
			}
		}
	}
	return h, players
}

// Skeleton.tick: the snow clock fills first, and only then does the
// conversion clock start — the skeleton does not turn at the first timer.
func TestSkeletonFreezesIntoAStray(t *testing.T) {
	h, players := snowHub(t)
	s := putMob(t, h, players, entitySkeleton, 0.5, 71, 0.5)
	eid := s.eid

	for i := 0; i < strayFreezeSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[eid]; !still {
		t.Fatal("the skeleton turned the moment the snow clock filled")
	}
	if s.strayIn != strayConvertSecs {
		t.Errorf("conversion timer %d, want %d once the snow clock is full", s.strayIn, strayConvertSecs)
	}

	for i := 0; i < strayConvertSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[eid]; still {
		t.Fatal("the skeleton never froze into a stray")
	}
	var stray bool
	for _, m := range h.mobs {
		if m.etype == entityStray {
			stray = true
		}
	}
	if !stray {
		t.Error("no stray came out of the conversion")
	}
}

// …and stepping out of the drift throws BOTH clocks away, however far the
// conversion had got — unlike the drowning one, which finishes on dry land.
func TestLeavingThePowderSnowCancelsTheStrayConversion(t *testing.T) {
	h, players := snowHub(t)
	s := putMob(t, h, players, entitySkeleton, 0.5, 71, 0.5)
	for i := 0; i < strayFreezeSecs; i++ {
		h.mobEnvironment(players)
	}
	if s.strayIn == 0 {
		t.Fatal("the conversion never started")
	}

	s.x, s.y, s.z = 40, 71, 40 // out of the snow, one second short of turning
	h.mobEnvironment(players)
	if s.snowSecs != 0 || s.strayIn != 0 {
		t.Errorf("clocks %d/%d after leaving the snow, want both reset", s.snowSecs, s.strayIn)
	}
	for i := 0; i < strayFreezeSecs+strayConvertSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[s.eid]; !still {
		t.Error("a skeleton that walked out of the drift still turned into a stray")
	}
}

// ConversionType.convertCommon carries the mob's identity over: the name tag
// survives the change, and so does the flag that keeps it from despawning.
func TestAConversionCarriesTheNameAndPersistence(t *testing.T) {
	h, players := snowHub(t)
	s := putMob(t, h, players, entitySkeleton, 0.5, 71, 0.5)
	s.customName, s.persistent = "Bonesy", true

	for i := 0; i < strayFreezeSecs+strayConvertSecs; i++ {
		h.mobEnvironment(players)
	}
	if _, still := h.mobs[s.eid]; still {
		t.Fatal("the skeleton never froze into a stray")
	}
	var stray *mob
	for _, m := range h.mobs {
		if m.etype == entityStray {
			stray = m
		}
	}
	if stray == nil {
		t.Fatal("no stray came out of the conversion")
	}
	if stray.customName != "Bonesy" {
		t.Errorf("stray name %q, want the skeleton's", stray.customName)
	}
	if !stray.persistent {
		t.Error("the stray came out on the despawn clock: the name tag was spent for nothing")
	}
}
