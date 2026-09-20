package server

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

// inInterest reports whether a point is inside the viewer's entity interest
// radius — the same window that carries an entity's movement, so anything
// tracked is also kept up to date.
func inInterest(t *tracked, dim int, x, z float64) bool {
	if t.dim != dim {
		return false
	}
	return abs(chunkFloor(t.x)-chunkFloor(x)) <= viewRadius &&
		abs(chunkFloor(t.z)-chunkFloor(z)) <= viewRadius
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
			if m != h.dragon && inInterest(t, m.dim, m.x, m.z) {
				want[m.eid] = true
			}
		}
		for _, it := range h.items {
			if inInterest(t, it.dim, it.x, it.z) {
				want[it.eid] = true
			}
		}
		for _, o := range h.orbs {
			if inInterest(t, o.dim, o.x, o.z) {
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
