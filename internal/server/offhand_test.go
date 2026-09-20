package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A shield works in the offhand, which is where it normally lives: the
// raise takes the hand the client used, and what the shield stops wears
// the offhand stack, not whatever the main hand happens to hold.
func TestShieldRaisesAndWearsInTheOffhand(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(50)
	pl := testTracked()
	h.playersRef = map[int32]*tracked{pl.p.eid: pl}
	pl.offhand = invStack{item: itemShield, count: 1}
	pl.p.setHotbarSlot(0, itemByName["diamond_pickaxe"])
	pl.inv.slots[0] = invStack{item: itemByName["diamond_pickaxe"], count: 1}

	h.raiseShield(pl, handOffhand)
	if pl.blockingSince != 50 || pl.blockingSlot != offhandSlot {
		t.Fatalf("an offhand shield should raise: since=%d slot=%d", pl.blockingSince, pl.blockingSlot)
	}
	h.tick.Store(50 + shieldDelay)
	if !pl.isBlockingShield(h.tick.Load()) {
		t.Fatal("past the delay the offhand shield is blocking")
	}
	pl.yaw = 0 // facing south, toward the blow
	blocked := h.shieldBlocked(pl, 6, dtMobAttack, from(pl.x, pl.z+3))
	if blocked <= 0 {
		t.Fatalf("an offhand shield should catch the blow, blocked %v", blocked)
	}
	h.shieldBlockFX(h.playersRef, pl, blocked)
	if pl.offhand.dmg == 0 {
		t.Fatalf("the offhand shield should take the wear, dmg=%d", pl.offhand.dmg)
	}
	if pl.inv.slots[0].dmg != 0 {
		t.Fatalf("the main-hand pick must not wear from a block, dmg=%d", pl.inv.slots[0].dmg)
	}
	// An empty offhand raises nothing.
	bare := testTracked()
	h.raiseShield(bare, handOffhand)
	if bare.blockingSince != 0 {
		t.Fatal("an empty offhand raises no shield")
	}
}

// The use-item dispatch reads the hand: an offhand right-click uses the
// offhand's item, not whatever the main hand holds.
func TestUseItemDispatchesByHand(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.playersRef = map[int32]*tracked{pl.p.eid: pl}
	pl.offhand = invStack{item: itemShield, count: 1}
	pl.p.setOffhand(itemShield)
	pl.p.setHotbarSlot(0, itemBow)
	pl.inv.slots[0] = invStack{item: itemBow, count: 1}
	r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}

	r.Action(attachproto.UseItem{Hand: handOffhand})
	r.Action(attachproto.UseItem{}) // main hand: the bow
	var sawBlock, sawBow bool
	for drained := false; !drained; {
		select {
		case ev := <-h.events:
			switch e := ev.(type) {
			case evBlockStart:
				sawBlock = e.hand == handOffhand
			case evBowStart:
				sawBow = true
			}
		default:
			drained = true
		}
	}
	if !sawBlock {
		t.Error("an offhand right-click with a shield there should raise it")
	}
	if !sawBow {
		t.Error("a main-hand right-click should still draw the bow")
	}
}

// A beacon's effect is ambient — the flag the client draws with fainter
// particles — while an ordinary effect is vanilla's default instance.
func TestBeaconEffectsAreAmbient(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	drainEvents(pl)
	h.applyEffectFrom(players, pl, effSpeed, 0, 30, true)
	h.applyEffect(players, pl, effHaste, 0, 30)
	if e := pl.effects[effSpeed]; e == nil || !e.ambient {
		t.Fatalf("a beacon effect is ambient: %+v", e)
	}
	if e := pl.effects[effHaste]; e == nil || e.ambient {
		t.Fatalf("an ordinary effect is not ambient: %+v", e)
	}
	var ambient, plain bool
	for drained := false; !drained; {
		select {
		case pkt := <-pl.p.out:
			if ev, ok := pkt.ev.(attachproto.Effect); ok {
				switch ev.ID {
				case effSpeed:
					ambient = ev.Ambient
				case effHaste:
					plain = !ev.Ambient && !ev.NoIcon && !ev.NoParticles
				}
			}
		default:
			drained = true
		}
	}
	if !ambient {
		t.Error("the beacon effect frame should carry the ambient flag")
	}
	if !plain {
		t.Error("an ordinary effect frame should be vanilla's default instance (visible, with an icon)")
	}
}
