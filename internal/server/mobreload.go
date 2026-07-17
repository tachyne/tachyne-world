package server

import "encoding/hex"

// Mob reconstruction from persistence (see mobstore.go + mobchunks.go). Mobs load
// and unload with their chunk, so reloadMob is driven by reconcileMobChunks as
// chunks enter range — there is no boot-time bulk restore.

// persistMob reports whether a live mob should be written to mobs.json. It keeps
// every non-dying mob this pod owns except the bosses and LLM NPCs (villager-
// bodied, but their identity lives in the npc registry + memory files — they
// stay resident and are respawned by their own system).
func (h *hub) persistMob(m *mob) bool {
	if m == nil || m.dying > 0 || m == h.dragon {
		return false
	}
	if m.etype == entityWither {
		return false
	}
	if _, isNPC := h.npcs[m.eid]; isNPC {
		return false
	}
	return h.ownedAt(m.x, m.z)
}

// reloadMob rebuilds one mob: route through the normal spawn setup (so behaviour,
// stance and species statics are correct) with no players present, then overwrite
// the persisted per-instance state on top.
func (h *hub) reloadMob(players map[int32]*tracked, sm *savedMob) *mob {
	x, y, z := sm.X, sm.Y, sm.Z
	var m *mob
	if sm.Hostile {
		m = h.spawnHostileY(players, sm.Etype, x, y, z) // hostile stance + per-species quirks
	} else {
		m = h.spawnMob(players, sm.Etype, x, y, z)
		h.applySpecies(players, m) // roster stance/quirks (no-op for the legacy animals)
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
	m.home, m.bed, m.work, m.meet = unpackPos(sm.Home), unpackPos(sm.Bed), unpackPos(sm.Work), unpackPos(sm.Meet)
	switch m.etype {
	case entityVillager:
		// The village-population stance (updateVillages) — spawnMob alone
		// leaves a villager as a generic grazer.
		m.behavior, m.usesDoors, m.speed = villagerBehavior{}, true, 0.135
		m.profession = sm.Profession % len(professionNames)
		if m.profession < 0 {
			m.profession = 0
		}
		m.tradeLevel = max(1, sm.TradeLevel)
		m.tradeXP = sm.TradeXP
		m.offers = nil
		for _, o := range sm.Offers {
			m.offers = append(m.offers, unpackOffer(o))
		}
		if len(m.offers) == 0 { // pre-v2.1 row (or a fresh one): deal tier-1 stock
			h.unlockTier(m, 1)
		}
	case entityIronGolem:
		m.behavior, m.noKB = golemBehavior{}, true // village-guardian stance
	}
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
