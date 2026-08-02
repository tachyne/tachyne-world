package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Live-grown trees carry their decorators. The same PlaceTree runs behind
// growSapling, so a planted mega spruce podzols its ground and a grown
// mangrove hangs propagules — reading and writing the REAL world, which is
// what the podzol #dirt check and the propagule air rule are about.

func TestPlantedMegaSprucePodzolsTheGround(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 760, 200, 760
	grass := worldgen.BlockBase("grass_block") + 1 // snowy=false
	podzol := worldgen.BlockBase("podzol") + 1
	for dx := -10; dx <= 10; dx++ {
		for dz := -10; dz <= 10; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, grass)
			h.world.SetBlock(x+dx, y-2, z+dz, worldgen.Dirt)
		}
	}
	if !h.placeLiveTree(players, 0, x, y, z, "mega_spruce") {
		t.Fatal("a mega spruce refused to grow in the open")
	}
	found := 0
	for dx := -8; dx <= 8; dx++ {
		for dz := -8; dz <= 8; dz++ {
			if h.world.At(x+dx, y-1, z+dz) == podzol {
				found++
			}
			// Podzol belongs in the ground it replaced, never above it.
			for dy := 0; dy <= 2; dy++ {
				if h.world.At(x+dx, y+dy, z+dz) == podzol {
					t.Fatalf("floating podzol at dy=%d (%d,%d)", dy, dx, dz)
				}
			}
		}
	}
	if found == 0 {
		t.Error("a planted mega spruce podzoled nothing")
	}
}

func TestPlantedMangrovesHangPropagules(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	plo, phi := worldgen.BlockRange("mangrove_propagule")
	pods := 0
	for i := 0; i < 12; i++ {
		x, y, z := 800+i*24, 200, 800
		// Roots must find ground: a floor, not a floating block.
		for dx := -10; dx <= 10; dx++ {
			for dz := -10; dz <= 10; dz++ {
				h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Dirt)
			}
		}
		// A refused root walk is vanilla behaviour (the propagule just tries
		// again next tick), so retry the way the game would.
		grown := false
		for attempt := 0; attempt < 30 && !grown; attempt++ {
			grown = h.placeLiveTree(players, 0, x, y, z, "tall_mangrove")
		}
		if !grown {
			t.Fatalf("mangrove %d refused thirty growth attempts", i)
		}
		for dx := -6; dx <= 6; dx++ {
			for dy := 0; dy < 18; dy++ {
				for dz := -6; dz <= 6; dz++ {
					s := h.world.At(x+dx, y+dy, z+dz)
					if s < plo || s > phi {
						continue
					}
					pods++
					if (s-plo)%8 != 1 {
						t.Fatalf("propagule state %d is not hanging/stage0/dry", s)
					}
					if above := h.world.At(x+dx, y+dy+1, z+dz); !worldgen.IsLeaves(above) {
						t.Fatalf("propagule at (%d,%d,%d) hangs from %d, not a leaf", dx, dy, dz, above)
					}
				}
			}
		}
	}
	if pods == 0 {
		t.Error("twelve planted mangroves hung no propagules")
	}
}

// Bone meal on an azalea bush grows the azalea tree in place at 45% — the
// bush is lifted out for the attempt and restored when the tree refuses.
func TestBonemealGrowsAzaleaTrees(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	azalea := worldgen.BlockBase("azalea")
	rooted := worldgen.BlockBase("rooted_dirt")
	x, y, z := 1200, 200, 800
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, azalea)
	for i := 0; i < 400 && !worldgen.IsLog(h.world.At(x, y, z)); i++ {
		if !h.applyBoneMeal(players, 0, x, y, z, h.world.At(x, y, z)) {
			t.Fatal("bone meal on an azalea reported no effect — it must consume either way")
		}
	}
	if !worldgen.IsLog(h.world.At(x, y, z)) {
		t.Fatal("four hundred bone meals never grew the azalea")
	}
	if h.world.At(x, y-1, z) != rooted {
		t.Errorf("ground under the azalea tree is %d, want forced rooted dirt", h.world.At(x, y-1, z))
	}
	azLo, azHi := worldgen.BlockRange("azalea_leaves")
	flLo, flHi := worldgen.BlockRange("flowering_azalea_leaves")
	leavesSeen := false
	for dx := -4; dx <= 4 && !leavesSeen; dx++ {
		for dy := 0; dy < 10 && !leavesSeen; dy++ {
			for dz := -4; dz <= 4; dz++ {
				s := h.world.At(x+dx, y+dy, z+dz)
				if (s >= azLo && s <= azHi) || (s >= flLo && s <= flHi) {
					leavesSeen = true
					break
				}
			}
		}
	}
	if !leavesSeen {
		t.Error("the grown azalea tree has no azalea leaves")
	}
}
