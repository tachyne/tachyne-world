package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestTemptFollowsHeldFood: a cow walks after a player holding wheat, stops
// short beside them, ignores an empty hand, and calms down after the food
// goes away.
func TestTemptFollowsHeldFood(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	cow := h.spawnAnimal(players, entityCow, 0, 0)
	cow.x, cow.y, cow.z = 0.5, 70, 0.5
	pl.x, pl.y, pl.z = 6.5, 70, 0.5
	if h.temptStep(players, cow) {
		t.Fatal("an empty hand tempts nothing")
	}
	pl.p.setHotbarSlot(0, itemWheat)
	pl.inv.slots[0] = invStack{item: itemWheat, count: 1}
	if !h.temptStep(players, cow) || cow.vx <= 0 || math.Abs(cow.vz) > 1e-9 {
		t.Fatalf("the cow should head east after the wheat: vx=%.3f vz=%.3f", cow.vx, cow.vz)
	}
	if want := cow.moveSpeed() * 1.25; math.Abs(cow.vx-want) > 1e-9 {
		t.Fatalf("cows tempt at 1.25×: %.4f want %.4f", cow.vx, want)
	}
	cow.x = 5 // close enough: it stands and looks
	if !h.temptStep(players, cow) || cow.vx != 0 {
		t.Fatal("beside the player the cow should stop")
	}
	pl.inv.slots[0] = invStack{}
	if h.temptStep(players, cow) || cow.temptCalm != temptCalm {
		t.Fatalf("food away: the goal stops and calms down (%d)", cow.temptCalm)
	}
	pl.inv.slots[0] = invStack{item: itemWheat, count: 1}
	if h.temptStep(players, cow) {
		t.Fatal("a calming cow ignores the wheat")
	}
	// Out of range, or the wrong species' food, or the offhand: as vanilla.
	cow.temptCalm = 0
	pl.x = 20
	if h.temptStep(players, cow) {
		t.Fatal("beyond TEMPT_RANGE nothing happens")
	}
	pl.x = 6.5
	pl.inv.slots[0] = invStack{item: itemCarrot, count: 1}
	if h.temptStep(players, cow) {
		t.Fatal("a carrot does not tempt a cow")
	}
	pl.offhand = invStack{item: itemWheat, count: 1}
	if !h.temptStep(players, cow) {
		t.Fatal("wheat in the offhand tempts too")
	}
}

// TestTemptRodsAndFamily: a carrot on a stick tempts a pig, a warped fungus
// on a stick a strider, and the horse family follows its golden foods.
func TestTemptRodsAndFamily(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 6.5, 70, 0.5
	for _, c := range []struct {
		etype int
		item  string
		speed float64
	}{{entityPig, "carrot_on_a_stick", 1.2}, {entityStrider, "warped_fungus_on_a_stick", 1.4}, {entityMule, "golden_apple", 1.25}, {entityCamel, "cactus", 2.5}} {
		m := h.spawnAnimal(players, c.etype, 0, 0)
		m.x, m.y, m.z = 0.5, 70, 0.5
		pl.inv.slots[0] = invStack{item: itemByName[c.item], count: 1}
		pl.p.setHotbarSlot(0, itemByName[c.item])
		if !h.temptStep(players, m) || math.Abs(m.vx-m.moveSpeed()*c.speed) > 1e-9 {
			t.Errorf("etype %d on %s: vx=%.4f want %.4f", c.etype, c.item, m.vx, m.moveSpeed()*c.speed)
		}
	}
	w := h.spawnAnimal(players, entityWolf, 0, 0)
	w.x, w.y, w.z = 0.5, 70, 0.5
	pl.inv.slots[0] = invStack{item: itemByName["beef"], count: 1}
	pl.p.setHotbarSlot(0, itemByName["beef"])
	if h.temptStep(players, w) {
		t.Error("wolves have no tempt goal")
	}
}

// TestScareableTempt: an ocelot creeps up on a still player holding cod at
// 0.6×, and a step or a turn within six blocks breaks the spell.
func TestScareableTempt(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	oc := h.spawnAnimal(players, entityOcelot, 0, 0)
	oc.x, oc.y, oc.z = 0.5, 70, 0.5
	pl.x, pl.y, pl.z = 4.5, 70, 0.5
	pl.p.setHotbarSlot(0, itemByName["cod"])
	pl.inv.slots[0] = invStack{item: itemByName["cod"], count: 1}
	if !h.temptStep(players, oc) || math.Abs(oc.vx-oc.moveSpeed()*0.6) > 1e-9 {
		t.Fatalf("the ocelot should creep east at 0.6×: vx=%.4f", oc.vx)
	}
	if !h.temptStep(players, oc) {
		t.Fatal("a still player keeps it coming")
	}
	pl.yaw += 10 // a turn of more than five degrees
	if h.temptStep(players, oc) || oc.tempted || oc.temptCalm != temptCalm {
		t.Fatal("a turn within six blocks should scare it off")
	}
	oc.temptCalm = 0
	pl.x = 20.5 // out of range: a move there is nothing to it
	oc.x = 12.5 // …but back in tempt range
	if !h.temptStep(players, oc) {
		t.Fatal("far off, the player may move")
	}
	pl.x += 1
	if !h.temptStep(players, oc) {
		t.Fatal("a step taken beyond six blocks does not scare")
	}
}
