package worldgen

import "testing"

func TestSetProperty(t *testing.T) {
	// oak_log (default = axis y; range base..base+2 = x,y,z).
	oakLog := blockID("oak_log")
	logBase := blockBase("oak_log")
	log, ok := OrientInfo(oakLog)
	if !ok {
		t.Fatal("oak_log not in orientInfo")
	}
	if got := SetProperty(log, oakLog, "axis", "x"); got != logBase {
		t.Errorf("log axis x = %d, want %d", got, logBase)
	}
	if got := SetProperty(log, oakLog, "axis", "z"); got != logBase+2 {
		t.Errorf("log axis z = %d, want %d", got, logBase+2)
	}

	// oak_slab (default = bottom; type then waterlogged: top=base+1, double=base+5).
	oakSlab := blockID("oak_slab")
	slabBase := blockBase("oak_slab")
	slab, _ := OrientInfo(oakSlab)
	if got := SetProperty(slab, oakSlab, "type", "top"); got != slabBase+1 {
		t.Errorf("slab type top = %d, want %d", got, slabBase+1)
	}
	if got := SetProperty(slab, oakSlab, "type", "double"); got != slabBase+5 {
		t.Errorf("slab type double = %d, want %d", got, slabBase+5)
	}

	// oak_stairs (default = facing north, bottom half).
	oakStairs := blockID("oak_stairs")
	stairBase := blockBase("oak_stairs")
	st, _ := OrientInfo(oakStairs)
	if !st.HasProperty("half") || !st.HasProperty("facing") {
		t.Fatal("stairs should have half+facing")
	}
	if got := SetProperty(st, oakStairs, "facing", "east"); got != stairBase+71 {
		t.Errorf("stairs facing east = %d, want %d", got, stairBase+71)
	}
	if got := SetProperty(st, oakStairs, "half", "top"); got != stairBase+1 {
		t.Errorf("stairs half top = %d, want %d", got, stairBase+1)
	}

	// Unknown property/value leaves it unchanged.
	if got := SetProperty(log, oakLog, "facing", "north"); got != oakLog {
		t.Errorf("unknown prop changed state to %d", got)
	}
}
