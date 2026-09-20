package server

// charged_creeper/* — the only way to get a mob's head in survival. A
// charged creeper's blast makes whatever it kills drop its own head: a
// creeper, a skeleton, a wither skeleton, a zombie or a piglin. Vanilla
// hangs this off the blast rather than the victim's own loot table, which
// is why one charged creeper gives one head at most.

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

// chargedHeadDrop gives the victim of a charged creeper's blast its head.
// Called from the blast itself: vanilla marks the killed mob and the loot
// table reads the mark, which comes to the same thing.
func (h *hub) chargedHeadDrop(players map[int32]*tracked, m *mob) {
	item := chargedHeadFor(m.etype)
	if item == 0 || m.baby {
		return
	}
	h.spawnItemIn(players, m.dim, item, 1, m.x, m.y+0.5, m.z)
}
