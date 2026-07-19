package server

import (
	"math"
)

// Player ranged weapons: the bow (a draw-hold released for a charge-scaled
// arrow) and instant throwables (snowballs, eggs). Player arrows reuse the
// arrow entity but carry their shooter + charge damage, hit MOBS through the
// normal attack bookkeeping, and can be picked back up once stuck (vanilla).

const (
	bowFullDraw   = 20.0 // ticks to full power (1 s); min power 0.1 gates short pulls
	bowMaxSpeed   = 3.0  // blocks/tick at full draw (vanilla)
	throwSpeed    = 1.5  // snowball/egg launch speed
	arrowNoSelfHT = 5    // ticks a fresh arrow ignores its shooter
)

var (
	itemArrowAmmo = itemByName["arrow"]
	itemSnowball  = itemByName["snowball"]
	itemEgg       = itemByName["egg"]

	entitySnowball = entityID("snowball")
	entityEggProj  = entityID("egg")
)

type evBowStart struct{ eid int32 }
type evThrow struct {
	eid  int32
	item int32
}

func (evBowStart) isHubEvent() {}
func (evThrow) isHubEvent()    {}

// lookVector converts a player's yaw/pitch (degrees) to a unit direction.
func lookVector(yaw, pitch float32) (float64, float64, float64) {
	ry := float64(yaw) * math.Pi / 180
	rp := float64(pitch) * math.Pi / 180
	return -math.Sin(ry) * math.Cos(rp), -math.Sin(rp), math.Cos(ry) * math.Cos(rp)
}

// startDraw begins a bow hold (validated: a bow in hand, and ammo to fire).
func (h *hub) startDraw(t *tracked) {
	if t.dead || heldStack(t).item != itemBow {
		return
	}
	if t.gamemode == gmSurvival && !h.hasArrow(t) {
		return // nothing to nock
	}
	t.drawingAt = h.tick.Load()
}

// hasArrow reports whether the inventory holds any arrows.
func (h *hub) hasArrow(t *tracked) bool {
	for _, s := range t.inv.slots {
		if s.item == itemArrowAmmo && s.count > 0 {
			return true
		}
	}
	return false
}

// releaseDraw fires the arrow if the bow was held long enough, consuming ammo
// and durability. Called from the release_use_item path (shared with eating).
func (h *hub) releaseDraw(players map[int32]*tracked, t *tracked) {
	if t.drawingAt == 0 {
		return
	}
	charge := h.tick.Load() - t.drawingAt
	t.drawingAt = 0
	if t.dead || heldStack(t).item != itemBow {
		return
	}
	// BowItem.getPowerForTime (vanilla behavior): QUADRATIC ramp f=(x²+2x)/3 with
	// x=ticks/20, capped at 1 — full power at exactly 1 s; refuse under 0.1.
	x := float64(charge) / bowFullDraw
	power := (x*x + 2*x) / 3
	if power > 1 {
		power = 1
	}
	if power < 0.1 {
		return // vanilla: too weak to loose
	}
	if t.gamemode == gmSurvival {
		if !h.consumeArrow(t) {
			return
		}
		h.applyToolWear(t, t.p.heldSlot(), 1) // bows wear one per shot
	}
	v := bowMaxSpeed * power
	// AbstractArrow: damage = ceil(speed × baseDamage); Power adds 0.5·lvl+0.5 to
	// the base (before ×speed). A full-power draw is critical and adds
	// random.nextInt(damage/2 + 2) — a RANGE, not a flat +1.
	arrowBase := 2.0
	if pl := heldStack(t).enchLvl(enchPower); pl > 0 {
		arrowBase += 0.5*float64(pl) + 0.5
	}
	dmg := int(math.Ceil(arrowBase * v))
	if power >= 1 {
		dmg += h.rng.Intn(dmg/2 + 2)
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	a := h.launchProjectileIn(players, entityArrow, t.dim, t.x, t.y+1.5, t.z, dx*v, dy*v, dz*v)
	a.shooter, a.dmg, a.noHitUntil = t.p.eid, dmg, h.tick.Load()+arrowNoSelfHT
	a.punch = heldStack(t).enchLvl(enchPunch) // Punch: extra hit knockback
	a.playerShot = true
	h.playSound(players, "minecraft:entity.arrow.shoot", sndPlayer, t.x, t.y, t.z, 1, 0.8+float32(power)*0.4)
}

// consumeArrow removes one arrow from the inventory (first stack found).
func (h *hub) consumeArrow(t *tracked) bool {
	for i := range t.inv.slots {
		if s := &t.inv.slots[i]; s.item == itemArrowAmmo && s.count > 0 {
			if s.count--; s.count == 0 {
				*s = invStack{}
			}
			h.sendSlot(t, i)
			return true
		}
	}
	return false
}

// throwProjectile flings a snowball/egg: an instant, flat-damage projectile
// reusing the arrow physics with a lighter punch.
func (h *hub) throwProjectile(players map[int32]*tracked, t *tracked, item int32) {
	if t.dead {
		return
	}
	etype, slot := 0, -1
	switch item {
	case itemSnowball:
		etype = entitySnowball
	case itemEgg:
		etype = entityEggProj
	default:
		return
	}
	for i := range t.inv.slots { // find + consume one (survival)
		if s := &t.inv.slots[i]; s.item == item && s.count > 0 {
			slot = i
			break
		}
	}
	if t.gamemode == gmSurvival {
		if slot < 0 {
			return
		}
		if s := &t.inv.slots[slot]; true {
			if s.count--; s.count == 0 {
				*s = invStack{}
			}
			h.sendSlot(t, slot)
		}
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	a := h.launchProjectileIn(players, etype, t.dim, t.x, t.y+1.5, t.z, dx*throwSpeed, dy*throwSpeed, dz*throwSpeed)
	a.shooter, a.dmg, a.noHitUntil = t.p.eid, 0, h.tick.Load()+arrowNoSelfHT
	a.playerShot, a.breaks = true, true // throwables shatter on impact, never stick
	a.egg = item == itemEgg
	h.playSound(players, "minecraft:entity.snowball.throw", sndPlayer, t.x, t.y, t.z, 0.5, 0.6+h.rng.Float32()*0.4)
}
