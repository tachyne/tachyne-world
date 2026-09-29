package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// fencePen rings (cx, cz) with fences one block out, height high.
func fencePen(h *hub, cx, cz, high int) {
	oakFence := worldgen.BlockBase("oak_fence") + 31
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			fx, fz := cx+dx, cz+dz
			y := h.world.SurfaceFeet(fx, fz)
			for i := 0; i < high; i++ {
				h.world.SetBlock(fx, y+i, fz, oakFence)
			}
		}
	}
}

// A camel's STEP_HEIGHT of 1.5 walks it up onto a fence top and over: a
// one-high pen does not hold it, and while it is over the fence it stands
// 1.5 above the fence's cell. A two-high pen does, and a horse (1.0) stays
// in the one-high pen.
func TestCamelStepsOverFences(t *testing.T) {
	run := func(etype, high int) (escaped, perched bool) {
		h := newTestHub(world.New(1))
		players := map[int32]*tracked{}
		cx, cz := h.findLand(0, 0)
		g := h.world.GroundY(cx, cz)
		fencePen(h, cx, cz, high)
		m := h.spawnMob(players, etype, float64(cx)+0.5, float64(g), float64(cz)+0.5)
		m.behavior = wanderBehavior{}
		h.tick.Store(10000)
		for i := 0; i < 3000; i++ {
			m.poseTick = int64(h.tick.Load()) - camelStandUpTicks - 1 // standing, and too lately to sit
			h.updateMobs(players)
			fx, fz := int(math.Floor(m.x)), int(math.Floor(m.z))
			if worldgen.IsTallCollision(h.world.Block(fx, h.world.SurfaceFeet(fx, fz)-1, fz)) &&
				m.y-math.Floor(m.y) == 0.5 {
				perched = true
			}
			if abs(fx-cx) > 1 || abs(fz-cz) > 1 {
				return true, perched
			}
		}
		return false, perched
	}
	if escaped, perched := run(entityCamel, 1); !escaped || !perched {
		t.Fatalf("a camel crosses a one-high fence by standing on it: escaped %v, perched %v", escaped, perched)
	}
	if escaped, _ := run(entityCamel, 2); escaped {
		t.Fatal("a two-high fence holds a camel")
	}
	if escaped, _ := run(entityHorse, 1); escaped {
		t.Fatal("a horse does not step over a fence")
	}
}

// Camel navigation plans over a fence line (setCanWalkOverFences); the
// same search for any other walker goes round or nowhere.
func TestCamelPlansOverFences(t *testing.T) {
	h := newTestHub(world.New(1))
	cx, cz := h.findLand(0, 0)
	h.world.ForceLoad(cx, cz, 2)
	g := h.world.GroundY(cx, cz)
	oakFence := worldgen.BlockBase("oak_fence") + 31
	for x := cx - 8; x <= cx+8; x++ { // a flat, open floor
		for z := cz - 9; z <= cz+9; z++ {
			for y := g - 10; y < g; y++ { // solid up from below the sea, so the floor is found from any start
				h.world.SetBlock(x, y, z, worldgen.Stone)
			}
			for y := g; y <= g+30; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	for z := cz - 9; z <= cz+9; z++ {
		h.world.SetBlock(cx, g, z, oakFence) // a fence line across it
	}
	camel := &mob{etype: entityCamel}
	plain, reached := findPathLimits(h.world, defaultMalus, cx-3, cz, cx+3, cz, 8, 350, 1)
	if reached {
		t.Fatalf("a plain walker crosses the fence: %v", plain)
	}
	path, reached := findPathLimits(fencePather{pather: h.world, bodyH: camel.box().h}, defaultMalus, cx-3, cz, cx+3, cz, 8, 350, 1)
	if !reached {
		t.Fatalf("a camel plans over the fence: %v", path)
	}
}
