package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Level.isRainingAt reads the height: a player standing under a glass roof
// in the rain is not rained on (glass blocks motion, if not light), so a
// Riptide trident will not launch them — where the old column check, which
// looked for opaque blocks only, let it.
func TestRiptideNeedsRainOnThePlayer(t *testing.T) {
	h, pl, players := tridentSetup()
	pl.inv.slots[0] = invStack{item: itemTrident, count: 1, ench: enchList{{id: enchRiptide, lvl: 2}}}
	h.raining = true
	fx, fy, fz := floorInt(pl.x), floorInt(pl.y), floorInt(pl.z)
	glass := worldgen.BlockBase("glass")
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(fx+dx, fy+4, fz+dz, glass)
		}
	}
	h.startTridentCharge(pl)
	h.tick.Add(tridentMinCharge)
	h.finishTridentThrow(players, pl)
	if pl.spinUntil != 0 {
		t.Fatal("riptide launched a player sheltered under glass")
	}
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(fx+dx, fy+4, fz+dz, worldgen.Air)
		}
	}
	if !h.rainAt(dimOverworld, fx, fy, fz) {
		t.Skip("the test column's biome does not rain")
	}
	h.startTridentCharge(pl)
	h.tick.Add(tridentMinCharge)
	h.finishTridentThrow(players, pl)
	if pl.spinUntil <= h.tick.Load() {
		t.Fatal("riptide under open rain did not launch")
	}
}
