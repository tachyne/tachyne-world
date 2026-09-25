package server

import "math"

// The hanging entities' shared rules (BlockAttachedEntity / HangingEntity):
//
//   - every hundred ticks each one checks it still survives — its support,
//     its room, and for a knot its fence — and pops when not, whatever
//     changed the blocks (a piston, a blast, a flow, not just a player);
//   - any damage breaks one outright (hurtServer), so explosions within
//     twice their power and any projectile that meets one take it down,
//     unless a mob caused it with mob_griefing off; a frame holding an item
//     gives up only the item to anything but a blast;
//   - what they drop obeys entity_drops.

// hangingCheckInterval is BlockAttachedEntity's ticksSinceLastCheck period.
const hangingCheckInterval = 100

// hangingSurvivalTick runs each tick; every hanging entity checks on its own
// hundred-tick beat (staggered by id, as their spawn ticks stagger them).
func (h *hub) hangingSurvivalTick(players map[int32]*tracked) {
	now := h.tick.Load()
	due := func(eid int32) bool { return (now+uint64(eid))%hangingCheckInterval == 0 }
	for _, f := range h.itemFrames {
		if due(f.eid) && !h.frameFits(f.dim, f.x, f.y, f.z, f.dir, f.eid) {
			h.breakFrame(players, f, false)
		}
	}
	for _, pt := range h.paintings {
		if due(pt.eid) && !h.paintingFitsIgnoringSelf(pt) {
			h.breakPainting(players, pt, nil)
		}
	}
	for _, k := range h.knots {
		if due(k.eid) && !isFence(h.worldFor(k.dim).At(k.pos.x, k.pos.y, k.pos.z)) {
			h.breakKnot(players, k)
		}
	}
}

// breakKnot is a knot's removal (kill + dropItem): the untied sound, and
// every mob tied to it loses its lead — dropped as an item when entity
// drops are on (the holder can no longer interact with the level).
func (h *hub) breakKnot(players map[int32]*tracked, k *leashKnot) {
	x, y, z := float64(k.pos.x)+0.5, float64(k.pos.y)+leashKnotYOffset, float64(k.pos.z)+0.5
	h.playSoundDim(players, k.dim, "minecraft:entity.lead.untied", sndNeutral, x, y, z, 1, 1)
	for _, m := range h.mobs {
		if m.leash == k.eid {
			h.dropLeash(players, m, h.rules.EntityDrops)
		}
	}
	if h.knots[k.eid] != nil {
		delete(h.knots, k.eid)
		h.toDimEv(players, k.dim, entGone(k.eid))
	}
}

// hangingHurtAllowed is hurtServer's mob_griefing gate: a mob's damage
// breaks nothing hanging when griefing is off.
func (h *hub) hangingHurtAllowed(byMob bool) bool { return !byMob || h.rules.MobGriefing }

// explosionHitsHanging is ServerExplosion.hurtEntities for the hanging
// entities: everything within twice the power is hurt, and a hurt one
// breaks (a framed item comes down with the frame).
func (h *hub) explosionHitsHanging(players map[int32]*tracked, dim int, cx, cy, cz, power float64) {
	if !h.hangingHurtAllowed(h.blastSrc.causerMob) {
		return
	}
	dr := power * 2
	for _, f := range h.itemFrames {
		if f.dim == dim && dist3(float64(f.x)+0.5, float64(f.y)+0.5, float64(f.z)+0.5, cx, cy, cz) <= dr {
			h.breakFrame(players, f, false)
		}
	}
	for _, pt := range h.paintings {
		if pt.dim == dim && dist3(float64(pt.x)+0.5, float64(pt.y)+0.5, float64(pt.z)+0.5, cx, cy, cz) <= dr {
			h.breakPainting(players, pt, nil)
		}
	}
	for _, k := range h.knots {
		if k.dim == dim && dist3(float64(k.pos.x)+0.5, float64(k.pos.y)+leashKnotYOffset, float64(k.pos.z)+0.5, cx, cy, cz) <= dr {
			h.breakKnot(players, k)
		}
	}
}

// arrowHitsHanging is a projectile meeting a hanging entity: it is hurt —
// a painting or a knot breaks, a frame gives up its item or else breaks —
// and the projectile is spent on it.
func (h *hub) arrowHitsHanging(players map[int32]*tracked, a *arrowEntity, px, py, pz float64) bool {
	if a.pearl || a.xpBottle || a.breath || a.splash {
		return false
	}
	by := players[a.shooter]
	allowed := h.hangingHurtAllowed(a.mobShot)
	for _, f := range h.itemFrames {
		if f.dim != a.dim || !pointInFrame(f, px, py, pz) {
			continue
		}
		if allowed {
			h.hitFrame(players, by, f)
		}
		return true
	}
	for _, pt := range h.paintings {
		if pt.dim != a.dim || !pointInPainting(pt, px, py, pz) {
			continue
		}
		if allowed {
			h.breakPainting(players, pt, by)
		}
		return true
	}
	for _, k := range h.knots {
		if k.dim != a.dim {
			continue
		}
		kx, ky, kz := float64(k.pos.x)+0.5, float64(k.pos.y)+leashKnotYOffset-0.25, float64(k.pos.z)+0.5
		if math.Abs(px-kx) > 0.1875+0.3 || math.Abs(pz-kz) > 0.1875+0.3 || py < ky-0.3 || py > ky+0.5+0.3 {
			continue
		}
		if allowed {
			h.breakKnot(players, k)
		}
		return true
	}
	return false
}

// pointInFrame: within the frame's thin box against its support (0.75 across,
// 1/16 deep), grown by the 0.3 a projectile's reach adds.
func pointInFrame(f *itemFrame, px, py, pz float64) bool {
	off := frameSupportOffset[f.dir]
	cx, cy, cz := float64(f.x)+0.5+float64(off[0])*0.46875, float64(f.y)+0.5+float64(off[1])*0.46875, float64(f.z)+0.5+float64(off[2])*0.46875
	hx, hy, hz := 0.375, 0.375, 0.375
	switch {
	case off[0] != 0:
		hx = 0.03125
	case off[1] != 0:
		hy = 0.03125
	default:
		hz = 0.03125
	}
	return math.Abs(px-cx) <= hx+0.3 && math.Abs(py-cy) <= hy+0.3 && math.Abs(pz-cz) <= hz+0.3
}

// pointInPainting: inside any of the canvas's cells, near the wall.
func pointInPainting(pt *painting, px, py, pz float64) bool {
	facing := faceName(pt.dir)
	bdx, bdz := facingDelta(oppositeFacing(facing))
	for _, c := range paintingCells(pt.x, pt.y, pt.z, pt.w, pt.h, facing) {
		cx, cz := float64(c[0])+0.5+float64(bdx)*0.46875, float64(c[2])+0.5+float64(bdz)*0.46875
		hx, hz := 0.5, 0.5
		if bdx != 0 {
			hx = 0.03125
		} else {
			hz = 0.03125
		}
		if math.Abs(px-cx) <= hx+0.3 && math.Abs(pz-cz) <= hz+0.3 && py >= float64(c[1])-0.3 && py <= float64(c[1])+1.3 {
			return true
		}
	}
	return false
}

// frameSound is the frame's sound: GlowItemFrame has its own five.
func frameSound(f *itemFrame, what string) string {
	if f.glow {
		return "minecraft:entity.glow_item_frame." + what
	}
	return "minecraft:entity.item_frame." + what
}
