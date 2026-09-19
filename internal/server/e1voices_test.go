package server

import (
	"math"
	"testing"
)

// The species the audit found silent all have a hurt and a death voice;
// the ambient chatter follows vanilla (slimes, magma cubes and iron golems
// have none).
func TestOnceSilentSpeciesHaveVoices(t *testing.T) {
	for _, e := range []int{entityHusk, entityDrowned, entityStray, entityEnderman, entityWitch, entityBlaze,
		entitySlime, entityMagmaCube, entityZombifiedPiglin, entityIronGolem, entityVillager} {
		hurt, death, _ := mobSounds(e)
		if hurt == "" || death == "" {
			t.Errorf("%s has no hurt/death voice", entityNameByID[e])
		}
	}
	for _, e := range []int{entitySlime, entityMagmaCube, entityIronGolem} {
		if _, _, amb := mobSounds(e); amb != "" {
			t.Errorf("%s has no ambient sound in vanilla, got %q", entityNameByID[e], amb)
		}
	}
	small := &mob{etype: entitySlime, size: 1}
	h := &hub{}
	if hurt, _, _ := h.mobSoundsFor(small); hurt != "minecraft:entity.slime.hurt_small" {
		t.Errorf("a tiny slime has the small voice, got %q", hurt)
	}
}

// Babies: animals drop nothing and pay nothing; monsters drop and pay
// whatever their age; a baby hoglin pays but drops nothing.
func TestBabyDeathDrops(t *testing.T) {
	cases := []struct {
		m         mob
		loot, pay bool
	}{
		{mob{etype: entityCow, baby: true}, false, false},
		{mob{etype: entityCow}, true, true},
		{mob{etype: entityZombie, baby: true, hostile: true}, true, true},
		{mob{etype: entityZombifiedPiglin, baby: true}, true, true},
		{mob{etype: entityHoglin, baby: true}, false, true},
		{mob{etype: entityTadpole}, true, false},
	}
	for _, c := range cases {
		loot, pay := deathDropsAllowed(&c.m)
		if loot != c.loot || pay != c.pay {
			t.Errorf("%s baby=%v: loot %v pay %v, want %v %v", entityNameByID[c.m.etype], c.m.baby, loot, pay, c.loot, c.pay)
		}
	}
}

// Eye heights come from the vanilla table: an enderman looks from 2.55, a
// villager from 1.62, a baby from half its adult height, an unlisted type
// from 0.85 of its box.
func TestEyeHeightsPerType(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	if e := mobEyeHeight(&mob{etype: entityEnderman}); !near(e, 2.55) {
		t.Errorf("enderman eye height %v", e)
	}
	if e := mobEyeHeight(&mob{etype: entityVillager}); !near(e, 1.62) {
		t.Errorf("villager eye height %v", e)
	}
	if e := mobEyeHeight(&mob{etype: entityCow, baby: true}); !near(e, 0.65) {
		t.Errorf("baby cow eye height %v", e)
	}
	if e := mobEyeHeight(&mob{etype: entitySlime, size: 4}); !near(e, 1.3) {
		t.Errorf("big slime eye height %v", e)
	}
	if e := mobEyeHeight(&mob{etype: entityCreeper}); !near(e, (&mob{etype: entityCreeper}).box().h*0.85) {
		t.Errorf("creeper (no explicit eye height) should fall back to 0.85 of its box, got %v", e)
	}
}
