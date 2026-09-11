package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// On a hard world well past its first days, hostile spawns wear armour at
// vanilla's rate, skeletons carry bows and wither skeletons stone swords;
// spawn-issued gear drops at 8.5% a piece; a converted mob keeps its gear;
// the pickup roll follows the special multiplier.
func TestSpawnGearByDifficulty(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.rules.Difficulty = diffHard
	h.tick.Store(2_000_000) // global difficulty saturated: effective 3 × (0.75 + 0.25 + moon) → f ≈ 0.5–0.9
	f := h.specialMultiplier()
	if f < 0.4 || f > 1 {
		t.Fatalf("special multiplier %v", f)
	}
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	armoured, enchanted, total := 0, 0, 400
	for i := 0; i < total; i++ {
		m := h.spawnHostileY(players, entityZombie, x, y, z)
		wore := false
		for _, g := range m.gear {
			if g.item != 0 {
				wore = true
				if g.ench[0].id != 0 || g.ench[0].lvl != 0 {
					enchanted++
				}
			}
		}
		if wore {
			armoured++
			if !m.spawnGear || m.gearDrop != spawnGearDropChance || m.gear[0].item == 0 {
				t.Errorf("armoured zombie %+v: spawnGear=%v drop=%v head=%d", m.gear, m.spawnGear, m.gearDrop, m.gear[0].item)
			}
		}
		delete(h.mobs, m.eid)
	}
	lo, hi := int(0.15*f*float64(total)*0.4), int(0.15*f*float64(total)*1.8)+5
	if armoured < lo || armoured > hi {
		t.Errorf("%d of %d zombies armoured, want about %.0f", armoured, total, 0.15*f*float64(total))
	}
	if enchanted == 0 {
		t.Error("some spawn armour should be enchanted")
	}
	sk := h.spawnHostileY(players, entitySkeleton, x, y, z)
	if sk.held != itemBow {
		t.Errorf("skeleton holds %d, want a bow", sk.held)
	}
	// enchantSpawnedWeapon: the bow is enchanted at 0.25×f.
	bows := 0
	for i := 0; i < total; i++ {
		s := h.spawnHostileY(players, entitySkeleton, x, y, z)
		if s.heldEnch[0].id != 0 || s.heldEnch[0].lvl != 0 {
			bows++
		}
		delete(h.mobs, s.eid)
	}
	if lo, hi := int(0.25*f*float64(total)*0.4), int(0.25*f*float64(total)*1.8)+5; bows < lo || bows > hi {
		t.Errorf("%d of %d skeleton bows enchanted, want about %.0f", bows, total, 0.25*f*float64(total))
	}
	ws := h.spawnHostileY(players, entityWitherSkeleton, x, y, z)
	if ws.held != itemStoneSword {
		t.Errorf("wither skeleton holds %d, want a stone sword", ws.held)
	}
	// Conversion keeps the gear.
	z1 := h.spawnHostileY(players, entityZombie, x, y, z)
	z1.gear[1] = invStack{item: armourTiers[3][1], count: 1}
	z1.spawnGear, z1.gearDrop = true, spawnGearDropChance
	h.convertMob(players, z1, entityDrowned)
	var dr *mob
	for _, m := range h.mobs {
		if m.etype == entityDrowned {
			dr = m
		}
	}
	if dr == nil || dr.gear[1].item != armourTiers[3][1] || !dr.spawnGear {
		t.Errorf("converted drowned gear %+v", dr)
	}
	// Peaceful/easy worlds early on: nothing is issued.
	h.rules.Difficulty = diffNormal
	h.tick.Store(0)
	if h.specialMultiplier() != 0 {
		t.Errorf("fresh normal world multiplier %v", h.specialMultiplier())
	}
	for i := 0; i < 50; i++ {
		m := h.spawnHostileY(players, entityZombie, x, y, z)
		if m.gear[0].item != 0 || m.canPickup {
			t.Fatal("no gear or pickup below an effective difficulty of 2")
		}
		delete(h.mobs, m.eid)
	}
}

// Lightning swaps a mooshroom's colour and kills a turtle outright.
func TestLightningMooshroomAndTurtle(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	moo := h.spawnMob(players, entityMooshroom, x, y, z)
	moo.variant, moo.variantSet, moo.health = mooshroomRed, true, 20
	turtle := h.spawnMob(players, entityTurtle, x+1, y, z)
	turtle.health = 30
	h.strikeLightning(players, x, y, z, false)
	if moo.variant != mooshroomBrown {
		t.Error("a struck red mooshroom turns brown")
	}
	if h.mobs[turtle.eid] != nil && turtle.dying == 0 && turtle.health > 0 {
		t.Error("a struck turtle dies")
	}
}
