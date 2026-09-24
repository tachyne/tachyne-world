package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestParseBlockState(t *testing.T) {
	if st, ok := parseBlockState("minecraft:stone"); !ok || st != worldgen.Stone {
		t.Errorf("stone: %d %v", st, ok)
	}
	st, ok := parseBlockState("oak_stairs[facing=south,half=top]")
	info, _ := worldgen.InfoForState(st)
	if !ok || worldgen.GetProperty(info, st, "facing") != "south" || worldgen.GetProperty(info, st, "half") != "top" {
		t.Errorf("stairs: %d %v", st, ok)
	}
	for _, bad := range []string{"no_such_block", "oak_stairs[facing=up]", "stone[colour=red]", "oak_stairs[facing=north"} {
		if _, ok := parseBlockState(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

// FillCommand: replace, hollow, outline, keep and a replace filter.
func TestFillModes(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	box := func(mode string, st uint32) evSetBlocks {
		return evSetBlocks{dim: 0, from: blockPos{0, 200, 0}, to: blockPos{2, 202, 2}, state: st, mode: mode}
	}
	count := func(st uint32) int {
		n := 0
		for x := 0; x <= 2; x++ {
			for y := 200; y <= 202; y++ {
				for z := 0; z <= 2; z++ {
					if h.world.At(x, y, z) == st {
						n++
					}
				}
			}
		}
		return n
	}
	glass := worldgen.BlockID("glass")
	h.applySetBlocks(players, box("replace", worldgen.Stone))
	if count(worldgen.Stone) != 27 {
		t.Fatalf("replace filled %d of 27", count(worldgen.Stone))
	}
	h.applySetBlocks(players, box("hollow", glass))
	if count(glass) != 26 || h.world.At(1, 201, 1) != worldgen.Air {
		t.Errorf("hollow: %d glass, centre %d", count(glass), h.world.At(1, 201, 1))
	}
	h.applySetBlocks(players, box("keep", worldgen.Stone)) // only the air centre
	if h.world.At(1, 201, 1) != worldgen.Stone || count(glass) != 26 {
		t.Error("keep replaced a non-air block or skipped the air one")
	}
	e := box("replace", worldgen.Dirt)
	e.filter, e.hasFilter = worldgen.Stone, true
	h.applySetBlocks(players, e)
	if h.world.At(1, 201, 1) != worldgen.Dirt || count(glass) != 26 {
		t.Errorf("the replace filter touched the wrong blocks: centre %d (dirt %d), glass %d", h.world.At(1, 201, 1), worldgen.Dirt, count(glass))
	}
}

// EnchantCommand: the held item takes a supported, compatible enchantment;
// an unsupported or conflicting one leaves it alone.
func TestEnchantCommand(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	sword := itemByName["diamond_sword"]
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: sword, count: 1}
	h.applyEnchantCommand(players, evEnchantCmd{by: pl.p.eid, target: "@s", ench: enchByName["sharpness"], lvl: 5})
	if pl.inv.slots[pl.p.heldSlot()].enchLvl(enchByName["sharpness"]) != 5 {
		t.Fatal("the sword did not take Sharpness V")
	}
	h.applyEnchantCommand(players, evEnchantCmd{by: pl.p.eid, target: "@s", ench: enchByName["smite"], lvl: 1})
	if pl.inv.slots[pl.p.heldSlot()].enchLvl(enchByName["smite"]) != 0 {
		t.Error("Smite joined Sharpness")
	}
	h.applyEnchantCommand(players, evEnchantCmd{by: pl.p.eid, target: "@s", ench: enchByName["efficiency"], lvl: 1})
	if pl.inv.slots[pl.p.heldSlot()].enchLvl(enchByName["efficiency"]) != 0 {
		t.Error("a sword took Efficiency")
	}
}
