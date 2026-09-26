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
// It owns the entities that move and come and go — other players, mobs,
// dropped items and experience orbs. Players were the last in: their bodies
// were spawned for the whole dimension at join while their moves reached only
// viewers within six chunks, and nothing removed them on leaving range — so
// a player who flew or teleported away stayed standing, frozen, wherever
// another client had last seen them (bug #41). The fixed furniture (paintings, item frames, armour
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

// inRangeOf is ChunkMap.TrackedEntity.updatePlayer's visibility test for
// one entity type, taking the viewer's chunk view itself.
func inRangeOf(t *tracked, dim int, x, z float64, etype int) bool {
	v := t.p.lockView()
	defer t.p.unlockView()
	return visibleIn(t, v, dim, x, z, trackRange(etype))
}

// visibleIn is the test itself: the HORIZONTAL distance only, height
// ignored, within the smaller of the entity's effective range and the
// viewer's render distance (getPlayerViewDistance × 16) — and the entity's
// chunk one the viewer's client holds (isChunkTracked: inside its tracking
// view and already sent).
func visibleIn(t *tracked, v chunkView, dim int, x, z, rng float64) bool {
	if t.dim != dim {
		return false
	}
	r := math.Min(rng, float64(v.trackingDistance())*16)
	dx, dz := t.x-x, t.z-z
	if dx*dx+dz*dz > r*r {
		return false
	}
	return v.holds(int32(dim), int32(chunkFloor(x)), int32(chunkFloor(z)))
}

// mobTrackRange is TrackedEntity.getEffectiveRange for a mob: its type's
// range, or a passenger's if that is longer — a player riding a horse
// carries the horse out to the player's own 32 chunks.
func (h *hub) mobTrackRange(m *mob) float64 {
	r := trackRange(m.etype)
	if m.rider != 0 || m.rider2 != 0 || len(m.riders) > 0 {
		r = math.Max(r, trackRange(playerEntityType))
	}
	if p := h.mobs[m.mobRider]; m.mobRider != 0 && p != nil {
		r = math.Max(r, trackRange(p.etype))
	}
	return r
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
		v := t.p.lockView()
		for eid, o := range players {
			if eid != t.p.eid && playerVisibleIn(t, v, o) {
				want[eid] = true
			}
		}
		for _, m := range h.mobs {
			if m != h.dragon && visibleIn(t, v, m.dim, m.x, m.z, h.mobTrackRange(m)) {
				want[m.eid] = true
			}
		}
		itemR, orbR := trackRange(entityItem), trackRange(entityXPOrb)
		for _, it := range h.items {
			if visibleIn(t, v, it.dim, it.x, it.z, itemR) {
				want[it.eid] = true
			}
		}
		for _, o := range h.orbs {
			if visibleIn(t, v, o.dim, o.x, o.z, orbR) {
				want[o.eid] = true
			}
		}
		t.p.unlockView()
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
			if o := players[eid]; o != nil {
				h.showPlayerTo(t, o)
				continue
			}
			h.showEntityTo(t, eid)
		}
	}
}

// playerVisibleIn is TrackedEntity.updatePlayer for a player's body: the
// same horizontal range test as any entity (a player's clientTrackingRange is
// 32 chunks, so in practice the viewer's render distance decides), then
// ServerPlayer.broadcastToPlayer — a spectating viewer sees every player not
// looking through another entity's eyes, and anyone else sees no spectator.
func playerVisibleIn(v *tracked, view chunkView, o *tracked) bool {
	// A player's own range (32 chunks) is the longest there is, so no
	// passenger can lengthen it: getEffectiveRange is just the type's.
	if !visibleIn(v, view, o.dim, o.x, o.z, trackRange(playerEntityType)) {
		return false
	}
	if v.gamemode == gmSpectator {
		return o.camera == 0
	}
	return o.gamemode != gmSpectator
}

// showPlayerTo is ServerEntity.sendPairingData for another player's body: the
// spawn, then everything a client would otherwise only learn when it next
// changed — what they hold and wear, their attributes, the flags byte
// (crouch, sprint, swim, glide, fire, invisibility, glow), the crouch pose,
// sleep, shoulder parrots, effect swirls, and the seat they ride in.
func (h *hub) showPlayerTo(v, o *tracked) {
	v.p.sendEvReliable(bundleOpen{})
	defer v.p.sendEvReliable(bundleClose{})
	v.p.trySendEv(entAdd(o.p.eid, playerEntityType, o.p.uuid, o.x, o.y, o.z, o.yaw, o.pitch))
	v.p.trySendEv(equipEv(o.p.eid, heldStack(o), o.offhand, o.armor))
	sendAttrsTo(v, playerAttrFrame(o))
	if playerEntityFlags(o) != 0 {
		v.p.trySendEv(metaEv(playerFlagsMeta(o)))
	}
	if o.sneaking && !o.flying {
		v.p.trySendEv(metaEv(poseMeta(o.p.eid, poseSneaking)))
	}
	if o.sleeping {
		v.p.trySendEv(metaEv(sleepMetadata(o.p.eid, o.sleepPos)))
	}
	if o.shoulderOccupied() {
		v.p.trySendEv(metaEv(shoulderMeta(o)))
	}
	if len(o.effects) > 0 {
		v.p.trySendEv(metaEv(effectSwirlMeta(o.p.eid, o.effects)))
	}
	if o.ridingEID != 0 { // Entity.isPassenger: the vehicle's passenger list
		if m := h.mobs[o.ridingEID]; m != nil && v.tracked[m.eid] {
			v.p.trySendEv(passengersBody(m.eid, m.playerPassengers()...))
		} else if veh := h.vehicles[o.ridingEID]; veh != nil {
			v.p.trySendEv(passengersBody(veh.eid, veh.passengers()...))
		}
	}
}

// untrackPlayer takes a player's body out of every viewer's set — they left,
// or changed dimension — sending the removal to those still holding it.
func (h *hub) untrackPlayer(players map[int32]*tracked, eid int32) {
	for _, v := range players {
		if v.tracked[eid] {
			delete(v.tracked, eid)
			v.p.trySendEv(entGone(eid))
		}
	}
}

// toPlayerViewers sends a frame about a player's body to the viewers whose
// clients are holding it.
func toPlayerViewers(players map[int32]*tracked, eid int32, ev any) {
	for _, v := range players {
		if v.tracked[eid] {
			v.p.trySendEv(ev)
		}
	}
}

// showEntityTo sends one entity's spawn and the state vanilla sends with it.
func (h *hub) showEntityTo(t *tracked, eid int32) {
	switch kind, ok := h.trackable(eid); {
	case !ok:
		delete(t.tracked, eid) // it went in the same tick — nothing to show
	case kind == trackMob:
		// ServerEntity.sendPairingData: the spawn and its state in one bundle.
		t.p.sendEvReliable(bundleOpen{})
		h.showMobTo(t, h.mobs[eid])
		t.p.sendEvReliable(bundleClose{})
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
	if m.ticksFrozen > 0 {
		t.p.trySendEv(metaEv(frozenMetadata(m.eid, m.ticksFrozen)))
	}
	if m.wearsAnything() || m.showTrades.showing {
		t.p.trySendEv(equipEv(m.eid, m.handShown(), invStack{}, m.gear))
	} else if m.etype == entitySkeleton {
		t.p.trySendEv(skeletonEquip(m.eid))
	}
	if m.baby {
		t.p.trySendEv(metaEv(mobBabyMeta(m, true)))
	}
	if m.celebrating {
		t.p.trySendEv(metaEv(boolMeta(m.eid, metaIndexRaiderCelebrating, true)))
	}
	if m.sheared {
		t.p.trySendEv(metaEv(sheepMeta(m, true)))
	}
	if m.etype == entitySulfurCube {
		// Its own fields at their 26.x indices (size 18, not the slime's
		// canonical 16), and the block it has swallowed.
		t.p.trySendEv(metaEv(sulfurMeta(m)))
		if m.hasBody() {
			t.p.trySendEv(cubeBodyEquip(m))
		}
	} else if m.size > 0 {
		t.p.trySendEv(metaEv(slimeMeta(m.eid, m.size)))
	}
	if vm := variantMeta(m); vm != nil {
		t.p.trySendEv(metaEv(vm))
	}
	if sm := speciesStateMeta(m); sm != nil { // a goat's horns, a turtle's egg
		t.p.trySendEv(metaEv(sm))
	}
	if nv, ok := nautilusVariantEv(m); ok { // the coral zombie nautilus
		t.p.trySendEv(nv)
	}
	if m.etype == entitySniffer && m.sniffState != sniffIdling { // mid-sniff or mid-dig
		t.p.trySendEv(metaEv(snifferStateMeta(m)))
	}
	if m.health > 0 && m.health != m.maxHP() { // hurt: its health (cracks, hearts)
		t.p.trySendEv(metaEv(mobHealthMeta(m.eid, m.health)))
	}
	if len(m.effects) > 0 { // its effect swirls
		t.p.trySendEv(metaEv(effectSwirlMeta(m.eid, m.effects)))
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
		t.p.trySendEv(passengersBody(m.eid, m.playerPassengers()...))
	}
	if len(m.riders) > 0 {
		t.p.trySendEv(passengersBody(m.eid, m.riders...))
	}
	if m.mobRider != 0 {
		t.p.trySendEv(passengersBody(m.eid, m.mobPassengers()...))
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
	if _, isPlayer := players[eid]; isPlayer {
		toPlayerViewers(players, eid, ev)
		return
	}
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

// bundleOpen / bundleClose bracket a spawn's frames (ClientboundBundlePacket).
// Both go on the reliable path, so an opened bundle is always closed.
type bundleOpen struct{}
type bundleClose struct{}
