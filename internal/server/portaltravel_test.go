package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// buildPortal lights a 2x3 portal sheet on an obsidian frame at (x,y,z) in
// dim, and returns the sheet's base cell — what portalBaseKey resolves to.
func buildPortal(h *hub, dim, x, y, z int) blockPos {
	w := h.worldFor(dim)
	for dy := -1; dy <= 3; dy++ { // frame sides (the sheet runs along x)
		w.SetBlock(x-1, y+dy, z, worldgen.Obsidian)
		w.SetBlock(x+2, y+dy, z, worldgen.Obsidian)
	}
	for dx := 0; dx < 2; dx++ {
		w.SetBlock(x+dx, y-1, z, worldgen.Obsidian) // floor
		w.SetBlock(x+dx, y+3, z, worldgen.Obsidian) // lintel
		for dy := 0; dy < 3; dy++ {
			w.SetBlock(x+dx, y+dy, z, portalX)
		}
		// Somewhere safe to stand on both sides of the sheet.
		for _, dz := range []int{-1, 1} {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	return portalBaseKey(w, x, y, z)
}

// linkPortals records the pair a player's first trip would have recorded.
func linkPortals(h *hub, a, b dimPos) {
	h.portalLinks[a] = b
	h.portalLinks[b] = a
}

// A mob standing in a portal goes through at once — no dwell, because vanilla
// only makes players wait — and cannot come back until its cooldown runs out.
func TestMobTakesThePortal(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	over := buildPortal(h, dimOverworld, 40, 70, 40)
	nether := buildPortal(h, 1, 5, 40, 5)
	linkPortals(h, dimPos{dimOverworld, over}, dimPos{1, nether})

	m := h.spawnMob(players, entityZombie, float64(over.x)+0.5, float64(over.y), float64(over.z)+0.5)
	if m == nil {
		t.Fatal("the zombie should have spawned")
	}
	m.dim = dimOverworld
	h.updatePortalTravel(players)

	if m.dim != 1 {
		t.Fatalf("the zombie should be in the nether, dim=%d", m.dim)
	}
	if floorInt(m.x) != nether.x || floorInt(m.z) != nether.z {
		t.Fatalf("it should stand in the far portal, got %.1f,%.1f", m.x, m.z)
	}
	if m.portalCool != entityPortalCooldown {
		t.Fatalf("it should carry the 300-tick cooldown, got %d", m.portalCool)
	}
	// It is standing in a portal again; the cooldown is what stops the bounce.
	h.updatePortalTravel(players)
	if m.dim != 1 {
		t.Fatal("the cooldown must stop it bouncing straight back")
	}
	for i := 0; i < entityPortalCooldown; i++ {
		h.updatePortalTravel(players)
	}
	if m.dim != dimOverworld {
		t.Fatalf("once the cooldown lapses it travels again, dim=%d", m.dim)
	}
}

// A dropped item travels the same way — this is what makes a portal-fed farm
// work at all.
func TestDroppedItemTakesThePortal(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	over := buildPortal(h, dimOverworld, 40, 70, 40)
	nether := buildPortal(h, 1, 5, 40, 5)
	linkPortals(h, dimPos{dimOverworld, over}, dimPos{1, nether})

	h.spawnItemIn(players, dimOverworld, int32(itemByName["diamond"]), 3,
		float64(over.x)+0.5, float64(over.y), float64(over.z)+0.5)
	var it *itemEntity
	for _, e := range h.items {
		it = e
	}
	if it == nil {
		t.Fatal("the drop should exist")
	}
	it.dim, it.x, it.y, it.z = dimOverworld, float64(over.x)+0.5, float64(over.y), float64(over.z)+0.5
	h.updatePortalTravel(players)
	if it.dim != 1 {
		t.Fatalf("the drop should be in the nether, dim=%d", it.dim)
	}
	if it.count != 3 {
		t.Fatalf("the stack must arrive whole, got %d", it.count)
	}
}

// An unpaired portal carries nobody: the engine never builds the far side for
// a mob, so it waits for a player to make the pair.
func TestUnpairedPortalCarriesNobody(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	over := buildPortal(h, dimOverworld, 40, 70, 40)

	m := h.spawnMob(players, entityZombie, float64(over.x)+0.5, float64(over.y), float64(over.z)+0.5)
	m.dim = dimOverworld
	h.updatePortalTravel(players)
	if m.dim != dimOverworld {
		t.Fatalf("with no pair recorded the zombie stays put, dim=%d", m.dim)
	}
}

// A pair whose far side has been broken is forgotten, so the next player
// through records a fresh one instead of failing forever.
func TestBrokenPairIsForgotten(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	over := buildPortal(h, dimOverworld, 40, 70, 40)
	nether := buildPortal(h, 1, 5, 40, 5)
	linkPortals(h, dimPos{dimOverworld, over}, dimPos{1, nether})
	h.worldFor(1).SetBlock(nether.x, nether.y, nether.z, worldgen.Air) // sheet broken

	m := h.spawnMob(players, entityZombie, float64(over.x)+0.5, float64(over.y), float64(over.z)+0.5)
	m.dim = dimOverworld
	h.updatePortalTravel(players)
	if m.dim != dimOverworld {
		t.Fatal("a broken far side must not carry anyone")
	}
	if len(h.portalLinks) != 0 {
		t.Fatalf("the dead pair should have been forgotten, %d links left", len(h.portalLinks))
	}
}
