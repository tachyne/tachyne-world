package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Brewing: nether wart farming, water bottles, the brewing stand, and
// drinkable potions. Potions are potion items carrying a server-side type
// (invStack.potion) and a custom name — no potion_contents component on the
// wire (its id shifts per client version like stored_enchantments did; the
// name is already chain-remapped, so this is version-proof). The liquid
// renders default purple; the label and the effect are real.

const (
	menuBrewing = 11

	brewTicks = 400 // vanilla 20s
)

var (
	brewStandMin = worldgen.BlockBase("brewing_stand") // has_bottle_0 × has_bottle_1 × has_bottle_2
	brewStandMax = worldgen.BlockBase("brewing_stand") + 7

	netherWartMin = worldgen.BlockBase("nether_wart") // + age 0..3
	netherWartMax = worldgen.BlockBase("nether_wart") + 3
)

var (
	itemNetherWart   = itemByName["nether_wart"]
	itemGlassBottle  = itemByName["glass_bottle"]
	itemPotion       = itemByName["potion"]
	itemSplashPotion = itemByName["splash_potion"]
	itemLingerPotion = itemByName["lingering_potion"]
	itemBlazePowder  = itemByName["blaze_powder"]
	itemGlisterMel   = itemByName["glistering_melon_slice"]
	itemSugarBrew    = itemByName["sugar"]
	itemGoldCarrot   = itemByName["golden_carrot"]
)

// Potion kinds (invStack.potion). The first nine ids predate the vanilla
// brewing port and are persisted in item rows, so they keep their numbers;
// the rest of vanilla's potion registry follows. A kind is a recipe of
// effects (potionDefs); the container item (potion / splash_potion /
// lingering_potion) says how it is delivered.
const (
	potNone = iota
	potWater
	potAwkward
	potSwiftness
	potStrength
	potHealing
	potPoison
	potFireRes
	potNightVision
	potMundane
	potThick
	potLongNightVision
	potInvisibility
	potLongInvisibility
	potLeaping
	potLongLeaping
	potStrongLeaping
	potLongFireRes
	potLongSwiftness
	potStrongSwiftness
	potSlowness
	potLongSlowness
	potStrongSlowness
	potTurtleMaster
	potLongTurtleMaster
	potStrongTurtleMaster
	potWaterBreathing
	potLongWaterBreathing
	potStrongHealing
	potHarming
	potStrongHarming
	potLongPoison
	potStrongPoison
	potRegen
	potLongRegen
	potStrongRegen
	potLongStrength
	potStrongStrength
	potWeakness
	potLongWeakness
	potLuck
	potSlowFalling
	potLongSlowFalling
	potWindCharged
	potWeaving
	potOozing
	potInfested
	potCount
)

// potionDef is one vanilla Potion: its display base name and effects
// (Potions.java; durations there are ticks, kept here in seconds).
type potionDef struct {
	label   string // "Swiftness" → "Potion of Swiftness"; "" = plain bottle names
	effects []potEffect
}

var potionDefs = map[int8]potionDef{
	potWater:              {label: "Water"},
	potMundane:            {label: "Mundane"},
	potThick:              {label: "Thick"},
	potAwkward:            {label: "Awkward"},
	potNightVision:        {"Night Vision", []potEffect{{effNightVision, 0, 180}}},
	potLongNightVision:    {"Night Vision", []potEffect{{effNightVision, 0, 480}}},
	potInvisibility:       {"Invisibility", []potEffect{{effInvisibility, 0, 180}}},
	potLongInvisibility:   {"Invisibility", []potEffect{{effInvisibility, 0, 480}}},
	potLeaping:            {"Leaping", []potEffect{{effJumpBoost, 0, 180}}},
	potLongLeaping:        {"Leaping", []potEffect{{effJumpBoost, 0, 480}}},
	potStrongLeaping:      {"Leaping", []potEffect{{effJumpBoost, 1, 90}}},
	potFireRes:            {"Fire Resistance", []potEffect{{effFireRes, 0, 180}}},
	potLongFireRes:        {"Fire Resistance", []potEffect{{effFireRes, 0, 480}}},
	potSwiftness:          {"Swiftness", []potEffect{{effSpeed, 0, 180}}},
	potLongSwiftness:      {"Swiftness", []potEffect{{effSpeed, 0, 480}}},
	potStrongSwiftness:    {"Swiftness", []potEffect{{effSpeed, 1, 90}}},
	potSlowness:           {"Slowness", []potEffect{{effSlowness, 0, 90}}},
	potLongSlowness:       {"Slowness", []potEffect{{effSlowness, 0, 240}}},
	potStrongSlowness:     {"Slowness", []potEffect{{effSlowness, 3, 20}}},
	potTurtleMaster:       {"the Turtle Master", []potEffect{{effSlowness, 3, 20}, {effResistance, 2, 20}}},
	potLongTurtleMaster:   {"the Turtle Master", []potEffect{{effSlowness, 3, 40}, {effResistance, 2, 40}}},
	potStrongTurtleMaster: {"the Turtle Master", []potEffect{{effSlowness, 5, 20}, {effResistance, 3, 20}}},
	potWaterBreathing:     {"Water Breathing", []potEffect{{effWaterBreathing, 0, 180}}},
	potLongWaterBreathing: {"Water Breathing", []potEffect{{effWaterBreathing, 0, 480}}},
	potHealing:            {"Healing", []potEffect{{effInstantHealth, 0, 0}}},
	potStrongHealing:      {"Healing", []potEffect{{effInstantHealth, 1, 0}}},
	potHarming:            {"Harming", []potEffect{{effInstantDamage, 0, 0}}},
	potStrongHarming:      {"Harming", []potEffect{{effInstantDamage, 1, 0}}},
	potPoison:             {"Poison", []potEffect{{effPoison, 0, 45}}},
	potLongPoison:         {"Poison", []potEffect{{effPoison, 0, 90}}},
	potStrongPoison:       {"Poison", []potEffect{{effPoison, 1, 21}}},
	potRegen:              {"Regeneration", []potEffect{{effRegen, 0, 45}}},
	potLongRegen:          {"Regeneration", []potEffect{{effRegen, 0, 90}}},
	potStrongRegen:        {"Regeneration", []potEffect{{effRegen, 1, 22}}},
	potStrength:           {"Strength", []potEffect{{effStrength, 0, 180}}},
	potLongStrength:       {"Strength", []potEffect{{effStrength, 0, 480}}},
	potStrongStrength:     {"Strength", []potEffect{{effStrength, 1, 90}}},
	potWeakness:           {"Weakness", []potEffect{{effWeakness, 0, 90}}},
	potLongWeakness:       {"Weakness", []potEffect{{effWeakness, 0, 240}}},
	potLuck:               {"Luck", []potEffect{{effLuck, 0, 300}}},
	potSlowFalling:        {"Slow Falling", []potEffect{{effSlowFalling, 0, 90}}},
	potLongSlowFalling:    {"Slow Falling", []potEffect{{effSlowFalling, 0, 240}}},
	potWindCharged:        {"Wind Charging", []potEffect{{effWindCharged, 0, 180}}},
	potWeaving:            {"Weaving", []potEffect{{effWeaving, 0, 180}}},
	potOozing:             {"Oozing", []potEffect{{effOozing, 0, 180}}},
	potInfested:           {"Infestation", []potEffect{{effInfested, 0, 180}}},
}

// potionName is the item's display name for a kind in a container: "Water
// Bottle" / "Splash Water Bottle", "Awkward Potion", "Potion of Swiftness",
// "Splash Potion of Harming", "Lingering Potion of Weakness".
func potionName(kind int8, container int32) string {
	d, ok := potionDefs[kind]
	if !ok {
		return ""
	}
	prefix := ""
	switch container {
	case itemSplashPotion:
		prefix = "Splash "
	case itemLingerPotion:
		prefix = "Lingering "
	}
	switch kind {
	case potWater:
		return prefix + "Water Bottle"
	case potMundane, potThick, potAwkward:
		return prefix + d.label + " Potion"
	}
	return prefix + "Potion of " + d.label
}

// potionNames keeps the plain-bottle names for the callers that want them.
var potionNames = func() map[int8]string {
	m := map[int8]string{}
	for k := range potionDefs {
		m[k] = potionName(k, itemPotion)
	}
	return m
}()

// brewMix is one PotionBrewing mix: a bottle of `from` plus `ingredient`
// brews `to`. Container recipes (gunpowder, dragon's breath) change the
// bottle item instead and keep the kind.
type brewMix struct {
	from       int8
	ingredient int32
	to         int8
}

var (
	brewMixes          []brewMix
	brewContainerMixes = map[int32]map[int32]int32{} // ingredient → from item → to item
)

func init() {
	item := func(name string) int32 { return itemByName[name] }
	mix := func(from int8, ing string, to int8) {
		if id := item(ing); id != 0 {
			brewMixes = append(brewMixes, brewMix{from, id, to})
		}
	}
	// addStartMix: water + ingredient → mundane, awkward + ingredient → potion.
	start := func(ing string, to int8) {
		mix(potWater, ing, potMundane)
		mix(potAwkward, ing, to)
	}
	// PotionBrewing.bootstrap, in vanilla's order.
	if gp, dr := item("gunpowder"), item("dragon_breath"); gp != 0 && dr != 0 {
		brewContainerMixes[gp] = map[int32]int32{itemPotion: itemSplashPotion}
		brewContainerMixes[dr] = map[int32]int32{itemSplashPotion: itemLingerPotion}
	}
	mix(potWater, "glowstone_dust", potThick)
	mix(potWater, "redstone", potMundane)
	mix(potWater, "nether_wart", potAwkward)
	start("breeze_rod", potWindCharged)
	start("slime_block", potOozing)
	start("stone", potInfested)
	start("cobweb", potWeaving)
	mix(potAwkward, "golden_carrot", potNightVision)
	mix(potNightVision, "redstone", potLongNightVision)
	mix(potNightVision, "fermented_spider_eye", potInvisibility)
	mix(potLongNightVision, "fermented_spider_eye", potLongInvisibility)
	mix(potInvisibility, "redstone", potLongInvisibility)
	start("magma_cream", potFireRes)
	mix(potFireRes, "redstone", potLongFireRes)
	start("rabbit_foot", potLeaping)
	mix(potLeaping, "redstone", potLongLeaping)
	mix(potLeaping, "glowstone_dust", potStrongLeaping)
	mix(potLeaping, "fermented_spider_eye", potSlowness)
	mix(potLongLeaping, "fermented_spider_eye", potLongSlowness)
	mix(potSlowness, "redstone", potLongSlowness)
	mix(potSlowness, "glowstone_dust", potStrongSlowness)
	mix(potAwkward, "turtle_helmet", potTurtleMaster)
	mix(potTurtleMaster, "redstone", potLongTurtleMaster)
	mix(potTurtleMaster, "glowstone_dust", potStrongTurtleMaster)
	mix(potSwiftness, "fermented_spider_eye", potSlowness)
	mix(potLongSwiftness, "fermented_spider_eye", potLongSlowness)
	start("sugar", potSwiftness)
	mix(potSwiftness, "redstone", potLongSwiftness)
	mix(potSwiftness, "glowstone_dust", potStrongSwiftness)
	mix(potAwkward, "pufferfish", potWaterBreathing)
	mix(potWaterBreathing, "redstone", potLongWaterBreathing)
	start("glistering_melon_slice", potHealing)
	mix(potHealing, "glowstone_dust", potStrongHealing)
	mix(potHealing, "fermented_spider_eye", potHarming)
	mix(potStrongHealing, "fermented_spider_eye", potStrongHarming)
	mix(potHarming, "glowstone_dust", potStrongHarming)
	mix(potPoison, "fermented_spider_eye", potHarming)
	mix(potLongPoison, "fermented_spider_eye", potHarming)
	mix(potStrongPoison, "fermented_spider_eye", potStrongHarming)
	start("spider_eye", potPoison)
	mix(potPoison, "redstone", potLongPoison)
	mix(potPoison, "glowstone_dust", potStrongPoison)
	start("ghast_tear", potRegen)
	mix(potRegen, "redstone", potLongRegen)
	mix(potRegen, "glowstone_dust", potStrongRegen)
	start("blaze_powder", potStrength)
	mix(potStrength, "redstone", potLongStrength)
	mix(potStrength, "glowstone_dust", potStrongStrength)
	mix(potWater, "fermented_spider_eye", potWeakness)
	mix(potWeakness, "redstone", potLongWeakness)
	mix(potAwkward, "phantom_membrane", potSlowFalling)
	mix(potSlowFalling, "redstone", potLongSlowFalling)
}

// brewOne is PotionBrewing.mix for a single bottle: the stack it becomes
// with this ingredient, or ok=false when nothing applies. Container mixes
// come first (gunpowder turns any potion into its splash form, whatever the
// kind); then the kind mixes, which apply in any container.
func brewOne(bottle invStack, ingredient int32) (invStack, bool) {
	if bottle.item != itemPotion && bottle.item != itemSplashPotion && bottle.item != itemLingerPotion {
		return invStack{}, false
	}
	if to, ok := brewContainerMixes[ingredient][bottle.item]; ok {
		return potionStackIn(to, bottle.potion), true
	}
	for _, m := range brewMixes {
		if m.ingredient == ingredient && m.from == bottle.potion {
			return potionStackIn(bottle.item, m.to), true
		}
	}
	return invStack{}, false
}

// potionStackIn is a potion kind in a given container item, named.
func potionStackIn(container int32, kind int8) invStack {
	return invStack{item: container, count: 1, potion: kind, name: potionName(kind, container)}
}

func isBrewStand(s uint32) bool  { return s >= brewStandMin && s <= brewStandMax }
func isNetherWart(s uint32) bool { return s >= netherWartMin && s <= netherWartMax }

// potionStack builds a named potion item.
func potionStack(p int8) invStack { return potionStackIn(itemPotion, p) }

// updateBrewing runs once a second: any brewing stand with a valid batch
// makes progress; at brewTicks the bottles transform and the ingredient +
// one blaze-powder fuel are consumed.
func (h *hub) updateBrewing(players map[int32]*tracked) {
	for pos, b := range h.bins {
		w := h.worldFor(pos.dim)
		if len(b.slots) != 5 || w == nil || !isBrewStand(w.At(pos.x, pos.y, pos.z)) {
			continue
		}
		outs, ok := brewResult(b)
		if !ok {
			delete(h.brewProg, pos)
			continue
		}
		// Fuel gate: a blaze powder grants 20 brews (vanilla FUEL_USES). Need a
		// remaining charge, or a powder in the fuel slot to burn for a new one.
		if h.brewFuel[pos] <= 0 && (b.slots[4].item != itemBlazePowder || b.slots[4].count == 0) {
			delete(h.brewProg, pos)
			continue
		}
		h.brewProg[pos] += survivalTickN
		if h.brewProg[pos] < brewTicks {
			continue
		}
		delete(h.brewProg, pos)
		for i := 0; i < 3; i++ {
			if b.slots[i].item != 0 {
				b.slots[i] = outs[i]
			}
		}
		b.slots[3].count--
		if b.slots[3].count <= 0 {
			b.slots[3] = invStack{}
		}
		// Burn one fuel charge; refill from a blaze powder (20 charges) when empty.
		if h.brewFuel[pos] <= 0 {
			b.slots[4].count--
			if b.slots[4].count <= 0 {
				b.slots[4] = invStack{}
			}
			h.brewFuel[pos] = 20
		}
		h.brewFuel[pos]--
		h.playSound(players, "minecraft:block.brewing_stand.brew", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.6, 1)
		h.refreshBinViewers(players, pos)
		for _, t := range players {
			// vanilla fires brewed_potion on taking the potion; the taker is
			// anonymous in our generic window path, so credit the players
			// standing at the open stand when the brew completes.
			if t.winID != 0 && t.winPos == pos {
				h.advance(players, t, "brewed_potion", advMatch{})
			}
		}
	}
}

// brewResult: what the current stand contents brew into (ok=false → idle).
// brewResult is BrewingStandBlockEntity.isBrewable + doBrew: each bottle
// brews on its own against the ingredient; the stand runs when at least one
// bottle has a recipe, and bottles without one are left as they are.
func brewResult(b *bin) (out [3]invStack, ok bool) {
	ing := b.slots[3]
	if ing.item == 0 || ing.count == 0 { // fuel is checked separately in updateBrewing (20 uses/powder)
		return out, false
	}
	for i := 0; i < 3; i++ {
		s := b.slots[i]
		if s.item == 0 {
			continue
		}
		if res, brews := brewOne(s, ing.item); brews {
			out[i], ok = res, true
		} else {
			out[i] = s
		}
	}
	return out, ok
}

// potEffect is one effect a potion carries: effect id, 0-based amplifier, and
// the base duration in seconds (0 = an instant effect like Healing).
type potEffect struct {
	id   int32
	amp  int
	secs int
}

// potionEffects is the single source of truth for what each potion kind does —
// shared by drink, splash, and lingering so their tunings never drift.
func potionEffects(kind int8) []potEffect { return potionDefs[kind].effects }

// drinkPotion applies a potion's effect and hands back the glass bottle.
func (h *hub) drinkPotion(players map[int32]*tracked, t *tracked, slot int) {
	s := &t.inv.slots[slot]
	p := s.potion
	*s = invStack{item: itemGlassBottle, count: 1}
	h.sendSlot(t, slot)
	for _, e := range potionEffects(p) {
		h.applyEffect(players, t, e.id, e.amp, e.secs) // instant effects apply at secs 0
	}
	h.playSound(players, "minecraft:entity.generic.drink", sndPlayer, t.x, t.y, t.z, 0.6, 1)
}

// fillBottle turns a held glass bottle into a water bottle (right-click water).
func (h *hub) fillBottle(t *tracked, slot int32) {
	if t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	s := &t.inv.slots[slot]
	if s.item != itemGlassBottle || s.count == 0 {
		return
	}
	s.count--
	if s.count == 0 {
		*s = invStack{}
	}
	wb := potionStack(potWater)
	if changed, left := t.inv.addStack(wb); left == 0 {
		for _, sl := range changed {
			h.sendSlot(t, sl)
		}
	}
	h.sendSlot(t, int(slot))
}

type evFillBottle struct {
	eid  int32
	slot int32
}

func (evFillBottle) isHubEvent() {}

// tickWart ports NetherWartBlock.randomTick: age < 3 && nextInt(10) == 0.
//
// This used to be a SCHEDULED tick that rearmed itself 2400-7200 ticks out,
// which ignored the randomTickSpeed gamerule entirely (including 0, which must
// stop growth dead) and ran about 3x too fast — a block is random-ticked once
// per ~1365 ticks, so vanilla averages ~13650 ticks a stage against ~4800.
func (h *hub) tickWart(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isNetherWart(state) {
		return false
	}
	if state < netherWartMax && h.rng.Intn(10) == 0 {
		h.setBlockAt(players, dim, blockPos{x, y, z}, state+1)
	}
	return true
}
