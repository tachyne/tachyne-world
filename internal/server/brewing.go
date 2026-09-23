package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Brewing: nether wart farming, water bottles, the brewing stand, and
// drinkable potions. Potions are potion items carrying a server-side type
// (invStack.potion) and a custom name — no potion_contents component on the
// wire (its id shifts per client version like stored_enchantments did; the
// name is already chain-remapped, so this is version-proof). The liquid
// renders default purple; the label and the effect are real.

const (
	menuBrewing = 11

	brewTicks    = 400 // vanilla 20s
	brewFuelUses = 20  // BrewingStandBlockEntity.FUEL_USES: one powder, twenty brews
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

// updateBrewing is BrewingStandBlockEntity.serverTick on our one-second
// cadence: a blaze powder in the fuel slot is burnt straight away for twenty
// charges, a brewable stand spends one of them to start a twenty-second brew,
// the brew is abandoned if the ingredient changes under it, and the bottle
// flags on the block state (and the menu's two bars) follow along so the
// stand actually looks like it is working.
func (h *hub) updateBrewing(players map[int32]*tracked) {
	for pos, b := range h.bins {
		w := h.worldFor(pos.dim)
		if len(b.slots) != 5 || w == nil {
			continue
		}
		if !w.Ticking(int32(pos.x>>4), int32(pos.z>>4)) {
			continue // a stand brews only in a loaded chunk, as vanilla's does
		}
		state := w.At(pos.x, pos.y, pos.z)
		if !isBrewStand(state) {
			continue
		}
		// A powder is consumed on sight, brewing or not — that is why a stand
		// swallows the powder the moment you drop it in.
		if h.brewFuel[pos] <= 0 && b.slots[4].item == itemBlazePowder && b.slots[4].count > 0 {
			h.brewFuel[pos] = brewFuelUses
			if b.slots[4].count--; b.slots[4].count <= 0 {
				b.slots[4] = invStack{}
			}
			h.refreshBinViewers(players, pos)
		}
		outs, brewable := brewResult(b)
		switch {
		case h.brewProg[pos] > 0:
			h.brewProg[pos] -= survivalTickN
			switch {
			case h.brewProg[pos] <= 0 && brewable:
				delete(h.brewProg, pos)
				h.finishBrew(players, pos, b, outs)
			case !brewable || b.slots[3].item != h.brewIng[pos]:
				// The ingredient was taken out or swapped: the brew is lost.
				delete(h.brewProg, pos)
				delete(h.brewIng, pos)
			}
		case brewable && h.brewFuel[pos] > 0:
			h.brewFuel[pos]--
			h.brewProg[pos] = brewTicks
			h.brewIng[pos] = b.slots[3].item
		}
		h.brewBottleState(players, pos, state, b)
		h.sendBrewBars(players, pos)
	}
}

// finishBrew turns the bottles, eats one ingredient and rings the stand.
func (h *hub) finishBrew(players map[int32]*tracked, pos simPos, b *bin, outs [3]invStack) {
	for i := 0; i < 3; i++ {
		if b.slots[i].item != 0 {
			b.slots[i] = outs[i]
		}
	}
	if b.slots[3].count--; b.slots[3].count <= 0 {
		b.slots[3] = invStack{}
	}
	delete(h.brewIng, pos)
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

// brewBottleState keeps the stand's has_bottle_N properties in step with what
// is in the three bottle slots — the arms on the model.
func (h *hub) brewBottleState(players map[int32]*tracked, pos simPos, state uint32, b *bin) {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("has_bottle_0") {
		return
	}
	want := state
	for i := 0; i < 3; i++ {
		v := "false"
		if b.slots[i].item != 0 && b.slots[i].count > 0 {
			v = "true"
		}
		want = worldgen.SetProperty(info, want, "has_bottle_"+string(rune('0'+i)), v)
	}
	if want != state {
		h.setBlockAt(players, pos.dim, pos.blockPos, want)
	}
}

// sendBrewBars pushes BrewingStandMenu's two data slots (brew time left and
// fuel charges) to whoever has the stand open. Without them the bubbles never
// move and the fuel gauge reads empty however much powder went in.
func (h *hub) sendBrewBars(players map[int32]*tracked, pos simPos) {
	for _, t := range players {
		if t.winID == 0 || t.winKind != winBin || t.winPos != pos {
			continue
		}
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(h.brewProg[pos])})
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 1, Value: int32(h.brewFuel[pos])})
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
	id  int32
	amp int
	// ticks is the duration vanilla stores. It was whole seconds once, which
	// quietly rounded strong poison (432 ticks) and strong regeneration (450)
	// down to the nearest second.
	ticks int
}

// effectIsInstant is MobEffect.isInstantenous: healing and harming land in
// one go rather than running for a duration. Vanilla gives them a one-tick
// instance, so the duration cannot be what tells them apart.
func effectIsInstant(id int32) bool {
	return id == effInstantHealth || id == effInstantDamage
}

// potionEffects is the single source of truth for what each potion kind does —
// shared by drink, splash, and lingering so their tunings never drift.
func potionEffects(kind int8) []potEffect { return potionDefs[kind].effects }

// drinkPotion applies a potion's effect and hands back the glass bottle.
func (h *hub) drinkPotion(players map[int32]*tracked, t *tracked, slot int) {
	s := t.handStack(slot)
	if s == nil {
		return
	}
	p := s.potion
	h.vibAt(t.dim, freqDrink, t.x, t.y, t.z, t.p.eid)
	*s = invStack{item: itemGlassBottle, count: 1}
	h.sendHandSlot(t, slot)
	for _, e := range potionEffects(p) {
		h.applyEffectTicks(players, t, e.id, e.amp, e.ticks) // instant effects apply at secs 0
	}
	h.playSound(players, "minecraft:entity.generic.drink", sndPlayer, t.x, t.y, t.z, 0.6, 1)
}

// fillBottle turns a held glass bottle into a water bottle (right-click water).
// fillBottle is BottleItem.use. The dragon's breath comes first: any living
// area-effect cloud the dragon left within two blocks of the player fills the
// bottle with it, and the cloud gives up half a block of radius for the
// trouble. Failing that the player's look ray is walked out to a water SOURCE
// — flowing water will not do — and the bottle comes back as water.
//
// Without the first branch dragon's breath is unobtainable, and with it the
// whole lingering-potion half of brewing: everything downstream of it was
// already built and simply had no way to start.
func (h *hub) fillBottle(players map[int32]*tracked, t *tracked, slot int32) {
	if t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	if s := &t.inv.slots[slot]; s.item != itemGlassBottle || s.count == 0 {
		return
	}
	if c := h.dragonBreathNear(t); c != nil {
		c.radius -= 0.5
		if c.radius <= 0 {
			delete(h.clouds, c.eid)
			h.entityGone(players, c.dim, c.eid)
		}
		h.playSound(players, "minecraft:item.bottle.fill_dragonbreath", sndNeutral, t.x, t.y, t.z, 1, 1)
		h.vibAt(t.dim, freqFluidPickup, t.x, t.y, t.z, t.p.eid)
		h.turnBottleInto(t, slot, invStack{item: int32(itemByName["dragon_breath"]), count: 1})
		return
	}
	if !h.waterSourceInSight(t) {
		return
	}
	h.playSound(players, "minecraft:item.bottle.fill", sndNeutral, t.x, t.y, t.z, 1, 1)
	h.vibAt(t.dim, freqFluidPickup, t.x, t.y, t.z, t.p.eid)
	h.turnBottleInto(t, slot, potionStack(potWater))
}

// dragonBreathNear is the AreaEffectCloud search BottleItem.use opens with:
// a live cloud the DRAGON owns, inside the player's box grown by two.
func (h *hub) dragonBreathNear(t *tracked) *effectCloud {
	for _, c := range h.clouds {
		if !c.breath || c.dim != t.dim || c.ttl <= 0 {
			continue
		}
		if math.Abs(c.x-t.x) <= 2+c.radius && math.Abs(c.z-t.z) <= 2+c.radius &&
			math.Abs(c.y-t.y) <= 2+playerEyeHeightStand {
			return c
		}
	}
	return nil
}

// bottleFillReach is Player.blockInteractionRange, which is what
// Item.getPlayerPOVHitResult clips to.
const bottleFillReach = 4.5

// waterSourceInSight walks the look ray for a water SOURCE, which is what
// ClipContext.Fluid.SOURCE_ONLY means: a bottle cannot be filled from the
// flowing edge of a stream, and a solid block stops the ray.
func (h *hub) waterSourceInSight(t *tracked) bool {
	var found bool
	h.lookRay(t, bottleFillReach, func(_ blockPos, st uint32) bool {
		if st == worldgen.WaterBase {
			found = true
			return true
		}
		return rayStopsAt(st) // something solid before any water ends it
	})
	return found
}

// turnBottleInto is Item.turnBottleIntoItem: one bottle out of the stack, the
// filled thing in, and on the floor if there is nowhere for it.
func (h *hub) turnBottleInto(t *tracked, slot int32, filled invStack) {
	s := &t.inv.slots[slot]
	s.count--
	if s.count == 0 {
		*s = invStack{}
	}
	if changed, left := t.inv.addStack(filled); left == 0 {
		for _, sl := range changed {
			h.sendSlot(t, sl)
		}
	} else {
		h.spawnItemIn(h.playersRef, t.dim, filled.item, left, t.x, t.y, t.z)
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
