package server

import "math"

// The crossbow: a two-phase ranged weapon. A first use HOLDS to charge; when
// the charge completes on release the shot latches "loaded" (consuming one
// arrow) and stays ready even across a hotbar switch; a later use fires the
// loaded bolt(s) instantly. quick_charge shortens the charge, multishot looses
// three bolts in a spread, piercing lets a bolt pass through several mobs.
// Unlike the bow, a crossbow shot is fixed-power (no draw-scaled damage) and
// never critical — the trade for holding a shot ready. Reuses the arrow entity.

const (
	xbowBaseCharge  = 25   // ticks to a full charge (1.25 s); quick_charge -5/level
	xbowSpeed       = 3.15 // bolt launch speed, blocks/tick (vanilla arrow crossbow)
	xbowMultiSpread = 10.0 // degrees of yaw between multishot bolts
)

var itemCrossbow = int32(itemByName["crossbow"])

// xbowDamage is a crossbow bolt's flat damage: ceil(speed × arrow baseDamage 2),
// vanilla's fixed-power shot (no draw scaling, no crit bonus).
var xbowDamage = int(math.Ceil(2 * xbowSpeed))

// evXbowUse is one crossbow right-click. The hub decides charge-vs-fire from the
// player's latched loaded state (session-side code can't read hub-owned fields).
type evXbowUse struct{ eid int32 }

func (evXbowUse) isHubEvent() {}

// xbowChargeTicks is the held crossbow's charge time, shortened by quick_charge.
func xbowChargeTicks(t *tracked) uint64 {
	d := xbowBaseCharge - 5*heldStack(t).enchLvl(enchQuickCharge)
	if d < 0 {
		d = 0
	}
	return uint64(d)
}

// useXbow routes a crossbow use: fire if it's loaded, otherwise begin charging.
func (h *hub) useXbow(players map[int32]*tracked, t *tracked) {
	if t.xbowLoaded {
		h.fireXbow(players, t)
		return
	}
	h.startXbowCharge(t)
}

// startXbowCharge begins loading the crossbow (needs ammo in survival). The
// client keeps the item "in use" until it releases, which finishes the charge.
func (h *hub) startXbowCharge(t *tracked) {
	if t.dead || heldStack(t).item != itemCrossbow || t.xbowLoaded {
		return
	}
	if t.gamemode == gmSurvival && !h.hasArrow(t) {
		return // nothing to load
	}
	t.xbowAt = h.tick.Load()
}

// finishXbowCharge completes (or, if released early, cancels) the charge on the
// release_use_item path. A full charge consumes one arrow and latches the shot.
func (h *hub) finishXbowCharge(players map[int32]*tracked, t *tracked) {
	if t.xbowAt == 0 {
		return
	}
	held := h.tick.Load() - t.xbowAt
	t.xbowAt = 0
	if t.dead || heldStack(t).item != itemCrossbow || t.xbowLoaded {
		return
	}
	if held < xbowChargeTicks(t) {
		return // released before the crossbow finished charging — no load
	}
	if t.gamemode == gmSurvival && !h.consumeArrow(t) {
		return
	}
	st := heldStack(t)
	t.xbowLoaded = true
	t.xbowMulti = st.enchLvl(enchMultishot) > 0
	t.xbowPierce = st.enchLvl(enchPiercing)
	h.playSound(players, "minecraft:item.crossbow.loading_end", sndPlayer, t.x, t.y, t.z, 1, 1)
}

// fireXbow looses the loaded bolt(s) (vanilla performShooting): one bolt, or a
// three-bolt spread with multishot. Piercing rides along on each bolt. The
// crossbow takes one point of durability wear per shot.
func (h *hub) fireXbow(players map[int32]*tracked, t *tracked) {
	if t.dead || !t.xbowLoaded || heldStack(t).item != itemCrossbow {
		return
	}
	multi, pierce := t.xbowMulti, t.xbowPierce
	t.xbowLoaded, t.xbowMulti, t.xbowPierce = false, false, 0
	if t.gamemode == gmSurvival {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	offsets := []float64{0}
	if multi {
		offsets = []float64{-xbowMultiSpread, 0, xbowMultiSpread}
	}
	for i, off := range offsets {
		dx, dy, dz := lookVector(t.yaw+float32(off), t.pitch)
		a := h.launchProjectileIn(players, entityArrow, t.dim, t.x, t.y+1.5, t.z,
			dx*xbowSpeed, dy*xbowSpeed, dz*xbowSpeed)
		a.shooter, a.dmg, a.noHitUntil = t.p.eid, xbowDamage, h.tick.Load()+arrowNoSelfHT
		a.playerShot = true
		if a.pierce = pierce; pierce > 0 {
			a.hitMobs = map[int32]bool{}
		}
		a.noPickup = multi && i != 1 // multishot side bolts are creative-only pickup
	}
	h.playSound(players, "minecraft:item.crossbow.shoot", sndPlayer, t.x, t.y, t.z, 1, 1)
}
