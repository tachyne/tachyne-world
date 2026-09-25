package server

// charged_creeper/* — the only way to get a mob's head in survival. A
// charged creeper's blast makes whatever it kills drop its own head: a
// creeper, a skeleton, a wither skeleton, a zombie or a piglin. Vanilla
// hangs this off the blast rather than the victim's own loot table, which
// is why one charged creeper gives one head at most (Creeper.killedEntity
// sets droppedSkulls once a head comes out).

// chargedHeadFor is the head a species drops to a charged creeper's blast
// (0 = it has none).
func chargedHeadFor(etype int) int32 {
	name := ""
	switch etype {
	case entityCreeper:
		name = "creeper_head"
	case entitySkeleton:
		name = "skeleton_skull"
	case entityWitherSkeleton:
		name = "wither_skeleton_skull"
	case entityZombie:
		name = "zombie_head"
	case entityPiglin:
		name = "piglin_head"
	}
	if name == "" {
		return 0
	}
	return itemByName[name]
}

// chargedHeadDrop is Creeper.killedEntity for a victim of a charged
// creeper's blast: while the creeper has dropped no head yet and mob loot
// is on, the victim rolls charged_creeper/root, which gives the five
// species their head (babies included: the tables ask only the type).
func (h *hub) chargedHeadDrop(players map[int32]*tracked, m *mob) {
	item := chargedHeadFor(m.etype)
	if item == 0 || h.blastSkullDropped || !h.rules.DoMobLoot {
		return
	}
	h.spawnItemIn(players, m.dim, item, 1, m.x, m.y, m.z) // spawnAtLocation, no offset
	h.blastSkullDropped = true
}
