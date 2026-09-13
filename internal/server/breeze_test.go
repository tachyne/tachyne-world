package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestBreezeJumpsThenShoots: with a player ten blocks off, a breeze draws
// breath, long-jumps toward a spot behind them, lands, and in the window
// after the landing inhales and fires a wind charge.
func TestBreezeJumpsThenShoots(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -30; x <= 30; x++ {
		for z := -30; z <= 30; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 190; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = 10.5, 180, 0.5
	b := h.spawnMob(players, entityBreeze, 0.5, 180, 0.5)
	if !h.breezeStep(players, b) || b.brzState != brzInhaling {
		t.Fatalf("the breeze should draw breath for a jump: state %d", b.brzState)
	}
	for i := 0; i < 10 && b.brzState == brzInhaling; i++ {
		h.breezeStep(players, b)
	}
	if b.brzState != brzJumping || b.brzVY <= 0 {
		t.Fatalf("then jump: state %d vy %.2f", b.brzState, b.brzVY)
	}
	x0 := b.x
	for i := 0; i < 200 && b.brzState == brzJumping; i++ {
		h.breezeFlight(players, b)
	}
	if b.brzState != brzStanding || b.y != 180 || b.x <= x0+2 {
		t.Fatalf("it should land further along: state %d y %.2f x %.1f", b.brzState, b.y, b.x)
	}
	if b.brzShootWindow != breezeShootWindow {
		t.Fatal("a landing opens the shoot window")
	}
	for i := 0; i < 10 && b.brzState != brzShooting; i++ {
		h.breezeStep(players, b)
	}
	if b.brzState != brzShooting {
		t.Fatalf("it should be inhaling to shoot: state %d", b.brzState)
	}
	before := len(h.arrows)
	for i := 0; i < 12 && b.brzState == brzShooting; i++ {
		h.breezeStep(players, b)
	}
	if len(h.arrows) != before+1 {
		t.Fatalf("one wind charge in the air: %d", len(h.arrows)-before)
	}
	if b.brzState != brzStanding || b.brzShootCD != breezeShootCD {
		t.Fatalf("recovered with the cooldown set: state %d cd %d", b.brzState, b.brzShootCD)
	}
}
