package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A player arriving at an End city seeds its shulker sentries and the
// ship's elytra frame once; its chests fill from end_city_treasure.
func TestEndCitySeedsAndLoots(t *testing.T) {
	h := newHub(world.New(5))
	players := map[int32]*tracked{}
	h.playersRef = players
	g := h.worldFor(dimEnd).Gen()
	var c = g.EndCityIn(0, 0)
	for cx := -8; cx < 8 && !c.Exists; cx++ {
		for cz := -8; cz < 8 && !c.Exists; cz++ {
			c = g.EndCityIn(cx*320+8, cz*320+8)
		}
	}
	if !c.Exists {
		t.Fatal("no End city found")
	}
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.dim = dimEnd
	pl.x, pl.y, pl.z = float64(c.X), float64(c.Y), float64(c.Z)
	h.populateEndCities(players)
	shulkers := 0
	for _, m := range h.mobs {
		if m.dim == dimEnd && m.etype == entityShulker {
			shulkers++
		}
	}
	if shulkers == 0 {
		t.Errorf("the city should have seeded shulkers")
	}
	if !h.endCityDone[[2]int32{int32(c.X), int32(c.Z)}] {
		t.Error("the city must be remembered as seeded")
	}
	n := len(h.mobs)
	h.populateEndCities(players)
	if len(h.mobs) != n {
		t.Error("a seeded city must not seed again")
	}
	hasShip := false
	for _, m := range g.EndCityMobs(c) {
		if m.Type == "elytra_frame" {
			hasShip = true
		}
	}
	frames := 0
	for _, f := range h.itemFrames {
		if f.dim == dimEnd && f.held.item == itemByName["elytra"] {
			frames++
		}
	}
	if hasShip && frames != 1 {
		t.Errorf("a city with a ship hangs exactly one elytra frame, got %d", frames)
	}
	chests := g.EndCityChests(c)
	if len(chests) == 0 {
		t.Skip("this city rolled no chests")
	}
	ch := &chest{}
	h.fillStructureChestIn(dimEnd, blockPos{chests[0].X, chests[0].Y, chests[0].Z}, ch)
	filled := 0
	for _, s := range ch.slots {
		if s.count > 0 {
			filled++
		}
	}
	if filled == 0 {
		t.Errorf("End city chest at %+v should fill with loot", chests[0])
	}
}
