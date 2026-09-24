package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A wind charge's burst (vanilla AbstractWindCharge.explode → a TRIGGER
// explosion): power 1.2, breaks nothing, hurts nothing, but every block its
// blast rays reach gets its onExplosionHit — wooden doors, trapdoors and
// fence gates swing, buttons press, levers flip, bells ring, candles go out,
// beehives send their bees after whoever threw it. Iron doors and trapdoors
// ignore it (BlockSetType.canOpenByWindCharge), as does anything a redstone
// signal is holding.

const (
	windChargeRadius    = 1.2  // WindCharge RADIUS (a player's or a dispenser's)
	breezeChargeRadius  = 3.0  // BreezeWindCharge RADIUS
	windChargeKnockback = 1.22 // SimpleExplosionDamageCalculator knockbackMultiplier
)

// isWindCharge reports the AbstractWindCharge family: a player's or a
// dispenser's wind_charge and a breeze's breeze_wind_charge. A breeze's
// charge keeps its type (and its wider burst) after a player bats it back.
func isWindCharge(etype int) bool {
	return etype == entityWindCharge || etype == entityBreezeWindCharge
}

// breezeOwned reports whether a projectile's owner is a breeze, which a
// breeze is invulnerable to (Breeze.isInvulnerableTo).
func (h *hub) breezeOwned(a *arrowEntity) bool {
	if a.playerShot {
		return false // a player's, or one a player batted back
	}
	m := h.mobs[a.shooter]
	return m != nil && m.etype == entityBreeze
}

var (
	ironDoorMin, ironDoorMax         = worldgen.BlockRange("iron_door")
	ironTrapdoorMin, ironTrapdoorMax = worldgen.BlockRange("iron_trapdoor")
)

func isIronDoor(s uint32) bool     { return s >= ironDoorMin && s <= ironDoorMax }
func isIronTrapdoor(s uint32) bool { return s >= ironTrapdoorMin && s <= ironTrapdoorMax }

// windBurst is the gust at the point of impact.
func (h *hub) windBurst(players map[int32]*tracked, dim int, cx, cy, cz float64, shooter int32) {
	h.windBurstR(players, dim, cx, cy, cz, shooter, windChargeRadius)
}

// chargeBurst is a wind charge's own burst where it struck: a breeze's
// charge (BreezeWindCharge.explode) bursts wider and with its own sound.
func (h *hub) chargeBurst(players map[int32]*tracked, a *arrowEntity, cx, cy, cz float64) {
	if a.breezeBorn {
		h.windBurstSnd(players, a.dim, cx, cy, cz, a.shooter, breezeChargeRadius, "minecraft:entity.breeze_wind_charge.burst")
		return
	}
	h.windBurstR(players, a.dim, cx, cy, cz, a.shooter, windChargeRadius)
}

// windBurstR is the gust at a given radius (the breeze's is wider).
func (h *hub) windBurstR(players map[int32]*tracked, dim int, cx, cy, cz float64, shooter int32, radius float64) {
	h.windBurstSnd(players, dim, cx, cy, cz, shooter, radius, "minecraft:entity.wind_charge.wind_burst")
}

// windBurstSnd is the gust with the burst sound its source makes.
func (h *hub) windBurstSnd(players map[int32]*tracked, dim int, cx, cy, cz float64, shooter int32, radius float64, sound string) {
	h.playSoundDim(players, dim, sound, sndNeutral, cx, cy, cz, 1, 1)
	h.spawnParticles(players, dim, particlePoof, cx, cy, cz, 0.4, 0.1, 12)
	h.windPush(players, dim, cx, cy, cz, radius)
	w := h.worldFor(dim)
	if w == nil {
		return
	}
	for pos := range h.blastPositions(w, cx, cy, cz, radius) {
		h.triggerBlock(players, dim, pos, w.At(pos.x, pos.y, pos.z), shooter)
	}
}

// windPush is ServerExplosion.hurtEntities for a gust: no damage, but
// everything within twice the radius is pushed away from the centre —
// from the eyes, by (1 − distance/2r) × 1.22 × (1 − explosion knockback
// resistance) — which is the wind-charge jump when the charge bursts at
// your feet. Exposure is taken as full (no seen-percent ray cast).
func (h *hub) windPush(players map[int32]*tracked, dim int, cx, cy, cz, radius float64) {
	dr := radius * 2
	now := h.tick.Load()
	for _, t := range players {
		if t.dim != dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		dist := dist3(t.x, t.y, t.z, cx, cy, cz) / dr
		if dist > 1 {
			continue
		}
		ex, ey, ez := t.x-cx, t.y+playerEyeHeightStand-cy, t.z-cz
		n := math.Sqrt(ex*ex + ey*ey + ez*ez)
		if n < 1e-9 {
			continue
		}
		power := (1 - dist) * windChargeKnockback * t.explosionKnockScale()
		if power <= 0 {
			continue
		}
		t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: ex / n * power, VY: ey / n * power, VZ: ez / n * power})
		t.spinUntil = now + windBurstGrace // let the launch through the speed check
		t.launchCause = "wind_charge"      // fall_after_explosion, until the next landing
	}
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 {
			continue
		}
		dist := dist3(m.x, m.y, m.z, cx, cy, cz) / dr
		if dist > 1 {
			continue
		}
		ex, ez := m.x-cx, m.z-cz
		n := math.Hypot(ex, ez)
		power := (1 - dist) * windChargeKnockback * m.kbScale()
		if n < 1e-9 || power <= 0 {
			continue
		}
		m.vx, m.vz, m.kb, m.reroute = m.vx+ex/n*power, m.vz+ez/n*power, 3, 0
		h.mobKnockVelocity(players, m)
	}
}

// triggerBlock is one block's reaction to a triggering explosion.
func (h *hub) triggerBlock(players map[int32]*tracked, dim int, pos blockPos, st uint32, shooter int32) {
	info, ok := worldgen.InfoForState(st)
	if !ok {
		return
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	switch {
	case isBell(st):
		h.ringBell(players, dim, pos, -1)
	case inRanges(candleRanges, st):
		if boolProp(st, "lit") {
			h.extinguishCandle(players, dim, pos, st)
		}
	case isBeeHome(st):
		if t := players[shooter]; t != nil {
			h.angerBees(players, t, dim, pos)
		}
	case isLever(st):
		// Redstone runs in every dimension since the simulation learned which
		// one it is in, so a gust flips a lever in the Nether too — the
		// overworld-only guard here outlived its reason.
		h.inDim(dim, func() { h.toggleLever(players, pos, st) })
	case isButton(st):
		h.inDim(dim, func() { h.pressButton(players, pos, st) })
	case info.HasProperty("hinge"): // a door: the lower half swings both
		if isIronDoor(st) || boolProp(st, "powered") || worldgen.GetProperty(info, st, "half") != "lower" {
			return
		}
		open := !boolProp(st, "open")
		h.setBlockAt(players, dim, pos, setBoolProp(st, "open", open))
		w := h.worldFor(dim)
		if upper := w.At(pos.x, pos.y+1, pos.z); upper != st {
			if ui, ok := worldgen.InfoForState(upper); ok && ui.HasProperty("hinge") {
				h.setBlockAt(players, dim, blockPos{pos.x, pos.y + 1, pos.z}, setBoolProp(upper, "open", open))
			}
		}
		h.playSoundDim(players, dim, openSound("minecraft:block.wooden_door", open), sndBlock, cx, cy, cz, 1, 0.9+h.rng.Float32()*0.1)
	case info.HasProperty("in_wall"): // a fence gate
		if boolProp(st, "powered") {
			return
		}
		open := !boolProp(st, "open")
		h.setBlockAt(players, dim, pos, setBoolProp(st, "open", open))
		h.playSoundDim(players, dim, openSound("minecraft:block.fence_gate", open), sndBlock, cx, cy, cz, 1, 0.9+h.rng.Float32()*0.1)
	case info.HasProperty("open") && info.HasProperty("half"): // a trapdoor
		if isIronTrapdoor(st) || boolProp(st, "powered") {
			return
		}
		open := !boolProp(st, "open")
		h.setBlockAt(players, dim, pos, setBoolProp(st, "open", open))
		h.playSoundDim(players, dim, openSound("minecraft:block.wooden_trapdoor", open), sndBlock, cx, cy, cz, 1, 0.9+h.rng.Float32()*0.1)
	}
}

func openSound(base string, open bool) string {
	if open {
		return base + ".open"
	}
	return base + ".close"
}
