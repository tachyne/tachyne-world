package worldgen

import (
	"strings"
	"testing"
)

// TestVillageVariantMapping pins the biome → village-style mapping.
func TestVillageVariantMapping(t *testing.T) {
	cases := map[string]string{
		"minecraft:desert":                "desert",
		"minecraft:savanna":               "savanna",
		"minecraft:savanna_plateau":       "savanna",
		"minecraft:snowy_plains":          "snowy",
		"minecraft:ice_spikes":            "snowy",
		"minecraft:taiga":                 "taiga",
		"minecraft:snowy_taiga":           "taiga",
		"minecraft:old_growth_pine_taiga": "taiga",
		"minecraft:plains":                "plains",
		"minecraft:sunflower_plains":      "plains",
		"minecraft:meadow":                "plains",
		"minecraft:forest":                "plains", // no village style → default
	}
	for biome, want := range cases {
		if got := villageVariant(biome); got != want {
			t.Errorf("villageVariant(%q) = %q, want %q", biome, got, want)
		}
	}
}

// TestVillageVariantSelectsTemplateSet: each variant assembles pieces drawn from
// that biome's own template set (village/<variant>/*), not plains, and the house
// chest table falls back to the biome-specific "<variant>_house" table.
func TestVillageVariantSelectsTemplateSet(t *testing.T) {
	g := NewGenerator(7)
	base, ok := findVillage(g)
	if !ok {
		t.Skip("no village rolled")
	}
	for _, variant := range []string{"plains", "desert", "savanna", "snowy", "taiga"} {
		v := base
		v.Variant = variant
		// Fresh cache slot per variant: the cache keys on (seed,x,z), so shift
		// each variant's site so they don't collide in villCache.
		v.X += map[string]int{"plains": 0, "desert": 1, "savanna": 2, "snowy": 3, "taiga": 4}[variant]
		pieces := g.AssembleVillage(v)
		if len(pieces) == 0 {
			t.Errorf("%s: assembled no pieces", variant)
			continue
		}
		// Every building piece must come from this biome's set; the iron golem
		// and pets legitimately come from the shared village/common pool.
		prefix := "village/" + variant + "/"
		ownPieces := 0
		for _, p := range pieces {
			if p.Tmpl.name == "" || strings.HasPrefix(p.Tmpl.name, "village/common/") {
				continue
			}
			if !strings.HasPrefix(p.Tmpl.name, prefix) {
				t.Errorf("%s village stamped a %q piece (expected %s* or village/common/*)", variant, p.Tmpl.name, prefix)
				break
			}
			ownPieces++
		}
		if ownPieces == 0 {
			t.Errorf("%s village assembled no %s* pieces", variant, prefix)
		}
		// A non-profession house chest routes to the biome house table.
		if got := villageTableForPiece("village/"+variant+"/houses/some_house", variant); got != "chests/village/village_"+variant+"_house" {
			t.Errorf("%s house table = %q", variant, got)
		}
	}
	// Profession chests share one table regardless of biome.
	if got := villageTableForPiece("village/desert/houses/desert_weaponsmith_house", "desert"); got != "chests/village/village_weaponsmith" {
		t.Errorf("weaponsmith table = %q, want shared village_weaponsmith", got)
	}
}
