package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A player arriving at a bastion seeds its garrison once; the bastion's
// chests fill from their own vanilla tables.
func TestBastionSeedsAndLoots(t *testing.T) {
	h := newHub(world.New(3))
	players := map[int32]*tracked{}
	h.playersRef = players
	g := h.worldFor(dimNether).Gen()
	var b = g.BastionIn(0, 0)
	for cx := -30; cx < 30 && !b.Exists; cx++ {
		for cz := -30; cz < 30 && !b.Exists; cz++ {
			b = g.BastionIn(cx*448+8, cz*448+8)
		}
	}
	if !b.Exists {
		t.Fatal("no bastion found")
	}
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.dim = dimNether
	pl.x, pl.y, pl.z = float64(b.X), float64(b.Y), float64(b.Z)
	before := len(h.mobs)
	h.populateBastions(players)
	seeded := 0
	for _, m := range h.mobs {
		if m.dim == dimNether && (m.etype == entityPiglin || m.etype == entityPiglinBrute || m.etype == entityHoglin) {
			seeded++
		}
	}
	if seeded == 0 || len(h.mobs) <= before {
		t.Fatalf("the bastion should have seeded piglins/hoglins, got %d", seeded)
	}
	if !h.bastionDone[[2]int32{int32(b.X), int32(b.Z)}] {
		t.Error("the bastion must be remembered as seeded")
	}
	n := len(h.mobs)
	h.populateBastions(players)
	if len(h.mobs) != n {
		t.Error("a seeded bastion must not seed again")
	}
	chests := g.BastionChests(b)
	if len(chests) == 0 {
		t.Fatal("no chests")
	}
	c := &chest{}
	h.fillStructureChestIn(dimNether, blockPos{chests[0].X, chests[0].Y, chests[0].Z}, c)
	filled := 0
	for _, s := range c.slots {
		if s.count > 0 {
			filled++
		}
	}
	if filled == 0 {
		t.Errorf("bastion chest at %+v (%s) should fill with loot", chests[0], chests[0].Table)
	}
}
