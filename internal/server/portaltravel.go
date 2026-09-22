package server

// Portal travel for everything that is not a player.
//
// Entity.handlePortal runs for every entity, and NetherPortalBlock's
// getPortalTransitionTime is zero for anything but a player: a mob or a
// dropped item standing in a portal goes through on the first tick it touches
// one, with none of the dwell a player serves. What stops it coming straight
// back is Entity.getDimensionChangingDelay — 300 ticks of portal cooldown,
// which vanilla gives every non-player.
//
// The destination is the portal this one is PAIRED with. Vanilla would search
// the far side and build a portal where it found none; the engine only builds
// one for a player, whose connection it can talk to while it carves the
// pocket. So an unpaired portal carries nobody — which in practice means the
// player walks through first, as they always do, and everything after them
// follows the pair that trip recorded.
//
// Not modelled: a mob's passengers and whatever it is leashed to travel with
// it in vanilla. Here the mob goes alone, and a leash to a stranded holder
// breaks the way any over-long leash does.

// entityPortalCooldown is Entity.getDimensionChangingDelay for a non-player.
const entityPortalCooldown = 300

// updatePortalTravel walks mobs and dropped items through nether portals.
func (h *hub) updatePortalTravel(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.portalCool > 0 {
			m.portalCool--
			continue
		}
		if !h.portalTravelCandidate(m.dim, m.x, m.y, m.z) {
			continue
		}
		if to, ok := h.portalPartner(m.dim, m.x, m.y, m.z); ok {
			h.entityGone(players, m.dim, m.eid)
			m.dim = to.dim
			m.x, m.y, m.z = float64(to.pos.x)+0.5, float64(to.pos.y), float64(to.pos.z)+0.5
			m.portalCool = entityPortalCooldown
			m.targetEID, m.hasTarget = 0, false // whatever it was chasing is a world away
		}
	}
	for _, it := range h.items {
		if it.portalCool > 0 {
			it.portalCool--
			continue
		}
		if !h.portalTravelCandidate(it.dim, it.x, it.y, it.z) {
			continue
		}
		if to, ok := h.portalPartner(it.dim, it.x, it.y, it.z); ok {
			h.entityGone(players, it.dim, it.eid)
			it.dim = to.dim
			it.x, it.y, it.z = float64(to.pos.x)+0.5, float64(to.pos.y), float64(to.pos.z)+0.5
			it.portalCool = entityPortalCooldown
		}
	}
}

// portalTravelCandidate is the cheap half of the test: the right dimensions,
// the gamerule, and a portal block where the entity's feet are.
func (h *hub) portalTravelCandidate(dim int, x, y, z float64) bool {
	if dim > 1 || len(h.portalLinks) == 0 {
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

// portalPartner resolves the portal an entity is standing in to the portal it
// is paired with, checking that the far side is still a real, lit, enterable
// portal — and forgetting a pair whose other half has been broken, so the next
// player through records a fresh one.
func (h *hub) portalPartner(dim int, x, y, z float64) (dimPos, bool) {
	w := h.worldFor(dim)
	from := dimPos{dim, portalBaseKey(w, floorInt(x), floorInt(y+0.05), floorInt(z))}
	to, ok := h.portalLinks[from]
	if !ok {
		return dimPos{}, false
	}
	tw := h.worldFor(to.dim)
	st := tw.At(to.pos.x, to.pos.y, to.pos.z)
	if isPortalBlock(st) && portalIntact(tw, to.pos.x, to.pos.y, to.pos.z, st) &&
		portalSpotSafe(tw, to.pos.x, to.pos.y, to.pos.z) {
		return to, true
	}
	delete(h.portalLinks, from)
	delete(h.portalLinks, to)
	return dimPos{}, false
}
