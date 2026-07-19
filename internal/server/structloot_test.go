package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestDesertTempleChestLoots(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	g := w.Gen()
	// Find a temple in this seed.
	var found bool
	var chestPos blockPos
	for cx := 0; cx < 24000 && !found; cx += 336 {
		for cz := 0; cz < 24000; cz += 336 {
			d := g.DesertTempleIn(cx+168, cz+168)
			if d.Exists {
				c := d.Chests()[0]
				chestPos = blockPos{c[0], c[1], c[2]}
				found = true
				break
			}
		}
	}
	if !found {
		t.Skip("no desert temple in range")
	}
	c := &chest{}
	h.chests[chestPos] = c
	h.fillStructureChest(chestPos, c)
	items := 0
	for _, s := range c.slots {
		if s.item != 0 {
			items++
		}
	}
	if items == 0 {
		t.Fatal("a desert-temple chest should hold loot")
	}
}

func TestVillageHouseChestLoot(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	g := w.Gen()
	// Find a village with a chest near origin (chests come from the real jigsaw
	// templates now — VillageChests).
	var found bool
	var chestPos worldgen.VillageChest
	for cx := 0; cx < 30000 && !found; cx += 512 {
		for cz := 0; cz < 30000; cz += 512 {
			vl := g.VillageIn(cx, cz)
			if !vl.Exists {
				continue
			}
			cs := g.VillageChests(vl)
			if len(cs) == 0 {
				continue
			}
			chestPos = cs[0]
			found = true
			break
		}
	}
	if !found {
		t.Skip("no village chest in range for this seed")
	}
	// structureChestTable must recognize the chest and return its village table.
	name, ok := h.structureChestTable(blockPos{chestPos.X, chestPos.Y, chestPos.Z})
	if !ok {
		t.Fatalf("village chest at (%d,%d,%d) not recognized", chestPos.X, chestPos.Y, chestPos.Z)
	}
	if name != chestPos.Table {
		t.Fatalf("village chest table %q, want %q", name, chestPos.Table)
	}
	// And it fills with loot.
	c := &chest{}
	h.fillStructureChest(blockPos{chestPos.X, chestPos.Y, chestPos.Z}, c)
	items := 0
	for _, s := range c.slots {
		if s.item != 0 {
			items++
		}
	}
	if items == 0 {
		t.Fatal("a village house chest should hold loot")
	}
}
