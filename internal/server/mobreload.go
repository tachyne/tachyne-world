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
	if m == nil || m.dying > 0 {
		return false
	}
	if m.etype == entityEnderDragon || m.etype == entityWither {
		return false // bosses: the fight's dragon is staged by the End itself, a summoned one is an event, not a resident
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
	// A zombie nautilus saved while the engine still ran it as a monster
	// comes back as the animal it is.
	if sm.Hostile && sm.Etype != entityZombieNautilus {
		m = h.spawnHostileYIn(players, sm.Etype, sm.Dim, x, y, z) // hostile stance + per-species quirks
	} else {
		m = h.spawnMobIn(players, sm.Etype, sm.Dim, x, y, z)
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
		m.setMaxHP(sm.Max)
	}
	m.dmgFrac = sm.DmgFrac
	m.baby, m.growLeft = sm.Baby, sm.GrowLeft
	m.jockey, m.trap = sm.Jockey, sm.Trap
	m.savedMount, m.mountDrives = sm.Mount, sm.MountDrives
	m.refreshBabySpeed() // the spawn roll may have set a different flag
	m.loveTicks, m.breedCD = sm.LoveTicks, sm.BreedCD
	m.sheared, m.eggIn = sm.Sheared, sm.EggIn
	m.color, m.customName, m.fromBucket = sm.Color, sm.CustomName, sm.FromBucket
	m.tags = tagSet(sm.Tags)
	m.collar = sm.Collar
	m.stew = sm.Stew
	m.persistent = sm.Persistent
	m.raidCenter = unpackPos(sm.Raid)
	m.raidWave = sm.RaidWave
	if sm.RestrictR > 0 {
		m.homePos, m.homeR = blockPos{sm.Restrict[0], sm.Restrict[1], sm.Restrict[2]}, sm.RestrictR
	}
	if sm.Charged {
		m.charged = true
	}
	h.raiderReloaded(m)
	if sm.Variant > 0 {
		m.variant, m.variantSet = sm.Variant-1, true
	}
	if m.etype == entityVillager || m.etype == entityZombieVillager {
		h.villagerType(m) // villagers saved before clothes were synced dress by their biome
	}
	if sm.Size > 0 {
		m.size = sm.Size
		m.applyCubeSize() // health/speed/damage/armour all follow a cube's size
		if sm.Health > 0 {
			m.health = sm.Health // …but the saved health wins over the full reset
		}
	}
	m.anger, m.neutral, m.patrolCaptain = sm.Anger, sm.Neutral, sm.PatrolCaptain
	m.carriedBlock = sm.CarriedBlk
	m.oxidation, m.waxed, m.carrying = sm.Oxidation, sm.Waxed, unpackStack(sm.Carrying)
	m.trident, m.canPickup = sm.Trident, sm.CanPickup
	for i := range m.gear {
		m.gear[i] = unpackStack(sm.Gear[i])
	}
	m.refreshGearArmor() // saved gear protects again after a restart — it used not to
	m.saddled = sm.Saddled
	m.saddleSt, m.armorSt = unpackStack(sm.SaddleSt), unpackStack(sm.ArmorSt)
	m.carry, m.dupCD, m.sniffCD = unpackStack(sm.Carry), sm.DupCD, sm.SniffCD
	if m.etype == entitySulfurCube {
		m.cube.maxFuse = -1
		if sm.CubeBody != nil {
			h.setCubeBody(players, m, unpackStack(*sm.CubeBody)) // the archetype's modifiers go back on
		}
		if sm.CubeFuse > 0 {
			m.cube.lit, m.cube.fuse, m.cube.maxFuse = true, sm.CubeFuse-1, sm.CubeMaxFuse
		}
		m.cube.pickup = sm.CubePickup
	}
	for _, r := range sm.Hoard {
		m.hoard = append(m.hoard, unpackStack(r))
	}
	m.chested = sm.Chested
	if sm.Strength > 0 { // a row without one keeps the spawn roll (llamas)
		m.strength = sm.Strength
	}
	if len(sm.Chest) > 0 {
		m.chest = make([]invStack, 0, len(sm.Chest))
		for _, c := range sm.Chest {
			m.chest = append(m.chest, unpackStack(c))
		}
	}
	if sm.Held != 0 {
		m.held = sm.Held
		if sm.HeldSt[0] == sm.Held {
			st := unpackStack(sm.HeldSt)
			m.heldEnch, m.heldDmg, m.heldCount = st.ench, st.dmg, st.count
		}
	}
	m.gearSure = sm.GearSure
	if m.patrolCaptain && m.gear[0].item == 0 {
		// A captain saved before the banner was worn as gear: it goes back
		// on its head, and still always drops.
		m.gear[0] = invStack{item: itemByName["white_banner"], count: 1}
		m.gearSure[0] = true
	}
	m.harness = sm.Harness
	m.tamed, m.sitting = sm.Tamed, sm.Sitting
	if m.tamed && tameable(m.etype) && !nautilusKind(m.etype) {
		petStance(m) // a pet comes back following its owner, not wandering off
	}
	if sm.OwnerUUID != "" {
		if b, err := hex.DecodeString(sm.OwnerUUID); err == nil && len(b) == 16 {
			copy(m.ownerUUID[:], b)                  // owner eid re-resolves when that player joins
			m.ownerUUID = ids.remapUUID(m.ownerUUID) // uuidmap.json: the owner moved to a new UUID
		}
	}
	h.restoreLeash(players, m, sm.LeashPos)    // re-tie to its fence knot, rebuilding it
	if horseFamily(m.etype) && sm.HSpeed > 0 { // the rolled horse survives a restart as itself
		m.setMoveSpeed(sm.HSpeed)
		m.setJumpStrength(sm.HJump)
	}
	m.ovrSpeed, m.ovrDamage = sm.OvrSpeed, sm.OvrDamage
	m.hasEgg = sm.HasEgg
	m.screaming, m.hornsGone = sm.Screaming, sm.HornsGone
	m.soundSet, m.lastSlept = sm.SoundSet, sm.LastSlept
	m.breaksDoors = sm.BreaksDoors
	m.poseTick = sm.PoseTick
	m.ravStunTick, m.ravRoarTick = sm.RavStun, sm.RavRoar
	m.overworldTicks, m.immuneZombify = sm.Overworld, sm.ImmuneZombify
	m.noHunt = sm.NoHunt
	if m.etype == entityPiglin {
		// HUNTED_RECENTLY is not carried across a reload: a reloaded piglin
		// waits a fresh 30–120 seconds, as a new one does, before it hunts.
		m.huntedUntil = h.tick.Load() + uint64(piglinHuntMin+h.rng.Intn(piglinHuntSpan))
	}
	m.traderDespawn = sm.TraderDespawn
	if sm.WanderTarget != nil {
		m.traderWander, m.traderWandering = unpackPos(*sm.WanderTarget), true
	}
	m.endermiteLife = sm.Lifetime
	m.tadpoleAge = sm.TadpoleAge
	for i, n := range sm.Trusted {
		if i < 2 {
			m.trusted[i] = n
		}
	}
	if m.ovrSpeed > 0 {
		m.setMoveSpeed(m.ovrSpeed)
	}
	if m.ovrDamage > 0 {
		m.setAttackDamage(m.ovrDamage)
	}
	m.home, m.bed, m.work, m.meet = unpackPos(sm.Home), unpackPos(sm.Bed), unpackPos(sm.Work), unpackPos(sm.Meet)
	switch m.etype {
	case entityVillager, entityZombieVillager:
		if m.etype == entityVillager {
			// The village-population stance (updateVillages) — spawnMob alone
			// leaves a villager as a generic grazer.
			m.behavior, m.usesDoors = villagerBehavior{}, true
			m.setMoveSpeed(0.135)
		} else {
			m.converting, m.curer = sm.Converting, sm.Curer
			if m.converting > 0 {
				m.persistent = true
			}
		}
		m.profession = sm.Profession
		if m.profession >= len(professionNames) {
			m.profession %= len(professionNames)
		}
		m.vFood = sm.Food
		if len(sm.Gossip) > 0 {
			m.gossip = gossipBook{}
			for k, v := range sm.Gossip {
				m.gossip[k] = v
			}
		}
		if m.profession < 0 && m.profession != profNitwit {
			m.profession = profUnemployed // born or fired: no workstation yet
		}
		m.tradeLevel = max(1, sm.TradeLevel)
		m.tradeXP = sm.TradeXP
		// The daily restock budget persists: without it a reloaded villager
		// got two fresh restocks every time the world came back.
		m.restocksToday, m.lastRestockTick = sm.Restocks, sm.LastStock
		m.offers = nil
		for _, o := range sm.Offers {
			m.offers = append(m.offers, unpackOffer(o))
		}
		if len(m.offers) == 0 { // pre-v2.1 row (or a fresh one): deal tier-1 stock
			h.unlockTier(m, 1)
		}
	case entityIronGolem:
		m.behavior = golemBehavior{} // village-guardian stance
		m.setKBResist(1)
	case entityDrowned:
		if m.trident { // a saved trident drowned throws from ten blocks again
			m.behavior = holdRangedBehavior{radius: tridentRange}
		}
	}
	h.reassessWeapon(m)                                // …and a saved skeleton's goal follows what it holds
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
		if (m.tamed || m.etype == entityAllay) && m.owner == 0 && m.ownerUUID != ([16]byte{}) {
			if moved := ids.remapUUID(m.ownerUUID); moved == t.p.uuid {
				m.owner, m.ownerUUID = t.p.eid, moved // an owner whose UUID moved takes their pets along
			}
		}
	}
}
