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

	brewTicks    = 400  // vanilla 20s
	wartGrowMin  = 2400 // 2-6 min per growth stage (overworld farms)
	wartGrowSpan = 4800
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

// Potion types (invStack.potion).
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
)

var potionNames = map[int8]string{
	potWater: "Water Bottle", potAwkward: "Awkward Potion",
	potSwiftness: "Potion of Swiftness", potStrength: "Potion of Strength",
	potHealing: "Potion of Healing", potPoison: "Potion of Poison",
	potFireRes: "Potion of Fire Resistance", potNightVision: "Potion of Night Vision",
}

// brewIngredient maps an ingredient item to the awkward→final transition.
var brewIngredient = map[int32]int8{
	itemBlazePowder: potStrength,
	itemSugarBrew:   potSwiftness,
	itemGlisterMel:  potHealing,
	itemSpiderEye:   potPoison,
	itemMagmaCream:  potFireRes,
	itemGoldCarrot:  potNightVision,
}

func isBrewStand(s uint32) bool  { return s >= brewStandMin && s <= brewStandMax }
func isNetherWart(s uint32) bool { return s >= netherWartMin && s <= netherWartMax }

// potionStack builds a named potion item.
func potionStack(p int8) invStack {
	return invStack{item: itemPotion, count: 1, potion: p, name: potionNames[p]}
}

// updateBrewing runs once a second: any brewing stand with a valid batch
// makes progress; at brewTicks the bottles transform and the ingredient +
// one blaze-powder fuel are consumed.
func (h *hub) updateBrewing(players map[int32]*tracked) {
	for pos, b := range h.bins {
		if len(b.slots) != 5 || !isBrewStand(h.world.At(pos.x, pos.y, pos.z)) {
			continue
		}
		out, ok := brewResult(b)
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
			if b.slots[i].item == itemPotion {
				b.slots[i] = potionStack(out)
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
func brewResult(b *bin) (int8, bool) {
	ing := b.slots[3]
	if ing.item == 0 { // fuel is checked separately in updateBrewing (20 uses/powder)
		return 0, false
	}
	// All bottles present must agree on the input potion stage.
	stage := int8(potNone)
	for i := 0; i < 3; i++ {
		s := b.slots[i]
		if s.item == 0 {
			continue
		}
		if s.item != itemPotion {
			return 0, false
		}
		if stage == potNone {
			stage = s.potion
		} else if stage != s.potion {
			return 0, false
		}
	}
	if stage == potNone {
		return 0, false
	}
	if stage == potWater && ing.item == itemNetherWart {
		return potAwkward, true
	}
	if stage == potAwkward {
		if out, ok := brewIngredient[ing.item]; ok {
			return out, true
		}
	}
	return 0, false
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
func potionEffects(kind int8) []potEffect {
	switch kind {
	case potSwiftness:
		return []potEffect{{effSpeed, 0, 180}}
	case potStrength:
		return []potEffect{{effStrength, 0, 180}}
	case potHealing:
		return []potEffect{{effInstantHealth, 0, 0}}
	case potPoison:
		return []potEffect{{effPoison, 0, 45}}
	case potFireRes:
		return []potEffect{{effFireRes, 0, 180}}
	case potNightVision:
		return []potEffect{{effNightVision, 0, 180}}
	}
	return nil
}

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

// updateWart is the scheduled growth step (overworld farms only — the block
// simulation is dimension-0).
func (h *hub) updateWart(players map[int32]*tracked, pos blockPos, state uint32) {
	if state < netherWartMax { // age 0-2: grow one stage and rearm
		h.setBlock(players, pos, state+1)
		if state+1 < netherWartMax {
			h.schedule(pos, uint64(wartGrowMin+h.rng.Intn(wartGrowSpan)))
		}
	}
}
