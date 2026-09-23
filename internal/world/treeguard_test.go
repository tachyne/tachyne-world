package world

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The world hands generation its edit overlay, so a generated tree standing
// where a player laid a floor is not grown (worldgen/treeguard.go).
func TestWorldTreesSkipPlayerFloors(t *testing.T) {
	g := worldgen.NewGenerator(1)
	for cx := int32(0); cx < 40; cx++ {
		ch := g.GenerateChunk(cx, 0)
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				x, z := int(cx)*16+lx, lz
				y := g.Height(x, z)
				at := func(y int) uint32 {
					return ch.Sections[(y-worldgen.MinY)/16][((y-worldgen.MinY)%16*16+lz)*16+lx]
				}
				if !worldgen.IsLog(at(y)) || !worldgen.IsLog(at(y+1)) || !worldgen.IsLog(at(y+2)) {
					continue
				}
				w := New(1)
				if !worldgen.IsLog(w.Block(x, y+1, z)) {
					t.Fatalf("an unedited world must grow the tree at %d,%d,%d", x, y, z)
				}
				w = New(1)
				w.SetBlock(x, y-1, z, worldgen.StoneBricks)
				if worldgen.IsLog(w.Block(x, y+1, z)) {
					t.Errorf("a tree grew on a player's floor at %d,%d,%d", x, y, z)
				}
				return
			}
		}
	}
	t.Fatal("no tree found")
}
