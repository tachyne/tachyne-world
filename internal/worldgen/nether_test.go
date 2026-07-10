package worldgen

import "testing"

func TestNetherTerrainShape(t *testing.T) {
	g := NewNetherGenerator(7)
	ch := g.GenerateChunk(0, 0)
	counts := map[uint32]int{}
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			counts[b]++
		}
	}
	if counts[Netherrack] < 10000 {
		t.Fatalf("nether should be mostly netherrack, got %d", counts[Netherrack])
	}
	if counts[Air] < 5000 {
		t.Fatalf("nether needs caverns, air=%d", counts[Air])
	}
	if counts[Lava] == 0 {
		t.Fatal("no lava sea")
	}
	if counts[Bedrock] == 0 {
		t.Fatal("no bedrock floor")
	}
	if ch.Biomes[0] != "minecraft:nether_wastes" {
		t.Fatalf("biome %q", ch.Biomes[0])
	}
	// Determinism.
	ch2 := NewNetherGenerator(7).GenerateChunk(0, 0)
	for s := range ch.Sections {
		if ch.Sections[s] != ch2.Sections[s] {
			t.Fatal("nether generation must be deterministic")
		}
	}
	// Above the ceiling is open void.
	top := ch.Sections[SectionCount-1]
	for _, b := range top {
		if b != Air {
			t.Fatalf("void above the ceiling has %d", b)
		}
	}
}

func TestNetherFloorStandable(t *testing.T) {
	g := NewNetherGenerator(7)
	found := false
	for x := 0; x < 200 && !found; x += 8 {
		y := g.NetherFloor(x, 0)
		if y > NetherLavaSea && y < NetherCeiling &&
			g.netherBlock(x, y, 0) == Air && g.netherBlock(x, y-1, 0) == Netherrack {
			found = true
		}
	}
	if !found {
		t.Fatal("no standable cavern floor found in 200 blocks")
	}
}
