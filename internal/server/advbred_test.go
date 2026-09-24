package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Two by Two's frog, sniffer and turtle criteria name the parents (those
// species breed into an egg or spawn, with no offspring): breeding cows
// earns the cow and none of them; breeding turtles earns the turtle.
func TestBredAllAnimalsEggLayersNeedTheirParents(t *testing.T) {
	const adv = "minecraft:husbandry/bred_all_animals"
	breed := func(etype int) *tracked {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.adv = advState{}
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		w := h.worldFor(0)
		for x := -2; x <= 3; x++ {
			for z := -2; z <= 2; z++ {
				w.SetBlock(x, 179, z, worldgen.Sand)
				w.SetBlock(x, 180, z, worldgen.Air)
			}
		}
		a := h.spawnAnimal(players, etype, 0, 0)
		b := h.spawnAnimal(players, etype, 1, 0)
		if a == nil || b == nil {
			t.Fatalf("no pair of %s", advEntityName[etype])
		}
		a.x, a.y, a.z = 0.5, 180, 0.5
		b.x, b.y, b.z = 1.5, 180, 0.5
		a.loveTicks, b.loveTicks = loveTicks, loveTicks
		a.lovedBy, b.lovedBy = pl.p.eid, pl.p.eid
		courtBreeding(h, players)
		if a.breedCD == 0 {
			t.Fatalf("the %s pair did not breed", advEntityName[etype])
		}
		return pl
	}
	has := func(pl *tracked, crit string) bool { _, ok := pl.adv[adv][crit]; return ok }

	pl := breed(entityCow)
	if !has(pl, "minecraft:cow") {
		t.Error("breeding cows does not earn the cow criterion")
	}
	for _, egg := range []string{"minecraft:frog", "minecraft:sniffer", "minecraft:turtle"} {
		if has(pl, egg) {
			t.Errorf("breeding cows earned %s", egg)
		}
	}
	pl = breed(entityTurtle)
	if !has(pl, "minecraft:turtle") {
		t.Error("breeding turtles does not earn the turtle criterion")
	}
	if has(pl, "minecraft:cow") || has(pl, "minecraft:frog") {
		t.Error("breeding turtles earned another species' criterion")
	}
}
