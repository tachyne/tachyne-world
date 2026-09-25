package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The fast-break check allows for what the hub knows: a player on Haste X
// (from /effect) or with a raised BLOCK_BREAK_SPEED breaks stone by hand far
// faster than Haste II allows, and the break stands; the same quick finish
// without them is still reverted.
func TestDigAuthorityFollowsHasteAndAttributes(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	s.modes.set(p.key(), gmSurvival)
	var y int
	var digger *tracked
	onHub(t, h, func() {
		digger = h.playersRef[p.eid]
		digger.gamemode = gmSurvival
		digger.onGround = true
		y = int(p.y) + 2
		h.world.SetBlock(2, y, 0, worldgen.Stone)
		h.world.SetBlock(3, y, 0, worldgen.Stone)
	})
	// 30 × 1.5 / speed × 0.5: 16 ticks by hand at Haste II, 8 at Haste X.
	dig := func(x int, took uint64) bool {
		s.handleDig(p, digBody(digStartBreak, x, y, 0))
		p.digStartAt -= took
		s.handleDig(p, digBody(digFinishBreak, x, y, 0))
		var gone bool
		onHub(t, h, func() { gone = h.world.Block(x, y, 0) != worldgen.Stone })
		return gone
	}
	if dig(2, 10) {
		t.Fatal("a 10-tick bare-handed stone break stood without Haste")
	}
	onHub(t, h, func() { h.applyEffect(h.playersRef, digger, effHaste, 9, 60) })
	onHub(t, h, func() {}) // a tick for the mirror
	if !dig(2, 10) {
		t.Error("a 10-tick stone break on Haste X was reverted")
	}
	onHub(t, h, func() {
		h.removeEffect(digger, effHaste)
		digger.playerAttrs().SetBase(attr.BlockBreakSpeed, 3)
	})
	onHub(t, h, func() {})
	if !dig(3, 10) {
		t.Error("a 10-tick stone break with block_break_speed 3 was reverted")
	}
}
