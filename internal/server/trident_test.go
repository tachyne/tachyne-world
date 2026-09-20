package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// tridentSetup arms a survival player with a plain trident, high in the air.
func tridentSetup() (*hub, *tracked, map[int32]*tracked) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	pl.yaw, pl.pitch = 0, 0
	pl.p.setHotbarSlot(0, itemTrident)
	pl.inv.slots[0] = invStack{item: itemTrident, count: 1}
	h.tick.Store(100)
	return h, pl, map[int32]*tracked{1: pl}
}

func TestTridentThrowConsumesAndCarriesStack(t *testing.T) {
	h, pl, players := tridentSetup()
	// A loyalty II trident, so the thrown projectile should carry the enchant.
	pl.inv.slots[0] = invStack{item: itemTrident, count: 1,
		ench: enchList{{id: enchLoyalty, lvl: 2}}}

	h.startTridentCharge(pl)
	if pl.tridentAt == 0 {
		t.Fatal("holding a trident should begin the charge")
	}
	h.tick.Add(tridentMinCharge)
	h.finishTridentThrow(players, pl)

	if len(h.arrows) != 1 {
		t.Fatalf("a charged release should throw one trident, got %d", len(h.arrows))
	}
	if pl.inv.slots[0].item != 0 {
		t.Fatal("throwing the trident should empty the hand (survival)")
	}
	for _, a := range h.arrows {
		if a.loyalty != 2 || a.pickupStack.item != itemTrident {
			t.Fatalf("thrown trident must carry loyalty + its return stack: %+v", a)
		}
		if a.pickupStack.dmg != 1 || a.pickupStack.enchLvl(enchLoyalty) != 2 {
			t.Fatalf("return stack should keep the enchant and take one wear: %+v", a.pickupStack)
		}
	}
}

func TestTridentEarlyReleaseKeepsIt(t *testing.T) {
	h, pl, players := tridentSetup()
	h.startTridentCharge(pl)
	h.tick.Add(3) // released well before the minimum charge
	h.finishTridentThrow(players, pl)
	if len(h.arrows) != 0 || pl.inv.slots[0].item != itemTrident {
		t.Fatal("an under-charged trident must not be thrown")
	}
}

func TestTridentLoyaltyReturns(t *testing.T) {
	h, pl, players := tridentSetup()
	pl.inv.slots[0] = invStack{} // hand already empty (trident is mid-flight)
	stack := invStack{item: itemTrident, count: 1, dmg: 1,
		ench: enchList{{id: enchLoyalty, lvl: 3}}}

	// A returning trident well away from the owner should steer closer, not catch.
	a := h.launchProjectileIn(players, entityTrident, 0, 0.5, 80, 20.5, 0, 0, 0)
	a.shooter, a.playerShot, a.loyalty, a.returning, a.pickupStack = pl.p.eid, true, 3, true, stack
	before := a.z
	if caught := h.updateReturningTrident(players, a); caught {
		t.Fatal("a distant loyal trident should still be flying, not caught")
	}
	if a.z >= before {
		t.Fatalf("a returning trident should move toward its owner: %.2f -> %.2f", before, a.z)
	}

	// Placed on top of the owner, it should be caught and restored with enchants.
	a.x, a.y, a.z = pl.x, pl.y, pl.z
	if caught := h.updateReturningTrident(players, a); !caught {
		t.Fatal("a loyal trident reaching its owner should be caught")
	}
	got := pl.inv.slots[0]
	if got.item != itemTrident || got.enchLvl(enchLoyalty) != 3 {
		t.Fatalf("caught trident should return enchanted to the inventory: %+v", got)
	}
}

func TestTridentRiptideLaunchesInRain(t *testing.T) {
	h, pl, players := tridentSetup()
	pl.inv.slots[0] = invStack{item: itemTrident, count: 1,
		ench: enchList{{id: enchRiptide, lvl: 2}}}

	// Dry land, no rain: riptide can't charge — nothing happens.
	h.startTridentCharge(pl)
	h.tick.Add(tridentMinCharge)
	h.finishTridentThrow(players, pl)
	if pl.spinUntil != 0 || len(h.arrows) != 0 || pl.inv.slots[0].item != itemTrident {
		t.Fatal("riptide on dry land in clear weather must fizzle")
	}

	// Now it's raining: the release launches the player (spin grace), trident kept.
	h.raining = true
	h.startTridentCharge(pl)
	h.tick.Add(tridentMinCharge)
	h.finishTridentThrow(players, pl)
	if pl.spinUntil <= h.tick.Load() {
		t.Fatalf("riptide in rain should open a spin window: spinUntil=%d tick=%d", pl.spinUntil, h.tick.Load())
	}
	if len(h.arrows) != 0 || pl.inv.slots[0].item != itemTrident {
		t.Fatal("riptide launches the player, it does not throw the trident")
	}
}

func TestTridentImpalingHitsTheAquatic(t *testing.T) {
	h, pl, players := tridentSetup()
	h.raining = true

	// Impaling is #sensitive_to_impaling (= #aquatic) since 1.17: a zombie
	// standing in the rain takes the trident's plain damage.
	z := &mob{eid: 9, etype: entityZombie, hostile: true, health: 100, x: 0.5, y: 80, z: 4.5}
	h.mobs[9] = z
	a := h.launchProjectileIn(players, entityTrident, 0, 0.5, 81, 0.5, 0, 0, tridentSpeed)
	a.shooter, a.dmg, a.playerShot, a.impaling = pl.p.eid, tridentDamage, true, 2
	h.arrowHitsMob(players, a, z.x, 80.5, z.z)
	if want := 100 - 8; z.health != want {
		t.Fatalf("a rained-on zombie takes the plain 8: health=%d want %d", z.health, want)
	}

	// A dolphin, wet or dry, takes the bonus: 8 + ceil(2.5 × 2) = 13.
	delete(h.mobs, z.eid) // one target at a time: the hit picks whatever is in reach
	d := &mob{eid: 10, etype: entityDolphin, health: 100, x: 0.5, y: 80, z: 4.5}
	h.mobs[10] = d
	a2 := h.launchProjectileIn(players, entityTrident, 0, 0.5, 81, 0.5, 0, 0, tridentSpeed)
	a2.shooter, a2.dmg, a2.playerShot, a2.impaling = pl.p.eid, tridentDamage, true, 2
	h.arrowHitsMob(players, a2, d.x, 80.5, d.z)
	if want := 100 - 13; d.health != want {
		t.Fatalf("impaling II on a dolphin should deal 13: health=%d want %d", d.health, want)
	}
}
