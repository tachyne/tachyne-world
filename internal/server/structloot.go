package server

// Loot tables for the new overworld structures (desert temple, ruined portal),
// filled on a chest's first open. Item pools/weights follow vanilla's loot
// tables (facts from the wiki/datapack); rolls are seed-deterministic so a chest
// holds the same contents every time it's opened for the first time.

// desertTempleLoot fills one of a desert temple's four chamber chests.
func (h *hub) desertTempleLoot(pos blockPos, c *chest) {
	d := h.world.Gen().DesertTempleIn(pos.x, pos.z)
	if !d.Exists {
		return
	}
	hit := false
	for _, ch := range d.Chests() {
		if pos.x == ch[0] && pos.y == ch[1] && pos.z == ch[2] {
			hit = true
			break
		}
	}
	if !hit {
		return
	}
	seed := h.world.Seed()
	rollInto := func(name string, min, span, slot int, salt uint64) {
		id, ok := itemByName[name]
		if !ok {
			return
		}
		n := min + int(hash01ServerSeed(seed, pos.x+slot, pos.z, salt)*float64(span+1))
		if n > 0 {
			c.slots[slot] = invStack{item: id, count: n}
		}
	}
	rollInto("bone", 1, 5, 2, 0x71)
	rollInto("rotten_flesh", 1, 6, 5, 0x72)
	rollInto("gunpowder", 1, 6, 8, 0x73)
	rollInto("sand", 1, 7, 11, 0x74)
	rollInto("string", 1, 6, 14, 0x75)
	rollInto("gold_ingot", 1, 4, 17, 0x76) // the desert-temple gold
	chance := func(p float64, salt uint64) bool {
		return hash01ServerSeed(seed, pos.x, pos.z, salt) < p
	}
	if chance(0.30, 0x77) {
		rollInto("emerald", 1, 2, 4, 0x78)
	}
	if chance(0.20, 0x79) {
		rollInto("diamond", 1, 2, 13, 0x7A)
	}
	if chance(0.15, 0x7B) {
		rollInto("golden_apple", 1, 0, 22, 0x7C)
	}
	if chance(0.10, 0x7D) {
		rollInto("enchanted_golden_apple", 1, 0, 24, 0x7E)
	}
	if chance(0.25, 0x7F) {
		rollInto("iron_ingot", 1, 4, 20, 0x80)
	}
}

// ruinedPortalLoot fills a ruined portal's loot chest.
func (h *hub) ruinedPortalLoot(pos blockPos, c *chest) {
	p := h.world.Gen().RuinedPortalIn(pos.x, pos.z)
	if !p.Exists || pos.x != p.ChestX || pos.y != p.ChestY || pos.z != p.ChestZ {
		return
	}
	seed := h.world.Seed()
	rollInto := func(name string, min, span, slot int, salt uint64) {
		id, ok := itemByName[name]
		if !ok {
			return
		}
		n := min + int(hash01ServerSeed(seed, pos.x+slot, pos.z, salt)*float64(span+1))
		if n > 0 {
			c.slots[slot] = invStack{item: id, count: n}
		}
	}
	rollInto("obsidian", 1, 2, 2, 0x91)
	rollInto("flint", 1, 4, 5, 0x92)
	rollInto("iron_nugget", 4, 8, 8, 0x93)
	rollInto("gold_nugget", 4, 8, 11, 0x94)
	rollInto("fire_charge", 1, 1, 14, 0x95)
	chance := func(pr float64, salt uint64) bool {
		return hash01ServerSeed(seed, pos.x, pos.z, salt) < pr
	}
	if chance(0.30, 0x96) {
		rollInto("flint_and_steel", 1, 0, 17, 0x97)
	}
	if chance(0.20, 0x98) {
		rollInto("gold_ingot", 1, 2, 20, 0x99)
	}
	if chance(0.15, 0x9A) {
		rollInto("golden_apple", 1, 0, 22, 0x9B)
	}
	if chance(0.10, 0x9C) {
		rollInto("glowstone_dust", 1, 3, 24, 0x9D)
	}
}

// pillagerOutpostLoot fills a pillager outpost's watchtower chest. Pool follows
// vanilla's chests/pillager_outpost (crossbow, crops, dark oak, iron, arrows).
func (h *hub) pillagerOutpostLoot(pos blockPos, c *chest) {
	p := h.world.Gen().OutpostIn(pos.x, pos.z)
	if !p.Exists || pos.x != p.ChestX || pos.y != p.ChestY || pos.z != p.ChestZ {
		return
	}
	seed := h.world.Seed()
	rollInto := func(name string, min, span, slot int, salt uint64) {
		id, ok := itemByName[name]
		if !ok {
			return
		}
		n := min + int(hash01ServerSeed(seed, pos.x+slot, pos.z, salt)*float64(span+1))
		if n > 0 {
			c.slots[slot] = invStack{item: id, count: n}
		}
	}
	rollInto("arrow", 2, 6, 2, 0xB1)
	rollInto("wheat", 3, 5, 5, 0xB2)
	rollInto("potato", 2, 5, 8, 0xB3)
	rollInto("carrot", 2, 5, 11, 0xB4)
	rollInto("dark_oak_log", 2, 4, 14, 0xB5)
	chance := func(pr float64, salt uint64) bool {
		return hash01ServerSeed(seed, pos.x, pos.z, salt) < pr
	}
	if chance(0.35, 0xB6) {
		rollInto("crossbow", 1, 0, 17, 0xB7)
	}
	if chance(0.30, 0xB8) {
		rollInto("iron_ingot", 1, 4, 20, 0xB9)
	}
	if chance(0.25, 0xBA) {
		rollInto("tripwire_hook", 1, 2, 22, 0xBB)
	}
	if chance(0.20, 0xBC) {
		rollInto("experience_bottle", 1, 2, 24, 0xBD)
	}
	if chance(0.15, 0xBE) {
		rollInto("emerald", 1, 2, 26, 0xBF)
	}
}
