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
	enchLoyalty    = 19
	enchImpaling   = 15
	enchChanneling = 5
	enchRiptide    = 31
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
	h.dropShoulderParrots(players, t) // startAutoSpinAttack
	if isSurvival(t.gamemode) {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	power := 3.0 * float64(1+riptide) / 4.0 // vanilla riptide impulse magnitude
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: dx * power, VY: dy * power, VZ: dz * power})
	now := h.tick.Load()
	t.spinUntil, t.spinSpent = now+tridentSpinTicks, false
	h.playSoundDim(players, t.dim, riptideSound(riptide), sndPlayer, t.x, t.y, t.z, 1, 1)
}

// throwTrident looses the trident as a projectile carrying the whole stack (so
// it returns enchanted), consuming it from a survival hand.
func (h *hub) throwTrident(players map[int32]*tracked, t *tracked, st invStack) {
	vx, vy, vz := h.throwFromRotation(t, 0, tridentSpeed, throwUncertainty) // TridentItem.releaseUsing
	a := h.launchProjectileIn(players, entityTrident, t.dim, t.x, t.y+1.5, t.z, vx, vy, vz)
	a.shooter, a.dmg, a.noHitUntil = t.p.eid, tridentDamage, h.tick.Load()+arrowNoSelfHT
	a.playerShot = true
	a.loyalty = st.enchLvl(enchLoyalty)
	a.impaling = st.enchLvl(enchImpaling)
	a.channeling = st.enchLvl(enchChanneling) > 0
	if isSurvival(t.gamemode) {
		slot := t.p.heldSlot()
		st.dmg++           // one durability point of wear rides with the thrown stack
		a.pickupStack = st // retrieved / returned trident restores this exact stack
		t.inv.slots[slot] = invStack{}
		h.sendSlot(t, slot) // the trident leaves the hand
	} else {
		a.noPickup = true // creative tridents are throw-only (vanilla CREATIVE_ONLY)
	}
	h.playSoundDim(players, t.dim, "minecraft:item.trident.throw", sndPlayer, t.x, t.y, t.z, 1, 1)
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
		if isSurvival(owner.gamemode) && owner.inv != nil && a.pickupStack.item != 0 {
			if changed, left := owner.inv.addStack(a.pickupStack); left == 0 {
				for _, sl := range changed {
					h.sendSlot(owner, sl)
				}
			} else {
				return false // inventory full — keep circling until there's room
			}
		}
		h.playSoundDim(players, a.dim, "minecraft:item.trident.return", sndPlayer, a.x, a.y, a.z, 1, 1)
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
		h.toTracking(players, a.eid, a.dim, a.x, a.z, entMove(a.eid, a.x, a.y, a.z, arrowYaw(a), arrowPitch(a), false))
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

// sensitiveToImpaling is #minecraft:sensitive_to_impaling, which is
// #minecraft:aquatic: the sea's own. Impaling stopped being "anything wet"
// in 1.17 — standing in the rain no longer makes a zombie easier to spear.
var sensitiveToImpaling = entityTypeSet(
	"turtle", "axolotl", "guardian", "elder_guardian", "cod", "pufferfish",
	"salmon", "tropical_fish", "dolphin", "squid", "glow_squid", "tadpole",
	"nautilus", "zombie_nautilus",
)

// spinAttackDamage is what a riptiding player deals on contact — vanilla's
// startAutoSpinAttack(20, 8.0F, stack): a flat eight, whatever the Riptide
// level, which only decides how far you fly.
const spinAttackDamage = 8

// spinAttackReach is how close a mob has to be to be caught by the spin. The
// engine does not sweep the player's box along its travel the way vanilla
// does, so this stands in for that sweep: roughly the player's own width plus
// the mob's.
const spinAttackReach = 1.2

// riptideSpinAttacks is LivingEntity.checkAutoSpinAttack: while the spin is
// running, the first living thing the player passes through takes the hit and
// the spin ENDS — a riptide is one strike, not a drill. Vanilla also bounces
// the attacker back off what they hit.
func (h *hub) riptideSpinAttacks(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, t := range players {
		if t.dead || !isSurvival(t.gamemode) || now >= t.spinUntil || t.spinSpent {
			continue
		}
		for _, m := range h.mobs {
			if m.dim != t.dim || m.dying > 0 {
				continue
			}
			if dist3(m.x, m.y, m.z, t.x, t.y, t.z) > spinAttackReach {
				continue
			}
			h.hurtByPlayerOn(m, t) // the kill pays experience and drops as a player kill
			h.hurtMobOf(players, m, spinAttackDamage, dtPlayerAttack)
			// Vanilla ends the spin here (autoSpinAttackTicks = 0) and bounces
			// the attacker off what they hit. The engine cannot bounce a
			// client it does not simulate, and cutting the movement grace
			// short would rubber-band a player still travelling at spin
			// speed — so the STRIKE is spent and the grace window runs out on
			// its own.
			t.spinSpent = true
			break
		}
	}
}
