package server

import (
	"math/rand"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A mob's death table draws among a pool's entries by weight: the polar
// bear's cod (3) against salmon (1), the witch's stick (2) against each of
// its five other weight-1 drops. Even draws would give 1:1 for both.
func TestEntityLootHonoursEntryWeights(t *testing.T) {
	h := newHub(world.New(1))
	r := rand.New(rand.NewSource(7))
	ratio := func(etype int32, a, b string) float64 {
		na, nb := 0, 0
		for i := 0; i < 6000; i++ {
			for _, d := range mustEntity(t, h, etype, lootCtx{rng: r.Intn, randf: r.Float64}) {
				switch d.item {
				case itemByName[a]:
					na++
				case itemByName[b]:
					nb++
				}
			}
		}
		if nb == 0 {
			t.Fatalf("%s never dropped", b)
		}
		return float64(na) / float64(nb)
	}
	if got := ratio(int32(entityPolarBear), "cod", "salmon"); got < 2.5 || got > 3.5 {
		t.Errorf("polar bear cod:salmon = %.2f, want about 3", got)
	}
	if got := ratio(int32(entityWitch), "stick", "sugar"); got < 1.7 || got > 2.3 {
		t.Errorf("witch stick:sugar = %.2f, want about 2", got)
	}
}

// The weight of a leaf is max(0, floor(weight + quality·luck)), and an empty
// entry takes its share of the draw while dropping nothing.
func TestLootPickWeightQualityAndEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	a, b := int32(itemByName["stick"]), int32(itemByName["sugar"])
	pool := []lootEntry{
		{Type: "item", ID: a, W: 1},
		{Type: "item", ID: b, W: 1, Q: 2},
		{Type: "empty", W: 2},
	}
	share := func(luck float64) (fa, fb, fe float64) {
		ctx := lootCtx{rng: r.Intn, randf: r.Float64, luck: luck}
		const n = 20000
		var ca, cb, ce int
		for i := 0; i < n; i++ {
			switch e := ctx.pick(pool); {
			case e == nil:
				t.Fatal("nothing picked")
			case e.Type == "empty":
				ce++
			case e.ID == a:
				ca++
			default:
				cb++
			}
		}
		return float64(ca) / n, float64(cb) / n, float64(ce) / n
	}
	near := func(what string, got, want float64) {
		if got < want-0.02 || got > want+0.02 {
			t.Errorf("%s = %.3f, want %.3f", what, got, want)
		}
	}
	// luck 0: weights 1:1:2
	fa, fb, fe := share(0)
	near("luck 0 item a", fa, 0.25)
	near("luck 0 item b", fb, 0.25)
	near("luck 0 empty", fe, 0.5)
	// luck 1.5: b weighs floor(1+3)=4 → 1:4:2
	fa, fb, fe = share(1.5)
	near("luck 1.5 item a", fa, 1.0/7)
	near("luck 1.5 item b", fb, 4.0/7)
	near("luck 1.5 empty", fe, 2.0/7)
	// luck -1: b weighs max(0, -1) = 0 and is never drawn
	_, fb, _ = share(-1)
	if fb != 0 {
		t.Errorf("a zero-weight entry was drawn %.3f of the time", fb)
	}
}
