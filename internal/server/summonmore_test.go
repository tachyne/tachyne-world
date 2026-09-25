package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// SummonCommand makes the non-living entities too: a boat and a cart where
// they were asked for (off any water or rail), a TNT charge on the default
// eighty-tick fuse with no hop, an arrow at rest, an end crystal.
func TestSummonNonLiving(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	summon := func(name string, x, y, z float64) {
		t.Helper()
		et, ok := summonableType(name)
		if !ok {
			t.Fatalf("/summon %s: unknown entity", name)
		}
		h.withSpawnCause(0, func() { h.summonAt(players, evSummon{etype: et, x: x, y: y, z: z, dim: dimOverworld}) })
	}
	summon("minecraft:oak_boat", 1.5, 180, 1.5)
	summon("minecart", 4.5, 180, 1.5)
	summon("tnt", 7.5, 180, 1.5)
	summon("arrow", 10.5, 185, 1.5)
	summon("end_crystal", 13.5, 180, 1.5)
	var boat, cart bool
	for _, v := range h.vehicles {
		switch {
		case v.etype == entityID("oak_boat") && v.x == 1.5 && v.y == 180:
			boat = true
		case v.etype == entityMinecart && v.x == 4.5 && v.y == 180:
			cart = true
		}
	}
	if !boat || !cart {
		t.Fatalf("a boat (%v) and a cart (%v) where they were summoned", boat, cart)
	}
	if len(h.tnt) != 1 || h.tnt[0].fuse != tntDefaultFuse || h.tnt[0].vx != 0 || h.tnt[0].x != 7.5 {
		t.Fatalf("a TNT charge at rest on an 80-tick fuse: %+v", h.tnt)
	}
	arrows := 0
	for _, a := range h.arrows {
		if a.etype == entityArrow && a.x == 10.5 {
			arrows++
		}
	}
	if arrows != 1 {
		t.Fatal("an arrow where it was summoned")
	}
	crystals := 0
	for _, c := range h.crystals {
		if c.x == 13.5 {
			crystals++
		}
	}
	if crystals != 1 {
		t.Fatal("an end crystal where it was summoned")
	}
	if _, ok := summonableType("item_frame"); ok {
		t.Fatal("an item frame needs a wall; it is not in the summonable set")
	}
}
