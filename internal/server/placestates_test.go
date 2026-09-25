package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Campfire, trapdoor, ladder and concrete powder placement states.
func TestPlacementStatesFromTheLook(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1340, 180, 1340
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	selectSlot(p, 0)

	// CampfireBlock: facing = the way the player looks; unlit into water.
	p.setHotbarSlot(0, itemByName["campfire"])
	p.yaw = 0 // south
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	got := w.Block(x, y, z)
	if !isCampfireBlock(got) || propOf(t, got, "facing") != "south" || propOf(t, got, "lit") != "true" {
		t.Fatalf("campfire: %d", got)
	}
	w.SetBlock(x, y, z, worldgen.WaterBase)
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got = w.Block(x, y, z); propOf(t, got, "lit") != "false" || propOf(t, got, "waterlogged") != "true" {
		t.Fatalf("a campfire set into water should be unlit and waterlogged: %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)

	// TrapDoorBlock: set on a floor it faces away from the player (its
	// hinge toward them); on a side face it hinges on that face.
	p.setHotbarSlot(0, itemByName["oak_trapdoor"])
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got = w.Block(x, y, z); propOf(t, got, "facing") != "north" || propOf(t, got, "half") != "bottom" {
		t.Fatalf("floor trapdoor: facing %s half %s, want north bottom", propOf(t, got, "facing"), propOf(t, got, "half"))
	}
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x+1, y, z, worldgen.Stone)
	s.handlePlace(p, placeBodyAt(x+1, y, z, 4, 0.8)) // the west face, high
	if got = w.Block(x, y, z); propOf(t, got, "facing") != "west" || propOf(t, got, "half") != "top" {
		t.Fatalf("side trapdoor: facing %s half %s, want west top", propOf(t, got, "facing"), propOf(t, got, "half"))
	}
	w.SetBlock(x, y, z, worldgen.Air)

	// LadderBlock: never onto the front of a ladder facing that way.
	p.setHotbarSlot(0, itemByName["ladder"])
	p.yaw = 270 // looking east at the stone
	s.handlePlace(p, placeBody(x+1, y, z, 4))
	lad := w.Block(x, y, z)
	if !isSameBlock(lad, ladderState) || propOf(t, lad, "facing") != "west" {
		t.Fatalf("ladder on the stone: %d", lad)
	}
	s.handlePlace(p, placeBody(x, y, z, 4)) // its front face
	if got = w.Block(x-1, y, z); got != worldgen.Air {
		t.Fatalf("a ladder went onto another ladder's face: %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x+1, y, z, worldgen.Air)

	// ConcretePowderBlock: set beside water it is concrete at once.
	w.SetBlock(x+1, y, z, worldgen.WaterBase)
	p.setHotbarSlot(0, itemByName["white_concrete_powder"])
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got = w.Block(x, y, z); got != worldgen.BlockID("white_concrete") {
		t.Fatalf("powder set beside water: %d, want concrete", got)
	}
}

// BigDripleafBlock: a leaf placed on a leaf takes its facing
// (getStateForPlacement), and the leaf below becomes stem (updateShape).
func TestBigDripleafOnADripleaf(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1310, 180, 1310
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y-1, z, worldgen.BlockID("moss_block"))
	p.setHotbarSlot(0, itemByName["big_dripleaf"])
	selectSlot(p, 0)
	p.yaw = 90 // looking west: a leaf faces east
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got := w.Block(x, y, z); !isBigDripleaf(got) || propOf(t, got, "facing") != "east" {
		t.Fatalf("first leaf: %d", got)
	}
	p.yaw = 0 // looking south now
	s.handlePlace(p, placeBody(x, y, z, 1))
	if got := w.Block(x, y+1, z); !isBigDripleaf(got) || propOf(t, got, "facing") != "east" {
		t.Fatalf("a leaf set on a leaf should keep its facing (east): %d", got)
	}
	onHub(t, h, func() {}) // the placement's sweep has run
	if got := w.Block(x, y, z); !inRange(got, dripleafStemRng) {
		t.Fatalf("the lower leaf did not become stem: %d", got)
	}
	if got := w.Block(x, y, z); propOf(t, got, "facing") != "east" {
		t.Errorf("the stem turned: %s", propOf(t, got, "facing"))
	}
}

// Door, trapdoor or gate placed where power already reaches it goes down
// open and powered, silently (getStateForPlacement), rather than swinging
// open with a sound on the update after.
func TestPoweredOpenablePlacedOpenAndSilent(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 8, 180, 8
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x+1, y, z, worldgen.BlockBase("redstone_block"))
	p.setHotbarSlot(0, itemByName["oak_trapdoor"])
	selectSlot(p, 0)
	onHub(t, h, func() {})
	for len(p.out) > 0 {
		<-p.out
	}
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	onHub(t, h, func() {})
	onHub(t, h, func() {})
	got := w.Block(x, y, z)
	if !boolProp(got, "open") || !boolProp(got, "powered") {
		t.Fatalf("a trapdoor placed beside a redstone block: %d, want open and powered", got)
	}
	for len(p.out) > 0 {
		if snd, ok := (<-p.out).ev.(attachproto.Sound); ok && snd.Name == "minecraft:block.wooden_trapdoor.open" {
			t.Fatal("placing a powered trapdoor played its open sound")
		}
	}
}
