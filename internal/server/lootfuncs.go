package server

import "github.com/tachyne/tachyne-common/attach"

// The loot functions that shape a chest item beyond count and enchantment:
// SetPotionFunction, SetNameFunction, SetInstrumentFunction,
// SetStewEffectFunction, SetOminousBottleAmplifierFunction and
// ExplorationMapFunction. Until 2026-09-19 the generator dropped them, so
// treasure maps were blank paper and chest potions were water.

// potionByVanillaName maps a vanilla potion registry name to the engine's
// potion id (brewing.go).
var potionByVanillaName = map[string]int8{
	"water": potWater, "mundane": potMundane, "thick": potThick, "awkward": potAwkward,
	"night_vision": potNightVision, "long_night_vision": potLongNightVision,
	"invisibility": potInvisibility, "long_invisibility": potLongInvisibility,
	"leaping": potLeaping, "long_leaping": potLongLeaping, "strong_leaping": potStrongLeaping,
	"fire_resistance": potFireRes, "long_fire_resistance": potLongFireRes,
	"swiftness": potSwiftness, "long_swiftness": potLongSwiftness, "strong_swiftness": potStrongSwiftness,
	"slowness": potSlowness, "long_slowness": potLongSlowness, "strong_slowness": potStrongSlowness,
	"turtle_master": potTurtleMaster, "long_turtle_master": potLongTurtleMaster, "strong_turtle_master": potStrongTurtleMaster,
	"water_breathing": potWaterBreathing, "long_water_breathing": potLongWaterBreathing,
	"healing": potHealing, "strong_healing": potStrongHealing,
	"harming": potHarming, "strong_harming": potStrongHarming,
	"poison": potPoison, "long_poison": potLongPoison, "strong_poison": potStrongPoison,
	"regeneration": potRegen, "long_regeneration": potLongRegen, "strong_regeneration": potStrongRegen,
	"strength": potStrength, "long_strength": potLongStrength, "strong_strength": potStrongStrength,
	"weakness": potWeakness, "long_weakness": potLongWeakness,
	"luck": potLuck, "slow_falling": potSlowFalling, "long_slow_falling": potLongSlowFalling,
	"wind_charged": potWindCharged, "weaving": potWeaving, "oozing": potOozing, "infested": potInfested,
}

// decorRedX is map decoration type red_x — the treasure map's cross.
const decorRedX = 26

// mapDecorationByName names the decoration types a loot table may ask for.
var mapDecorationByName = map[string]int32{"red_x": decorRedX, "target_x": 4, "target_point": 5, "mansion": 8, "monument": 9, "trial_chambers": 34}

// applyChestExtraFn applies the item-shaping loot functions to a chest stack.
func (h *hub) applyChestExtraFn(c *lootCtx, f *lootFn, st invStack) invStack {
	switch f.F {
	case "set_potion": // SetPotionFunction: the potion contents
		if id, ok := potionByVanillaName[f.Potion]; ok {
			if st.item == itemArrow { // an arrow with potion contents is a tipped arrow
				st.item = itemTippedArrow
			}
			st.potion = id
		}
	case "set_name": // SetNameFunction (item_name)
		st.name = f.Name
	case "set_instrument": // SetInstrumentFunction: a random horn of the kind
		st.instrument = int8(c.rng(4))
		if f.Options == "screaming" {
			st.instrument += 4
		}
	case "set_stew": // SetStewEffectFunction: one of the listed effects
		if len(f.Effects) > 0 {
			if eff, ok := effectNames[f.Effects[c.rng(len(f.Effects))]]; ok {
				for i, se := range stewEffects {
					if se.effect == eff {
						st.stew = int8(i + 1)
						break
					}
				}
			}
		}
	case "set_ominous": // SetOminousBottleAmplifierFunction: the bottle's level
		st.potion = int8(c.np(f.NP)) + 1
	case "set_trim":
		// SetComponentsFunction, and the only component a 1.21.11 loot table
		// sets: the armour trim on the trial chamber's equipment. Stored the
		// way the smithing table stores it, registry id + 1.
		if smithTrimmable[st.item] {
			st.trimMat, st.trimPat = int8(f.Mat+1), int8(f.Pat+1)
		}
	case "exploration_map": // ExplorationMapFunction: a map to the nearest treasure
		if !c.located || h.maps == nil || h.world == nil {
			break
		}
		x, z, ok := h.world.Gen().LocateStructure(f.Dest, c.pos.x, c.pos.z, treasureMapSearch)
		if !ok {
			break // vanilla hands out the plain map when nothing is in reach
		}
		md := h.maps.create(x, z, int8(f.Zoom), 0)
		typ, known := mapDecorationByName[f.Decoration]
		if !known {
			typ = decorRedX
		}
		md.Marks = append(md.Marks, mapMark{X: int32(x), Z: int32(z), Type: typ})
		h.maps.markDirty()
		st.item, st.mapID = itemFilledMap, md.ID
	}
	return st
}

// treasureMapSearch is ExplorationMapFunction's default search radius, 50
// chunks, in blocks.
const treasureMapSearch = 50 * 16

// mapMark is a fixed decoration on a map: the treasure map's cross.
type mapMark struct {
	X    int32 `json:"x"`
	Z    int32 `json:"z"`
	Type int32 `json:"type"`
}

// mapMarkDecorations renders a map's fixed marks.
func mapMarkDecorations(md *mapData) []attach.MapDecoration {
	if len(md.Marks) == 0 {
		return nil
	}
	scale := float64(int(1) << md.Scale)
	var out []attach.MapDecoration
	for _, m := range md.Marks {
		xd := (float64(m.X) + 0.5 - float64(md.CenterX)) / scale
		zd := (float64(m.Z) + 0.5 - float64(md.CenterZ)) / scale
		if xd < -63 || xd > 63 || zd < -63 || zd > 63 {
			continue
		}
		out = append(out, attach.MapDecoration{Type: m.Type, X: int8(xd*2 + 0.5), Z: int8(zd*2 + 0.5)})
	}
	return out
}
