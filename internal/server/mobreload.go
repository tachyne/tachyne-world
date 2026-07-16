package server

import (
	"encoding/hex"
	"log"
)

// Boot-time reconstruction of persisted mobs (see mobstore.go). Runs once at the
// top of hub.run(), before the tick loop and before any player joins, so the
// restored mobs are simply present when players stream in.

// persistMob reports whether a live mob should be written to mobs.json. v1 keeps
// every non-dying mob this pod owns except villagers (trade state deferred) and
// the bosses; LLM NPCs live in a separate registry and are never in h.mobs.
func (h *hub) persistMob(m *mob) bool {
	if m == nil || m.dying > 0 || m == h.dragon {
		return false
	}
	switch m.etype {
	case entityVillager, entityWither:
		return false
	}
	return h.ownedAt(m.x, m.z)
}

// loadMobs reconstructs every persisted mob into h.mobs. The reloading guard
// suppresses MobSpawnEvent (these are restorations, not fresh spawns).
func (h *hub) loadMobs() {
	if h.mobstore == nil {
		return
	}
	if h.seededChunks == nil {
		h.seededChunks = map[[2]int32]bool{} // so restored herds mark their chunks seeded
	}
	h.reloading = true
	n := 0
	for _, sm := range h.mobstore.saved() {
		if h.reloadMob(&sm) != nil {
			n++
		}
	}
	h.reloading = false
	if n > 0 {
		log.Printf("restored %d mobs from persistence", n)
	}
}

// reloadMob rebuilds one mob: route through the normal spawn setup (so behaviour,
// stance and species statics are correct) with no players present, then overwrite
// the persisted per-instance state on top.
func (h *hub) reloadMob(sm *savedMob) *mob {
	empty := map[int32]*tracked{}
	x, y, z := sm.X, sm.Y, sm.Z
	var m *mob
	if sm.Hostile {
		m = h.spawnHostileY(empty, sm.Etype, x, y, z) // hostile stance + per-species quirks
	} else {
		m = h.spawnMob(empty, sm.Etype, x, y, z)
		h.applySpecies(empty, m) // roster stance/quirks (no-op for the legacy animals)
	}
	if m == nil {
		return nil // plugin-cancelled or unknown species
	}
	m.dim = sm.Dim
	m.yaw, m.syaw = sm.Yaw, sm.Yaw
	if sm.Health > 0 {
		m.health = sm.Health
	}
	if sm.Max > 0 {
		m.maxHealth = sm.Max
	}
	m.dmgFrac = sm.DmgFrac
	m.baby, m.growLeft = sm.Baby, sm.GrowLeft
	m.loveTicks, m.breedCD = sm.LoveTicks, sm.BreedCD
	m.sheared, m.eggIn = sm.Sheared, sm.EggIn
	if sm.Size > 0 {
		m.size = sm.Size
	}
	m.anger, m.neutral, m.patrolCaptain = sm.Anger, sm.Neutral, sm.PatrolCaptain
	m.oxidation, m.waxed, m.carrying = sm.Oxidation, sm.Waxed, unpackStack(sm.Carrying)
	m.trident, m.canPickup = sm.Trident, sm.CanPickup
	for i := range m.gear {
		m.gear[i] = unpackStack(sm.Gear[i])
	}
	m.saddled = sm.Saddled
	m.saddleSt, m.armorSt = unpackStack(sm.SaddleSt), unpackStack(sm.ArmorSt)
	m.chested, m.strength = sm.Chested, sm.Strength
	if len(sm.Chest) > 0 {
		m.chest = make([]invStack, 0, len(sm.Chest))
		for _, c := range sm.Chest {
			m.chest = append(m.chest, unpackStack(c))
		}
	}
	if sm.Held != 0 {
		m.held = sm.Held
	}
	m.harness = sm.Harness
	m.tamed, m.sitting = sm.Tamed, sm.Sitting
	if sm.OwnerUUID != "" {
		if b, err := hex.DecodeString(sm.OwnerUUID); err == nil && len(b) == 16 {
			copy(m.ownerUUID[:], b) // owner eid re-resolves when that player joins
		}
	}
	m.ovrSpeed, m.ovrDamage = sm.OvrSpeed, sm.OvrDamage
	m.x, m.y, m.z, m.sx, m.sy, m.sz = x, y, z, x, y, z // seat the broadcast baseline at the load position
	// Mark the restored mob's chunk seeded so the vanilla spawner does not lay a
	// second chunk-generation herd on top of it.
	if h.seededChunks != nil {
		h.seededChunks[[2]int32{int32(chunkFloor(x)), int32(chunkFloor(z))}] = true
	}
	return m
}

// resolvePetOwners re-links a joining player to any restored pets they own (the
// persisted owner eid was discarded — pets carry the owner's stable UUID).
func (h *hub) resolvePetOwners(t *tracked) {
	for _, m := range h.mobs {
		if m.tamed && m.owner == 0 && m.ownerUUID != ([16]byte{}) && m.ownerUUID == t.p.uuid {
			m.owner = t.p.eid
		}
	}
}
