package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The trident: a charge-held throwable (like the bow). Holding right-click winds
// it up; releasing after the minimum charge either THROWS it as a projectile or,
// with riptide while in water or rain, LAUNCHES the player forward. A thrown
// trident reuses the arrow entity: it sticks and is retrievable, or with loyalty
// flies home to the thrower and is auto-collected — either way it comes back
// with its enchantments intact (the whole stack rides the projectile). impaling
// adds damage to targets in water or rain. channeling (lightning) is deferred:
// no lightning entity exists yet.

const (
	tridentMinCharge = 10   // ticks held before a throw / riptide resolves (vanilla)
	tridentSpeed     = 2.5  // thrown-trident launch speed, blocks/tick (vanilla power)
	tridentDamage    = 8    // base impact damage (vanilla ThrownTrident)
	tridentSpinTicks = 20   // riptide auto-spin-attack duration = movement grace window
	loyaltyPull      = 0.05 // per loyalty level: acceleration toward the owner (vanilla)
	riptideReach     = 1.6  // catch radius for a returning trident
)

// enchRiptide / enchLoyalty / enchImpaling ids (our declared registry order).
const (
	enchLoyalty  = 19
	enchImpaling = 15
	enchRiptide  = 31
)

// evTridentUse begins a trident charge-hold; the hub owns the draw state.
type evTridentUse struct{ eid int32 }

func (evTridentUse) isHubEvent() {}

// startTridentCharge begins winding up a trident (no ammo — the trident itself
// is the projectile). Resolves on release_use_item via finishTridentThrow.
func (h *hub) startTridentCharge(t *tracked) {
	if t.dead || heldStack(t).item != itemTrident {
		return
	}
	t.tridentAt = h.tick.Load()
}

// finishTridentThrow resolves a released trident charge: a riptide launch in
// water/rain, otherwise a thrown trident. A charge shorter than the minimum
// does nothing (vanilla).
func (h *hub) finishTridentThrow(players map[int32]*tracked, t *tracked) {
	if t.tridentAt == 0 {
		return
	}
	held := h.tick.Load() - t.tridentAt
	t.tridentAt = 0
	if t.dead || heldStack(t).item != itemTrident || held < tridentMinCharge {
		return
	}
	st := heldStack(t)
	if riptide := st.enchLvl(enchRiptide); riptide > 0 {
		h.riptideLaunch(players, t, riptide)
		return
	}
	h.throwTrident(players, t, st)
}

// riptideLaunch flings the player along their look vector — only while in water
// or being rained on (vanilla isInWaterOrRain). The trident stays in hand; the
// movement-authority spin window (spinUntil) keeps the fast travel from being
// rubber-banded.
func (h *hub) riptideLaunch(players map[int32]*tracked, t *tracked, riptide int) {
	if !h.inWater(t.dim, t.x, t.y, t.z) && !h.raining {
		return // riptide needs water or rain to charge
	}
	if riptide > 3 {
		riptide = 3
	}
	if t.gamemode == gmSurvival {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	power := 3.0 * float64(1+riptide) / 4.0 // vanilla riptide impulse magnitude
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: dx * power, VY: dy * power, VZ: dz * power})
	now := h.tick.Load()
	t.spinUntil = now + tridentSpinTicks
	t.moveBudget = budgetCapTicks * spinPerTick // let the launch through the speed check
	h.playSound(players, riptideSound(riptide), sndPlayer, t.x, t.y, t.z, 1, 1)
}

// throwTrident looses the trident as a projectile carrying the whole stack (so
// it returns enchanted), consuming it from a survival hand.
func (h *hub) throwTrident(players map[int32]*tracked, t *tracked, st invStack) {
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	a := h.launchProjectileIn(players, entityTrident, t.dim, t.x, t.y+1.5, t.z,
		dx*tridentSpeed, dy*tridentSpeed, dz*tridentSpeed)
	a.shooter, a.dmg, a.noHitUntil = t.p.eid, tridentDamage, h.tick.Load()+arrowNoSelfHT
	a.playerShot = true
	a.loyalty = st.enchLvl(enchLoyalty)
	a.impaling = st.enchLvl(enchImpaling)
	if t.gamemode == gmSurvival {
		slot := t.p.heldSlot()
		st.dmg++           // one durability point of wear rides with the thrown stack
		a.pickupStack = st // retrieved / returned trident restores this exact stack
		t.inv.slots[slot] = invStack{}
		h.sendSlot(t, slot) // the trident leaves the hand
	} else {
		a.noPickup = true // creative tridents are throw-only (vanilla CREATIVE_ONLY)
	}
	h.playSound(players, "minecraft:item.trident.throw", sndPlayer, t.x, t.y, t.z, 1, 1)
}

// updateReturningTrident steers a loyal trident home and auto-collects it when
// it reaches the owner; despawns it if the owner is gone or in another world.
func (h *hub) updateReturningTrident(players map[int32]*tracked, a *arrowEntity) bool {
	owner := players[a.shooter]
	if owner == nil || owner.dead || owner.dim != a.dim {
		return true // nobody to return to — drop the entity
	}
	dx, dy, dz := owner.x-a.x, (owner.y+1)-a.y, owner.z-a.z
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist < riptideReach { // caught
		if owner.gamemode == gmSurvival && owner.inv != nil && a.pickupStack.item != 0 {
			if changed, left := owner.inv.addStack(a.pickupStack); left == 0 {
				for _, sl := range changed {
					h.sendSlot(owner, sl)
				}
			} else {
				return false // inventory full — keep circling until there's room
			}
		}
		h.playSound(players, "minecraft:item.trident.return", sndPlayer, a.x, a.y, a.z, 1, 1)
		return true
	}
	if dist > 1e-6 { // accelerate toward the owner (vanilla loyalty pull)
		pull := loyaltyPull * float64(a.loyalty)
		a.vx = a.vx*0.95 + dx/dist*pull
		a.vy = a.vy*0.95 + dy/dist*pull
		a.vz = a.vz*0.95 + dz/dist*pull
	}
	a.x, a.y, a.z = a.x+a.vx, a.y+a.vy, a.z+a.vz
	if a.x != a.sx || a.y != a.sy || a.z != a.sz {
		a.sx, a.sy, a.sz = a.x, a.y, a.z
		h.toNearbyEv(players, a.dim, a.x, a.z,
			entMove(a.eid, a.x, a.y, a.z, arrowYaw(a), arrowPitch(a), false))
	}
	return false
}

func riptideSound(level int) string {
	switch {
	case level >= 3:
		return "minecraft:item.trident.riptide_3"
	case level == 2:
		return "minecraft:item.trident.riptide_2"
	default:
		return "minecraft:item.trident.riptide_1"
	}
}
