package worldgen

import (
	"os"
	"testing"
)

// The native Nether and End are frozen: these chunks, with every feature
// and structure, hash as they did before the vanilla generator's Nether and
// End shared their code (vanillanether.go's frame) — a live world's
// dimensions must not move a block.

func vdmHashChunk(ch *Chunk) uint64 {
	h := uint64(1469598103934665603)
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			h ^= uint64(b)
			h *= 1099511628211
		}
		h = vdmHashName(h, ch.Biomes[s])
	}
	for _, v := range ch.Heightmap {
		h ^= uint64(uint16(v))
		h *= 1099511628211
	}
	return h
}

var nativeDimChunks = [][2]int32{{0, 0}, {3, -7}, {-12, 5}, {40, 40}, {-300, 211}, {100, -50}, {9, 123}, {-1, -1}, {6, 2}, {-5, 0}, {75, 0}, {0, 80}, {-70, -40}, {90, 30}, {2, 1}, {-2, -3}, {66, -33}, {-81, 12}}

var nativeNetherFrozen = map[int64][]uint64{
	1:                    {9795765560443736758, 8125530582364355813, 17461766019514166909, 15843109200750393576, 1802908483776175949, 16418260161339455542, 4639836821840982548, 2222451335236982431, 1357564964349151391, 17873034680079437925, 14885482049927224875, 3256667641750030090, 5690449124347994868, 291846827373909024, 18113961892953112854, 6785284992686080326, 3308326056362900012, 17170107135950457748},
	-4172144997902289642: {1370238978051273351, 1605702456194834354, 7501675863532640233, 13370416984772944956, 13994169377370934384, 10061439007621074677, 7342809240677664187, 842973811613722621, 4685516479974284068, 13951438312387961608, 6036421982299663303, 15937371777604946923, 9316006821145323961, 16071143282683881023, 11572590196425949542, 7572894036397492647, 730504642148856353, 16178418079939150002},
}

var nativeEndFrozen = map[int64][]uint64{
	1:                    {15367533437032665477, 12337060406145559555, 12337060406145559555, 12337060406145559555, 4819033237139203603, 15345450852733081807, 4819033237139203603, 3739452799586255270, 12337060406145559555, 16021573462142662348, 371385574446653371, 371385574446653371, 8651071465028477243, 16832557838408723, 17168663863728705435, 15182918669725347426, 15005188086723646339, 16832557838408723},
	-4172144997902289642: {12226501884616142139, 12337060406145559555, 12337060406145559555, 12337060406145559555, 4819033237139203603, 4819033237139203603, 4819033237139203603, 9452082731149813050, 12337060406145559555, 2858262575976380181, 597270026100871291, 371385574446653371, 15005188086723646339, 371385574446653371, 14090546888029584622, 970973817700531742, 17292800780805493757, 371385574446653371},
}

func TestNativeNetherEndUnchanged(t *testing.T) {
	for _, seed := range []int64{1, -4172144997902289642} {
		ng, eg := NewNetherGenerator(seed), NewEndGenerator(seed)
		for i, c := range nativeDimChunks {
			nh, eh := vdmHashChunk(ng.GenerateChunk(c[0], c[1])), vdmHashChunk(eg.GenerateChunk(c[0], c[1]))
			if os.Getenv("VDIMS_PRINT") != "" {
				t.Logf("seed %d chunk %d,%d nether %d end %d", seed, c[0], c[1], nh, eh)
				continue
			}
			if want := nativeNetherFrozen[seed]; i < len(want) && nh != want[i] {
				t.Errorf("seed %d: native nether chunk %d,%d hash %d, frozen %d", seed, c[0], c[1], nh, want[i])
			}
			if want := nativeEndFrozen[seed]; i < len(want) && eh != want[i] {
				t.Errorf("seed %d: native end chunk %d,%d hash %d, frozen %d", seed, c[0], c[1], eh, want[i])
			}
		}
		if len(nativeNetherFrozen[seed]) != len(nativeDimChunks) || len(nativeEndFrozen[seed]) != len(nativeDimChunks) {
			t.Errorf("seed %d: frozen tables incomplete", seed)
		}
	}
}
