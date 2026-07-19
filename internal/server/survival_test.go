package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func testTracked() *tracked {
	t := &tracked{p: newPlayer(1, "tester", [16]byte{}), gamemode: gmSurvival}
	initSurvival(t)
	return t
}

func TestDamageAndDeath(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.damage(nil, pl, 5)
	if pl.health != 15 || pl.dead {
		t.Fatalf("after 5 damage: health=%v dead=%v", pl.health, pl.dead)
	}
	h.damage(nil, pl, 25)
	if pl.health != 0 || !pl.dead {
		t.Fatalf("lethal damage should kill: health=%v dead=%v", pl.health, pl.dead)
	}
	h.damage(nil, pl, 5) // no effect once dead
	if pl.health != 0 {
		t.Fatalf("dead player took more damage: %v", pl.health)
	}
}

func TestFallDamage(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.onFallAndExhaust(nil, pl, evMove{y: 80, onGround: false}) // leave ground at 80
	h.onFallAndExhaust(nil, pl, evMove{y: 90, onGround: false}) // rise to peak 90
	h.onFallAndExhaust(nil, pl, evMove{y: 70, onGround: true})  // land at 70 → fell 20
	if want := float32(maxHealth - (20 - 3)); pl.health != want {
		t.Fatalf("fall of 20 should deal 17: health=%v want %v", pl.health, want)
	}
}

func TestRegenAndStarve(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl

	pl.health, pl.food, pl.saturation = 10, 20, 5 // full food + saturation → FAST regen
	h.fastRegen(players)
	if pl.health <= 10 {
		t.Errorf("saturated player should fast-regen: %v", pl.health)
	}
	before := pl.health
	h.survivalTick(players) // vanilla else-if: slow regen must NOT stack on fast
	if pl.health > before {
		t.Errorf("slow regen fired alongside saturation regen (vanilla is else-if): %v", pl.health)
	}

	pl.health, pl.food, pl.saturation, pl.exhaustion = 10, 18, 0, 0 // fed, no saturation → SLOW regen
	h.survivalTick(players)
	if pl.health != 11 {
		t.Errorf("fed player should slow-regen 1 HP: %v", pl.health)
	}

	pl.health, pl.food, pl.saturation, pl.exhaustion = 10, 0, 0, 0 // starving
	h.survivalTick(players)
	if pl.health >= 10 {
		t.Errorf("starving player should lose health: %v", pl.health)
	}
}

func TestRespawnResets(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.damage(nil, pl, 25)
	if !pl.dead {
		t.Fatal("should be dead")
	}
	h.respawn(pl)
	if pl.dead || pl.health != maxHealth || pl.food != maxFood {
		t.Fatalf("respawn should restore: dead=%v health=%v food=%v", pl.dead, pl.health, pl.food)
	}
}

func TestDrowning(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	// Fill the head/eye block with water so the air supply drains.
	w.SetBlock(0, 71, 0, worldgen.Water)

	// maxAir/airDrainPerSec seconds submerged drain the supply; after that, damage.
	for i := 0; i < maxAir/airDrainPerSec; i++ {
		h.survivalTick(players)
	}
	if pl.air != 0 {
		t.Fatalf("air should be depleted after %d s, got %d", maxAir/airDrainPerSec, pl.air)
	}
	before := pl.health
	pl.food = 10            // below full: fast saturation regen must not mask the drowning
	h.survivalTick(players) // now drowning
	if pl.health >= before {
		t.Fatalf("submerged with no air should drown: health %v -> %v", before, pl.health)
	}
	// Surfacing refills the air supply.
	w.SetBlock(0, 71, 0, worldgen.Air)
	h.survivalTick(players)
	if pl.air == 0 {
		t.Fatalf("air should refill out of water, still %d", pl.air)
	}
}

func TestLavaDamage(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	pl.food = 10                            // below regen threshold, so damage isn't masked
	w.SetBlock(0, 70, 0, worldgen.LavaBase) // feet in lava
	before := pl.health
	h.survivalTick(players)
	if want := before - lavaDamagePerSec; pl.health != want {
		t.Fatalf("lava should deal %d: health %v -> %v (want %v)", lavaDamagePerSec, before, pl.health, want)
	}
}

func TestCactusDamage(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	pl.food = 10                    // below regen threshold, so damage isn't masked
	w.SetBlock(1, 70, 0, cactusMin) // cactus in the neighbouring column at feet
	before := pl.health
	h.survivalTick(players)
	if want := before - cactusDamagePerSec; pl.health != want {
		t.Fatalf("adjacent cactus should deal %d: health %v -> %v", cactusDamagePerSec, before, pl.health)
	}
}

func TestDeathDropsInventory(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: 1, count: 5}                          // some stone
	pl.inv.slots[9] = invStack{item: itemByName["wheat_seeds"], count: 12} // and torches
	h.damage(players, pl, 25)                                              // lethal
	if !pl.dead {
		t.Fatal("should be dead")
	}
	if len(h.items) != 2 {
		t.Fatalf("death should drop 2 stacks as item entities, got %d", len(h.items))
	}
	if pl.inv.slots[0].item != 0 || pl.inv.slots[9].item != 0 {
		t.Fatalf("inventory should be cleared after death: %+v", pl.inv.slots)
	}
}

func TestInventoryAdd(t *testing.T) {
	inv := &inventory{}
	changed, leftover := inv.add(itemByName["wheat_seeds"], 100) // two stacks: 64 + 36
	if leftover != 0 || len(changed) != 2 {
		t.Fatalf("add 100: changed=%v leftover=%d", changed, leftover)
	}
	if inv.slots[0].count != 64 || inv.slots[1].count != 36 {
		t.Fatalf("stacks wrong: %d, %d", inv.slots[0].count, inv.slots[1].count)
	}
	inv.add(itemByName["wheat_seeds"], 28) // tops up slot 1 (36→64)
	if inv.slots[1].count != 64 {
		t.Fatalf("slot 1 should top to 64, got %d", inv.slots[1].count)
	}
}

// TestEatHoldTiming: use_item starts the 32-tick chew; the food applies only
// after the hold completes — one right-click no longer instant-eats.
func TestEatHoldTiming(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.food = 10
	pl.inv.slots[0] = invStack{item: itemByName["apple"], count: 2} // apples

	h.startEating(pl, 0)
	if pl.eatingSlot != 0 {
		t.Fatal("eat-hold should start")
	}
	for i := 0; i < eatDuration-1; i++ {
		h.tick.Add(1)
		h.updateEating(players)
	}
	if pl.food != 10 || pl.inv.slots[0].count != 2 {
		t.Fatalf("food must not apply mid-chew: food=%d count=%d", pl.food, pl.inv.slots[0].count)
	}
	h.tick.Add(1)
	h.updateEating(players)
	if pl.food <= 10 || pl.inv.slots[0].count != 1 {
		t.Fatalf("chew complete should eat one apple: food=%d count=%d", pl.food, pl.inv.slots[0].count)
	}
	if pl.eatingSlot != -1 {
		t.Fatal("eat-hold should clear after applying")
	}
}

// TestEatReleaseCancels: releasing early cancels; releasing at the last moment
// applies (absorbs the client/server timer race).
func TestEatReleaseCancels(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.food = 10
	pl.inv.slots[0] = invStack{item: itemByName["apple"], count: 1}

	h.startEating(pl, 0)
	for i := 0; i < 10; i++ {
		h.tick.Add(1)
	}
	h.stopEating(nil, pl) // early release
	if pl.food != 10 || pl.inv.slots[0].count != 1 || pl.eatingSlot != -1 {
		t.Fatalf("early release must cancel: food=%d count=%d", pl.food, pl.inv.slots[0].count)
	}

	h.startEating(pl, 0)
	for i := 0; i < eatNearlyTicks(pl.inv.slots[0].item); i++ {
		h.tick.Add(1)
	}
	h.stopEating(nil, pl) // released right at the finish — counts as eaten
	if pl.food <= 10 || pl.inv.slots[0].count != 0 {
		t.Fatalf("near-complete release should apply: food=%d count=%d", pl.food, pl.inv.slots[0].count)
	}
}
