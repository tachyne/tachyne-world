package server

import "math"

// Per-viewer entity tracking — vanilla's ChunkMap.TrackedEntity. Every
// player carries the set of entities their client is currently holding,
// and each pass compares that set against what is actually within the
// interest radius: whatever has come into view is spawned for them in
// full, whatever has left it (or stopped existing) is removed.
//
// Without it an entity that walked out of the radius simply stopped being
// talked about, and the client went on rendering it where it last stood —
// a creature frozen at the edge of view that nothing would ever correct.
// The set is also what makes a spawn exact: an entity is announced to the
// players who can see it, once, rather than broadcast and hoped for.
//
// It owns the entities that move and come and go — mobs, dropped items and
// experience orbs. The fixed furniture (paintings, item frames, armour
// stands, vehicles, end crystals) is sent at join and removed by id when it
// is destroyed; none of it wanders, so none of it goes stale.

// trackedKinds reports whether an id belongs to the tracker. Only these
// entities may be added to or removed from a viewer's set, so the pass can
// never retract something another path owns.
func (h *hub) trackable(eid int32) (kind int, ok bool) {
	if _, live := h.mobs[eid]; live {
		return trackMob, true
	}
	if _, live := h.items[eid]; live {
		return trackItem, true
	}
	if _, live := h.orbs[eid]; live {
		return trackOrb, true
	}
	return 0, false
}

const (
	trackMob = iota
	trackItem
	trackOrb
)

// trackRange is an entity type's tracking range in blocks — vanilla's
// clientTrackingRange, which varies from four chunks for an arrow to
// sixteen for an end crystal.
func trackRange(etype int) float64 {
	if r, ok := trackRanges[entityNameByID[etype]]; ok {
		return float64(r) * 16
	}
	return trackRangeDefault * 16
}

// inRangeOf is ChunkMap.TrackedEntity.updatePlayer's visibility test: the
// HORIZONTAL distance only, height ignored, against the smaller of the
// entity type's own range and the viewer's render distance.
func inRangeOf(t *tracked, dim int, x, z float64, etype int) bool {
	if t.dim != dim {
		return false
	}
	r := math.Min(trackRange(etype), float64(t.p.radius())*16)
	dx, dz := t.x-x, t.z-z
	return dx*dx+dz*dz <= r*r
}

// syncTracking is the pass: one reconciliation per viewer.
func (h *hub) syncTracking(players map[int32]*tracked) {
	if h.scratchWant == nil {
		h.scratchWant = map[int32]bool{}
	}
	want := h.scratchWant
	for _, t := range players {
		if t.tracked == nil {
			t.tracked = map[int32]bool{}
		}
		clear(want)
		for _, m := range h.mobs {
			if m != h.dragon && inRangeOf(t, m.dim, m.x, m.z, m.etype) {
				want[m.eid] = true
			}
		}
		for _, it := range h.items {
			if inRangeOf(t, it.dim, it.x, it.z, entityItem) {
				want[it.eid] = true
			}
		}
		for _, o := range h.orbs {
			if inRangeOf(t, o.dim, o.x, o.z, entityXPOrb) {
				want[o.eid] = true
			}
		}
		// Out of view or out of the world: one removal frame for the lot.
		var gone []int32
		for eid := range t.tracked {
			if !want[eid] {
				gone = append(gone, eid)
				delete(t.tracked, eid)
			}
		}
		if len(gone) > 0 {
			t.p.trySendEv(entGone(gone...))
		}
		// Come into view: spawned in full, with the state that rides a spawn.
		for eid := range want {
			if t.tracked[eid] {
				continue
			}
			t.tracked[eid] = true
			h.showEntityTo(t, eid)
		}
	}
}

// showEntityTo sends one entity's spawn and the state vanilla sends with it.
func (h *hub) showEntityTo(t *tracked, eid int32) {
	switch kind, ok := h.trackable(eid); {
	case !ok:
		delete(t.tracked, eid) // it went in the same tick — nothing to show
	case kind == trackMob:
		h.showMobTo(t, h.mobs[eid])
	case kind == trackItem:
		it := h.items[eid]
		t.p.trySendEv(entAdd(it.eid, entityItem, it.uuid, it.x, it.y, it.z, 0, 0))
		t.p.trySendEv(metaEv(itemMetadata(it.eid, it.stack())))
	case kind == trackOrb:
		o := h.orbs[eid]
		t.p.trySendEv(entAdd(o.eid, entityXPOrb, o.uuid, o.x, o.y, o.z, 0, 0))
	}
}

// showMobTo spawns a mob for one viewer with everything that rides a spawn:
// its attributes, the equipment it carries, and the one-shot metadata that
// would otherwise only be re-asserted on the next resync.
func (h *hub) showMobTo(t *tracked, m *mob) {
	if m == nil {
		return
	}
	t.p.trySendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
	sendAttrsTo(t, mobAttrFrame(m)) // addPairing: the attributes ride with the spawn
	if m.etype == entityPufferfish && m.puff != 0 {
		t.p.trySendEv(metaEv(puffMeta(m.eid, m.puff)))
	}
	if m.burning {
		t.p.trySendEv(metaEv(fireMetadata(m.eid, true)))
	}
	if m.wearsAnything() {
		t.p.trySendEv(equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
	} else if m.etype == entitySkeleton {
		t.p.trySendEv(skeletonEquip(m.eid))
	}
	if m.baby {
		t.p.trySendEv(metaEv(babyMeta(m.eid, true)))
	}
	if m.sheared {
		t.p.trySendEv(metaEv(sheepMeta(m, true)))
	}
	if m.size > 0 {
		t.p.trySendEv(metaEv(slimeMeta(m.eid, m.size)))
	}
	if vm := variantMeta(m); vm != nil {
		t.p.trySendEv(metaEv(vm))
	}
	if sm := speciesStateMeta(m); sm != nil { // a goat's horns, a turtle's egg
		t.p.trySendEv(metaEv(sm))
	}
	if m.aggressive { // arms already up when it comes into view
		t.p.trySendEv(metaEv(mobFlagsMeta(m.eid, true)))
	}
	if m.harness != 0 {
		t.p.trySendEv(ghastHarnessEquip(m.eid, m.harness))
	}
	if m.etype == entityCopperGolem && m.oxidation > 0 {
		t.p.trySendEv(metaEv(copperWeatherMeta(m.eid, int32(m.oxidation))))
	}
	if m.etype == entityEnderman && m.carriedBlock != 0 {
		t.p.trySendEv(metaEv(enderCarryMeta(m.eid, m.carriedBlock)))
	}
	if m.etype == entityBee {
		if m.beeSentFlags != 0 {
			t.p.trySendEv(metaEv(beeFlagsMeta(m.eid, m.beeSentFlags)))
		}
		if m.beeSentAngry {
			t.p.trySendEv(metaEv(beeAngerMeta(m.eid, m.anger)))
		}
	}
	if m.saddled && !horseFamily(m.etype) {
		t.p.trySendEv(saddleEquip(m.eid))
	}
	if m.tamed {
		t.p.trySendEv(metaEv(petMeta(m)))
	}
	if m.sleeping {
		t.p.trySendEv(metaEv(sleepMetadata(m.eid, m.bed)))
	}
	if m.rider != 0 {
		t.p.trySendEv(passengersBody(m.eid, m.rider))
	}
	if len(m.riders) > 0 {
		t.p.trySendEv(passengersBody(m.eid, m.riders...))
	}
	if m.mobRider != 0 {
		t.p.trySendEv(passengersBody(m.eid, m.mobRider))
	}
}

// forgetTracked drops a viewer's whole set — on a dimension change, where
// the old dimension's entities are removed wholesale and the next pass
// spawns the new one's.
func forgetTracked(t *tracked) {
	if t.tracked != nil {
		clear(t.tracked)
	}
}

// dropTracked removes every entity a viewer holds and empties their set —
// a dimension change, where the whole view is replaced at once and the
// next pass spawns what belongs in the new one.
func (h *hub) dropTracked(t *tracked) {
	if len(t.tracked) == 0 {
		return
	}
	gone := make([]int32, 0, len(t.tracked))
	for eid := range t.tracked {
		gone = append(gone, eid)
	}
	clear(t.tracked)
	t.p.sendEv(entGone(gone...))
}

// toTracking sends a frame about one entity to the viewers whose clients
// are holding it — vanilla's sendToTrackingPlayers. An entity's own frames
// have to follow its tracked set rather than a fixed radius: the moment the
// two disagree, a viewer who can see the entity stops hearing about it and
// is left rendering it where it last stood. Kinds the tracker does not own
// yet fall back to the positional broadcast they have always used.
func (h *hub) toTracking(players map[int32]*tracked, eid int32, dim int, x, z float64, ev any) {
	if _, managed := h.trackable(eid); !managed {
		h.toNearbyEv(players, dim, x, z, ev)
		return
	}
	for _, t := range players {
		if t.tracked[eid] {
			t.p.trySendEv(ev)
		}
	}
}
