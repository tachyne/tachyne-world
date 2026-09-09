package worldgen

import "testing"

func TestStateNameAndProps(t *testing.T) {
	st := SetProperty(mustInfo(t, blockBase("oak_stairs")), blockBase("oak_stairs"), "facing", "east")
	name, ok := StateName(st)
	if !ok || name != "oak_stairs" {
		t.Fatalf("name %q ok=%v", name, ok)
	}
	props := StateProps(st)
	if props["facing"] != "east" || props["half"] == "" || props["shape"] == "" {
		t.Errorf("props %v", props)
	}
	if n, _ := StateName(Air); n != "air" {
		t.Errorf("air names %q", n)
	}
	if n, _ := StateName(Stone); n != "stone" {
		t.Errorf("stone names %q", n)
	}
	if StateProps(Stone) != nil {
		t.Error("stone has properties")
	}
	if _, ok := StateName(1 << 30); ok {
		t.Error("an out-of-range id names a block")
	}
}

func mustInfo(t *testing.T, state uint32) BlockInfo {
	info, ok := InfoForState(state)
	if !ok {
		t.Fatalf("no info for %d", state)
	}
	return info
}
