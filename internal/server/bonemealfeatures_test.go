package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The feature-driven bone-meal targets: what grows where.
func TestBoneMealFeatureTargets(t *testing.T) {
	h, players := bonemealWorld(t)
	w := h.world
	apply := func(x, y, z int) bool { return h.applyBoneMeal(players, 0, x, y, z, w.At(x, y, z)) }

	// A melon stem ages two to five; at seven it may fruit.
	w.SetBlock(0, 179, 0, farmlandMin)
	w.SetBlock(0, 180, 0, melonStemBase)
	if !apply(0, 180, 0) || w.At(0, 180, 0) < melonStemBase+2 || w.At(0, 180, 0) > melonStemBase+5 {
		t.Errorf("melon stem → %d", w.At(0, 180, 0)-melonStemBase)
	}
	// A bamboo sapling shoots its first segment.
	w.SetBlock(2, 180, 0, bambooSapling)
	if !apply(2, 180, 0) || !isBamboo(w.At(2, 181, 0)) {
		t.Error("bamboo sapling did not shoot")
	}
	// Netherrack beside warped nylium turns warped.
	w.SetBlock(-3, 179, 0, warpedNylium)
	w.SetBlock(-2, 179, 0, netherrackState)
	if !apply(-2, 179, 0) || w.At(-2, 179, 0) != warpedNylium {
		t.Error("netherrack did not take nylium")
	}
	w.SetBlock(-2, 175, 0, netherrackState)
	if apply(-2, 175, 0) {
		t.Error("lone netherrack took bone meal")
	}
	// Crimson nylium sprouts crimson vegetation about it.
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 160, z, crimsonNylium)
		}
	}
	grew := false
	for i := 0; i < 5 && !grew; i++ {
		apply(0, 160, 0)
		for x := -3; x <= 3 && !grew; x++ {
			for z := -3; z <= 3; z++ {
				if s := w.At(x, 161, z); s == crimsonRoots || s == crimsonFungus || s == warpedFungus {
					grew = true
					break
				}
			}
		}
	}
	if !grew {
		t.Error("crimson nylium sprouted nothing")
	}
	// A crimson fungus on crimson nylium grows a huge fungus (two in five).
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 140, z, crimsonNylium)
		}
	}
	stem := worldgen.BlockID("crimson_stem")
	huge := false
	for i := 0; i < 40 && !huge; i++ {
		w.SetBlock(0, 141, 0, crimsonFungus)
		if !apply(0, 141, 0) {
			t.Fatal("fungus on its nylium refused")
		}
		huge = w.At(0, 141, 0) == stem && w.At(0, 144, 0) == stem
	}
	if !huge {
		t.Error("no huge fungus in forty tries")
	}
	w.SetBlock(3, 141, 3, warpedFungus)
	if apply(3, 141, 3) {
		t.Error("a warped fungus grew on crimson nylium")
	}
	// A moss block lays a patch: stone around it turns to moss.
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 120, z, worldgen.Stone)
			w.SetBlock(x, 121, z, worldgen.Air)
			w.SetBlock(x, 122, z, worldgen.Air)
		}
	}
	w.SetBlock(0, 120, 0, worldgen.MossBlock)
	if !apply(0, 120, 0) {
		t.Fatal("moss refused")
	}
	moss := 0
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			if w.At(x, 120, z) == worldgen.MossBlock {
				moss++
			}
		}
	}
	if moss < 5 {
		t.Errorf("moss patch laid %d moss, want a patch", moss)
	}
	// Glow lichen on a wall spreads a face.
	for y := 100; y <= 103; y++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(5, y, z, worldgen.Stone)
			w.SetBlock(4, y, z, worldgen.Air)
		}
	}
	lichen := withProp(worldgen.BlockID("glow_lichen"), "east", "true") // held by the wall at x=5
	w.SetBlock(4, 101, 0, lichen)
	if !apply(4, 101, 0) {
		t.Fatal("glow lichen refused")
	}
	faces := 0
	for y := 100; y <= 103; y++ {
		for z := -2; z <= 2; z++ {
			if s := w.At(4, y, z); inRange(s, glowLichenRange) {
				for _, f := range faceDirs {
					if prop(s, f.prop) == "true" {
						faces++
					}
				}
			}
		}
	}
	if faces != 2 {
		t.Errorf("lichen faces after spreading: %d, want 2", faces)
	}
	// Hanging moss lengthens at the tip.
	w.SetBlock(-5, 178, -5, worldgen.Stone)
	moss1 := withProp(worldgen.BlockBase("pale_hanging_moss"), "tip", "true")
	w.SetBlock(-5, 177, -5, moss1)
	if !apply(-5, 177, -5) || prop(w.At(-5, 177, -5), "tip") != "false" || prop(w.At(-5, 176, -5), "tip") != "true" {
		t.Error("hanging moss did not lengthen")
	}
	// Mangrove leaves drop a propagule.
	w.SetBlock(5, 178, 5, worldgen.BlockID("mangrove_leaves"))
	if !apply(5, 178, 5) || !isPropagule(w.At(5, 177, 5)) || !propaguleHanging(w.At(5, 177, 5)) {
		t.Error("mangrove leaves grew no propagule")
	}
	// A bush spreads to a neighbour on dirt; dry grass grows tall and spreads.
	w.SetBlock(0, 179, 3, worldgen.Dirt)
	w.SetBlock(1, 179, 3, worldgen.Dirt)
	w.SetBlock(-1, 179, 3, worldgen.Dirt)
	w.SetBlock(0, 179, 2, worldgen.Dirt)
	w.SetBlock(0, 179, 4, worldgen.Dirt)
	w.SetBlock(0, 180, 3, bushState)
	if !apply(0, 180, 3) {
		t.Error("bush did not spread")
	}
	w.SetBlock(0, 180, -3, shortDryGrass)
	if !apply(0, 180, -3) || w.At(0, 180, -3) != tallDryGrass {
		t.Error("short dry grass did not grow tall")
	}
	w.SetBlock(1, 179, -3, worldgen.BlockBase("red_sand"))
	w.SetBlock(-1, 179, -3, worldgen.Stone)
	w.SetBlock(0, 179, -2, worldgen.Stone)
	w.SetBlock(0, 179, -4, worldgen.Stone)
	if !apply(0, 180, -3) || w.At(1, 180, -3) != shortDryGrass {
		t.Error("tall dry grass did not spread onto sand")
	}
	// Pale moss carpet climbs a wall beside it.
	w.SetBlock(3, 180, -6, worldgen.Stone)
	w.SetBlock(3, 181, -6, worldgen.Stone)
	base := withProp(worldgen.BlockID("pale_moss_carpet"), "bottom", "true")
	w.SetBlock(2, 180, -6, base)
	if !apply(2, 180, -6) || !inRange(w.At(2, 181, -6), paleCarpetRange) || prop(w.At(2, 181, -6), "east") != "low" {
		t.Errorf("pale moss carpet did not climb: %d east=%s", w.At(2, 181, -6), prop(w.At(2, 181, -6), "east"))
	}
}
