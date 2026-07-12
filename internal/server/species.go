package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The full creature roster. Every remaining 1.21.5 living species is defined
// here as DATA — attributes straight from vanilla's server source
// (createAttributes builders; melee = ATTACK_DAMAGE + held-weapon modifier,
// the sum vanilla's equipment attribute-modifiers produce at normal
// difficulty) — plus an archetype that maps the species onto our behavior
// primitives (wander/panic/breed, hostile chase, ranged kiting, water/flight
// movement modes, neutral retaliation). Species with signature mechanics
// beyond the archetype (poison bites, wither strikes, projectiles) carry the
// numbers here; the small amount of per-species CODE lives with its system.
//
// Entity type ids are minecraft:entity_type protocol ids (Mojang datagen
// registries.json, 1.21.5).

const (
	attrToStep = 0.45 // MOVEMENT_SPEED attribute → per-step blocks (cow-anchored)
	xpNone     = -1   // species that pay no experience (bats, golems, traders)
)

var (
	entityAllay           = entityID("allay")
	entityArmadillo       = entityID("armadillo")
	entityAxolotl         = entityID("axolotl")
	entityBat             = entityID("bat")
	entityBee             = entityID("bee")
	entityBogged          = entityID("bogged")
	entityBreeze          = entityID("breeze")
	entityCamel           = entityID("camel")
	entityCamelHusk       = entityID("camel_husk")
	entityCat             = entityID("cat")
	entityCaveSpider      = entityID("cave_spider")
	entityCod             = entityID("cod")
	entityCopperGolem     = entityID("copper_golem")
	entityCreaking        = entityID("creaking")
	entityDolphin         = entityID("dolphin")
	entityDonkey          = entityID("donkey")
	entityElderGuardian   = entityID("elder_guardian")
	entityEndermite       = entityID("endermite")
	entityEvoker          = entityID("evoker")
	entityFox             = entityID("fox")
	entityFrog            = entityID("frog")
	entityGhast           = entityID("ghast")
	entityGiant           = entityID("giant")
	entityGlowSquid       = entityID("glow_squid")
	entityGoat            = entityID("goat")
	entityGuardian        = entityID("guardian")
	entityHappyGhast      = entityID("happy_ghast")
	entityHoglin          = entityID("hoglin")
	entityHorse           = entityID("horse")
	entityIllusioner      = entityID("illusioner")
	entityLlama           = entityID("llama")
	entityMooshroom       = entityID("mooshroom")
	entityMule            = entityID("mule")
	entityNautilus        = entityID("nautilus")
	entityOcelot          = entityID("ocelot")
	entityPanda           = entityID("panda")
	entityParched         = entityID("parched")
	entityParrot          = entityID("parrot")
	entityPhantom         = entityID("phantom")
	entityPiglin          = entityID("piglin")
	entityPiglinBrute     = entityID("piglin_brute")
	entityPillager        = entityID("pillager")
	entityPolarBear       = entityID("polar_bear")
	entityPufferfish      = entityID("pufferfish")
	entityRabbit          = entityID("rabbit")
	entityRavager         = entityID("ravager")
	entitySalmon          = entityID("salmon")
	entityShulker         = entityID("shulker")
	entitySilverfish      = entityID("silverfish")
	entitySkeletonHorse   = entityID("skeleton_horse")
	entitySniffer         = entityID("sniffer")
	entitySnowGolem       = entityID("snow_golem")
	entitySquid           = entityID("squid")
	entityStrider         = entityID("strider")
	entityTadpole         = entityID("tadpole")
	entityTraderLlama     = entityID("trader_llama")
	entityTropicalFish    = entityID("tropical_fish")
	entityTurtle          = entityID("turtle")
	entityVex             = entityID("vex")
	entityVindicator      = entityID("vindicator")
	entityWanderingTrader = entityID("wandering_trader")
	entityWarden          = entityID("warden")
	entityWither          = entityID("wither")
	entityWitherSkeleton  = entityID("wither_skeleton")
	entityWolf            = entityID("wolf")
	entityZoglin          = entityID("zoglin")
	entityZombieHorse     = entityID("zombie_horse")
	entityZombieNautilus  = entityID("zombie_nautilus")
	entityZombieVillager  = entityID("zombie_villager")

	// Projectile entity types the new shooters launch.
	entityLargeFireball = entityID("fireball")       // ghast fireball
	entityShulkerBullet = entityID("shulker_bullet") // shulker's homing bullet (ours flies straight)
	entityWindCharge    = entityID("wind_charge")    // breeze wind charge
	entityWitherSkull   = entityID("wither_skull")
)

// archetype maps a species onto the shared behavior machinery.
type archetype int

const (
	archPassive      archetype = iota // wander/panic/breed (cow pattern)
	archSkittish                      // passive that bolts from close players (fox/ocelot)
	archHostile                       // hunts players on sight, melee (zombie pattern)
	archRanged                        // hunts from a distance (skeleton kiting)
	archWater                         // water-bound wanderer (squid/fish)
	archWaterHostile                  // water-bound hunter (guardian)
	archFlyer                         // hovering wanderer (bat/parrot/allay)
	archFlyerHostile                  // hovering hunter (phantom/ghast/vex)
	archStatic                        // anchored in place (shulker)
)

// specDrop is one loot roll: count = min + rand(rnd+1); chance = 1-in-N to
// roll at all (0 = always). Items are named — resolved via itemByName.
type specDrop struct {
	item   string
	min    int
	rnd    int
	chance int
}

type speciesDef struct {
	name   string  // registry name; sound events derive from it
	health int     // MAX_HEALTH
	speed  float64 // MOVEMENT_SPEED attribute (per-step = ×attrToStep)
	step   float64 // explicit per-step override (species whose attr feeds a
	//                 custom move controller — fish/frogs — not plain walking)
	damage    float64 // melee at normal difficulty (0 = never melees)
	armor     float64 // base ARMOR attribute
	follow    float64 // FOLLOW_RANGE override (0 = Mob default 16)
	arch      archetype
	retaliate bool    // peaceful until hit, then hunts the attacker (wolf/goat/bee)
	burns     bool    // undead: daylight sets it on fire
	noKB      bool    // KNOCKBACK_RESISTANCE ≈1: never shoved (warden/ravager)
	hover     float64 // flyers: preferred altitude above the ground
	xp        int     // xpReward override (0 = derive from category; xpNone = nothing)
	held      string  // rendered main-hand item ("iron_axe")
	love      string  // breeding food ("" = not breedable)
	soundAs   string  // borrow another species' voice (mooshroom → cow)
	quiet     bool    // no ambient chatter (fish); hurt/death still play
	poison    [2]int  // melee poison seconds at [normal, hard]
	wither    int     // melee wither-effect seconds (wither skeleton)
	drops     []specDrop
}

// speciesTable holds every species added by the roster batch. The original
// hand-tuned species (zombie/skeleton/cow/…) keep their existing code paths;
// lookups fall through to this table for everything newer.
var speciesTable = map[int]*speciesDef{
	// ── Overworld passives + neutrals ────────────────────────────────────
	entityMooshroom: {name: "mooshroom", health: 10, speed: 0.20, arch: archPassive,
		love: "wheat", soundAs: "cow",
		drops: []specDrop{{item: "beef", min: 1, rnd: 2}, {item: "leather", rnd: 2}}},
	entityRabbit: {name: "rabbit", health: 3, speed: 0.30, arch: archSkittish, love: "carrot",
		drops: []specDrop{{item: "rabbit_hide", rnd: 1}, {item: "rabbit", min: 1},
			{item: "rabbit_foot", min: 1, chance: 10}}},
	entityFox: {name: "fox", health: 10, speed: 0.30, damage: 2, follow: 32,
		arch: archSkittish, love: "sweet_berries"},
	entityOcelot: {name: "ocelot", health: 10, speed: 0.30, damage: 3,
		arch: archSkittish, love: "cod"},
	entityCat: {name: "cat", health: 10, speed: 0.30, damage: 3, arch: archSkittish,
		love: "cod", drops: []specDrop{{item: "string", rnd: 2}}},
	entityWolf: {name: "wolf", health: 8, speed: 0.30, damage: 4,
		arch: archPassive, retaliate: true, love: "beef"},
	entityGoat: {name: "goat", health: 10, speed: 0.20, damage: 2,
		arch: archPassive, retaliate: true, love: "wheat"},
	entityPanda: {name: "panda", health: 20, speed: 0.15, damage: 6,
		arch: archPassive, retaliate: true, love: "bamboo",
		drops: []specDrop{{item: "bamboo", rnd: 2}}},
	entityPolarBear: {name: "polar_bear", health: 30, speed: 0.25, damage: 6, follow: 20,
		arch: archPassive, retaliate: true,
		drops: []specDrop{{item: "cod", rnd: 2}, {item: "salmon", rnd: 2}}},
	entityArmadillo: {name: "armadillo", health: 12, speed: 0.14, arch: archPassive,
		love: "spider_eye", drops: []specDrop{{item: "armadillo_scute", min: 1}}},
	entitySniffer: {name: "sniffer", health: 14, speed: 0.10, arch: archPassive,
		love: "torchflower_seeds"},
	entityCamel: {name: "camel", health: 32, speed: 0.09, arch: archPassive, love: "cactus"},
	// camel_husk (1.21.11): a wild, undead-looking desert camel. Inherits the
	// camel's stats but never breeds or falls in love (vanilla CamelHusk
	// extends Camel; canFallInLove=false). Renders as a plain Camel on
	// pre-1.21.11 clients (entity substitution guard).
	entityCamelHusk: {name: "camel_husk", health: 32, speed: 0.09, arch: archPassive},
	entityHorse: {name: "horse", health: 22, speed: 0.225, arch: archPassive,
		love: "golden_carrot", drops: []specDrop{{item: "leather", rnd: 2}}},
	entityDonkey: {name: "donkey", health: 22, speed: 0.175, arch: archPassive,
		love: "golden_carrot", drops: []specDrop{{item: "leather", rnd: 2}}},
	entityMule: {name: "mule", health: 22, speed: 0.175, arch: archPassive,
		drops: []specDrop{{item: "leather", rnd: 2}}},
	entitySkeletonHorse: {name: "skeleton_horse", health: 15, speed: 0.20, arch: archPassive,
		drops: []specDrop{{item: "bone", rnd: 2}}},
	entityZombieHorse: {name: "zombie_horse", health: 25, speed: 0.20, arch: archPassive,
		drops: []specDrop{{item: "rotten_flesh", rnd: 2}}}, // MAX_HEALTH 25 (vanilla ZombieHorse)
	entityLlama: {name: "llama", health: 19, speed: 0.175, damage: 1,
		arch: archPassive, retaliate: true, love: "hay_block",
		drops: []specDrop{{item: "leather", rnd: 2}}},
	entityTraderLlama: {name: "trader_llama", health: 19, speed: 0.175, damage: 1,
		arch: archPassive, retaliate: true, soundAs: "llama",
		drops: []specDrop{{item: "leather", rnd: 2}}},
	entityTurtle: {name: "turtle", health: 30, speed: 0.25, step: 0.05, arch: archPassive,
		love: "seagrass", drops: []specDrop{{item: "seagrass", rnd: 2}}},
	entityFrog: {name: "frog", health: 10, speed: 1.0, step: 0.11, arch: archPassive,
		love: "slime_ball"},
	entityWanderingTrader: {name: "wandering_trader", health: 20, speed: 0.5, step: 0.135,
		arch: archPassive, xp: xpNone},
	entitySnowGolem: {name: "snow_golem", health: 4, speed: 0.20, arch: archPassive,
		xp: xpNone, drops: []specDrop{{item: "snowball", rnd: 15}}},
	// copper_golem (1.21.9): a small passive utility construct built from a
	// copper block + carved pumpkin (see coppergolem.go). Wanders; later sorts
	// items between copper and wooden chests and oxidizes into a statue. Renders
	// as a Frog on pre-1.21.9 clients (entity substitution guard).
	entityCopperGolem: {name: "copper_golem", health: 12, speed: 0.20, arch: archPassive,
		xp: xpNone}, // MOVEMENT_SPEED 0.2, MAX_HEALTH 12 (vanilla CopperGolem.createAttributes)

	// ── Water creatures ──────────────────────────────────────────────────
	entitySquid: {name: "squid", health: 10, step: 0.08, arch: archWater,
		drops: []specDrop{{item: "ink_sac", min: 1, rnd: 2}}},
	entityGlowSquid: {name: "glow_squid", health: 10, step: 0.08, arch: archWater,
		drops: []specDrop{{item: "glow_ink_sac", min: 1, rnd: 2}}},
	entityCod: {name: "cod", health: 3, step: 0.09, arch: archWater, quiet: true,
		drops: []specDrop{{item: "cod", min: 1}}},
	entitySalmon: {name: "salmon", health: 3, step: 0.10, arch: archWater, quiet: true,
		drops: []specDrop{{item: "salmon", min: 1}}},
	entityTropicalFish: {name: "tropical_fish", health: 3, step: 0.08, arch: archWater,
		quiet: true, drops: []specDrop{{item: "tropical_fish", min: 1}}},
	entityPufferfish: {name: "pufferfish", health: 3, step: 0.06, arch: archWater,
		quiet: true, drops: []specDrop{{item: "pufferfish", min: 1}}},
	entityTadpole: {name: "tadpole", health: 6, step: 0.06, arch: archWater, quiet: true},
	entityAxolotl: {name: "axolotl", health: 14, step: 0.10, damage: 2, arch: archWater},
	entityDolphin: {name: "dolphin", health: 10, step: 0.16, damage: 3,
		arch: archWater, retaliate: true, drops: []specDrop{{item: "cod", rnd: 1}}},
	entityGuardian: {name: "guardian", health: 30, step: 0.11, damage: 6,
		arch: archWaterHostile, xp: 10,
		drops: []specDrop{{item: "prismarine_shard", rnd: 2}, {item: "cod", rnd: 1}}},
	entityElderGuardian: {name: "elder_guardian", health: 80, step: 0.11, damage: 8,
		arch: archWaterHostile, xp: 10,
		drops: []specDrop{{item: "prismarine_shard", rnd: 2}, {item: "wet_sponge", min: 1}}},
	// nautilus (1.21.11): an armoured, tameable water creature (its shell can
	// be fitted with nautilus_armor — taming/riding is a later slice, like the
	// happy-ghast harness). MAX_HEALTH 15, ATTACK_DAMAGE 3, KB-resist 0.3
	// (vanilla AbstractNautilus.createAttributes). Squid on pre-1.21.11.
	entityNautilus: {name: "nautilus", health: 15, step: 0.10, damage: 3, arch: archWater,
		drops: []specDrop{{item: "nautilus_shell", rnd: 1}}},
	// zombie_nautilus (1.21.11): the drowned-analogue hostile nautilus variant.
	// Same body as the nautilus but hunts (createAttributes + MOVEMENT_SPEED
	// 1.1). Glow squid on pre-1.21.11.
	entityZombieNautilus: {name: "zombie_nautilus", health: 15, step: 0.11, damage: 3,
		arch: archWaterHostile, xp: 5},

	// ── Flyers ───────────────────────────────────────────────────────────
	entityBat: {name: "bat", health: 6, step: 0.13, arch: archFlyer, hover: 3, xp: xpNone},
	entityParrot: {name: "parrot", health: 6, step: 0.13, arch: archFlyer, hover: 2,
		drops: []specDrop{{item: "feather", min: 1, rnd: 1}}},
	entityAllay: {name: "allay", health: 20, step: 0.13, arch: archFlyer, hover: 2,
		xp: xpNone, quiet: true},
	entityBee: {name: "bee", health: 10, step: 0.13, damage: 2, arch: archFlyer,
		retaliate: true, hover: 2, love: "dandelion", poison: [2]int{10, 18}},
	// happy_ghast (1.21.6): a big, gentle passive flyer. Babies (ghastlings)
	// hatch from a hydrated dried_ghast block and grow up; adults can wear a
	// harness and carry up to four riders — see harness.go. Renders as a real
	// Ghast on pre-1.21.6 clients (tachyne-common entity substitution guard).
	entityHappyGhast: {name: "happy_ghast", health: 20, step: 0.06, arch: archFlyer,
		hover: 8, xp: xpNone}, // MAX_HEALTH 20, MOVEMENT/FLYING_SPEED 0.05 (vanilla HappyGhast)

	// ── Overworld hostiles ───────────────────────────────────────────────
	entityCaveSpider: {name: "cave_spider", health: 12, speed: 0.30, damage: 2,
		arch: archHostile, poison: [2]int{7, 15},
		drops: []specDrop{{item: "string", rnd: 2}, {item: "spider_eye", min: 1, chance: 3}}},
	entitySilverfish: {name: "silverfish", health: 8, speed: 0.25, damage: 1, arch: archHostile},
	entityEndermite:  {name: "endermite", health: 8, speed: 0.25, damage: 2, arch: archHostile},
	entityBogged: {name: "bogged", health: 16, speed: 0.25, arch: archRanged, burns: true,
		held: "bow", drops: []specDrop{{item: "bone", rnd: 2}, {item: "arrow", rnd: 2}}},
	// parched (1.21.11): a desert skeleton whose arrows inflict Weakness (see
	// spawnArrow). AbstractSkeleton stats + MAX_HEALTH 16 (vanilla Parched).
	// Renders as a plain Skeleton on pre-1.21.11 clients.
	entityParched: {name: "parched", health: 16, speed: 0.25, arch: archRanged, burns: true,
		held: "bow", drops: []specDrop{{item: "bone", rnd: 2}, {item: "arrow", rnd: 2}}},
	entityWitherSkeleton: {name: "wither_skeleton", health: 20, speed: 0.25, damage: 4,
		arch: archHostile, wither: 10, held: "stone_sword", // ATTACK_DAMAGE base 4 (source)
		drops: []specDrop{{item: "coal", rnd: 1}, {item: "bone", rnd: 2},
			{item: "wither_skeleton_skull", min: 1, chance: 40}}},
	entityPhantom: {name: "phantom", health: 20, damage: 6, step: 0.16,
		arch: archFlyerHostile, burns: true, hover: 12,
		drops: []specDrop{{item: "phantom_membrane", rnd: 1}}},
	entityCreaking: {name: "creaking", health: 1, speed: 0.40, damage: 3, follow: 32,
		arch: archHostile, xp: xpNone},
	entityBreeze: {name: "breeze", health: 30, speed: 0.63, step: 0.16, follow: 24,
		arch: archRanged, drops: []specDrop{{item: "breeze_rod", min: 1, rnd: 1}}},
	entityWarden: {name: "warden", health: 500, speed: 0.30, damage: 30, follow: 24,
		arch: archHostile, noKB: true, drops: []specDrop{{item: "sculk_catalyst", min: 1}}},
	entityRavager: {name: "ravager", health: 100, speed: 0.30, damage: 12, follow: 32,
		arch: archHostile, noKB: true, xp: 20, drops: []specDrop{{item: "saddle", min: 1}}},
	entityPillager: {name: "pillager", health: 24, speed: 0.35, follow: 32,
		arch: archRanged, held: "crossbow", drops: []specDrop{{item: "arrow", rnd: 2}}},
	entityVindicator: {name: "vindicator", health: 24, speed: 0.35, damage: 5, follow: 12,
		arch: archHostile, held: "iron_axe", // ATTACK_DAMAGE base 5 (source)
		drops: []specDrop{{item: "emerald", rnd: 1}}},
	entityEvoker: {name: "evoker", health: 24, speed: 0.5, step: 0.15, damage: 6, follow: 12,
		arch: archHostile, xp: 10,
		drops: []specDrop{{item: "totem_of_undying", min: 1}, {item: "emerald", rnd: 1}}},
	entityIllusioner: {name: "illusioner", health: 32, speed: 0.5, step: 0.15, follow: 18,
		arch: archRanged, held: "bow", soundAs: "pillager"},
	entityVex: {name: "vex", health: 14, damage: 4, step: 0.16, arch: archFlyerHostile,
		hover: 2, xp: 3},
	entityGiant: {name: "giant", health: 100, speed: 0.5, step: 0.2, damage: 50,
		arch: archHostile, soundAs: "zombie"},
	entityZombieVillager: {name: "zombie_villager", health: 20, speed: 0.23, damage: 3,
		armor: 2, follow: 35, arch: archHostile, burns: true,
		drops: []specDrop{{item: "rotten_flesh", rnd: 2}}},
	entityShulker: {name: "shulker", health: 30, arch: archStatic,
		drops: []specDrop{{item: "shulker_shell", min: 1, chance: 2}}},

	// ── Nether ───────────────────────────────────────────────────────────
	entityGhast: {name: "ghast", health: 10, follow: 100, step: 0.11,
		arch: archFlyerHostile, hover: 10,
		drops: []specDrop{{item: "ghast_tear", rnd: 1}, {item: "gunpowder", rnd: 2}}},
	entityPiglin: {name: "piglin", health: 16, speed: 0.35, damage: 5,
		arch: archHostile, held: "golden_sword"}, // ATTACK_DAMAGE base 5 (source)
	entityPiglinBrute: {name: "piglin_brute", health: 50, speed: 0.35, damage: 7,
		follow: 12, arch: archHostile, xp: 20, held: "golden_axe"}, // ATTACK_DAMAGE 7 (source)
	entityHoglin: {name: "hoglin", health: 40, speed: 0.30, damage: 6, arch: archHostile,
		love:  "crimson_fungus",
		drops: []specDrop{{item: "porkchop", min: 2, rnd: 2}, {item: "leather", rnd: 1}}},
	entityZoglin: {name: "zoglin", health: 40, speed: 0.30, damage: 6, arch: archHostile,
		drops: []specDrop{{item: "rotten_flesh", min: 1, rnd: 2}}},
	entityStrider: {name: "strider", health: 20, speed: 0.175, arch: archPassive,
		love: "warped_fungus", drops: []specDrop{{item: "string", min: 2, rnd: 3}}},

	// ── Bosses (summonable) ──────────────────────────────────────────────
	entityWither: {name: "wither", health: 300, armor: 4, follow: 40, step: 0.27,
		arch: archFlyerHostile, hover: 6, noKB: true, xp: 50,
		drops: []specDrop{{item: "nether_star", min: 1}}},
}

// speciesOf returns the table entry for an entity type (nil for the original
// hand-tuned species, which keep their dedicated code paths).
func speciesOf(etype int) *speciesDef { return speciesTable[etype] }

// isRosterPassive reports whether a table species is a non-hostile one (so
// /summon spawns it peaceful rather than routing it through spawnHostile).
func isRosterPassive(etype int) bool {
	d := speciesOf(etype)
	if d == nil {
		return false
	}
	switch d.arch {
	case archPassive, archSkittish, archWater, archFlyer:
		return true
	}
	return false
}

// poisonFor is the species' melee poison duration at a difficulty (vanilla
// cave spiders/bees poison on normal and hard only).
func (d *speciesDef) poisonFor(difficulty int) int {
	switch difficulty {
	case diffNormal:
		return d.poison[0]
	case diffHard:
		return d.poison[1]
	}
	return 0
}

// stepSpeed is the species' per-update movement (explicit override, else the
// MOVEMENT_SPEED attribute converted like speedFor's hand-tuned species).
func (d *speciesDef) stepSpeed() float64 {
	if d.step > 0 {
		return d.step
	}
	if d.speed > 0 {
		return d.speed * attrToStep
	}
	return mobSpeed
}

// applySpecies configures a freshly spawned mob from its table row: stance +
// behavior from the archetype, movement mode flags, equipment, follow range.
func (h *hub) applySpecies(players map[int32]*tracked, m *mob) {
	if m == nil {
		return // plugin-cancelled spawn upstream
	}
	d := speciesOf(m.etype)
	if d == nil {
		return
	}
	m.aggro, m.armor, m.noKB, m.hover = d.follow, d.armor, d.noKB, d.hover
	switch d.arch {
	case archHostile:
		m.hostile, m.behavior = true, Behavior(hostileBehavior{})
	case archRanged:
		m.hostile, m.behavior = true, Behavior(rangedBehavior{})
	case archWaterHostile:
		m.hostile, m.behavior, m.swims = true, Behavior(hostileBehavior{}), true
	case archWater:
		m.swims = true
	case archFlyer:
		m.flies = true
	case archFlyerHostile:
		m.hostile, m.behavior, m.flies = true, Behavior(hostileBehavior{}), true
	case archSkittish:
		m.skittish = true
	case archStatic:
		m.hostile, m.statik = true, true
	}
	if m.etype == entityCopperGolem {
		m.behavior = copperGolemBehavior{} // walks to containers to sort items
	}
	if d.retaliate {
		m.retaliates = true
	}
	if d.burns {
		m.burns = true
		m.burnDelay = h.rng.Intn(burnStaggerMax)
	}
	if d.held != "" {
		m.held = itemByName[d.held]
		h.toNearbyEv(players, m.dim, m.x, m.z, mobEquip(m.eid, m.held))
	}
}

// spawnSpecies spawns + configures a table species at a position in a
// dimension — the one entry point natural spawning, /summon and breeding use.
func (h *hub) spawnSpecies(players map[int32]*tracked, etype, dim int, x, y, z float64) *mob {
	m := h.spawnMobIn(players, etype, dim, x, y, z)
	if m == nil {
		return nil // plugin-cancelled spawn
	}
	h.applySpecies(players, m)
	return m
}

// provoke turns a retaliating neutral (wolf/goat/bee/llama) hostile against
// the player who hit it — and, for pack animals, wakes nearby kin too (vanilla
// wolf packs and bee swarms all turn on an attacker at once).
func (h *hub) provoke(m *mob, t *tracked) {
	m.hostile = true
	m.behavior = Behavior(hostileBehavior{})
	m.anger = spiderAnger * 4
	m.hasTarget, m.tx, m.tz = true, t.x, t.z
	pack := m.etype == entityWolf || m.etype == entityBee
	if !pack {
		return
	}
	for _, o := range h.mobs {
		if o == m || o.etype != m.etype || o.dim != m.dim || o.dying > 0 {
			continue
		}
		if dist3(o.x, o.y, o.z, m.x, m.y, m.z) > 16 {
			continue
		}
		o.hostile = true
		o.behavior = Behavior(hostileBehavior{})
		o.anger = spiderAnger * 4
		o.hasTarget, o.tx, o.tz = true, t.x, t.z
	}
}

// mobEquip builds the equipment event showing an item in a mob's main hand.
func mobEquip(eid int32, item int32) attachproto.Equipment {
	return equipEv(eid, invStack{item: item, count: 1}, invStack{}, [4]invStack{})
}

// speciesLoot rolls a table species' death drops.
func (h *hub) speciesLoot(d *speciesDef) []drop {
	var out []drop
	for _, sd := range d.drops {
		if sd.chance > 0 && h.rng.Intn(sd.chance) != 0 {
			continue
		}
		n := sd.min
		if sd.rnd > 0 {
			n += h.rng.Intn(sd.rnd + 1)
		}
		if n > 0 {
			out = append(out, drop{itemByName[sd.item], n})
		}
	}
	return out
}

// (waterSpawn retired 2026-07-11: aquatic mobs spawn via the vanilla
// NaturalSpawner port in spawn.go — water_creature/water_ambient categories.)

// spawnPhantom drops a phantom into the night sky above a player (vanilla
// phantoms harry players who haven't slept — we simplify to a rare night
// spawn overhead).
func (h *hub) spawnPhantom(players map[int32]*tracked, t *tracked) {
	y := t.y + 20 + h.rng.Float64()*8
	x := t.x + float64(h.rng.Intn(21)-10)
	z := t.z + float64(h.rng.Intn(21)-10)
	m := h.spawnSpecies(players, entityPhantom, t.dim, x, y, z)
	if m == nil {
		return
	}
	m.hasTarget, m.tx, m.tz, m.ty = true, t.x, t.z, t.y
}

// init registers every table species as /summon-able by name.
func init() {
	for etype, d := range speciesTable {
		summonable[d.name] = etype
	}
}
