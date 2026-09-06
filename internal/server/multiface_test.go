package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func vineWith(t *testing.T, faces ...string) uint32 {
	t.Helper()
	props := map[string]string{}
	for _, f := range faces {
		props[f] = "true"
	}
	return withProps(t, worldgen.BlockBase("vine"), props)
}

// A vine placed against a wall attaches through the face toward it; on a
// floor it gets no face and is refused; it survives while the wall stands,
// hangs from a vine above once the wall is gone, and drops when neither holds.
func TestVineFacesAndSurvival(t *testing.T) {
	info, _ := worldgen.InfoForState(worldgen.BlockBase("vine"))
	base := worldgen.BlockBase("vine")
	// Clicked the north face of a wall block: the vine sits north of it and
	// attaches through its SOUTH face.
	if got := orientMultiface(info, base, 2); worldgen.GetProperty(info, got, "south") != "true" {
		t.Errorf("north-face placement should set south, got state %d", got)
	}
	if got := orientMultiface(info, base, 1); got != base {
		t.Error("a vine cannot attach to a floor (DOWN is never a vine face)")
	}
	w := world.New(1)
	for dy := -1; dy <= 3; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(50+dx, 100+dy, 50+dz, worldgen.Air)
			}
		}
	}
	w.SetBlock(50, 100, 51, worldgen.Stone) // the wall, south of the vine
	v := vineWith(t, "south")
	w.SetBlock(50, 100, 50, v)
	if !supported(w, blockPos{50, 100, 50}, v) {
		t.Fatal("a vine on a wall is supported")
	}
	// Hanging: the wall goes, but a vine above carries the same face.
	w.SetBlock(50, 100, 51, worldgen.Air)
	w.SetBlock(50, 101, 50, vineWith(t, "south"))
	w.SetBlock(50, 101, 51, worldgen.Stone)
	if !supported(w, blockPos{50, 100, 50}, v) {
		t.Fatal("a vine hangs from the vine above it")
	}
	w.SetBlock(50, 101, 50, worldgen.Air)
	if supported(w, blockPos{50, 100, 50}, v) {
		t.Fatal("with neither wall nor vine above, the vine drops")
	}
	// Two faces, one lost: the state keeps the other rather than dropping.
	w.SetBlock(50, 100, 51, worldgen.Stone)
	w.SetBlock(51, 100, 50, worldgen.Stone)
	two := vineWith(t, "south", "east")
	w.SetBlock(51, 100, 50, worldgen.Air)
	ns, ok := multifaceUpdated(w, blockPos{50, 100, 50}, two)
	if !ok || worldgen.GetProperty(info, ns, "east") != "false" || worldgen.GetProperty(info, ns, "south") != "true" {
		t.Errorf("losing the east wall should prune only the east face: ok=%v state %d", ok, ns)
	}
}

// Scaffolding counts its distance: 0 on the ground, one more per scaffold
// out from support, and a scaffold at distance 7 cannot stand.
func TestScaffoldingDistance(t *testing.T) {
	w := world.New(1)
	for dx := -1; dx <= 9; dx++ {
		for dy := -1; dy <= 2; dy++ {
			w.SetBlock(60+dx, 100+dy, 60, worldgen.Air)
			w.SetBlock(60+dx, 100+dy, 61, worldgen.Air)
		}
	}
	w.SetBlock(60, 99, 60, worldgen.Stone)
	base := worldgen.BlockBase("scaffolding")
	info, _ := worldgen.InfoForState(base)
	st, ok := scaffoldUpdated(w, blockPos{60, 100, 60}, base)
	if !ok || worldgen.GetProperty(info, st, "distance") != "0" || worldgen.GetProperty(info, st, "bottom") != "false" {
		t.Fatalf("grounded scaffold: ok=%v distance %s bottom %s", ok, worldgen.GetProperty(info, st, "distance"), worldgen.GetProperty(info, st, "bottom"))
	}
	w.SetBlock(60, 100, 60, st)
	// Extend sideways, each one further out.
	for i := 1; i <= 7; i++ {
		s, ok := scaffoldUpdated(w, blockPos{60 + i, 100, 60}, base)
		want := i
		if i == 7 {
			if ok {
				t.Fatal("the seventh scaffold out cannot stand")
			}
			break
		}
		if !ok || worldgen.GetProperty(info, s, "distance") != itoa(want) || worldgen.GetProperty(info, s, "bottom") != "true" {
			t.Fatalf("scaffold %d: ok=%v distance %s (want %d), bottom %s (want true)", i, ok, worldgen.GetProperty(info, s, "distance"), want, worldgen.GetProperty(info, s, "bottom"))
		}
		w.SetBlock(60+i, 100, 60, s)
	}
	// Stacked on a scaffold, the distance carries straight up.
	s, _ := scaffoldUpdated(w, blockPos{63, 101, 60}, base)
	if worldgen.GetProperty(info, s, "distance") != "3" || worldgen.GetProperty(info, s, "bottom") != "false" {
		t.Errorf("stacked scaffold distance %s bottom %s, want 3/false", worldgen.GetProperty(info, s, "distance"), worldgen.GetProperty(info, s, "bottom"))
	}
}

// Sugar cane wants water beside the block it stands on; a cactus refuses a
// solid neighbour and stands only on sand or cactus.
func TestCaneAndCactusRules(t *testing.T) {
	w := world.New(1)
	for dx := -2; dx <= 2; dx++ {
		for dy := -1; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				w.SetBlock(70+dx, 100+dy, 70+dz, worldgen.Air)
			}
		}
	}
	cane := worldgen.BlockBase("sugar_cane")
	w.SetBlock(70, 99, 70, worldgen.Sand)
	if supported(w, blockPos{70, 100, 70}, cane) {
		t.Error("cane on dry sand is not supported")
	}
	w.SetBlock(71, 99, 70, worldgen.WaterBase)
	if !supported(w, blockPos{70, 100, 70}, cane) {
		t.Error("cane on sand beside water is supported")
	}
	w.SetBlock(70, 100, 70, cane)
	if !supported(w, blockPos{70, 101, 70}, cane) {
		t.Error("cane stacks on cane")
	}
	cactus := worldgen.BlockBase("cactus")
	w.SetBlock(72, 99, 72, worldgen.Sand)
	if !supported(w, blockPos{72, 100, 72}, cactus) {
		t.Error("a cactus on sand with clear sides is supported")
	}
	w.SetBlock(71, 100, 72, worldgen.Stone)
	if supported(w, blockPos{72, 100, 72}, cactus) {
		t.Error("a cactus touching a solid block breaks")
	}
	w.SetBlock(71, 100, 72, worldgen.Air)
	w.SetBlock(72, 99, 72, worldgen.Stone)
	if supported(w, blockPos{72, 100, 72}, cactus) {
		t.Error("a cactus needs sand or cactus below")
	}
}

// Bamboo roots in sand, dirt, gravel or bamboo — not stone.
func TestBambooPlantableOn(t *testing.T) {
	w := world.New(1)
	bamboo := worldgen.BlockBase("bamboo")
	for _, c := range []struct {
		below uint32
		want  bool
	}{{worldgen.Sand, true}, {worldgen.Dirt, true}, {worldgen.BlockBase("gravel"), true}, {worldgen.Stone, false}, {worldgen.Air, false}} {
		w.SetBlock(80, 99, 80, c.below)
		w.SetBlock(80, 100, 80, worldgen.Air)
		if got := supported(w, blockPos{80, 100, 80}, bamboo); got != c.want {
			t.Errorf("bamboo on state %d: supported %v, want %v", c.below, got, c.want)
		}
	}
}

// Chorus: a stem stands on end stone or a stem, or hangs off a horizontal
// stem that has one below; a flower stands on a stem, or on air beside
// exactly one stem.
func TestChorusSurvival(t *testing.T) {
	w := world.New(1)
	for dx := -2; dx <= 2; dx++ {
		for dy := -1; dy <= 3; dy++ {
			for dz := -2; dz <= 2; dz++ {
				w.SetBlock(90+dx, 100+dy, 90+dz, worldgen.Air)
			}
		}
	}
	stem := chorusPlantBase
	flower := chorusFlowerBase
	w.SetBlock(90, 99, 90, endStoneBlock)
	if !chorusSurvives(w, blockPos{90, 100, 90}, stem) {
		t.Error("a stem on end stone stands")
	}
	w.SetBlock(90, 99, 90, worldgen.Stone)
	if chorusSurvives(w, blockPos{90, 100, 90}, stem) {
		t.Error("a stem on stone falls")
	}
	// A branch: stem at (90,101,90) hangs off the stem at (91,101,90) which stands on a stem below.
	w.SetBlock(90, 99, 90, endStoneBlock)
	w.SetBlock(91, 100, 90, stem)
	w.SetBlock(91, 101, 90, stem)
	w.SetBlock(90, 100, 90, worldgen.Air)
	if !chorusSurvives(w, blockPos{90, 101, 90}, stem) {
		t.Error("a stem beside a supported stem hangs from it")
	}
	// A flower on air with exactly one stem beside it stands; two, and it falls.
	if !chorusSurvives(w, blockPos{92, 101, 90}, flower) {
		t.Error("a flower beside one stem stands")
	}
	w.SetBlock(92, 101, 91, stem)
	if chorusSurvives(w, blockPos{92, 101, 90}, flower) {
		t.Error("a flower beside two stems falls")
	}
}

// A vine on a wall spreads: over enough random ticks it grows sideways or
// downward, every new vine keeping a real face; hemmed in by five vines it
// stops.
func TestVineSpreads(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.world
	for dx := -6; dx <= 6; dx++ {
		for dy := -6; dy <= 4; dy++ {
			for dz := -6; dz <= 6; dz++ {
				w.SetBlock(120+dx, 100+dy, 120+dz, worldgen.Air)
			}
		}
	}
	// A stone wall along z at x=121; a vine on its west face at (120,100,120).
	for dz := -3; dz <= 3; dz++ {
		for dy := -5; dy <= 3; dy++ {
			w.SetBlock(121, 100+dy, 120+dz, worldgen.Stone)
		}
	}
	w.SetBlock(120, 100, 120, vineWith(t, "east"))
	count := func() int {
		n := 0
		for dx := -6; dx <= 6; dx++ {
			for dy := -6; dy <= 4; dy++ {
				for dz := -6; dz <= 6; dz++ {
					if isVineBlock(w.At(120+dx, 100+dy, 120+dz)) {
						n++
					}
				}
			}
		}
		return n
	}
	for i := 0; i < 4000 && count() < 3; i++ {
		for dx := -6; dx <= 6; dx++ {
			for dy := -6; dy <= 4; dy++ {
				for dz := -6; dz <= 6; dz++ {
					if s := w.At(120+dx, 100+dy, 120+dz); isVineBlock(s) {
						h.tickVine(players, 0, 120+dx, 100+dy, 120+dz, s)
					}
				}
			}
		}
	}
	if count() < 3 {
		t.Fatalf("the vine never spread: %d vines", count())
	}
	// Every vine placed is supported (no faceless or floating vine).
	for dx := -6; dx <= 6; dx++ {
		for dy := -6; dy <= 4; dy++ {
			for dz := -6; dz <= 6; dz++ {
				p := blockPos{120 + dx, 100 + dy, 120 + dz}
				if s := w.At(p.x, p.y, p.z); isVineBlock(s) && !supported(w, p, s) {
					t.Errorf("spread produced an unsupported vine at %v state %d", p, s)
				}
			}
		}
	}
	if h.vineCanSpread(w, 120, 100, 120) && count() >= 5 {
		t.Error("five or more vines nearby must stop spreading")
	}
}
