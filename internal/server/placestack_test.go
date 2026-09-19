package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// placeBodyAt is placeBody with a chosen cursor height on the clicked face.
func placeBodyAt(x, y, z int, face int32, cursorY float32) []byte {
	b := protocol.AppendVarInt(nil, 0) // hand
	b = protocol.AppendPosition(b, x, y, z)
	b = protocol.AppendVarInt(b, face)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, cursorY)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendBool(b, false)
	b = protocol.AppendBool(b, false)
	return protocol.AppendVarInt(b, 0)
}

// A slab item on a bottom slab's top face doubles it; on the upper half of
// its side too; low on the side it goes beside it instead.
func TestSlabDoublesByClick(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x+1, y-1, z, worldgen.Stone)
	slab := worldgen.BlockID("oak_slab")
	bottom := withProps(t, slab, map[string]string{"type": "bottom", "waterlogged": "false"})
	double := withProps(t, slab, map[string]string{"type": "double", "waterlogged": "false"})
	p.setHotbarSlot(0, itemByName["oak_slab"])
	p.held = 0

	w.SetBlock(x, y, z, bottom)
	s.handlePlace(p, placeBodyAt(x, y, z, 1, 1.0)) // top face
	if got := w.Block(x, y, z); got != double {
		t.Fatalf("clicking a bottom slab's top should double it, got %d", got)
	}
	w.SetBlock(x, y, z, bottom)
	s.handlePlace(p, placeBodyAt(x, y, z, 5, 0.8)) // east face, upper half
	if got := w.Block(x, y, z); got != double {
		t.Fatalf("clicking the upper half of a bottom slab's side should double it, got %d", got)
	}
	w.SetBlock(x, y, z, bottom)
	s.handlePlace(p, placeBodyAt(x, y, z, 5, 0.2)) // east face, lower half: a new slab beside
	if got := w.Block(x, y, z); got != bottom {
		t.Fatalf("a low side click must leave the slab single, got %d", got)
	}
	if got := w.Block(x+1, y, z); !sameBlockFamily(got, slab) || propOf(t, got, "type") != "bottom" {
		t.Fatalf("a low side click places a bottom slab beside, got %d", got)
	}
	// Placing "through" onto a slab: clicking the stone beside it, the target
	// cell already holds a bottom slab, which doubles.
	w.SetBlock(x+1, y, z, bottom)
	s.handlePlace(p, placeBodyAt(x+1, y-1, z, 1, 1.0))
	if got := w.Block(x+1, y, z); got != double {
		t.Fatalf("a slab placed into a cell holding a bottom slab doubles it, got %d", got)
	}
}

// Candles count up on their own block (but not while sneaking), snow layers
// pile from the top, and levers/hoppers take their vanilla orientation.
func TestCountStackingLeverAndHopper(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1220, 180, 1220
	clearAirBox(w, x, y, z, 3)
	for dx := -1; dx <= 1; dx++ {
		w.SetBlock(x+dx, y-1, z, worldgen.Stone)
	}
	candle := worldgen.BlockID("candle")
	one := withProps(t, candle, map[string]string{"candles": "1", "lit": "false", "waterlogged": "false"})
	w.SetBlock(x, y, z, one)
	p.setHotbarSlot(0, itemByName["candle"])
	p.held = 0
	s.handlePlace(p, placeBodyAt(x, y, z, 1, 1.0))
	if got := w.Block(x, y, z); propOf(t, got, "candles") != "2" {
		t.Fatalf("a candle on a candle should make two, got %d", got)
	}
	p.sneaking = true
	s.handlePlace(p, placeBodyAt(x, y, z, 1, 1.0))
	if got := w.Block(x, y, z); propOf(t, got, "candles") != "2" {
		t.Fatalf("sneaking must not stack candles, got %d", got)
	}
	p.sneaking = false

	snow := worldgen.BlockID("snow")
	w.SetBlock(x+1, y, z, snow) // layers=1
	p.setHotbarSlot(0, itemByName["snow"])
	s.handlePlace(p, placeBodyAt(x+1, y, z, 1, 1.0))
	if got := w.Block(x+1, y, z); propOf(t, got, "layers") != "2" {
		t.Fatalf("snow on snow piles a layer, got %d", got)
	}

	// Lever on the floor, looking down: floor face, facing the way the player faces.
	p.setHotbarSlot(0, itemByName["lever"])
	p.yaw, p.pitch = 0, 60
	s.handlePlace(p, placeBodyAt(x-1, y-1, z, 1, 1.0))
	if got := w.Block(x-1, y, z); propOf(t, got, "face") != "floor" || propOf(t, got, "facing") != "south" {
		t.Fatalf("floor lever should face south with the player, got %d", got)
	}
	// Lever on a wall, looking level at it: wall face, facing away from the wall.
	w.SetBlock(x-1, y, z, worldgen.Air)
	w.SetBlock(x-1, y, z+1, worldgen.Stone)
	w.SetBlock(x-1, y-1, z, worldgen.Air)
	p.yaw, p.pitch = 0, 0
	s.handlePlace(p, placeBodyAt(x-1, y, z+1, 2, 0.5))
	if got := w.Block(x-1, y, z); propOf(t, got, "face") != "wall" || propOf(t, got, "facing") != "north" {
		t.Fatalf("wall lever should face north away from its south wall, got %d", got)
	}

	// Hopper: the spout points into the clicked block; down from a floor.
	p.setHotbarSlot(0, itemByName["hopper"])
	w.SetBlock(x, y+1, z, worldgen.Air)
	w.SetBlock(x, y, z, worldgen.Stone)
	s.handlePlace(p, placeBodyAt(x, y, z, 1, 1.0))
	if got := w.Block(x, y+1, z); !isHopper(got) || propOf(t, got, "facing") != "down" {
		t.Fatalf("hopper on a floor points down, got %d", got)
	}
	w.SetBlock(x, y+1, z, worldgen.Air)
	w.SetBlock(x, y+1, z+1, worldgen.Stone)
	s.handlePlace(p, placeBodyAt(x, y+1, z+1, 2, 0.5)) // click the north face of the block to the south
	if got := w.Block(x, y+1, z); !isHopper(got) || propOf(t, got, "facing") != "south" {
		t.Fatalf("hopper against a block's north face points south into it, got %d", got)
	}
}
