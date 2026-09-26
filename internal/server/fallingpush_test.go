package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// fallPad clears a box of sky at y 180 over a stone floor (y 179) around
// (x,z), in loaded chunks.
func fallPad(h *hub, x, z int) {
	h.world.ForceLoad(x, z, 1)
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			h.world.SetBlock(x+dx, 179, z+dz, worldgen.Stone)
			for y := 180; y <= 192; y++ {
				h.world.SetBlock(x+dx, y, z+dz, worldgen.Air)
			}
		}
	}
}

func testFalling(h *hub, players map[int32]*tracked, x, y, z float64) *fallingBlock {
	fb := &fallingBlock{eid: h.allocEID(), dim: dimOverworld, x: x, y: y, z: z,
		state: worldgen.Sand, dropItem: true, hurtMax: fallDamageMaxDef}
	h.addFallingBlock(players, fb)
	return fb
}

// An upward bubble column lifts a falling block (onInsideBubbleColumn), a
// whirlpool pulls it down faster than it would fall.
func TestFallingBlockInBubbleColumn(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, z := 40, 40
	fallPad(h, x, z)
	for y := 180; y <= 185; y++ {
		h.world.SetBlock(x, y, z, worldgen.BubbleColumnUp)
		h.world.SetBlock(x+3, y, z, worldgen.BubbleColumnDrag)
	}
	up := testFalling(h, players, float64(x)+0.5, 181, float64(z)+0.5)
	for i := 0; i < 10; i++ {
		h.fallingBlockStep(players, h.world, up)
	}
	if up.y <= 181.5 {
		t.Fatalf("the column should carry the sand up, y=%.3f", up.y)
	}
	down := testFalling(h, players, float64(x+3)+0.5, 185, float64(z)+0.5)
	free := testFalling(h, players, float64(x-3)+0.5, 185, float64(z)+0.5)
	for i := 0; i < 5; i++ {
		h.fallingBlockStep(players, h.world, down)
		h.fallingBlockStep(players, h.world, free)
	}
	if down.y >= free.y {
		t.Fatalf("a whirlpool drags harder than gravity: %.3f vs %.3f", down.y, free.y)
	}
}

// A blast shoves a falling block like any other entity: sideways off its
// column, and it lands where the push carried it.
func TestFallingBlockPushedByExplosion(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, z := 60, 60
	fallPad(h, x, z)
	fb := testFalling(h, players, float64(x)+0.5, 183, float64(z)+0.5)
	h.explodeHurt(players, dimOverworld, float64(x)-2.5, 183, float64(z)+0.5, 2, dtExplosion, deathCause{})
	if fb.vx <= 0 || fb.vz != 0 {
		t.Fatalf("the blast west of it should push it east: v=%.3f,%.3f,%.3f", fb.vx, fb.vy, fb.vz)
	}
	for i := 0; i < 100 && h.fallingBlockStep(players, h.world, fb); i++ {
	}
	landed := -1
	for dx := 0; dx <= 12; dx++ {
		if h.world.At(x+dx, 180, z) == worldgen.Sand {
			landed = dx
		}
	}
	if landed < 1 {
		t.Fatalf("the sand should land east of its column, landed at +%d", landed)
	}
}
