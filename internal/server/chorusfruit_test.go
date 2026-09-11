package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The consumables table: a spider eye poisons, a pufferfish poisons, starves
// and sickens, honey lifts poison, and a chorus fruit moves the eater to
// solid ground within eight blocks.
func TestConsumeEffects(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	eat := func(name string) {
		pl.food = 1
		pl.inv.slots[0] = invStack{item: int32(itemByName[name]), count: 1}
		h.eat(players, pl, 0)
		if pl.inv.slots[0].count != 0 {
			t.Fatalf("%s not eaten", name)
		}
	}
	eat("spider_eye")
	if pl.hasEffect(effPoison) == 0 {
		t.Error("a spider eye should poison")
	}
	eat("honey_bottle")
	if pl.hasEffect(effPoison) != 0 {
		t.Error("honey should lift the poison")
	}
	eat("pufferfish")
	if pl.hasEffect(effPoison) == 0 || pl.hasEffect(effHunger) == 0 || pl.hasEffect(effNausea) == 0 {
		t.Error("a pufferfish should poison, starve and sicken")
	}
	// Chorus fruit: from mid-air over land, the eater lands on ground nearby.
	x, z := h.findLand(60, 60)
	pl.x, pl.z = float64(x)+0.5, float64(z)+0.5
	pl.y = float64(h.world.SurfaceFeet(x, z)) + 3
	ox, oy, oz := pl.x, pl.y, pl.z
	eat("chorus_fruit")
	if pl.x == ox && pl.y == oy && pl.z == oz {
		t.Fatal("the chorus fruit did not move the eater")
	}
	if math.Abs(pl.x-ox) > 8 || math.Abs(pl.z-oz) > 8 || math.Abs(pl.y-oy) > 8 {
		t.Errorf("landed too far: %v,%v,%v from %v,%v,%v", pl.x, pl.y, pl.z, ox, oy, oz)
	}
	bx, by, bz := int(math.Floor(pl.x)), int(math.Floor(pl.y)), int(math.Floor(pl.z))
	if !worldgen.Collides(h.world.At(bx, by-1, bz)) || worldgen.Collides(h.world.At(bx, by, bz)) {
		t.Errorf("landed in %d over %d", h.world.At(bx, by, bz), h.world.At(bx, by-1, bz))
	}
	if !h.onCooldown(pl, itemChorusFruit) {
		t.Error("the fruit should be on cooldown after a blink")
	}
	// Chance-gated foods land within their odds over many trials.
	hits := 0
	for i := 0; i < 400; i++ {
		q := testTracked()
		q.food = 1
		q.inv.slots[0] = invStack{item: itemRottenFlesh, count: 1}
		h.eat(players, q, 0)
		if q.hasEffect(effHunger) != 0 {
			hits++
		}
	}
	if hits < 260 || hits > 380 {
		t.Errorf("rotten flesh starved %d of 400, want about 80%%", hits)
	}
}
