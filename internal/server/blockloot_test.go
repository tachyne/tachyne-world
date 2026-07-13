package server

import (
	"math/rand"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func lootCtxFor(state uint32, silk bool, fortune int, r *rand.Rand) lootCtx {
	return lootCtx{state: state, silk: silk, fortune: fortune, rng: r.Intn, randf: r.Float64}
}

func TestBlockLootTables(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	r := rand.New(rand.NewSource(1))
	coalOre := worldgen.BlockBase("coal_ore")
	coal := int32(itemByName["coal"])
	coalOreItem := int32(itemByName["coal_ore"])

	// Coal ore has a baked table.
	if lootFor(coalOre) == nil {
		t.Fatal("coal_ore has no baked loot table")
	}

	// Silk touch → the ore block itself.
	got := h.evalBlockLoot(lootCtxFor(coalOre, true, 0, r))
	if len(got) != 1 || got[0].item != coalOreItem {
		t.Errorf("silk coal_ore = %+v, want the ore block", got)
	}

	// No silk → coal; Fortune raises the average yield above 1.
	total, n := 0, 4000
	for i := 0; i < n; i++ {
		for _, d := range h.evalBlockLoot(lootCtxFor(coalOre, false, 3, r)) {
			if d.item == coal {
				total += d.count
			}
		}
	}
	avg := float64(total) / float64(n)
	if avg <= 1.05 {
		t.Errorf("Fortune III coal average %.2f, expected well above 1", avg)
	}

	// Oak leaves: sometimes a sapling, sometimes nothing — never a crash, and
	// the fallback path isn't used (a baked table exists).
	oakLeaves := worldgen.BlockBase("oak_leaves")
	if lootFor(oakLeaves) == nil {
		t.Fatal("oak_leaves has no baked table")
	}
	sap := int32(itemByName["oak_sapling"])
	sawSapling := false
	for i := 0; i < 5000; i++ {
		for _, d := range h.evalBlockLoot(lootCtxFor(oakLeaves, false, 0, r)) {
			if d.item == sap {
				sawSapling = true
			}
		}
	}
	if !sawSapling {
		t.Error("oak leaves never dropped a sapling in 5000 breaks")
	}

	// Stone with no silk drops cobblestone (a set-via-alternatives table).
	stone := worldgen.Stone
	if lootFor(stone) != nil {
		cobble := int32(itemByName["cobblestone"])
		got := h.evalBlockLoot(lootCtxFor(stone, false, 0, r))
		if len(got) != 1 || got[0].item != cobble {
			t.Errorf("stone (no silk) = %+v, want cobblestone", got)
		}
	}
}

func TestEntityLootTables(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	r := rand.New(rand.NewSource(2))
	zombie := int32(entityByName["zombie"])
	rotten := int32(itemByName["rotten_flesh"])

	if lootForEntity(zombie) == nil {
		t.Fatal("zombie has no baked entity table")
	}
	// A zombie drops rotten flesh; Looting raises the average.
	base, loot := 0, 0
	n := 4000
	for i := 0; i < n; i++ {
		for _, d := range mustEntity(t, h, zombie, lootCtx{rng: r.Intn, randf: r.Float64}) {
			if d.item == rotten {
				base += d.count
			}
		}
		for _, d := range mustEntity(t, h, zombie, lootCtx{looting: 3, rng: r.Intn, randf: r.Float64}) {
			if d.item == rotten {
				loot += d.count
			}
		}
	}
	if float64(loot)/float64(n) <= float64(base)/float64(n) {
		t.Errorf("Looting didn't raise rotten-flesh yield: base %.2f looting %.2f",
			float64(base)/float64(n), float64(loot)/float64(n))
	}

	// A cow that died on fire yields cooked beef (furnace_smelt).
	cow := int32(entityByName["cow"])
	if lootForEntity(cow) != nil {
		cooked := int32(itemByName["cooked_beef"])
		sawCooked := false
		for i := 0; i < 50; i++ {
			for _, d := range mustEntity(t, h, cow, lootCtx{onFire: true, rng: r.Intn, randf: r.Float64}) {
				if d.item == cooked {
					sawCooked = true
				}
			}
		}
		if !sawCooked {
			t.Error("burning cow never dropped cooked beef")
		}
	}
}

func mustEntity(t *testing.T, h *hub, etype int32, ctx lootCtx) []drop {
	ds, ok := h.evalEntityLoot(etype, ctx)
	if !ok {
		t.Fatalf("no entity table for %d", etype)
	}
	return ds
}
