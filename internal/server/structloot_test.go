package server

import (
	"strings"
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
	h.chests[simPos{blockPos: chestPos}] = c
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

// A trial chamber's dispensers and corridor pots fill from their own loot
// tables the first time anything touches them, and a pot that has been
// emptied is never restocked.
func TestTrialChamberDispensersAndPotsFill(t *testing.T) {
	h := newHub(world.New(1))
	g := h.world.Gen()

	var dispenser, pot blockPos
	var haveD, haveP bool
	for r := 0; r < 12 && !(haveD && haveP); r++ {
		tc := g.TrialChamberIn(r*1024, r*1024)
		if !tc.Exists {
			continue
		}
		for _, b := range g.TrialChamberLootBlocks(tc) {
			switch {
			case !haveD && strings.HasPrefix(b.Table, "dispensers/"):
				dispenser, haveD = blockPos{b.X, b.Y, b.Z}, true
			case !haveP && strings.HasPrefix(b.Table, "pots/"):
				pot, haveP = blockPos{b.X, b.Y, b.Z}, true
			}
		}
	}
	if !haveD || !haveP {
		t.Skip("no trial chamber with both a dispenser and a pot in range")
	}

	if name, ok := h.structureBinTable(dimOverworld, dispenser); !ok {
		t.Fatalf("the dispenser at %v names no loot table", dispenser)
	} else if !strings.HasPrefix(name, "dispensers/trial_chambers/") {
		t.Fatalf("dispenser table = %q", name)
	}

	key := simPos{dim: dimOverworld, blockPos: pot}
	h.ensurePotLoot(key)
	st, ok := h.pots[key]
	if !ok {
		t.Fatal("the pot was not stocked")
	}
	if st.item == 0 || st.count == 0 {
		t.Fatalf("the pot came up empty: %+v (the corridor table has no empty entry)", st)
	}

	// Emptying it leaves a known-empty entry, and a second look does not
	// refill it.
	h.pots[key] = invStack{}
	h.ensurePotLoot(key)
	if got := h.pots[key]; got.item != 0 {
		t.Fatalf("an emptied pot restocked itself with %+v", got)
	}
}
