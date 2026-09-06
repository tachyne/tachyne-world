package server

import "testing"

// The vanilla brewing chain: nether wart makes awkward, an ingredient makes
// the potion, redstone lengthens, glowstone strengthens, a fermented spider
// eye corrupts, gunpowder makes it splash and dragon's breath lingering —
// each step keeping what it should.
func TestBrewingChainFollowsVanilla(t *testing.T) {
	ing := func(name string) int32 { return itemByName[name] }
	step := func(from invStack, ingredient string) invStack {
		t.Helper()
		out, ok := brewOne(from, ing(ingredient))
		if !ok {
			t.Fatalf("%s + %s should brew", from.name, ingredient)
		}
		return out
	}
	water := potionStack(potWater)
	awk := step(water, "nether_wart")
	if awk.potion != potAwkward || awk.name != "Awkward Potion" {
		t.Fatalf("awkward: %+v", awk)
	}
	swift := step(awk, "sugar")
	if swift.potion != potSwiftness || swift.name != "Potion of Swiftness" {
		t.Fatalf("swiftness: %+v", swift)
	}
	long := step(swift, "redstone")
	if long.potion != potLongSwiftness || potionEffects(long.potion)[0].secs != 480 {
		t.Fatalf("long swiftness: %+v", long)
	}
	strong := step(swift, "glowstone_dust")
	if strong.potion != potStrongSwiftness || potionEffects(strong.potion)[0].amp != 1 || potionEffects(strong.potion)[0].secs != 90 {
		t.Fatalf("strong swiftness: %+v", strong)
	}
	slow := step(swift, "fermented_spider_eye")
	if slow.potion != potSlowness || slow.name != "Potion of Slowness" {
		t.Fatalf("corrupted: %+v", slow)
	}
	splash := step(strong, "gunpowder")
	if splash.item != itemSplashPotion || splash.potion != potStrongSwiftness || splash.name != "Splash Potion of Swiftness" {
		t.Fatalf("splash: %+v", splash)
	}
	linger := step(splash, "dragon_breath")
	if linger.item != itemLingerPotion || linger.potion != potStrongSwiftness || linger.name != "Lingering Potion of Swiftness" {
		t.Fatalf("lingering: %+v", linger)
	}
	if _, ok := brewOne(water, ing("dragon_breath")); ok {
		t.Error("dragon's breath only lingers a SPLASH potion")
	}
	if m := step(water, "redstone"); m.potion != potMundane || m.name != "Mundane Potion" {
		t.Errorf("water + redstone: %+v", m)
	}
	if th := step(water, "glowstone_dust"); th.potion != potThick {
		t.Errorf("water + glowstone: %+v", th)
	}
	if w := step(water, "fermented_spider_eye"); w.potion != potWeakness {
		t.Errorf("water + fermented eye: %+v", w)
	}
	if s := step(water, "sugar"); s.potion != potMundane {
		t.Errorf("a start ingredient on water is mundane: %+v", s)
	}
	turtle := step(awk, "turtle_helmet")
	if turtle.name != "Potion of the Turtle Master" || len(potionEffects(turtle.potion)) != 2 {
		t.Errorf("turtle master: %+v", turtle)
	}
	if _, ok := brewOne(awk, ing("diamond")); ok {
		t.Error("a diamond brews nothing")
	}
}

// Every kind has a definition and a name; every recipe's ends are defined.
func TestPotionTableIsComplete(t *testing.T) {
	for k := int8(potWater); k < potCount; k++ {
		if _, ok := potionDefs[k]; !ok {
			t.Errorf("potion kind %d has no definition", k)
		}
		if potionName(k, itemPotion) == "" {
			t.Errorf("potion kind %d has no name", k)
		}
	}
	if len(brewMixes) < 50 {
		t.Errorf("only %d brewing mixes registered; vanilla has more than fifty", len(brewMixes))
	}
	for _, m := range brewMixes {
		if _, ok := potionDefs[m.from]; !ok {
			t.Errorf("mix from undefined kind %d", m.from)
		}
		if _, ok := potionDefs[m.to]; !ok {
			t.Errorf("mix to undefined kind %d", m.to)
		}
	}
}

// Bottles brew independently: a water bottle beside an awkward one with
// sugar in the stand leaves the water alone and makes the other swiftness.
func TestBrewingIsPerBottle(t *testing.T) {
	b := &bin{slots: make([]invStack, 5)}
	b.slots[0] = potionStack(potWater)
	b.slots[1] = potionStack(potAwkward)
	b.slots[3] = invStack{item: itemByName["sugar"], count: 1}
	outs, ok := brewResult(b)
	if !ok {
		t.Fatal("one brewable bottle is enough to run the stand")
	}
	if outs[0].potion != potMundane { // water + sugar IS a mix (mundane), so it brews too
		t.Errorf("water + sugar → %+v, want mundane", outs[0])
	}
	if outs[1].potion != potSwiftness {
		t.Errorf("awkward + sugar → %+v, want swiftness", outs[1])
	}
	b.slots[0] = potionStack(potSwiftness)
	b.slots[3] = invStack{item: itemByName["gunpowder"], count: 1}
	outs, ok = brewResult(b)
	if !ok || outs[0].item != itemSplashPotion || outs[1].item != itemSplashPotion {
		t.Errorf("gunpowder splashes every bottle: %+v", outs)
	}
}
