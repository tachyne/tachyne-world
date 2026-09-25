package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ItemEntity.hurtServer: 5 health. Lava takes 4 a tick and fire 1, so a
// cobblestone is gone from lava in two ticks and from fire in five; a
// fire-resistant netherite ingot floats through both. A blast destroys
// the items it reaches — not a nether star, and not the drops the blast
// itself makes.
func TestDroppedItemsCanBeDestroyed(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	h.rules.DoTileDrops = true
	players := map[int32]*tracked{}
	y := 180
	for x := -8; x <= 20; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, y-1, z, worldgen.Stone)
		}
	}
	h.world.SetBlock(0, y, 0, worldgen.LavaBase)
	h.world.SetBlock(4, y, 0, worldgen.BlockBase("fire"))
	cobble := int32(itemByName["cobblestone"])
	ingot := int32(itemByName["netherite_ingot"])
	inLava := h.spawnItemAt(players, 0, cobble, 1, 0.5, float64(y), 0.5, 0, 0, 0)
	safe := h.spawnItemAt(players, 0, ingot, 1, 0.5, float64(y), 0.5, 0, 0, 0)
	inFire := h.spawnItemAt(players, 0, cobble, 1, 4.5, float64(y), 0.5, 0, 0, 0)
	alive := func(it *itemEntity) bool { return h.items[it.eid] == it }
	h.tickItems(players)
	h.tickItems(players)
	if alive(inLava) || !alive(safe) {
		t.Fatalf("after two ticks in lava: cobblestone alive %v, netherite ingot alive %v", alive(inLava), alive(safe))
	}
	for i := 0; i < 3; i++ {
		h.tickItems(players)
	}
	if alive(inFire) {
		t.Fatal("five ticks in fire left the item")
	}

	near := h.spawnItemAt(players, 0, cobble, 1, 14.5, float64(y), 0.5, 0, 0, 0)
	star := h.spawnItemAt(players, 0, int32(itemByName["nether_star"]), 1, 14.5, float64(y), 1.5, 0, 0, 0)
	for x := 13; x <= 18; x++ { // a slab of dirt over the items: some of it drops
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, y+1, z, worldgen.BlockBase("dirt"))
		}
	}
	h.explodeAt(players, 15.5, float64(y), 0.5, 3, 3, blastTNT)
	if alive(near) || !alive(star) {
		t.Fatalf("after the blast: cobblestone alive %v, nether star alive %v", alive(near), alive(star))
	}
	drops := 0
	for _, it := range h.items {
		if it.item == int32(itemByName["dirt"]) {
			drops++
		}
	}
	if drops == 0 {
		t.Fatal("the blast destroyed its own dirt drop")
	}
}
