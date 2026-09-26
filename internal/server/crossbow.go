package server

import "math"

// The crossbow: a two-phase ranged weapon. A first use HOLDS to charge; when
// the charge completes on release the crossbow is loaded (consuming one
// arrow) — the load is the stack's charged_projectiles, so it stays with that
// crossbow through a hotbar switch, a chest or a restart; a later use fires the
// loaded bolt(s) instantly. quick_charge shortens the charge, multishot looses
// three bolts in a spread, piercing lets a bolt pass through several mobs.
// Unlike the bow, a crossbow shot is fixed-power (no draw-scaled damage) and
// never critical — the trade for holding a shot ready. Reuses the arrow entity.

const (
	xbowBaseCharge  = 25   // ticks to a full charge (1.25 s); quick_charge -5/level
	xbowSpeed       = 3.15 // bolt launch speed, blocks/tick (vanilla arrow crossbow)
	xbowMultiSpread = 10.0 // multishot projectile_spread: the fan's outer angle, degrees
)

var itemCrossbow = int32(itemByName["crossbow"])

// xbowDamage is a crossbow bolt's flat damage: ceil(speed × arrow baseDamage 2),
// vanilla's fixed-power shot (no draw scaling, no crit bonus).
var xbowDamage = int(math.Ceil(2 * xbowSpeed))

// evXbowUse is one crossbow right-click. The hub decides charge-vs-fire from the
// crossbow's load (session-side code can't read hub-owned stacks).
type evXbowUse struct {
	eid int32
	off bool
}

func (evXbowUse) isHubEvent() {}

// xbowChargeTicks is the held crossbow's charge time, shortened by quick_charge.
func xbowChargeTicks(t *tracked) uint64 {
	d := xbowBaseCharge - 5*usedStack(t).enchLvl(enchQuickCharge)
	if d < 0 {
		d = 0
	}
	return uint64(d)
}

// useXbow routes a crossbow use (CrossbowItem.use): fire if it's loaded,
// otherwise begin charging.
func (h *hub) useXbow(players map[int32]*tracked, t *tracked) {
	if usedStack(t).load.n > 0 {
		h.fireXbow(players, t)
		return
	}
	h.startXbowCharge(t)
}

// startXbowCharge begins loading the crossbow (needs ammo in survival). The
// client keeps the item "in use" until it releases, which finishes the charge.
func (h *hub) startXbowCharge(t *tracked) {
	if st := usedStack(t); t.dead || st.item != itemCrossbow || st.load.n > 0 {
		return
	}
	if isSurvival(t.gamemode) && xbowAmmoSlot(t) < 0 {
		return // nothing to load
	}
	t.xbowAt = h.tick.Load()
}

// finishXbowCharge completes (or, if released early, cancels) the charge on the
// release_use_item path. A full charge consumes one arrow and loads it onto the
// crossbow (CrossbowItem.releaseUsing → tryLoadProjectiles).
func (h *hub) finishXbowCharge(players map[int32]*tracked, t *tracked) {
	if t.xbowAt == 0 {
		return
	}
	held := h.tick.Load() - t.xbowAt
	t.xbowAt = 0
	if st := usedStack(t); t.dead || st.item != itemCrossbow || st.load.n > 0 {
		return
	}
	if held < xbowChargeTicks(t) {
		return // released before the crossbow finished charging — no load
	}
	ammo := invStack{item: itemArrowAmmo, count: 1} // a creative load with nothing to hand
	if i := xbowAmmoSlot(t); i >= 0 {
		if isSurvival(t.gamemode) {
			ammo = h.takeAmmo(t, i)
		} else {
			ammo = *t.handOrSlot(i)
			ammo.count = 1
		}
	} else if isSurvival(t.gamemode) {
		return
	}
	slot := t.useSlot()
	st := t.handStack(slot)
	if st == nil || st.item != itemCrossbow {
		return
	}
	// draw: Multishot loads three — the arrow and two copies.
	n := 1
	if st.enchLvl(enchMultishot) > 0 {
		n = 3
	}
	st.load = loadOf(ammo, n)
	h.syncUsedHand(players, t, slot) // the loaded model, for the holder and everyone watching
	h.playSoundDim(players, t.dim, "minecraft:item.crossbow.loading_end", sndPlayer, t.x, t.y, t.z, 1, 1)
}

// xbowRocketSpeed is CrossbowItem's FIREWORK_POWER.
const xbowRocketSpeed = 1.6

// fireXbow looses the loaded bolt(s) (vanilla performShooting): the load is
// emptied first, then each loaded projectile flies — one bolt, or a three-bolt
// spread with multishot. Piercing is the crossbow's own, read as it fires.
// Each projectile costs its own durability wear and plays its own shot sound.
func (h *hub) fireXbow(players map[int32]*tracked, t *tracked) {
	slot := t.useSlot()
	st := t.handStack(slot)
	if t.dead || st == nil || st.item != itemCrossbow || st.load.n <= 0 {
		return
	}
	load, pierce := st.load, st.enchLvl(enchPiercing)
	st.load = xbowLoad{}
	ammo := load.ammo()
	h.advance(players, t, "shot_crossbow", advMatch{item: itemCrossbow})
	n := int(load.n)
	rocket := ammo.item == itemFireworkRocket
	for i, angle := range xbowShotAngles(n) {
		// CrossbowItem.getDurabilityUse: each projectile costs its own wear,
		// three for a rocket and one for an arrow.
		if isSurvival(t.gamemode) {
			wear := 1
			if rocket {
				wear = 3
			}
			h.applyToolWear(t, t.useSlot(), wear)
		}
		// shootProjectile: the look turned about the player's own up axis,
		// then Projectile.shoot at the crossbow's power with uncertainty 1.
		dx, dy, dz := xbowShotVector(t.yaw, t.pitch, angle)
		if rocket { // FireworkRocketEntity shot at an angle, at FIREWORK_POWER 1.6
			if r := h.spawnRocket(players, t.dim, t.x, t.y+playerEyeHeightStand-0.15, t.z, 0, ammo); r != nil {
				r.angled, r.shooter = true, t.p.eid
				r.vx, r.vy, r.vz = h.shootVector(dx, dy, dz, xbowRocketSpeed, throwUncertainty)
			}
		} else {
			vx, vy, vz := h.shootVector(dx, dy, dz, xbowSpeed, throwUncertainty)
			a := h.launchProjectileIn(players, arrowEntityFor(ammo), t.dim, t.x, t.y+1.5, t.z, vx, vy, vz)
			loadArrow(a, ammo)
			a.weapon = itemCrossbow
			a.shooter, a.dmg, a.noHitUntil = t.p.eid, xbowDamage, h.tick.Load()+arrowNoSelfHT
			a.playerShot = true
			if a.pierce = pierce; pierce > 0 {
				a.hitMobs = map[int32]bool{}
			}
			a.noPickup = i != 0 // the first is the loaded arrow; multishot's copies are intangible
		}
		h.playSoundDim(players, t.dim, "minecraft:item.crossbow.shoot", sndPlayer, t.x, t.y, t.z, 1, h.xbowShotPitch(i))
	}
	h.syncUsedHand(players, t, slot) // unloaded (or broken)
}

// syncUsedHand resyncs the hand a use came from after its stack changed in a
// way others can see — a crossbow loading or firing changes its model.
func (h *hub) syncUsedHand(players map[int32]*tracked, t *tracked, slot int) {
	h.sendHandSlot(t, slot) // the offhand path broadcasts itself
	if slot != offhandSlot {
		h.broadcastEquipment(players, t)
	}
}

// xbowShotAngles is ProjectileWeaponItem.shoot's fan for n projectiles over
// multishot's ±10°: the first straight ahead, then alternating one step to
// either side (0, −10, +10 for three).
func xbowShotAngles(n int) []float64 {
	step := 0.0
	if n > 1 {
		step = 2 * xbowMultiSpread / float64(n-1)
	}
	offset := float64((n-1)%2) * step / 2
	out := make([]float64, n)
	dir := 1.0
	for i := range out {
		out[i] = offset + dir*float64((i+1)/2)*step
		dir = -dir
	}
	return out
}

// xbowShotVector is the view vector rotated angle degrees about the
// player's up vector (the view vector 90° further up the pitch), which is
// how CrossbowItem.shootProjectile fans a multishot volley.
func xbowShotVector(yaw, pitch float32, angle float64) (float64, float64, float64) {
	vx, vy, vz := lookVector(yaw, pitch)
	if angle == 0 {
		return vx, vy, vz
	}
	ux, uy, uz := lookVector(yaw, pitch-90)
	th := angle * math.Pi / 180
	c, s := math.Cos(th), math.Sin(th)
	cx, cy, cz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx // up × view
	dot := ux*vx + uy*vy + uz*vz
	return vx*c + cx*s + ux*dot*(1-c), vy*c + cy*s + uy*dot*(1-c), vz*c + cz*s + uz*dot*(1-c)
}

// xbowShotPitch is CrossbowItem.getShotPitch: the first shot at 1, the
// others a random pitch, higher for odd indices.
func (h *hub) xbowShotPitch(i int) float32 {
	if i == 0 {
		return 1
	}
	lo := float32(0.43)
	if i&1 == 1 {
		lo = 0.63
	}
	return 1/(h.rng.Float32()*0.5+1.8) + lo
}
