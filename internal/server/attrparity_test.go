package server

import (
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
