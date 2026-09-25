package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func standFixture(t *testing.T) (*hub, map[int32]*tracked, *armorStand) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			h.world.SetBlock(x, 180, z, worldgen.Air)
			h.world.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	players := map[int32]*tracked{}
	h.playersRef = players
	st := &armorStand{eid: h.allocEID(), x: 0.5, y: 180, z: 0.5}
	st.equip[5] = invStack{item: int32(itemByName["iron_helmet"]), count: 1}
	h.armorStands[st.eid] = st
	return h, players, st
}

func countItem(h *hub, item int32) int {
	n := 0
	for _, it := range h.items {
		if it.item == item {
			n += it.count
		}
	}
	return n
}

// ArmorStand.hurtServer: a blast breaks it and drops its gear but not the
// stand; an arrow breaks it outright, stand and all; an adventure-mode
// punch does nothing.
func TestArmorStandBlastAndArrow(t *testing.T) {
	h, players, st := standFixture(t)
	h.explodeHurt(players, 0, 0.5, 181, 2.5, 2, dtExplosion, deathCause{})
	if h.armorStands[st.eid] != nil {
		t.Fatal("a blast breaks the stand")
	}
	if countItem(h, int32(itemByName["iron_helmet"])) != 1 || countItem(h, itemArmorStand) != 0 {
		t.Fatal("a blast drops the gear, not the stand")
	}
	h2, players2, st2 := standFixture(t)
	a := &arrowEntity{etype: entityArrow, dim: 0, shooter: 0}
	if !h2.arrowHitsStand(players2, a, 0.5, 181, 0.5) || h2.armorStands[st2.eid] != nil {
		t.Fatal("an arrow breaks the stand at once")
	}
	if countItem(h2, itemArmorStand) != 1 {
		t.Fatal("an arrow's break drops the stand itself")
	}
	h3, players3, st3 := standFixture(t)
	pl := survPlayer(h3)
	pl.gamemode = gmAdventure
	h3.hitStand(players3, pl, st3)
	h3.tick.Add(1)
	h3.hitStand(players3, pl, st3)
	if h3.armorStands[st3.eid] == nil {
		t.Fatal("an adventure-mode player cannot break a stand")
	}
}

// Fire lights a stand and the burn eats it away: four of its twenty health
// a second, gone once half a point is left. Lava lights it too, but only
// burns it once it is out of the lava.
func TestArmorStandBurns(t *testing.T) {
	h, players, st := standFixture(t)
	h.world.SetBlock(0, 180, 0, worldgen.BlockBase("fire"))
	for i := 0; i < 20*8 && h.armorStands[st.eid] != nil; i++ {
		h.tick.Add(1)
		h.tickStands(players)
	}
	if h.armorStands[st.eid] != nil {
		t.Fatalf("a stand in fire burns away (hurt %.1f, fire %d)", st.hurt, st.fire)
	}
	h2, players2, st2 := standFixture(t)
	h2.world.SetBlock(0, 180, 0, worldgen.LavaBase)
	for i := 0; i < 20*3; i++ {
		h2.tick.Add(1)
		h2.tickStands(players2)
	}
	if h2.armorStands[st2.eid] == nil || st2.hurt != 0 || st2.fire == 0 {
		t.Fatal("in the lava a stand is lit but not burnt")
	}
	h2.world.SetBlock(0, 180, 0, worldgen.Air)
	for i := 0; i < 20*6 && h2.armorStands[st2.eid] != nil; i++ {
		h2.tick.Add(1)
		h2.tickStands(players2)
	}
	if h2.armorStands[st2.eid] != nil {
		t.Fatal("out of the lava, still burning, it burns away")
	}
}
