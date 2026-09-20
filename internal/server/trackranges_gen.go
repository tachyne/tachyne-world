package server

// Per-type entity tracking ranges — EntityType's clientTrackingRange, in
// chunks. Vanilla keeps an entity on a viewer's client out to this many
// chunks, clamped by that viewer's own render distance: four for arrows and
// thrown things, six for dropped items and experience orbs, eight for most
// monsters, ten for most animals and vehicles, sixteen for an end crystal or
// the warden, thirty-two for a player. Five is the builder's default, which
// only the bat and the dolphin take. The commonest value, ten, is the
// fallback below rather than 74 more lines here.
var trackRanges = map[string]int{
	"marker": 0,
	"arrow":  4, "breeze_wind_charge": 4, "cod": 4, "dragon_fireball": 4, "egg": 4,
	"ender_pearl": 4, "experience_bottle": 4, "eye_of_ender": 4, "fireball": 4,
	"firework_rocket": 4, "fishing_bobber": 4, "lingering_potion": 4, "llama_spit": 4,
	"pufferfish": 4, "salmon": 4, "small_fireball": 4, "snowball": 4, "spectral_arrow": 4,
	"splash_potion": 4, "trident": 4, "tropical_fish": 4, "wind_charge": 4, "wither_skull": 4,
	"bat": 5, "dolphin": 5,
	"evoker_fangs": 6, "experience_orb": 6, "item": 6,
	"allay": 8, "bee": 8, "blaze": 8, "bogged": 8, "cat": 8, "cave_spider": 8,
	"chest_minecart": 8, "command_block_minecart": 8, "creaking": 8, "creeper": 8, "drowned": 8,
	"enderman": 8, "endermite": 8, "evoker": 8, "fox": 8, "furnace_minecart": 8, "guardian": 8,
	"hoglin": 8, "hopper_minecart": 8, "husk": 8, "illusioner": 8, "magma_cube": 8, "minecart": 8,
	"mule": 8, "ominous_item_spawner": 8, "parched": 8, "parrot": 8, "phantom": 8, "piglin": 8,
	"piglin_brute": 8, "pillager": 8, "rabbit": 8, "shulker_bullet": 8, "silverfish": 8,
	"skeleton": 8, "snow_golem": 8, "spawner_minecart": 8, "spider": 8, "squid": 8, "stray": 8,
	"tnt_minecart": 8, "vex": 8, "vindicator": 8, "witch": 8, "wither_skeleton": 8, "zoglin": 8,
	"zombie": 8, "zombie_villager": 8, "zombified_piglin": 8,
	"end_crystal": 16, "lightning_bolt": 16, "warden": 16,
	"mannequin": 32, "player": 32,
}

// trackRangeDefault is the range of the largest group — most animals,
// vehicles, armour stands and boats.
const trackRangeDefault = 10
