package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func survivalAt(x, y, z float64) *tracked {
	t := testTracked()
	t.gamemode = gmSurvival
	t.x, t.y, t.z = x, y, z
	return t
}

// TestSplashPotionAoE — a splash potion doses every survival player in range,
// and skips those beyond the ~4-block reach.
func TestSplashPotionAoE(t *testing.T) {
	h := newHub(world.New(1))
	near := survivalAt(0.5, 70, 0.5)
	far := survivalAt(20, 70, 20)
	near.p.eid, far.p.eid = 1, 2
	players := map[int32]*tracked{1: near, 2: far}

	h.splashPotion(players, 0, 0.5, 70, 0.5, potPoison, false)
	if near.hasEffect(effPoison) == 0 {
		t.Fatal("a player at the splash centre must be poisoned")
	}
	if far.hasEffect(effPoison) != 0 {
		t.Fatal("a player 20 blocks away must not be affected")
	}
}

// TestSplashHealingScalesWithProximity — the instant-heal amount falls off with
// distance from the impact.
func TestSplashHealingScalesWithProximity(t *testing.T) {
	h := newHub(world.New(1))
	center := survivalAt(0.5, 70, 0.5)
	edge := survivalAt(3.4, 70, 0.5) // ~3 blocks out → small proximity
	center.health, edge.health = 1, 1
	center.p.eid, edge.p.eid = 1, 2
	players := map[int32]*tracked{1: center, 2: edge}

	h.splashPotion(players, 0, 0.5, 70, 0.5, potHealing, false)
	if center.health <= 1 {
		t.Fatalf("centre player should be healed most: %v", center.health)
	}
	if edge.health >= center.health {
		t.Fatalf("edge heal (%v) should be less than centre heal (%v)", edge.health, center.health)
	}
}

// TestLingeringCloudDosesOverTime — a lingering potion leaves a cloud that
// applies its effect to occupants and eventually expires.
func TestLingeringCloudDosesOverTime(t *testing.T) {
	h := newHub(world.New(1))
	pl := survivalAt(0.5, 70, 0.5)
	pl.p.eid = 1
	players := map[int32]*tracked{1: pl}

	h.splashPotion(players, 0, 0.5, 70, 0.5, potSwiftness, true)
	if len(h.clouds) != 1 {
		t.Fatalf("a lingering potion should spawn one cloud, got %d", len(h.clouds))
	}
	h.updateClouds(players) // first tick doses whoever is inside
	if pl.hasEffect(effSpeed) == 0 {
		t.Fatal("standing in a swiftness cloud should grant speed")
	}
	// The cloud eventually expires as it shrinks.
	for i := 0; i < cloudTicks+5 && len(h.clouds) > 0; i++ {
		h.tick.Add(1)
		h.updateClouds(players)
	}
	if len(h.clouds) != 0 {
		t.Fatalf("the cloud should have expired, %d left", len(h.clouds))
	}
}

// TestThrowSplashPotion — using a splash potion launches a shattering projectile
// and consumes one from the slot.
func TestThrowSplashPotion(t *testing.T) {
	h := newHub(world.New(1))
	pl := survivalAt(0.5, 70, 0.5)
	pl.p.eid = 1
	pl.inv.slots[0] = invStack{item: itemSplashPotion, count: 2, potion: potPoison}
	players := map[int32]*tracked{1: pl}
	h.arrows = map[int32]*arrowEntity{}

	h.throwSplashPotion(players, pl, 0)
	if len(h.arrows) != 1 {
		t.Fatalf("throwing should launch one projectile, got %d", len(h.arrows))
	}
	for _, a := range h.arrows {
		if !a.splash || !a.breaks || a.potion != potPoison {
			t.Fatalf("projectile not a splash carrying the potion: %+v", a)
		}
	}
	if pl.inv.slots[0].count != 1 {
		t.Fatalf("one potion should be consumed, left %d", pl.inv.slots[0].count)
	}
}

// TestDrinkPotionStillWorks — the potionEffects refactor keeps drinking intact.
func TestDrinkPotionStillWorks(t *testing.T) {
	h := newHub(world.New(1))
	pl := survivalAt(0.5, 70, 0.5)
	pl.p.eid = 1
	pl.inv.slots[0] = invStack{item: itemPotion, count: 1, potion: potSwiftness}
	players := map[int32]*tracked{1: pl}

	h.drinkPotion(players, pl, 0)
	if pl.hasEffect(effSpeed) == 0 {
		t.Fatal("drinking swiftness should grant speed")
	}
	if pl.inv.slots[0].item != itemGlassBottle {
		t.Fatalf("drinking should leave a glass bottle, got item %d", pl.inv.slots[0].item)
	}
}
