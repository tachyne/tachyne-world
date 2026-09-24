package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestCakeSliceSaturation: CakeBlock.eat is FoodData.eat(2, 0.1F), and
// saturationByModifier makes that 2 × 0.1 × 2 = 0.4 saturation a slice.
func TestCakeSliceSaturation(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	x, y, z := 3, 180, 3
	h.world.SetBlock(x, y, z, cakeBase)
	pl.food, pl.saturation = 10, 0
	h.eatCake(players, pl, blockPos{x, y, z})
	if pl.food != 12 || math.Abs(float64(pl.saturation)-0.4) > 1e-6 {
		t.Fatalf("a slice gives 2 food and 0.4 saturation: food %d saturation %.3f", pl.food, pl.saturation)
	}
}
