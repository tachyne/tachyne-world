package server

import "testing"

// elytraPlayer is a fully-built survival player wearing an elytra —
// initSurvival and all, because a bare &tracked{} dropped into a running
// hub's player map crashes its tick.
func elytraPlayer() *tracked {
	t := testTracked()
	t.y = 80
	t.armor[1] = invStack{item: int32(itemElytra), count: 1}
	return t
}

// Gliding is the client's own START_FALL_FLYING, not "airborne in an elytra".
// Falling while wearing one is falling: a rocket used then should go off in
// your hand rather than carry you.
func TestGlidingNeedsTheClientToStart(t *testing.T) {
	tr := elytraPlayer()
	tr.onGround = false
	if tr.gliding() {
		t.Error("merely falling in an elytra counted as gliding")
	}
	tr.fallFlying = true
	if !tr.gliding() {
		t.Error("a player who started fall-flying should be gliding")
	}
	// Taking the elytra off ends it even if the flag lingers a tick.
	tr.armor[1] = invStack{}
	if tr.gliding() {
		t.Error("gliding without an elytra")
	}
}

// The flag reaches other clients: it is what draws the glide pose, so without
// it a player on an elytra reads as falling with their arms down.
func TestFallFlyingIsInTheEntityFlags(t *testing.T) {
	tr := elytraPlayer()
	if playerEntityFlags(tr)&entFlagFallFlying != 0 {
		t.Error("a grounded player carries the fall-flying bit")
	}
	tr.fallFlying = true
	if playerEntityFlags(tr)&entFlagFallFlying == 0 {
		t.Error("a gliding player does not carry the fall-flying bit")
	}
	// …alongside the other flags rather than instead of them.
	tr.fireSecs = 5
	f := playerEntityFlags(tr)
	if f&entFlagFallFlying == 0 || f&entFlagOnFire == 0 {
		t.Errorf("flags %#x lost one of burning-while-gliding", f)
	}
}

// The server decides whether the start takes, rather than trusting the
// client: a player standing on the ground cannot glide off it, and neither
// can one with no elytra.
func TestFallFlyingStartIsRefusedOnTheGround(t *testing.T) {
	for _, tc := range []struct {
		name     string
		onGround bool
		elytra   bool
		want     bool
	}{
		{"airborne in an elytra", false, true, true},
		{"standing on the ground", true, true, false},
		{"airborne without one", false, false, false},
	} {
		tr := elytraPlayer()
		tr.onGround = tc.onGround
		if !tc.elytra {
			tr.armor[1] = invStack{}
		}
		if got := canStartFallFlying(tr); got != tc.want {
			t.Errorf("%s: the start %v, want %v", tc.name, got, tc.want)
		}
	}
}
