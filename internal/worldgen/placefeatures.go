package worldgen

// /place feature for the configured features the engine's decoration
// already grows, beyond trees and huge mushrooms: the overworld ores and
// stone blobs (OreFeature), the disks (DiskFeature), the springs
// (SpringFeature), the monster room (MonsterRoomFeature), the amethyst
// geode (GeodeFeature) and the deep dark's sculk patch and sculk vein. Each
// runs once at the command's origin on one random stream, and is stamped
// into every chunk it can reach the way generation stamps it. The features
// that plan against a view of the area (the room, the geode, the sculk)
// read the live world through it, as the ores, disks and springs read the
// live chunks they are stamped into.

// FeatureStamp is a configured feature /place grows at an origin.
type FeatureStamp struct {
	X0, Z0, X1, Z1 int // the block columns it may reach (inclusive)
	// Stamp writes the feature's blocks that fall in one chunk.
	Stamp    func(ch *Chunk, cx, cz int32)
	Chests   []LootChest
	Spawners []StructureSpawner
}

// placeOreCfgs are the overworld ore features by configured id
// (OreFeatures.bootstrap).
var placeOreCfgs = map[string]oreCfg{
	"ore_dirt": blobOf(Dirt, 33), "ore_gravel": blobOf(Gravel, 33), "ore_clay": blobOf(Clay, 33),
	"ore_granite": blobOf(stoneGranite, 64), "ore_diorite": blobOf(stoneDiorite, 64),
	"ore_andesite": blobOf(stoneAndesite, 64), "ore_tuff": blobOf(stoneTuff, 64),
	"ore_coal": oreOf(CoalOre, DeepslateCoalOre, 17, 0), "ore_coal_buried": oreOf(CoalOre, DeepslateCoalOre, 17, 0.5),
	"ore_iron": oreOf(IronOre, DeepslateIronOre, 9, 0), "ore_iron_small": oreOf(IronOre, DeepslateIronOre, 4, 0),
	"ore_gold": oreOf(GoldOre, DeepslateGoldOre, 9, 0), "ore_gold_buried": oreOf(GoldOre, DeepslateGoldOre, 9, 0.5),
	"ore_redstone":      oreOf(RedstoneOre, DeepslateRedstoneOre, 8, 0),
	"ore_diamond_small": oreOf(DiamondOre, DeepslateDiamondOre, 4, 0.5), "ore_diamond_medium": oreOf(DiamondOre, DeepslateDiamondOre, 8, 0.5),
	"ore_diamond_large": oreOf(DiamondOre, DeepslateDiamondOre, 12, 0.7), "ore_diamond_buried": oreOf(DiamondOre, DeepslateDiamondOre, 8, 1.0),
	"ore_lapis": oreOf(LapisOre, DeepslateLapisOre, 7, 0), "ore_lapis_buried": oreOf(LapisOre, DeepslateLapisOre, 7, 1.0),
	"ore_infested": oreOf(InfestedStone, InfestedDeepslate, 9, 0), "ore_emerald": oreOf(EmeraldOre, DeepslateEmeraldOre, 3, 0),
	"ore_copper_small": oreOf(CopperOre, DeepslateCopperOre, 10, 0), "ore_copper_large": oreOf(CopperOre, DeepslateCopperOre, 20, 0),
}

// PlaceFeatureStamp grows a configured feature (vanilla's id, namespace
// optional) at (x, y, z) on the random stream seed names; live reads the
// world the planned features see. ok is false for a feature the engine does
// not grow this way. Whether the feature placed anything shows in what the
// stamps write.
func (g *Generator) PlaceFeatureStamp(name string, x, y, z int, seed int64, live func(x, y, z int) uint32) (FeatureStamp, bool) {
	name = trimNS(name)
	reach := func(n int) FeatureStamp { return FeatureStamp{X0: x - n, Z0: z - n, X1: x + n, Z1: z + n} }
	if cfg, ok := placeOreCfgs[name]; ok {
		st := reach(16)
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
			oreBlob(reg, nil, cfg, newTreeRNG(seed, x, z), x, y, z, MinY+len(ch.Sections)*16, seed, 0)
		}
		return st, true
	}
	switch name {
	case "disk_sand", "disk_clay", "disk_gravel", "disk_grass":
		var spec diskSpec
		switch name {
		case "disk_grass":
			spec = grassDisk
		default:
			for _, d := range diskFeatures() {
				if (name == "disk_sand" && d.block == Sand) || (name == "disk_clay" && d.block == Clay) || (name == "disk_gravel" && d.block == Gravel) {
					spec = d
				}
			}
		}
		r := newTreeRNG(seed, x, z)
		radius := spec.radiusLo + r.Intn(spec.radiusHi-spec.radiusLo+1)
		st := reach(radius)
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			baseX, baseZ := int(cx)*16, int(cz)*16
			inChunk := func(px, pz int) bool { return px >= baseX && px < baseX+16 && pz >= baseZ && pz < baseZ+16 }
			at := func(px, py, pz int) uint32 { return sectionBlockAt(ch, px-baseX, py, pz-baseZ) }
			put := func(px, py, pz int, s uint32) { setSectionBlock(ch, px-baseX, py, pz-baseZ, s, true) }
			g.placeDisk(spec, radius, x, y, z, inChunk, at, put)
		}
		return st, true
	case "spring_water", "spring_lava_overworld", "spring_lava_frozen":
		fluid, valid := Water, func(s uint32) bool { return springStone(s, false) }
		switch name {
		case "spring_lava_overworld":
			fluid, valid = Lava, func(s uint32) bool { return springStone(s, true) }
		case "spring_lava_frozen":
			fluid, valid = Lava, frozenSpringStone
		}
		st := reach(0)
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
			g.springAt(reg, x, y, z, fluid, valid, nil, nil)
		}
		return st, true
	case "monster_room":
		view := &owRegion{g: g, baseX: x &^ 15, baseZ: z &^ 15, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}, under: live}
		d, ok := monsterRoom(view, newTreeRNG(seed, x, z), x, y, z)
		if !ok {
			return FeatureStamp{X0: x, Z0: z, X1: x, Z1: z, Stamp: func(*Chunk, int32, int32) {}}, true
		}
		st := cellsStamp(d.cells, x, z)
		for _, c := range d.Chests {
			st.Chests = append(st.Chests, LootChest{c[0], c[1], c[2], "chests/simple_dungeon"})
		}
		mobs := [3]string{"minecraft:zombie", "minecraft:skeleton", "minecraft:spider"}
		st.Spawners = []StructureSpawner{{d.X, d.Y, d.Z, mobs[d.Mob%3]}}
		return st, true
	case "amethyst_geode":
		p := g.geodeAt(newTreeRNG(seed, x, z), int32(x>>4), int32(z>>4), x, y, z, false, live)
		if p == nil {
			return FeatureStamp{X0: x, Z0: z, X1: x, Z1: z, Stamp: func(*Chunk, int32, int32) {}}, true
		}
		return cellsStamp(p.cells, x, z), true
	case "sculk_patch_deep_dark", "sculk_vein":
		view := &owRegion{g: g, baseX: x &^ 15, baseZ: z &^ 15, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}, under: live}
		w := &sculkWorld{view: view, orig: map[[3]int]uint32{}}
		r := newTreeRNG(seed, x, z)
		if name == "sculk_vein" {
			w.vein([3]int{x, y, z}, r)
		} else {
			w.patch([3]int{x, y, z}, r)
		}
		cells := map[[3]int]uint32{}
		for c, o := range w.orig {
			if s := view.capture[c]; s != o {
				cells[c] = s
			}
		}
		return cellsStamp(cells, x, z), true
	}
	return FeatureStamp{}, false
}

// PlaceFeatureNames are the features PlaceFeatureStamp grows.
func PlaceFeatureNames() []string {
	out := []string{"disk_sand", "disk_clay", "disk_gravel", "disk_grass", "spring_water", "spring_lava_overworld",
		"spring_lava_frozen", "monster_room", "amethyst_geode", "sculk_patch_deep_dark", "sculk_vein"}
	for n := range placeOreCfgs {
		out = append(out, n)
	}
	return out
}

// cellsStamp writes a planned set of cells, each chunk its own; with none
// it covers the origin's column alone.
func cellsStamp(cells map[[3]int]uint32, x, z int) FeatureStamp {
	st := FeatureStamp{X0: 1 << 30, Z0: 1 << 30, X1: -(1 << 30), Z1: -(1 << 30)}
	for c := range cells {
		st.X0, st.Z0 = min(st.X0, c[0]), min(st.Z0, c[2])
		st.X1, st.Z1 = max(st.X1, c[0]), max(st.Z1, c[2])
	}
	if len(cells) == 0 {
		st.X0, st.Z0, st.X1, st.Z1 = x, z, x, z
	}
	st.Stamp = func(ch *Chunk, cx, cz int32) {
		baseX, baseZ := int(cx)*16, int(cz)*16
		for c, s := range cells {
			if lx, lz := c[0]-baseX, c[2]-baseZ; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
				setSectionBlock(ch, lx, c[1], lz, s, true)
			}
		}
	}
	return st
}
