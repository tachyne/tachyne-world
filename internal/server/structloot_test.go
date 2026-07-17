package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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
	// Find a village near origin.
	var found bool
	var house struct{ X, Y, Z int }
	var table string
	for cx := 0; cx < 30000 && !found; cx += 512 {
		for cz := 0; cz < 30000; cz += 512 {
			vl := g.VillageIn(cx, cz)
			if !vl.Exists || len(vl.Houses) == 0 {
				continue
			}
			ho := vl.Houses[0]
			hx, hy, hz := g.HouseChest(ho)
			house.X, house.Y, house.Z = hx, hy, hz
			table = villageChestTable(g.HouseWorkstation(ho))
			found = true
			break
		}
	}
	if !found {
		t.Skip("no village in range for this seed")
	}
	// structureChestTable must recognize the house chest and return a village table.
	name, ok := h.structureChestTable(blockPos{house.X, house.Y, house.Z})
	if !ok {
		t.Fatalf("house chest at (%d,%d,%d) not recognized", house.X, house.Y, house.Z)
	}
	if name != table {
		t.Fatalf("house chest table %q, want %q", name, table)
	}
	// And it fills with loot.
	c := &chest{}
	h.fillStructureChest(blockPos{house.X, house.Y, house.Z}, c)
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
