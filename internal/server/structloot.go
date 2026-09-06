package server

// Structure chest loot. A generated structure chest fills on its first open
// from the vanilla data-driven table (chestloot.go) that matches its position.
// The worldgen structure queries stay the oracle for "which structure owns
// this chest"; the roll itself is the shared evaluator, so dungeon/temple/
// portal/outpost chests carry real vanilla loot (weights, enchanted gear).

// fillStructureChest fills a first-opened structure chest if its position
// matches a generated structure's chest cell.
func (h *hub) fillStructureChest(pos blockPos, c *chest) {
	if name, ok := h.structureChestTable(pos); ok {
		h.fillChest(c, name, pos)
	}
}

// fillStructureChestIn is fillStructureChest for any dimension: the
// overworld's full table, the Nether's ruined portals (the same loot as the
// surface ones — vanilla's ruined_portal table serves every variant).
func (h *hub) fillStructureChestIn(dim int, pos blockPos, c *chest) {
	switch dim {
	case dimOverworld:
		h.fillStructureChest(pos, c)
	case dimNether:
		g := h.worldFor(dim).Gen()
		if p := g.RuinedPortalNetherIn(pos.x, pos.z); p.Exists {
			for _, cp := range p.Chests {
				if pos.x == cp[0] && pos.y == cp[1] && pos.z == cp[2] {
					h.fillChest(c, "chests/ruined_portal", pos)
					return
				}
			}
		}
		if b := g.BastionIn(pos.x, pos.z); b.Exists {
			for _, bc := range g.BastionChests(b) {
				if pos.x == bc.X && pos.y == bc.Y && pos.z == bc.Z {
					h.fillChest(c, bc.Table, pos)
					return
				}
			}
		}
	case dimEnd:
		g := h.worldFor(dim).Gen()
		if ec := g.EndCityIn(pos.x, pos.z); ec.Exists {
			for _, cc := range g.EndCityChests(ec) {
				if pos.x == cc.X && pos.y == cc.Y && pos.z == cc.Z {
					h.fillChest(c, cc.Table, pos)
					return
				}
			}
		}
	}
}

// structureChestTable reports the loot-table name for a structure chest at pos
// (empty/false when pos is not a known structure chest cell).
func (h *hub) structureChestTable(pos blockPos) (string, bool) {
	g := h.world.Gen()
	if d := g.DungeonIn(pos.x, pos.z); d.Exists && pos.x == d.ChestX && pos.y == d.Y && pos.z == d.ChestZ {
		return "chests/simple_dungeon", true
	}
	if t := g.DesertTempleIn(pos.x, pos.z); t.Exists {
		for _, ch := range t.Chests() {
			if pos.x == ch[0] && pos.y == ch[1] && pos.z == ch[2] {
				return "chests/desert_pyramid", true
			}
		}
	}
	if p := g.RuinedPortalIn(pos.x, pos.z); p.Exists {
		for _, c := range p.Chests {
			if pos.x == c[0] && pos.y == c[1] && pos.z == c[2] {
				return "chests/ruined_portal", true
			}
		}
	}
	if p := g.OutpostIn(pos.x, pos.z); p.Exists {
		for _, c := range g.OutpostChests(p) {
			if pos.x == c[0] && pos.y == c[1] && pos.z == c[2] {
				return "chests/pillager_outpost", true
			}
		}
	}
	if a := g.AncientCityIn(pos.x, pos.z); a.Exists {
		for _, c := range g.AncientCityChests(a) {
			if pos.x == c[0] && pos.y == c[1] && pos.z == c[2] {
				return "chests/ancient_city", true
			}
		}
	}
	if t := g.TrialChamberIn(pos.x, pos.z); t.Exists {
		for _, c := range g.TrialChamberChests(t) {
			if pos.x == c.X && pos.y == c.Y && pos.z == c.Z {
				return c.Table, true
			}
		}
	}
	if mn := g.MansionIn(pos.x, pos.z); mn.Exists {
		for _, c := range g.MansionChests(mn) {
			if pos.x == c[0] && pos.y == c[1] && pos.z == c[2] {
				return "chests/woodland_mansion", true
			}
		}
	}
	if s := g.ShipwreckIn(pos.x, pos.z); s.Exists {
		for _, c := range s.Chests {
			if pos.x == c.X && pos.y == c.Y && pos.z == c.Z {
				return c.Table, true
			}
		}
	}
	if b := g.BuriedTreasureIn(pos.x, pos.z); b.Exists && pos.x == b.X && pos.y == b.Y && pos.z == b.Z {
		return "chests/buried_treasure", true
	}
	if ig := g.IglooIn(pos.x, pos.z); ig.Exists && ig.Basement && pos.x == ig.ChestX && pos.y == ig.ChestY && pos.z == ig.ChestZ {
		return "chests/igloo_chest", true
	}
	if v := g.VillageIn(pos.x, pos.z); v.Exists {
		for _, c := range g.VillageChests(v) {
			if pos.x == c.X && pos.y == c.Y && pos.z == c.Z {
				return c.Table, true
			}
		}
	}
	return "", false
}
