package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestBoneMeal(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		tr.inv.slots[0] = invStack{item: itemBoneMeal, count: 3}

		// Crops age up 2–5 stages, capped.
		wheat := worldgen.BlockBase("wheat")
		w.SetBlock(5, 70, 0, wheat) // age 0
		grew := false
		for i := 0; i < 20 && !grew; i++ {
			tr.inv.slots[0] = invStack{item: itemBoneMeal, count: 3}
			w.SetBlock(5, 70, 0, wheat)
			h.onBoneMeal(h.playersRef, evBoneMeal{eid: p.eid, x: 5, y: 70, z: 0, slot: 0})
			if s := w.At(5, 70, 0); s > wheat {
				grew = true
				if s-wheat < 2 { // at least +2
					t.Errorf("crop grew by %d, want >=2", s-wheat)
				}
			}
		}
		if !grew {
			t.Error("bone meal never advanced the crop")
		}
		// It consumed a bone meal.
		if tr.inv.slots[0].count != 2 {
			t.Errorf("bone meal count %d, want 2", tr.inv.slots[0].count)
		}

		// A mature crop is not advanced (returns false, no consume).
		for _, r := range cropRanges {
			if inRange(wheat, r) {
				w.SetBlock(6, 70, 0, r[1]) // mature
				tr.inv.slots[0] = invStack{item: itemBoneMeal, count: 5}
				h.onBoneMeal(h.playersRef, evBoneMeal{eid: p.eid, x: 6, y: 70, z: 0, slot: 0})
				if w.At(6, 70, 0) != r[1] || tr.inv.slots[0].count != 5 {
					t.Error("bone meal wasted on a mature crop")
				}
				break
			}
		}

		// Grass block scatters short grass / flowers around.
		for dx := -3; dx <= 3; dx++ {
			for dz := -3; dz <= 3; dz++ {
				w.SetBlock(20+dx, 70, dz, worldgen.GrassBlock)
				w.SetBlock(20+dx, 71, dz, worldgen.Air)
			}
		}
		tr.inv.slots[0] = invStack{item: itemBoneMeal, count: 5}
		h.onBoneMeal(h.playersRef, evBoneMeal{eid: p.eid, x: 20, y: 70, z: 0, slot: 0})
		scattered := 0
		for dx := -3; dx <= 3; dx++ {
			for dz := -3; dz <= 3; dz++ {
				if s := w.At(20+dx, 71, dz); s == worldgen.ShortGrass || contains(bmFlowerBlocks, s) {
					scattered++
				}
			}
		}
		if scattered == 0 {
			t.Error("bone meal on grass scattered nothing")
		}
	})
}

func contains(xs []uint32, v uint32) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func TestStemGrowth(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		// A mature melon stem over grass, with an open lit neighbour, fruits.
		x, y, z := 40, 71, 0
		w.SetBlock(x, y-1, z, worldgen.Dirt) // stem sits on dirt
		for _, d := range horizNeighbors {   // clear + ground the neighbours
			w.SetBlock(x+d.x, y-1, z+d.z, worldgen.GrassBlock)
			w.SetBlock(x+d.x, y, z+d.z, worldgen.Air)
			w.SetBlock(x+d.x, y+5, z+d.z, worldgen.Air) // sky light
		}
		w.SetBlock(x, y+5, z, worldgen.Air)
		fruited := false
		for i := 0; i < 2000 && !fruited; i++ { // growth is now probability-gated
			w.SetBlock(x, y, z, melonStemBase+7) // reset to mature each try
			h.tickStem(h.playersRef, x, y, z, melonStemBase+7)
			for _, d := range horizNeighbors {
				if w.At(x+d.x, y, z+d.z) == melonBlock {
					fruited = true
					// The stem attached toward the fruit.
					if s := w.At(x, y, z); s < attachedMelonBase || s > attachedMelonBase+3 {
						t.Errorf("stem didn't attach: %d", s)
					}
				}
			}
		}
		if !fruited {
			t.Error("mature melon stem never fruited")
		}
		// Below max age it advances one stage — now gated by the vanilla
		// growth-speed probability, so loop until it fires.
		grew := false
		for i := 0; i < 2000 && !grew; i++ {
			w.SetBlock(x, y, z, melonStemBase+2)
			h.tickStem(h.playersRef, x, y, z, melonStemBase+2)
			if w.At(x, y, z) == melonStemBase+3 {
				grew = true
			}
		}
		if !grew {
			t.Error("stem never advanced past age 2 under the growth gate")
		}
	})
}
