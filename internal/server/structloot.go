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
	if p := g.RuinedPortalIn(pos.x, pos.z); p.Exists && pos.x == p.ChestX && pos.y == p.ChestY && pos.z == p.ChestZ {
		return "chests/ruined_portal", true
	}
	if p := g.OutpostIn(pos.x, pos.z); p.Exists && pos.x == p.ChestX && pos.y == p.ChestY && pos.z == p.ChestZ {
		return "chests/pillager_outpost", true
	}
	if a := g.AncientCityIn(pos.x, pos.z); a.Exists && pos.x == a.ChestX && pos.y == a.ChestY && pos.z == a.ChestZ {
		return "chests/ancient_city", true
	}
	if s := g.ShipwreckIn(pos.x, pos.z); s.Exists {
		for i := 0; i < s.N; i++ {
			if c := s.Chests[i]; pos.x == c[0] && pos.y == c[1] && pos.z == c[2] {
				return []string{"chests/shipwreck_supply", "chests/shipwreck_treasure", "chests/shipwreck_map"}[c[3]], true
			}
		}
	}
	if b := g.BuriedTreasureIn(pos.x, pos.z); b.Exists && pos.x == b.X && pos.y == b.Y && pos.z == b.Z {
		return "chests/buried_treasure", true
	}
	if v := g.VillageIn(pos.x, pos.z); v.Exists {
		for _, house := range v.Houses {
			if cx, cy, cz := g.HouseChest(house); pos.x == cx && pos.y == cy && pos.z == cz {
				return villageChestTable(g.HouseWorkstation(house)), true
			}
		}
	}
	return "", false
}

// villageChestTable maps a house's workstation block to its vanilla chest loot
// table; a house with no dedicated profession table falls back to the plains
// house table.
func villageChestTable(workstation uint32) string {
	if name, ok := villageTableByWorkstation[workstation]; ok {
		return name
	}
	return "chests/village/village_plains_house"
}

// villageTableByWorkstation is keyed by the worldgen workstation state ids
// (worldgen/village.go workstations table).
var villageTableByWorkstation = map[uint32]string{
	19432: "chests/village/village_fisher",       // barrel
	19459: "chests/village/village_cartographer", // cartography_table
	19489: "chests/village/village_toolsmith",    // smithing_table
	19460: "chests/village/village_fletcher",     // fletching_table
	19427: "chests/village/village_shepherd",     // loom
	19490: "chests/village/village_mason",        // stonecutter
	19465: "chests/village/village_weaponsmith",  // grindstone
	19452: "chests/village/village_armorer",      // blast_furnace
	19444: "chests/village/village_butcher",      // smoker
	8181:  "chests/village/village_temple",       // brewing_stand (cleric)
}
