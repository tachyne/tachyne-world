package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// gameplay/piglin_bartering (26.3) pays out a dried ghast at weight 10 of
// 469: the table had no such line, so a dried ghast could not be bartered.
func TestBarteringPaysOutDriedGhasts(t *testing.T) {
	h := newHub(world.New(1))
	total := 0
	for _, e := range barterTable {
		total += e.weight
	}
	if total != 469 {
		t.Fatalf("barter weights sum to %d, want 26.3's 469", total)
	}
	ghast := int32(itemByName["dried_ghast"])
	const rolls = 40000
	n := 0
	for i := 0; i < rolls; i++ {
		if st := h.rollBarter(); st.item == ghast {
			if st.count != 1 {
				t.Fatalf("a dried ghast comes one at a time, got %d", st.count)
			}
			n++
		}
	}
	want := rolls * 10.0 / 469
	if math.Abs(float64(n)-want) > 5*math.Sqrt(want) {
		t.Fatalf("%d dried ghasts in %d barters, want about %.0f", n, rolls, want)
	}
}
