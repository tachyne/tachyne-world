package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Portal travel for everything that is not a player.
//
// Entity.handlePortal runs for every entity, and NetherPortalBlock's
// getPortalTransitionTime is zero for anything but a player: a mob, a dropped
// item, an arrow, a lit charge or a falling block standing in a portal goes
// through on the first tick it touches one, with none of the dwell a player
// serves. Where it comes out is the same search a player's trip makes
// (portalforcer.go): the closest portal on the far side, or a new one built
// there — a mob can open the way as well as a player can.
//
// What stops it coming straight back is Entity.getDimensionChangingDelay —
// 300 ticks of portal cooldown — and setAsInsidePortal, which starts the
// cooldown over on every tick the entity is still inside a portal: one that
// comes out standing in the far portal stays there until it steps off.
//
// Not modelled: a mob's passengers and whatever it is leashed to travel with
// it in vanilla. Here the mob goes alone, and a leash to a stranded holder
// breaks the way any over-long leash does.

// entityPortalCooldown is Entity.getDimensionChangingDelay for a non-player.
const entityPortalCooldown = 300

// portalCooldownTick is processPortalCooldown with setAsInsidePortal's reset:
// it reports whether the entity is still on cooldown this tick.
func (h *hub) portalCooldownTick(cool *int, dim int, x, y, z float64) bool {
	if *cool <= 0 {
		return false
	}
	if h.inAnyPortal(dim, x, y, z) {
		*cool = entityPortalCooldown
	} else {
		*cool--
	}
	return true
}

// inAnyPortal reports a nether or end portal block at an entity's feet.
func (h *hub) inAnyPortal(dim int, x, y, z float64) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	s := w.At(floorInt(x), floorInt(y+0.05), floorInt(z))
	return isPortalBlock(s) || s == worldgen.EndPortalBlock
}

// entityPortalExit resolves a non-player's trip through the nether portal it
// stands in (box width w, height ht).
func (h *hub) entityPortalExit(players map[int32]*tracked, dim int, x, y, z, w, ht float64) (portalArrival, bool) {
	entry := blockPos{floorInt(x), floorInt(y + 0.05), floorInt(z)}
	return h.netherPortalExit(players, dim, entry, x, y, z, w, ht, false)
}

// updatePortalTravel walks mobs, dropped items, projectiles, lit charges and
// falling blocks through nether portals.
func (h *hub) updatePortalTravel(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if h.portalCooldownTick(&m.portalCool, m.dim, m.x, m.y, m.z) {
			continue
		}
		if m.dying > 0 || m.mount != 0 || !h.portalTravelCandidate(m.dim, m.x, m.y, m.z) {
			continue // canUsePortal: alive and not riding anything
		}
		b := m.box()
		if a, ok := h.entityPortalExit(players, m.dim, m.x, m.y, m.z, b.w, b.h); ok {
			h.mobChangeDimension(players, m, a.dim, a.x, a.y, a.z)
			m.yaw += a.turn
			m.headYaw += a.turn
			m.vx, m.vz = rotateDelta(m.vx, m.vz, a.turn)
			m.portalCool = entityPortalCooldown
		}
	}
	for _, a := range h.arrows { // a projectile in flight goes through, still flying
		if h.portalCooldownTick(&a.portalCool, a.dim, a.x, a.y, a.z) {
			continue
		}
		if a.stuck || a.etype == entityPearlProj || !h.portalTravelCandidate(a.dim, a.x, a.y, a.z) {
			continue // an ender pearl's trip is its thrower's business
		}
		if to, ok := h.entityPortalExit(players, a.dim, a.x, a.y, a.z, 0.5, 0.5); ok {
			h.entityGone(players, a.dim, a.eid)
			a.dim = to.dim
			a.x, a.y, a.z = to.x, to.y, to.z
			a.vx, a.vz = rotateDelta(a.vx, a.vz, to.turn)
			a.sx, a.sy, a.sz, a.ox, a.oz = a.x, a.y, a.z, a.x, a.z
			a.portalCool = entityPortalCooldown
			add := entAdd(a.eid, a.etype, a.uuid, a.x, a.y, a.z, arrowYaw(a), arrowPitch(a))
			add.VX, add.VY, add.VZ = a.vx, a.vy, a.vz
			h.toNearbyEv(players, a.dim, a.x, a.z, add)
		}
	}
	for _, t := range h.tnt { // a lit charge goes through too, fuse and all
		if h.portalCooldownTick(&t.portalCool, t.dim, t.x, t.y, t.z) {
			continue
		}
		if !h.portalTravelCandidate(t.dim, t.x, t.y, t.z) {
			continue
		}
		if to, ok := h.entityPortalExit(players, t.dim, t.x, t.y, t.z, 2*tntHalfWidth, tntHeight); ok {
			h.entityGone(players, t.dim, t.eid)
			t.dim = to.dim
			t.x, t.y, t.z = to.x, to.y, to.z
			t.vx, t.vz = rotateDelta(t.vx, t.vz, to.turn)
			t.portalCool = entityPortalCooldown
			h.showPrimedTNT(players, t)
		}
	}
	for _, it := range h.items {
		if h.portalCooldownTick(&it.portalCool, it.dim, it.x, it.y, it.z) {
			continue
		}
		if !h.portalTravelCandidate(it.dim, it.x, it.y, it.z) {
			continue
		}
		if to, ok := h.entityPortalExit(players, it.dim, it.x, it.y, it.z, 2*itemHalfHeight, 2*itemHalfHeight); ok {
			h.entityGone(players, it.dim, it.eid)
			it.dim = to.dim
			it.x, it.y, it.z = to.x, to.y, to.z
			it.vx, it.vz = rotateDelta(it.vx, it.vz, to.turn)
			it.portalCool = entityPortalCooldown
		}
	}
	for _, fb := range h.fallingBlocks { // FallingBlockEntity.tick calls handlePortal too
		if h.portalCooldownTick(&fb.portalCool, fb.dim, fb.x, fb.y, fb.z) {
			continue
		}
		if !h.portalTravelCandidate(fb.dim, fb.x, fb.y, fb.z) {
			continue
		}
		if to, ok := h.entityPortalExit(players, fb.dim, fb.x, fb.y, fb.z, 2*fallHalfWidth, fallHeight); ok {
			h.moveFallingBlock(players, fb, to.dim, to.x, to.y, to.z)
			fb.vx, fb.vz = rotateDelta(fb.vx, fb.vz, to.turn)
		}
	}
}

// portalTravelCandidate is the cheap half of the test: the right dimensions,
// the gamerule, and a portal block where the entity's feet are.
func (h *hub) portalTravelCandidate(dim int, x, y, z float64) bool {
	if dim > 1 {
		return false // nether portals link the overworld and nether only
	}
	// ServerLevel.isAllowedToEnterPortal gates the way IN to the Nether only;
	// coming back out always works, or turning the rule off would strand
	// whoever was already there.
	if dim == dimOverworld && !h.rules.AllowNether {
		return false
	}
	return isPortalBlock(h.worldFor(dim).At(floorInt(x), floorInt(y+0.05), floorInt(z)))
}

// updateEndPortalEntities is EndPortalBlock.entityInside for mobs and dropped
// items (players go by updateEndPortalContact): into the End onto the
// obsidian pad at (100,50,0), built if it is missing; out of the End to the
// world spawn. Bosses never take a portal (canUsePortal).
func (h *hub) updateEndPortalEntities(players map[int32]*tracked) {
	inPortal := func(dim int, x, y, z float64) bool {
		w := h.worldFor(dim)
		return w != nil && w.At(floorInt(x), floorInt(y+0.05), floorInt(z)) == worldgen.EndPortalBlock
	}
	dest := func(from int) (int, float64, float64, float64, bool) {
		if from == dimEnd { // EndPortalBlock: out to the level the world spawn is in
			to := h.spawnDim()
			w := h.worldFor(to)
			x, z := h.worldSpawnX, h.worldSpawnZ
			if to != dimOverworld {
				return to, x, h.worldSpawnY, z, true
			}
			return dimOverworld, x, float64(w.MobFeet(floorInt(x), floorInt(z))), z, true
		}
		if h.end == nil {
			return 0, 0, 0, 0, false
		}
		endPlatform(h.end)
		return dimEnd, 100.5, 50, 0.5, true
	}
	for _, m := range h.mobs {
		if m.portalCool > 0 || m.dying > 0 || m == h.dragon || m.etype == entityWither || m.mount != 0 {
			continue
		}
		if !inPortal(m.dim, m.x, m.y, m.z) {
			continue
		}
		if d, x, y, z, ok := dest(m.dim); ok {
			h.entityGone(players, m.dim, m.eid)
			m.dim, m.x, m.y, m.z = d, x, y, z
			m.portalCool = entityPortalCooldown
			m.targetEID, m.hasTarget = 0, false
		}
	}
	for _, it := range h.items {
		if it.portalCool > 0 || !inPortal(it.dim, it.x, it.y, it.z) {
			continue
		}
		if d, x, y, z, ok := dest(it.dim); ok {
			h.entityGone(players, it.dim, it.eid)
			it.dim, it.x, it.y, it.z = d, x, y, z
			it.portalCool = entityPortalCooldown
		}
	}
	for _, fb := range h.fallingBlocks {
		if fb.portalCool > 0 || !inPortal(fb.dim, fb.x, fb.y, fb.z) {
			continue
		}
		if d, x, y, z, ok := dest(fb.dim); ok {
			h.moveFallingBlock(players, fb, d, x, y, z)
		}
	}
}

// mobChangeDimension moves a mob into another dimension at a spot, the way a
// portal's traveller goes: it leaves its seat and its rider behind, forgets
// what it was chasing, and the old dimension's viewers see it go (the new
// one's pick it up through tracking).
func (h *hub) mobChangeDimension(players map[int32]*tracked, m *mob, dim int, x, y, z float64) {
	h.unseatMob(players, m)
	if t := players[m.rider]; t != nil {
		h.dismountMob(players, t)
	}
	h.entityGone(players, m.dim, m.eid)
	m.dim = dim
	m.x, m.y, m.z = x, y, z
	m.sx, m.sy, m.sz = x, y, z
	m.targetEID, m.hasTarget = 0, false
}
