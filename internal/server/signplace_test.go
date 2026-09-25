package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// StandingAndWallBlockItem walks the look order: a sign clicked onto a
// ceiling goes on the wall the player faces (or, looking down, on the
// floor); a head faces the player's own yaw where a sign faces it reversed;
// a hanging sign on a wall with nothing sturdy to either side is refused.
func TestSignFamilyPlacementWalksTheLookOrder(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1320, 180, 1320
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y+1, z, worldgen.Stone) // the ceiling clicked
	w.SetBlock(x, y, z+1, worldgen.Stone) // a wall to the south

	p.setHotbarSlot(0, itemByName["oak_sign"])
	selectSlot(p, 0)
	p.yaw, p.pitch = 0, -30 // looking south and up at the ceiling
	s.handlePlace(p, placeBody(x, y+1, z, 0))
	got := w.Block(x, y, z)
	if !isSameBlock(got, worldgen.BlockID("oak_wall_sign")) || propOf(t, got, "facing") != "north" {
		t.Fatalf("a sign clicked on a ceiling should hang on the south wall facing north, got %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)

	// A head stands facing the player's yaw (SkullBlock), a sign reversed.
	w.SetBlock(x, y-1, z, worldgen.Stone)
	p.setHotbarSlot(0, itemByName["skeleton_skull"])
	p.yaw, p.pitch = 0, 60
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	got = w.Block(x, y, z)
	if !isSameBlock(got, worldgen.BlockID("skeleton_skull")) || propOf(t, got, "rotation") != "0" {
		t.Fatalf("a skull set down looking south should have rotation 0, got %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["oak_sign"])
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got = w.Block(x, y, z); propOf(t, got, "rotation") != "8" {
		t.Fatalf("a sign set down looking south should have rotation 8, got %s", propOf(t, got, "rotation"))
	}
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x, y-1, z, worldgen.Air)

	// A hanging sign set on a floor under a low ceiling: the look order
	// skips DOWN, so it hangs from the ceiling instead of being refused.
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y+1, z, worldgen.Stone)
	w.SetBlock(x, y, z+1, worldgen.Air)
	p.setHotbarSlot(0, itemByName["oak_hanging_sign"])
	p.yaw, p.pitch = 0, 60
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	got = w.Block(x, y, z)
	if k, ok := signKind(got); !ok || k != signHangingCeiling {
		t.Fatalf("a hanging sign set on a floor under a ceiling should hang from it, got %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x, y+1, z, worldgen.Air)

	// On the side of a pillar the bracket runs along the pillar's face.
	w.SetBlock(x+1, y, z, worldgen.Stone)
	p.yaw, p.pitch = 270, 0 // looking east at it
	s.handlePlace(p, placeBody(x+1, y, z, 4))
	got = w.Block(x, y, z)
	if k, ok := signKind(got); !ok || k != signHangingWall {
		t.Fatalf("a hanging sign on a pillar's side should be a wall hanging sign, got %d", got)
	}
	if f := propOf(t, got, "facing"); f != "north" && f != "south" {
		t.Errorf("its bracket should run off the clicked east face's axis: facing %s", f)
	}
}

// BellBlock.getStateForPlacement: a wall bell that cannot hang there falls
// back to the floor; one that has nowhere is refused.
func TestBellFallsBackToTheFloor(t *testing.T) {
	w := world.New(1)
	const x, y, z = 60, 180, 60
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	// Clicked "the north face of a block" at z+1 that is not there any more:
	// no wall to hang on, but a floor.
	st, ok := bellPlacedState(w, blockPos{x, y, z}, bellDefault, 2, 0)
	if !ok || propOf(t, st, "attachment") != "floor" {
		t.Fatalf("want a floor bell, got ok=%v %d", ok, st)
	}
	w.SetBlock(x, y-1, z, worldgen.Air)
	if _, ok := bellPlacedState(w, blockPos{x, y, z}, bellDefault, 2, 0); ok {
		t.Fatal("a bell with nothing around it was placed")
	}
	w.SetBlock(x, y, z-1, worldgen.Stone)
	st, ok = bellPlacedState(w, blockPos{x, y, z}, bellDefault, 3, 0) // the south face of the block at z-1
	if !ok || propOf(t, st, "attachment") != "single_wall" || propOf(t, st, "facing") != "north" {
		t.Fatalf("want a single-wall bell facing its wall (north), got ok=%v %d", ok, st)
	}
}

// BellBlock.onProjectileHit: only a proper hit rings, and a player's shot
// counts toward bell_ring.
func TestBellProjectileNeedsAProperHit(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	const x, y, z = 64, 180, 64
	shooter := &tracked{p: newPlayer(7, "archer", [16]byte{}), gamemode: gmSurvival, x: 64.5, y: 180, z: 60}
	players := map[int32]*tracked{7: shooter}
	info, _ := worldgen.InfoForState(bellDefault)
	bell := worldgen.SetProperty(info, bellDefault, "attachment", "floor")
	bell = worldgen.SetProperty(info, bell, "facing", "north")
	w.SetBlock(x, y, z, bell)
	// A floor bell facing north rings from the north or south, not the side.
	fromWest := &arrowEntity{etype: entityArrow, shooter: 7, x: 63.5, y: 180.5, z: 64.5, vx: 1}
	h.projectileHitBlock(players, fromWest, blockPos{x, y, z}, bell)
	fromNorth := &arrowEntity{etype: entityArrow, shooter: 7, x: 64.5, y: 180.5, z: 63.5, vz: 1}
	h.projectileHitBlock(players, fromNorth, blockPos{x, y, z}, bell)
	if n := shooter.stats[statKey{attachproto.StatCustom, customStatID["bell_ring"]}]; n != 1 {
		t.Fatalf("bell_ring = %d after a side hit and a front hit, want 1", n)
	}
}
