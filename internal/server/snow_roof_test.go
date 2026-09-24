package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Snow settles only on the top of a column's MOTION_BLOCKING heightmap, as
// ServerLevel.tickPrecipitation does: a floor under a roof 40 blocks up stays
// clear, however long it snows, and the roof collects it (bug #29).
func TestSnowNeverSettlesUnderAHighRoof(t *testing.T) {
	h := newHub(world.New(1))
	cold := func(b string) bool { return worldgen.PrecipitationAt(b, 200) == worldgen.PrecipSnow }
	cx, cz := biomeSpot(t, h.world, 500, cold)
	chx, chz := cx>>4, cz>>4
	h.world.ForceLoad(chx, chz, 1)
	h.raining = true
	h.rules.MaxSnowHeight = 1
	x0, z0 := chx*16, chz*16
	floorY, roofY := 150, 190
	for x := x0; x < x0+16; x++ {
		for z := z0; z < z0+16; z++ {
			for y := floorY; y <= roofY+2; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			h.world.SetBlock(x, floorY, z, worldgen.Stone) // the floor
			if x < x0+8 {
				h.world.SetBlock(x, roofY, z, worldgen.Stone) // a roof over half the chunk
			}
		}
	}
	players := map[int32]*tracked{}
	for i := 0; i < 20000; i++ {
		h.precipTick(players, dimOverworld, chx, chz)
	}
	underRoof, onRoof, open := 0, 0, 0
	for x := x0; x < x0+16; x++ {
		for z := z0; z < z0+16; z++ {
			if h.world.At(x, floorY+1, z) == snowLayer1 {
				if x < x0+8 {
					underRoof++
				} else {
					open++
				}
			}
			if x < x0+8 && h.world.At(x, roofY+1, z) == snowLayer1 {
				onRoof++
			}
		}
	}
	if underRoof != 0 {
		t.Errorf("%d floor cells under the roof gathered snow", underRoof)
	}
	if onRoof == 0 || open == 0 {
		t.Errorf("snow on the roof %d, on the open floor %d: want both", onRoof, open)
	}
}
