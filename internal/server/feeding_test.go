package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func feedRig(t *testing.T, etype int, item string, count int) (*hub, *tracked, map[int32]*tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	id := itemByName[item]
	if id == 0 {
		t.Fatalf("unknown item %q", item)
	}
	pl.p.setHotbarSlot(0, id)
	pl.inv.slots[0] = invStack{item: id, count: count}
	players := map[int32]*tracked{1: pl}
	m := h.spawnAnimal(players, etype, 3, 3)
	pl.x, pl.y, pl.z = m.x, m.y, m.z
	return h, pl, players, m
}

// TestFoodTagsCourt: the whole vanilla #<mob>_food tag courts, not just the
// roster's first item.
func TestFoodTagsCourt(t *testing.T) {
	cases := []struct {
		etype int
		item  string
	}{
		{entityPig, "potato"}, {entityPig, "beetroot"}, {entityRabbit, "dandelion"},
		{entityRabbit, "golden_carrot"}, {entityFox, "glow_berries"}, {entityChicken, "melon_seeds"},
		{entityChicken, "pitcher_pod"}, {entityWolf, "cooked_porkchop"}, {entityWolf, "rabbit_stew"},
		{entityWolf, "rotten_flesh"}, {entityCat, "salmon"}, {entityHorse, "golden_apple"},
		{entityDonkey, "enchanted_golden_apple"}, {entityLlama, "hay_block"}, {entityCamel, "cactus"},
	}
	for _, c := range cases {
		h, pl, players, m := feedRig(t, c.etype, c.item, 1)
		if !h.feedAnimal(players, pl, m) || m.loveTicks == 0 || pl.inv.slots[0].count != 0 {
			t.Errorf("etype %d should court on %s (love %d, left %d)", c.etype, c.item, m.loveTicks, pl.inv.slots[0].count)
		}
	}
	for _, c := range []struct {
		etype int
		item  string
	}{{entityPig, "wheat"}, {entityCow, "carrot"}, {entityHorse, "wheat"}, {entityMule, "golden_carrot"}, {entityHappyGhast, "snowball"}} {
		h, pl, players, m := feedRig(t, c.etype, c.item, 1)
		if h.feedAnimal(players, pl, m) || m.loveTicks != 0 {
			t.Errorf("etype %d must not court on %s", c.etype, c.item)
		}
	}
}

// TestFeedingBabyGrows: Animal.mobInteract on a baby spends the food and
// takes a tenth off the remaining growth.
func TestFeedingBabyGrows(t *testing.T) {
	h, pl, players, m := feedRig(t, entityCow, "wheat", 2)
	m.baby, m.growLeft = true, growUpTicks
	if !h.feedAnimal(players, pl, m) || m.growLeft != growUpTicks-growUpTicks/10 || m.loveTicks != 0 {
		t.Fatalf("calf: growLeft %d love %d", m.growLeft, m.loveTicks)
	}
	// A ghastling grows on a snowball though the adults never court.
	h, pl, players, m = feedRig(t, entityHappyGhast, "snowball", 1)
	m.baby, m.growLeft = true, 4000
	if !h.feedAnimal(players, pl, m) || m.growLeft != 3600 {
		t.Fatalf("ghastling: growLeft %d", m.growLeft)
	}
}

// TestPetHealsOnFood: a hurt tamed wolf eats meat for twice its nutrition; a
// cat heals its nutrition, only from its owner.
func TestPetHealsOnFood(t *testing.T) {
	h, pl, players, w := feedRig(t, entityWolf, "cooked_beef", 1)
	w.tamed, w.owner = true, pl.p.eid
	w.setMaxHP(wolfTamedHealth)
	w.health = 1
	if !h.feedAnimal(players, pl, w) || w.health != 17 || w.loveTicks != 0 {
		t.Fatalf("wolf: health %d love %d", w.health, w.loveTicks)
	}
	h, pl, players, c := feedRig(t, entityCat, "cod", 2)
	c.tamed, c.owner = true, 99
	c.health = 1
	if !h.feedAnimal(players, pl, c) || c.health != 1 || c.loveTicks == 0 {
		t.Fatalf("stranger's cod should court, not heal: health %d love %d", c.health, c.loveTicks)
	}
	c.owner, c.loveTicks = pl.p.eid, 0
	if !h.feedAnimal(players, pl, c) || c.health != 1+foodPoints[itemByName["cod"]] {
		t.Fatalf("owner's cod should heal: health %d", c.health)
	}
}

// TestHorseMeals: the handleEating table — wheat heals a hurt horse and ages
// a foal, does nothing to a healthy adult; a llama loves only hay.
func TestHorseMeals(t *testing.T) {
	h, pl, players, m := feedRig(t, entityHorse, "wheat", 3)
	if h.feedAnimal(players, pl, m) || pl.inv.slots[0].count != 3 {
		t.Fatal("a healthy adult horse has no use for wheat")
	}
	m.health = 1
	if !h.feedAnimal(players, pl, m) || m.health != 3 {
		t.Fatalf("wheat heals 2: %d", m.health)
	}
	m.health = m.maxHP()
	m.baby, m.growLeft = true, growUpTicks
	if !h.feedAnimal(players, pl, m) || m.growLeft != growUpTicks-400 || pl.inv.slots[0].count != 1 {
		t.Fatalf("wheat ages a foal 20 s: growLeft %d, left %d", m.growLeft, pl.inv.slots[0].count)
	}
	h, pl, players, m = feedRig(t, entityLlama, "wheat", 1)
	m.health = 1
	if !h.feedAnimal(players, pl, m) || m.health != 3 || m.loveTicks != 0 {
		t.Fatalf("llama wheat: health %d love %d", m.health, m.loveTicks)
	}
}

// TestAxolotlBucketFood: the tropical fish bucket courts and leaves water.
func TestAxolotlBucketFood(t *testing.T) {
	h, pl, players, m := feedRig(t, entityAxolotl, "tropical_fish_bucket", 1)
	if !h.feedAnimal(players, pl, m) || m.loveTicks == 0 || pl.inv.slots[0].item != itemByName["water_bucket"] {
		t.Fatalf("axolotl: love %d, slot %+v", m.loveTicks, pl.inv.slots[0])
	}
}

// TestTameFoodTags: salmon tames a cat, any seed a parrot; food never
// mounts a saddled horse.
func TestTameFoodTags(t *testing.T) {
	h, pl, players, c := feedRig(t, entityCat, "salmon", 20)
	for i := 0; i < 20 && !c.tamed; i++ {
		h.tryTame(players, pl, c)
	}
	if !c.tamed {
		t.Fatal("salmon should tame a cat")
	}
	h, pl, players, w := feedRig(t, entityWolf, "bone", 20)
	for i := 0; i < 20 && !w.tamed; i++ {
		h.tryTame(players, pl, w)
	}
	if !w.tamed || w.maxHP() != wolfTamedHealth || w.health != wolfTamedHealth {
		t.Fatalf("a tamed wolf has 40 max health: tamed %v max %d hp %d", w.tamed, w.maxHP(), w.health)
	}
	h, pl, players, p := feedRig(t, entityParrot, "beetroot_seeds", 20)
	for i := 0; i < 20 && !p.tamed; i++ {
		h.tryTame(players, pl, p)
	}
	if !p.tamed {
		t.Fatal("beetroot seeds should tame a parrot")
	}
	h, pl, players, m := feedRig(t, entityHorse, "golden_carrot", 1)
	m.saddled = true
	if h.tryMount(players, pl, m) || m.loveTicks != 0 {
		t.Fatal("a golden carrot must feed the horse, not mount it")
	}
}
