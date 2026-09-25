package server

// Per-species food: what each animal eats from a player's hand. The sets are
// vanilla's #<mob>_food item tags; how a meal lands (courting, growing a
// baby, healing a pet, the horse family's heal/age table) is the species'
// mobInteract rule, reimplemented below.

// breedFoodNames widens the roster's single love item to the whole tag for
// the species whose tag holds more than one item (or that have no roster
// love at all). Species not listed eat exactly their roster love item.
var breedFoodNames = map[string][]string{
	"pig":     {"carrot", "potato", "beetroot"},
	"rabbit":  {"carrot", "golden_carrot", "dandelion"},
	"fox":     {"sweet_berries", "glow_berries"},
	"cat":     {"cod", "salmon"},
	"ocelot":  {"cod", "salmon"},
	"chicken": seedFoodNames,
	"parrot":  seedFoodNames, // tames only: parrots never breed
	"wolf": {"beef", "chicken", "cooked_beef", "cooked_chicken", "cooked_mutton", "cooked_porkchop",
		"cooked_rabbit", "mutton", "porkchop", "rabbit", "rotten_flesh", // #meat
		"cod", "cooked_cod", "salmon", "cooked_salmon", "tropical_fish", "pufferfish", "rabbit_stew"},
	// The horse family's love foods; wheat/sugar/apple/hay only heal and
	// grow (horseMeals). Mules never fall in love.
	"horse":   {"golden_carrot", "golden_apple", "enchanted_golden_apple"},
	"donkey":  {"golden_carrot", "golden_apple", "enchanted_golden_apple"},
	"axolotl": {"tropical_fish_bucket"},
	// #nautilus_food: #fishes and the fish buckets (a tamed adult's meals and
	// love foods; a baby's growth; taming itself takes a pufferfish, tame.go).
	"nautilus": {"cod", "cooked_cod", "salmon", "cooked_salmon", "pufferfish", "tropical_fish",
		"pufferfish_bucket", "cod_bucket", "salmon_bucket", "tropical_fish_bucket"},
	"happy_ghast": {"snowball"}, // grows a ghastling; adults never court
}

// seedFoodNames is #chicken_food / #parrot_food.
var seedFoodNames = []string{"wheat_seeds", "melon_seeds", "pumpkin_seeds", "beetroot_seeds", "torchflower_seeds", "pitcher_pod"}

// neverLoves are the species whose canFallInLove is false: food grows their
// babies but the adults ignore it.
var neverLoves = map[int]bool{entityHappyGhast: true}

// breedFoods is breedFoodNames resolved to item ids, keyed by entity type.
var breedFoods = func() map[int]map[int32]bool {
	out := map[int]map[int32]bool{}
	for name, items := range breedFoodNames {
		set := map[int32]bool{}
		for _, n := range items {
			set[itemByName[n]] = true
		}
		out[entityID(name)] = set
	}
	return out
}()

// isLoveFood reports whether item is in the species' food tag.
func isLoveFood(etype int, item int32) bool {
	if item == 0 {
		return false
	}
	if etype == entityBee {
		return isBeeFood(item) // #bee_food: any flower
	}
	if set := breedFoods[etype]; set != nil {
		return set[item]
	}
	return item == loveFood(etype)
}

// horseMeal is one row of AbstractHorse/Llama/Camel.handleEating: hearts
// healed, seconds a foal ages, the temper it adds, and whether a tamed adult
// falls in love.
type horseMeal struct {
	heal   int
	age    int
	temper int
	love   bool
}

// horseMeals is the handleEating table per family branch. Keyed by item name.
var horseMeals = map[string]map[string]horseMeal{
	"horse": { // #horse_food
		"wheat": {2, 20, 3, false}, "sugar": {1, 30, 3, false}, "hay_block": {20, 180, 0, false},
		"apple": {3, 60, 3, false}, "carrot": {3, 60, 3, false}, "golden_carrot": {4, 60, 5, true},
		"golden_apple": {10, 240, 10, true}, "enchanted_golden_apple": {10, 240, 10, true},
	},
	// #zombie_horse_food: a red mushroom heals and settles it, and never
	// ages a foal.
	"zombie_horse": {"red_mushroom": {3, 0, 3, false}},
	"llama":        {"wheat": {2, 10, 3, false}, "hay_block": {10, 90, 6, true}},
	"camel":        {"cactus": {2, 10, 0, true}},
	// #camel_husk_food. A husk never courts and is never a calf, so a
	// rabbit's foot only heals.
	"camel_husk": {"rabbit_foot": {2, 10, 0, false}},
}

// horseMealFor resolves the family branch and the held item to a meal row.
func horseMealFor(etype int, item int32) (horseMeal, bool) {
	var branch string
	switch etype {
	case entityHorse, entityDonkey, entityMule, entitySkeletonHorse:
		branch = "horse"
	case entityZombieHorse:
		branch = "zombie_horse"
	case entityLlama, entityTraderLlama:
		branch = "llama"
	case entityCamel:
		branch = "camel"
	case entityCamelHusk:
		branch = "camel_husk"
	default:
		return horseMeal{}, false
	}
	for name, meal := range horseMeals[branch] {
		if itemByName[name] == item {
			if meal.love && (etype == entityMule || etype == entitySkeletonHorse || etype == entityZombieHorse) {
				meal.love = false // canFallInLove is false for these
			}
			return meal, true
		}
	}
	return horseMeal{}, false
}

// horseMaxTemperOf is getMaxTemper: a llama comes round at 30, the rest at 100.
func horseMaxTemperOf(etype int) int {
	if isLlama(etype) {
		return 30
	}
	return horseMaxTemper
}

// isMobFood is "this species would eat what the player holds" — the gate
// that keeps a meal from being read as a mount/saddle click (Pig and
// AbstractHorse both test isFood before riding).
func isMobFood(etype int, item int32) bool {
	if _, ok := horseMealFor(etype, item); ok {
		return true
	}
	return isLoveFood(etype, item)
}

// eatSound is the species' eating sound (getEatingSound / playEatingSound).
func eatSound(etype int) string {
	switch etype {
	case entityHorse:
		return "minecraft:entity.horse.eat"
	case entityZombieHorse:
		return "minecraft:entity.zombie_horse.eat"
	case entityDonkey:
		return "minecraft:entity.donkey.eat"
	case entityMule:
		return "minecraft:entity.mule.eat"
	case entityLlama, entityTraderLlama:
		return "minecraft:entity.llama.eat"
	case entityCamel:
		return "minecraft:entity.camel.eat"
	case entityCamelHusk:
		return "minecraft:entity.camel_husk.eat"
	case entityCat:
		return "minecraft:entity.cat.eat"
	}
	return "minecraft:entity.generic.eat"
}

// feedHorse is the horse family's fedFood: heal when hurt, age a foal, and
// court an adult on the love rows — the item is spent only if something
// happened. Returns whether the click was consumed.
func (h *hub) feedHorse(players map[int32]*tracked, t *tracked, m *mob, item int32) bool {
	if m.etype == entitySkeletonHorse && !m.tamed {
		return false // SkeletonHorse.mobInteract: an untamed one ignores the player (PASS)
	}
	meal, ok := horseMealFor(m.etype, item)
	if !ok {
		return false
	}
	did := false
	// Only a tamed adult courts (a camel counts as tamed from birth).
	tamed := m.tamed || m.etype == entityCamel
	if meal.love && tamed && !m.baby && m.loveTicks == 0 && m.breedCD == 0 {
		h.setInLove(players, t, m)
		did = true
	}
	if meal.heal > 0 && m.health < m.maxHP() {
		h.healMob(m, meal.heal)
		did = true
	}
	// A skeleton horse never grows up (canAgeUp is false).
	if m.baby && meal.age > 0 && m.etype != entitySkeletonHorse {
		h.ageUp(m, meal.age*20)
		did = true
	}
	// handleEating's temper column: a wild mount takes the meal for its
	// temper alone, which is why an apple or two shortens the taming.
	if max := horseMaxTemperOf(m.etype); meal.temper > 0 && (did || !m.tamed) && m.temper < max {
		if m.temper += meal.temper; m.temper > max {
			m.temper = max
		}
		did = true
	}
	if !did {
		return false
	}
	if isSurvival(t.gamemode) {
		h.consumeHeld(t)
	}
	if m.etype != entitySkeletonHorse { // a skeleton horse eats in silence (no getEatingSound)
		h.playSoundDim(players, m.dim, eatSound(m.etype), sndNeutral, m.x, m.y, m.z, 1, 1+(h.rng.Float32()-h.rng.Float32())*0.2)
	}
	return true
}

// ageUp shortens a baby's remaining growth by ticks (AgeableMob.ageUp).
func (h *hub) ageUp(m *mob, ticks int) {
	if !m.baby {
		return
	}
	if m.growLeft -= ticks; m.growLeft < 1 {
		m.growLeft = 1 // the 1 Hz sweep matures it
	}
}

// setInLove starts courting (Animal.setInLove): love mode, the hearts
// status, and the player credited as the breeder.
func (h *hub) setInLove(players map[int32]*tracked, t *tracked, m *mob) {
	m.loveTicks = loveTicks
	m.lovedBy = t.p.eid
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, statusInLove))
}

// nautilusBucketFood is #nautilus_bucket_food: the fish buckets, which (like
// the axolotl's tropical fish) are eaten out of the bucket.
var nautilusBucketFood = map[int32]bool{
	itemByName["pufferfish_bucket"]: true, itemByName["cod_bucket"]: true,
	itemByName["salmon_bucket"]: true, itemByName["tropical_fish_bucket"]: true,
}

// consumeFed spends the held food; a fish bucket leaves the water behind
// (Axolotl and AbstractNautilus.usePlayerItem).
func (h *hub) consumeFed(t *tracked, item int32) {
	if !isSurvival(t.gamemode) {
		return
	}
	if nautilusBucketFood[item] {
		slot := t.p.heldSlot()
		t.inv.slots[slot] = invStack{item: itemByName["water_bucket"], count: 1}
		h.sendSlot(t, slot)
		return
	}
	h.consumeHeld(t)
}
