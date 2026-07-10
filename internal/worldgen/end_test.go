package worldgen

import "testing"

func TestEndIslandShape(t *testing.T) {
	g := NewEndGenerator(7)
	ch := g.GenerateChunk(0, 0)
	counts := map[uint32]int{}
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			counts[b]++
		}
	}
	if counts[EndStone] < 5000 {
		t.Fatalf("origin chunk should be island interior, end stone=%d", counts[EndStone])
	}
	// Far chunk: pure void.
	far := g.GenerateChunk(40, 40)
	for s := range far.Sections {
		for _, b := range far.Sections[s] {
			if b != Air {
				t.Fatalf("void chunk contains %d", b)
			}
		}
	}
	// Pillars: obsidian on the ring.
	px := int(EndPillarRing * cos01(0))
	pz := int(EndPillarRing * sin01(0))
	if g.endBlock(px, 70, pz) != Obsidian {
		t.Fatalf("pillar 0 at (%d,%d) should be obsidian at y70, got %d", px, pz, g.endBlock(px, 70, pz))
	}
	if g.BiomeName(0, 0) != "minecraft:the_end" {
		t.Fatal("End biome wrong")
	}
}
