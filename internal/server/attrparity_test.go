package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// /attribute block_break_speed doubles how fast a dig advances: the crack
// stages another player sees move twice as fast.
func TestBlockBreakSpeedAttributeSpeedsDigging(t *testing.T) {
	progressAfter := func(set bool) float64 {
		h, pl, players := cmdHub()
		h.world.ForceLoad(0, 0, 1)
		pl.onGround = true
		h.world.SetBlock(1, 180, 0, worldgen.BlockBase("stone"))
		pl.inv.slots[pl.p.held] = invStack{item: itemByName["wooden_pickaxe"], count: 1}
		if set {
			h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: attr.BlockBreakSpeed, op: "base set", value: 2})
		}
		h.startDig(players, evDigStart{eid: pl.p.eid, x: 1, y: 180, z: 0})
		for i := 0; i < 5; i++ {
			h.tickDigCracks(players)
		}
		return h.digs[pl.p.eid].progress
	}
	base, fast := progressAfter(false), progressAfter(true)
	if base <= 0 || fast < base*1.99 || fast > base*2.01 {
		t.Fatalf("five ticks of digging: %v at block_break_speed 1, %v at 2; want double", base, fast)
	}
}

// flierRun spawns a parrot over a stone floor, optionally sets its
// FLYING_SPEED by /attribute, and returns how far it flew in 200 updates.
func flierRun(t *testing.T, set func(h *hub, players map[int32]*tracked)) (dist float64, m *mob) {
	t.Helper()
	h, _, players := cmdHub()
	h.world.ForceLoad(0, 0, 2)
	for x := -16; x < 32; x++ {
		for z := -16; z < 32; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	m = h.spawnSpecies(players, entityParrot, 0, 8.5, 182, 8.5)
	if set != nil {
		set(h, players)
	}
	for i := 0; i < 200; i++ {
		ox, oz := m.x, m.z
		h.updateMobs(players)
		dist += math.Hypot(m.x-ox, m.z-oz)
	}
	return dist, m
}

// /attribute flying_speed changes how fast a flier crosses the air, and a
// reset puts it back exactly where the species started.
func TestFlyingSpeedAttributeMovesFliers(t *testing.T) {
	base, _ := flierRun(t, nil)
	fast, _ := flierRun(t, func(h *hub, players map[int32]*tracked) {
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=parrot]", id: attr.FlyingSpeed, op: "base set", value: 0.8})
	})
	t.Logf("default %v, doubled %v", base, fast)
	if base <= 0 || fast < base*1.5 {
		t.Fatalf("a parrot flew %v at its own flying speed and %v at double it", base, fast)
	}
	reset, m := flierRun(t, func(h *hub, players map[int32]*tracked) {
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=parrot]", id: attr.FlyingSpeed, op: "base set", value: 0.8})
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=parrot]", id: attr.FlyingSpeed, op: "base reset"})
	})
	if reset != base {
		t.Fatalf("after a reset the parrot flew %v, not the %v it flies by default", reset, base)
	}
	if got := m.mobAttrs().Value(attr.FlyingSpeed); got != 0.4 {
		t.Fatalf("a parrot's flying speed resets to %v, want its species' 0.4", got)
	}
}
