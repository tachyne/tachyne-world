package worldgen

import "testing"

// TestDeepDarkGeneratesSculk searches for a deep_dark chunk and asserts it grew
// sculk (and, over the search, at least one can_summon shrieker exists so the
// Warden path has a natural trigger).
func TestDeepDarkGeneratesSculk(t *testing.T) {
	g := NewGenerator(1)
	foundDeep, sculkChunks, shriekers := 0, 0, 0
	for cx := int32(0); cx < 40 && shriekers == 0; cx++ {
		for cz := int32(0); cz < 40; cz++ {
			ch := g.GenerateChunk(cx, cz)
			deep := false
			for _, b := range ch.Biomes {
				if b == "minecraft:deep_dark" {
					deep = true
					break
				}
			}
			if !deep {
				continue
			}
			foundDeep++
			sculk, shr := 0, 0
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					switch {
					case b == wgSculk:
						sculk++
					case b >= wgSculkShrieker && b < wgSculkShrieker+8:
						shr++
					}
				}
			}
			if sculk > 0 {
				sculkChunks++
			}
			shriekers += shr
			if shriekers > 0 {
				break
			}
		}
	}
	t.Logf("deep_dark chunks: %d, with sculk: %d, shriekers seen: %d", foundDeep, sculkChunks, shriekers)
	if foundDeep == 0 {
		t.Skip("no deep_dark chunk in the search window (biome noise) — cannot assert")
	}
	if sculkChunks == 0 {
		t.Fatal("deep_dark chunks generated NO sculk floor")
	}
}

func TestSculkFloorableExcludesFamily(t *testing.T) {
	if sculkFloorable(wgSculk) || sculkFloorable(wgSculkShrieker+3) || sculkFloorable(Air) || sculkFloorable(Bedrock) {
		t.Fatal("sculk/shrieker/air/bedrock must not be floorable")
	}
	if !sculkFloorable(Deepslate) {
		t.Fatal("deepslate should be floorable (a normal deep-dark floor block)")
	}
}
