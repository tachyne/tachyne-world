package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestPandaSitsAndEats: an adult panda fetches bamboo lying nearby, sits
// with it, chews, and finishes it; a cub leaves it alone.
func TestPandaSitsAndEats(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -2; x <= 6; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone) // a floor, so the bamboo lands beside the panda
		}
	}
	p := h.spawnMob(players, entityPanda, 0.5, 180, 0.5)
	p.baby = false
	it := h.spawnItem(players, itemByName["bamboo"], 1, 3.5, 180, 0.5)
	it.noPickupUntil = 0
	now := h.tick.Load()
	trait := pandaTrait(p.variant)
	picked := false
	for i := 0; i < 200 && !picked; i++ {
		if !h.pandaSitEat(players, p, trait, now) {
			t.Fatalf("the panda should be after the bamboo (step %d)", i)
		}
		p.x += p.vx
		p.z += p.vz
		picked = p.held != 0
	}
	if !picked || p.pandaFlags&pandaFlagSit == 0 {
		t.Fatalf("picked up and sat: held %d flags %d", p.held, p.pandaFlags)
	}
	if _, ok := h.items[it.eid]; ok {
		t.Fatal("the bamboo is in its mouth, not on the ground")
	}
	chewed := false
	for i := 0; i < 20000 && p.held != 0; i++ {
		h.pandaSitEat(players, p, trait, now)
		if p.pandaEat > 0 {
			chewed = true
		}
		if len(h.items) > 0 { // got bored and dropped it: pick it back up for the test
			for _, d := range h.items {
				delete(h.items, d.eid)
			}
			p.held, p.pandaSitCD = itemByName["bamboo"], 0
			h.setPandaFlag(players, p, pandaFlagSit, true)
		}
	}
	if p.held != 0 || !chewed || p.pandaFlags&pandaFlagSit != 0 || p.pandaEat != 0 {
		t.Fatalf("eaten and up: held %d chewed %v flags %d eat %d", p.held, chewed, p.pandaFlags, p.pandaEat)
	}
	// A cub does not fetch.
	p.baby = true
	h.spawnItem(players, itemByName["cake"], 1, 3.5, 180, 0.5).noPickupUntil = 0
	if h.pandaSitEat(players, p, trait, now) {
		t.Fatal("cubs leave the food")
	}
}

// PandaBreedGoal: a panda in love with a mate about but no bamboo in reach
// sulks — UNHAPPY_COUNTER 32, two PANDA_CANT_BREED grumbles — and will not
// sulk again for 600 ticks.
func TestPandaSulksWithoutBamboo(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 184; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.p.eid = 500
	pl.x, pl.y, pl.z = 6.5, 180, 6.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	a := h.spawnSpecies(players, entityPanda, 0, 0.5, 180, 0.5)
	b := h.spawnSpecies(players, entityPanda, 0, 3.5, 180, 0.5)
	a.loveTicks, b.loveTicks = 600, 600
	pl.tracked = map[int32]bool{a.eid: true}
	h.gridDirty()
	drainEvs(pl.p)
	if h.breedApproachStep(a) {
		t.Fatal("no bamboo: the panda does not court")
	}
	if a.pandaUnhappy != pandaUnhappyTicks {
		t.Fatalf("it sulks: counter %d", a.pandaUnhappy)
	}
	grumbles, metas := 0, 0
	for i := 0; i < 20; i++ {
		h.tick.Add(mobMoveInterval)
		h.pandaSulkTick(players, a)
		h.breedApproachStep(a)
	}
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.Sound:
			if e.Name == "minecraft:entity.panda.cant_breed" {
				grumbles++
			}
		case attachproto.EntityMeta:
			if e.EID == a.eid && len(e.Meta) > 0 && e.Meta[0] == metaIndexPandaUnhappy {
				metas++
			}
		}
	}
	if grumbles != 2 || a.pandaUnhappy != 0 || metas != 2 {
		t.Fatalf("a sulk: two grumbles, counter synced up and down; got %d grumbles, counter %d, %d metas", grumbles, a.pandaUnhappy, metas)
	}
}
