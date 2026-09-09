package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An extending piston fills each destination with a moving_piston cell that
// carries the block (and the head as the source), tells viewers what each
// carries, and lays the real blocks down two ticks later.
func TestPistonExtensionAnimates(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	power := worldgen.BlockBase("redstone_block")
	stone := uint32(worldgen.Stone)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, stone)
	w.SetBlock(x, y, z-1, power)
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(x), float64(y), float64(z)+8
	players[pl.p.eid] = pl
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 1)
	if got := w.At(x+2, y, z); !isMovingPiston(got) {
		t.Fatalf("the stone's destination holds %d, want a moving_piston", got)
	}
	if got := w.At(x+1, y, z); !isMovingPiston(got) {
		t.Fatalf("the head's cell holds %d, want a moving_piston", got)
	}
	mb, ok := h.movingBlocks[blockPos{x + 2, y, z}]
	if !ok || mb.moved != stone || !mb.extending || mb.source || mb.facing != [3]int{1, 0, 0} {
		t.Fatalf("stone record %+v ok=%v", mb, ok)
	}
	if hb := h.movingBlocks[blockPos{x + 1, y, z}]; !hb.source || !isPistonHead(hb.moved) {
		t.Fatalf("head record %+v", hb)
	}
	frames := 0
	for len(pl.p.out) > 0 {
		pkt := <-pl.p.out
		if f, ok := pkt.ev.(attachproto.MovingPiston); ok {
			frames++
			if f.X == int32(x+2) {
				if f.Block != "minecraft:stone" || f.Facing != 5 || !f.Extending || f.Source || f.State != stone {
					t.Errorf("stone frame %+v", f)
				}
			}
			if f.X == int32(x+1) {
				if f.Block != "minecraft:piston_head" || !f.Source || f.Props["facing"] != "east" || f.Props["type"] != "normal" {
					t.Errorf("head frame %+v", f)
				}
			}
		}
	}
	if frames != 2 {
		t.Fatalf("%d moving-piston frames, want 2", frames)
	}
	stepTicks(h, players, movingPistonTicks)
	if w.At(x+2, y, z) != stone || !isPistonHead(w.At(x+1, y, z)) {
		t.Fatalf("after two ticks: %d %d", w.At(x+2, y, z), w.At(x+1, y, z))
	}
	if len(h.movingBlocks) != 0 {
		t.Fatalf("records left: %v", h.movingBlocks)
	}
	if !boolProp(w.At(x, y, z), "extended") {
		t.Fatal("the base should be extended")
	}
}

// Retraction turns the base into a moving cell carrying the retracted
// piston (the head slides back into it), and a sticky piston's pulled
// block travels the same way.
func TestPistonRetractionAnimates(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	power := worldgen.BlockBase("redstone_block")
	stone := uint32(worldgen.Stone)
	w.SetBlock(x, y, z, pistonEast(true))
	w.SetBlock(x+1, y, z, stone)
	w.SetBlock(x, y, z-1, power)
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 1+movingPistonTicks)
	if w.At(x+2, y, z) != stone || !isPistonHead(w.At(x+1, y, z)) {
		t.Fatalf("extension did not complete: %d %d", w.At(x+2, y, z), w.At(x+1, y, z))
	}
	w.SetBlock(x, y, z-1, worldgen.Air) // unpower
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 1)
	if got := w.At(x, y, z); !isMovingPiston(got) {
		t.Fatalf("the base holds %d during retraction, want a moving_piston", got)
	}
	mb := h.movingBlocks[blockPos{x, y, z}]
	if !mb.source || mb.extending || !isPistonBase(mb.moved) || boolProp(mb.moved, "extended") {
		t.Fatalf("base record %+v", mb)
	}
	if got := w.At(x+1, y, z); !isMovingPiston(got) {
		t.Fatalf("the pulled stone's destination holds %d", got)
	}
	if pb := h.movingBlocks[blockPos{x + 1, y, z}]; pb.moved != stone || pb.extending || pb.source {
		t.Fatalf("pulled record %+v", pb)
	}
	stepTicks(h, players, movingPistonTicks)
	if got := w.At(x, y, z); !isPistonBase(got) || boolProp(got, "extended") {
		t.Fatalf("base after retraction %d", got)
	}
	if w.At(x+1, y, z) != stone || w.At(x+2, y, z) != worldgen.Air {
		t.Fatalf("stone should be pulled back: %d %d", w.At(x+1, y, z), w.At(x+2, y, z))
	}
	if len(h.movingBlocks) != 0 {
		t.Fatalf("records left: %v", h.movingBlocks)
	}
}

// A moving cell with no record (left from before a restart) resolves to air
// instead of standing forever.
func TestMovingPistonWithoutRecordClears(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y+3, z, movingPistonState([3]int{0, 1, 0}, false))
	h.schedule(blockPos{x, y + 3, z}, 1)
	stepTicks(h, players, 1)
	if got := w.At(x, y+3, z); got != worldgen.Air {
		t.Fatalf("orphan moving cell holds %d", got)
	}
}
