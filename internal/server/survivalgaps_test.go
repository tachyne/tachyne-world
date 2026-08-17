package server

import "testing"

// Four gaps in the survival loop, each of them something vanilla does that
// this engine did not.

// FoodData.tick drains saturation on every difficulty but only takes from the
// FOOD bar when the difficulty is not peaceful. The bar used to empty on
// peaceful too — you just never starved once it had.
func TestPeacefulNeverEmptiesTheFoodBar(t *testing.T) {
	for _, c := range []struct {
		name string
		diff int
		want bool // does the food bar drop?
	}{
		{"peaceful", diffPeaceful, false},
		{"easy", diffEasy, true},
		{"hard", diffHard, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, players := pushWorld(t)
			h.rules.Difficulty = c.diff
			tr := leashPlayer(t, h, players, 0, 70, 0)
			tr.food, tr.saturation = 20, 0
			tr.exhaustion = exhaustionThreshold * 3 // three helpings' worth

			h.survivalTick(players)

			dropped := tr.food < 20
			if dropped != c.want {
				t.Errorf("food %d after exhaustion on %s (dropped=%v), want dropped=%v",
					tr.food, c.name, dropped, c.want)
			}
		})
	}
}

// …but saturation still burns on peaceful, exactly as vanilla does.
func TestPeacefulStillBurnsSaturation(t *testing.T) {
	h, players := pushWorld(t)
	h.rules.Difficulty = diffPeaceful
	tr := leashPlayer(t, h, players, 0, 70, 0)
	tr.food, tr.saturation = 20, 5
	tr.exhaustion = exhaustionThreshold * 2

	h.survivalTick(players)
	if tr.saturation >= 5 {
		t.Errorf("saturation %.1f, want it burnt down even on peaceful", tr.saturation)
	}
}

// HungerMobEffect adds exhaustion every tick. The effect was applied — a husk's
// bite grants it — but nothing consumed it, so it cost its victim nothing.
func TestTheHungerEffectCostsExhaustion(t *testing.T) {
	h, players := pushWorld(t)
	tr := leashPlayer(t, h, players, 0, 70, 0)
	h.applyEffect(players, tr, effHunger, 0, 30)
	tr.exhaustion = 0

	h.updateEffects(players)
	if tr.exhaustion <= 0 {
		t.Fatal("a second under Hunger cost no exhaustion at all")
	}
	if got := tr.exhaustion; got != hungerExhaustionPerSec {
		t.Errorf("exhaustion %.3f, want %.3f (0.005 a tick for a second)",
			got, hungerExhaustionPerSec)
	}
}

// …and it scales with the level, as the effect's amplifier does.
func TestHungerScalesWithItsLevel(t *testing.T) {
	h, players := pushWorld(t)
	tr := leashPlayer(t, h, players, 0, 70, 0)
	h.applyEffect(players, tr, effHunger, 1, 30) // Hunger II
	tr.exhaustion = 0

	h.updateEffects(players)
	if got, want := tr.exhaustion, float32(hungerExhaustionPerSec*2); got != want {
		t.Errorf("Hunger II cost %.3f, want %.3f", got, want)
	}
}

// Bee.doHurtTarget uses damageSources().sting, which is its own type — and
// what the death message reads from. Everything used to bite with mob_attack.
func TestABeeStingsRatherThanBites(t *testing.T) {
	if got := mobMeleeDamage(entityBee); got != dtSting {
		t.Errorf("a bee deals %q, want sting", got.name())
	}
	for _, etype := range []int{entityZombie, entitySpider, entityWolf} {
		if got := mobMeleeDamage(etype); got != dtMobAttack {
			t.Errorf("type %d deals %q, want mob_attack", etype, got.name())
		}
	}
}

// The dragon is not in mobs.json, so its health has to ride the world settings
// or a restart mid-fight hands it back full health.
func TestTheDragonResumesAtTheHealthItHad(t *testing.T) {
	h, players := pushWorld(t)
	h.end = h.world // any non-nil end world is enough to stage the fight
	tr := leashPlayer(t, h, players, 0, 70, 0)
	h.rules.DragonHealth = 37 // what a previous session left it on

	h.enterEnd(players, tr)
	if h.dragon == nil {
		t.Fatal("no dragon was staged")
	}
	if h.dragon.health != 37 {
		t.Errorf("dragon came back on %d health, want the 37 it was left with",
			h.dragon.health)
	}
}

// A fresh fight starts at full health, not at whatever a finished one left.
func TestAFreshDragonIsAtFullHealth(t *testing.T) {
	h, players := pushWorld(t)
	h.end = h.world
	tr := leashPlayer(t, h, players, 0, 70, 0)
	h.rules.DragonHealth = 0 // no fight in progress

	h.enterEnd(players, tr)
	if h.dragon == nil {
		t.Fatal("no dragon was staged")
	}
	if h.dragon.health != dragonHealth {
		t.Errorf("fresh dragon on %d health, want %d", h.dragon.health, dragonHealth)
	}
}
