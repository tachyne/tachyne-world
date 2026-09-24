package server

// Per-type eye heights (vanilla registers an explicit eyeHeight for these;
// every other type's eyes sit at 0.85 of its height). The eyes are where a
// mob looks from — line of sight, drowning, and every ranged mob's aim.
var mobEyeHeights = map[string]float64{
	"allay": 0.36, "armadillo": 0.26, "axolotl": 0.2751, "bat": 0.45, "bee": 0.3, "bogged": 1.74,
	"breeze": 1.3452, "camel": 2.275, "camel_husk": 2.275, "cat": 0.35, "cave_spider": 0.45,
	"chicken": 0.644, "cod": 0.195, "copper_golem": 0.8125, "cow": 1.3, "creaking": 2.3,
	"dolphin": 0.3, "donkey": 1.425, "drowned": 1.74, "elder_guardian": 0.99875, "enderman": 2.55,
	"endermite": 0.13, "fox": 0.4, "ghast": 2.6, "happy_ghast": 2.6, "giant": 10.44,
	"glow_squid": 0.4, "guardian": 0.425, "horse": 1.52, "husk": 1.74, "llama": 1.7765,
	"magma_cube": 0.325, "mooshroom": 1.3, "mule": 1.52, "nautilus": 0.2751, "parched": 1.74,
	"parrot": 0.54, "phantom": 0.175, "piglin": 1.79, "piglin_brute": 1.79, "pufferfish": 0.455,
	"salmon": 0.26, "sheep": 1.235, "shulker": 0.5, "silverfish": 0.13, "skeleton": 1.74,
	"skeleton_horse": 1.52, "slime": 0.325, "sniffer": 1.05, "sulfur_cube": 0.175, "snow_golem": 1.7, "spider": 0.65,
	"squid": 0.4, "stray": 1.74, "tadpole": 0.195, "trader_llama": 1.7765, "tropical_fish": 0.26,
	"vex": 0.51875, "villager": 1.62, "wandering_trader": 1.62, "witch": 1.62,
	"wither_skeleton": 2.1, "wolf": 0.68, "zombie": 1.74, "zombie_horse": 1.52,
	"zombie_nautilus": 0.2751, "zombie_villager": 1.74, "zombified_piglin": 1.79,
}

// mobEyeHeight is the mob's eye height: the registered value scaled the way
// its box is (a cube by its size, a baby by half), else 0.85 of its height.
func mobEyeHeight(m *mob) float64 {
	if e, ok := mobEyeHeights[entityNameByID[m.etype]]; ok {
		switch {
		case m.etype == entitySlime || m.etype == entityMagmaCube || m.etype == entitySulfurCube:
			if m.size > 0 {
				e *= float64(m.size)
			}
		case m.baby:
			e /= 2
		}
		return e
	}
	return m.box().h * 0.85
}
