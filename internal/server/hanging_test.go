package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// hangingWall is a stone wall in the z=1 plane (x -1..5, y 198..204) with a
// frame hung facing north at (0, 200, 0) and a painting beside it.
func hangingWall(t *testing.T) (*hub, map[int32]*tracked, *itemFrame, *painting) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := testTracked()
	pl.x, pl.y, pl.z = 2.5, 201, -6.5
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	for x := -1; x <= 5; x++ {
		for y := 198; y <= 204; y++ {
			h.world.SetBlock(x, y, 1, stone)
		}
	}
	f := &itemFrame{eid: h.allocEID(), x: 0, y: 200, z: 0, dim: 0, dir: 2}
	h.itemFrames[f.eid] = f
	h.onPlacePainting(players, evPlacePainting{eid: 1, x: 3, y: 200, z: 0, dir: 2, variant: "kebab"})
	var pt *painting
	for _, p := range h.paintings {
		pt = p
	}
	if pt == nil {
		t.Fatal("no painting")
	}
	return h, players, f, pt
}

// BlockAttachedEntity.tick: every hundred ticks a hanging entity checks its
// support, whatever took it away — here a world write, not a player's edit.
func TestHangingEntitiesCheckTheirSupport(t *testing.T) {
	h, players, f, pt := hangingWall(t)
	h.setBlockAt(players, 0, blockPos{0, 200, 1}, worldgen.Air)
	h.setBlockAt(players, 0, blockPos{3, 200, 1}, worldgen.Air)
	for i := 0; i < hangingCheckInterval; i++ {
		h.tick.Add(1)
		h.hangingSurvivalTick(players)
	}
	if h.itemFrames[f.eid] != nil || h.paintings[pt.eid] != nil {
		t.Fatal("a frame and a painting without their wall pop within a hundred ticks")
	}
}

// Any hurt breaks a hanging entity: a blast within twice its power takes
// the frame and the painting down, and an arrow breaks a painting and pops
// a framed item before the frame.
func TestExplosionsAndArrowsBreakHangingEntities(t *testing.T) {
	h, players, f, pt := hangingWall(t)
	h.explodeHurt(players, 0, 1.5, 200.5, -2, 2, dtExplosion, deathCause{})
	if h.itemFrames[f.eid] != nil || h.paintings[pt.eid] != nil {
		t.Fatal("a blast breaks the frame and the painting")
	}
	h2, players2, f2, pt2 := hangingWall(t)
	f2.held = invStack{item: int32(itemByName["diamond"]), count: 1, name: "Shiny"}
	a := &arrowEntity{etype: entityArrow, dim: 0}
	if !h2.arrowHitsHanging(players2, a, 0.5, 200.5, 0.9) {
		t.Fatal("the arrow meets the frame")
	}
	if h2.itemFrames[f2.eid] == nil || f2.held.count != 0 {
		t.Fatal("an arrow pops the framed item and leaves the frame")
	}
	var named bool
	for _, it := range h2.items {
		named = named || (it.item == int32(itemByName["diamond"]) && it.name == "Shiny")
	}
	if !named {
		t.Fatal("the framed item drops whole, its name and all")
	}
	if !h2.arrowHitsHanging(players2, a, 3.5, 200.5, 0.9) || h2.paintings[pt2.eid] != nil {
		t.Fatal("an arrow breaks the painting")
	}
}

// HangingEntity.canCoexist: no painting over a frame hung the same way.
func TestPaintingDoesNotCoverAFrame(t *testing.T) {
	h, _, f, _ := hangingWall(t)
	if h.paintingFits(0, f.x, f.y, f.z, f.dir, 1, 1) {
		t.Fatal("a painting may not hang over a frame facing the same way")
	}
	if !h.paintingFits(0, 1, 202, 0, 2, 1, 1) {
		t.Fatal("clear wall takes a painting")
	}
}

// Painting.dropItem obeys entity_drops; a glow frame has its own sounds.
func TestPaintingDropsFollowEntityDrops(t *testing.T) {
	h, players, _, pt := hangingWall(t)
	h.rules.EntityDrops = false
	h.breakPainting(players, pt, nil)
	for _, it := range h.items {
		if it.item == int32(itemPainting) {
			t.Fatal("entity_drops off: no painting item")
		}
	}
	if frameSound(&itemFrame{glow: true}, "break") != "minecraft:entity.glow_item_frame.break" {
		t.Fatal("a glow item frame breaks with its own sound")
	}
}

// A blow on a knot breaks it and every mob tied there drops its lead; with
// entity_drops off the leads are lost. A happy ghast's lead is longer.
func TestPunchingAKnotUntiesEverything(t *testing.T) {
	for _, drops := range []bool{true, false} {
		h, players := leashWorld(t)
		h.rules.EntityDrops = drops
		tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
		a := leashCow(t, h, players, 1.5, 70, 0.5)
		h.setLeash(players, a, tr.p.eid)
		fence := blockPos{2, 70, 2}
		lo, _ := worldgen.BlockRange("oak_fence")
		h.world.SetBlock(fence.x, fence.y, fence.z, lo)
		h.leashToFence(players, tr, fence)
		var kid int32
		for id := range h.knots {
			kid = id
		}
		h.onAttack(players, evAttack{attacker: tr.p.eid, target: kid})
		if len(h.knots) != 0 || a.leash != 0 {
			t.Fatal("the punched knot is gone and the cow is loose")
		}
		leads := 0
		for _, it := range h.items {
			if it.item == int32(itemLead) {
				leads++
			}
		}
		if (leads == 1) != drops {
			t.Fatalf("entity_drops %v: %d leads dropped", drops, leads)
		}
	}
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 80, 0.5)
	g := h.spawnSpecies(players, entityHappyGhast, 0, 14.5, 80, 0.5)
	h.setLeash(players, g, tr.p.eid)
	h.updateLeashes(players)
	if g.leash == 0 {
		t.Fatal("fourteen blocks is within a happy ghast's sixteen")
	}
}
