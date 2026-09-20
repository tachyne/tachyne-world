package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
)

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

// glidingPlayer is an elytra player already in the air with the flag set,
// dropped into a hub so the glide tick can find them.
func glidingPlayer(h *hub) (*tracked, map[int32]*tracked) {
	tr := elytraPlayer()
	tr.onGround, tr.fallFlying = false, true
	return tr, map[int32]*tracked{tr.p.eid: tr}
}

// The glider component charges the wing a durability point per second of
// flight (updateFallFlying spends one every twentieth tick), and only while
// the player is actually gliding.
func TestGlidingWearsTheElytra(t *testing.T) {
	h := newHub(world.New(1))
	tr, players := glidingPlayer(h)
	for i := 0; i < 59; i++ {
		h.tickGliding(players)
	}
	if tr.armor[chestArmorSlot].dmg != 2 {
		t.Errorf("59 ticks of gliding cost %d points, want 2", tr.armor[chestArmorSlot].dmg)
	}
	h.tickGliding(players) // the sixtieth
	if tr.armor[chestArmorSlot].dmg != 3 {
		t.Errorf("three seconds of gliding cost %d points, want 3", tr.armor[chestArmorSlot].dmg)
	}
	// Landing stops the clock AND resets it, so a second short hop does not
	// inherit the first one's ticks and charge early.
	tr.onGround = true
	h.tickGliding(players)
	tr.onGround, tr.fallFlying = false, true
	for i := 0; i < 19; i++ {
		h.tickGliding(players)
	}
	if tr.armor[chestArmorSlot].dmg != 3 {
		t.Errorf("a fresh glide charged early: %d points", tr.armor[chestArmorSlot].dmg)
	}
}

// Creative flies for free — hasInfiniteMaterials spares the wing.
func TestCreativeGlidingCostsNothing(t *testing.T) {
	h := newHub(world.New(1))
	tr, players := glidingPlayer(h)
	tr.gamemode = gmCreative
	for i := 0; i < 40; i++ {
		h.tickGliding(players)
	}
	if tr.armor[chestArmorSlot].dmg != 0 {
		t.Errorf("creative gliding wore the elytra by %d", tr.armor[chestArmorSlot].dmg)
	}
}

// The last durability point is a floor, not a break: canGlideUsing refuses a
// wing whose next damage would destroy it, so the flight ends, the elytra
// stays in the slot unusable, and the client is told the flag is off.
func TestASpentElytraEndsTheGlide(t *testing.T) {
	h := newHub(world.New(1))
	tr, players := glidingPlayer(h)
	max := itemMaxDurability[itemElytra]
	tr.armor[chestArmorSlot].dmg = max - 2 // one point left to spend
	for i := 0; i < 20; i++ {
		h.tickGliding(players)
	}
	if tr.armor[chestArmorSlot].dmg != max-1 {
		t.Fatalf("the last point should have been spent: dmg=%d want %d", tr.armor[chestArmorSlot].dmg, max-1)
	}
	if !tr.fallFlying {
		t.Fatal("the glide should survive the tick that spent the point")
	}
	for {
		select {
		case <-tr.p.out:
			continue
		default:
		}
		break
	}
	h.tickGliding(players)
	if tr.fallFlying {
		t.Error("a spent elytra must end the flight")
	}
	if s := tr.armor[chestArmorSlot]; s.item != itemElytra || s.count != 1 || s.dmg != max-1 {
		t.Errorf("a spent elytra becomes unusable, not destroyed: %+v", s)
	}
	if tr.armor[chestArmorSlot].dmg >= max {
		t.Error("gliding must never break the wing outright")
	}
	// The client learns from the entity flags, which is what stops its own
	// flight — without the frame it would keep gliding on a dead wing.
	told := false
	for {
		select {
		case pkt := <-tr.p.out:
			// index 0 is the shared entity-flags byte; the value sits just
			// before the list terminator.
			if m, ok := pkt.ev.(attachproto.EntityMeta); ok && m.EID == tr.p.eid &&
				len(m.Meta) == 4 && m.Meta[0] == 0 && m.Meta[2]&entFlagFallFlying == 0 {
				told = true
			}
			continue
		default:
		}
		break
	}
	if !told {
		t.Error("the client was never told the glide had ended")
	}
}

// …and it cannot be started again on that wing.
func TestGlidingRefusedOnASpentElytra(t *testing.T) {
	tr := elytraPlayer()
	tr.onGround = false
	tr.armor[chestArmorSlot].dmg = itemMaxDurability[itemElytra] - 1
	if canStartFallFlying(tr) {
		t.Error("a spent elytra should refuse to take off")
	}
	tr.armor[chestArmorSlot].dmg--
	if !canStartFallFlying(tr) {
		t.Error("one point of durability is enough to fly")
	}
}
