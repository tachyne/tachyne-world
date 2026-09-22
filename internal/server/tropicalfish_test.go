package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The packing is vanilla's: the body size in the low byte, the pattern's
// index in the next, then the two dye colours a byte each.
func TestTropicalVariantPacking(t *testing.T) {
	// KOB on the small body, orange over white — the common "clownfish".
	got := packTropicalVariant(patKob, dyeOrange, dyeWhite)
	if want := int32(0 | 0<<8 | 1<<16 | 0<<24); got != want {
		t.Fatalf("kob orange/white packed %d, want %d", got, want)
	}
	// STRIPEY is the large body, index 1 → 1 | 1<<8.
	if got := patStripey.packed(); got != 1|1<<8 {
		t.Fatalf("stripey packed %d, want %d", got, 1|1<<8)
	}
	// The colours come back out where vanilla reads them.
	v := packTropicalVariant(patBetty, dyeRed, dyeWhite)
	if body := v >> 16 & 0xFF; body != dyeRed {
		t.Fatalf("body colour %d, want %d", body, dyeRed)
	}
	if pat := v >> 24 & 0xFF; pat != dyeWhite {
		t.Fatalf("pattern colour %d, want %d", pat, dyeWhite)
	}
}

// Nine spawns in ten are one of the twenty-two named fish; the rest are drawn
// freely, which is where a fish nobody has seen before comes from.
func TestTropicalVariantRollFavoursTheNamedFish(t *testing.T) {
	h := newHub(world.New(1))
	common := map[int32]bool{}
	for _, v := range tropicalCommon {
		common[v] = true
	}
	hits, odd := 0, 0
	for i := 0; i < 2000; i++ {
		if common[h.rollTropicalVariant()] {
			hits++
		} else {
			odd++
		}
	}
	if hits == 0 || odd == 0 {
		t.Fatalf("expected both kinds of roll, got %d common and %d free", hits, odd)
	}
	if hits < 1500 { // 90% of 2000, with room for the tail
		t.Fatalf("the named fish should be the common case, got %d of 2000", hits)
	}
}

// The variant rides on index 17: AbstractFish's from-bucket flag takes 16, and
// a fish is not an ageable mob on any served version, so nothing shifts it.
func TestTropicalFishVariantIndex(t *testing.T) {
	e, ok := variantEntryFor(entityTropicalFish)
	if !ok {
		t.Fatal("a tropical fish should carry a variant")
	}
	if e.idx != 17 {
		t.Fatalf("variant index %d, want 17", e.idx)
	}
}
