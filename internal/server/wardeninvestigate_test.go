package server

import (
	"math"
	"testing"
)

// stepWarden advances the clock and the mobs n updates, returning the
// warden's top speed.
func stepWarden(h *hub, players map[int32]*tracked, m *mob, n int) float64 {
	top := 0.0
	for i := 0; i < n; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		top = math.Max(top, math.Hypot(m.vx, m.vz))
	}
	return top
}

// INVESTIGATE: a vibration it hears but is not yet angry about sends the
// warden over to look, at 0.7 of its pace and without stopping to sniff,
// and it gives up a hundred ticks later.
func TestWardenInvestigatesWhatItHears(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 11.5, 180, 3.5 // somebody about, far from the sound
	pl.p.eid = 900                    // clear of the mobs' ids
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityWarden, -10.5, 180, 0.5)
	h.wardenHeard(dimOverworld, -1.5, 180, 0.5, pl.p.eid)
	if !h.wardenInvestigating(m) {
		t.Fatal("a sound it is not yet angry about is a disturbance")
	}
	x0 := m.x
	top := stepWarden(h, players, m, 20)
	if m.x <= x0+1 {
		t.Fatalf("the warden should walk toward the sound, x %.2f -> %.2f", x0, m.x)
	}
	if want := m.moveSpeed() * wardenInvestigateMod; top > want*1.01 || top < want*0.6 {
		t.Errorf("it investigates at up to %.3f, want %.3f", top, want)
	}
	if m.wardenPose == poseSniffing {
		t.Error("investigating holds off the sniff")
	}
	stepWarden(h, players, m, wardenDisturbTicks/mobMoveInterval)
	if h.wardenInvestigating(m) {
		t.Error("the disturbance is forgotten after a hundred ticks")
	}
}

// Warden.doPush: a player it touches raises its grudge by 35, at most once
// a second, and becomes the place it goes to look.
func TestWardenBumpedIntoGetsAngry(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 1.0, 180, 0.5
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityWarden, 0.5, 180, 0.5)
	m.rest = 1 << 20
	m.wardenSniffCD = 1 << 20
	stepWarden(h, players, m, 1)
	if got := m.wardenAnger[pl.p.eid]; got != wardenAngerHeard {
		t.Fatalf("a touch raises anger by 35, got %d", got)
	}
	if !h.wardenInvestigating(m) {
		t.Error("a touch is a disturbance")
	}
	stepWarden(h, players, m, wardenTouchCooldown/mobMoveInterval-1)
	if got := m.wardenAnger[pl.p.eid]; got > wardenAngerHeard {
		t.Errorf("no second touch inside the cooldown, anger %d", got)
	}
	pl.x = 5.5 // out of reach
	stepWarden(h, players, m, 40)
	creative := survPlayer(h)
	creative.p.eid = 99
	creative.gamemode = gmCreative
	creative.x, creative.y, creative.z = m.x, m.y, m.z
	players[99] = creative
	stepWarden(h, players, m, 2)
	if _, ok := m.wardenAnger[99]; ok {
		t.Error("a creative player bumping it is nothing to it")
	}
}
