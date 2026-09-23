package worldgen

import "sync"

// Swamp huts — SwampHutStructure + SwampHutPiece: a spruce-plank hut on oak
// stilts over the swamp, a cauldron and a crafting table inside, a witch
// and her black cat at home (the server seeds them). One per 32×32-chunk
// cell (swamp_huts: spacing 32, separation 8), in the swamp.

const (
	swampHutCell  = 512
	swampHutWidth = 7
	swampHutDepth = 9
)

type SwampHut struct {
	scatteredPiece
}

var (
	shCache = map[dtKey]SwampHut{}
	shMu    sync.Mutex
)

// SwampHutIn returns the hut whose cell contains (wx,wz), if its site is
// swamp.
func (g *Generator) SwampHutIn(wx, wz int) SwampHut {
	if g.nether || g.end {
		return SwampHut{}
	}
	ox, oz := cellOrigin(wx, swampHutCell), cellOrigin(wz, swampHutCell)
	x := ox + int(hash01(g.seed, ox, oz, 0x5A01)*float64(swampHutCell-8*16))
	z := oz + int(hash01(g.seed, ox, oz, 0x5A02)*float64(swampHutCell-8*16))
	if g.BiomeName(x+3, z+4) != "minecraft:swamp" {
		return SwampHut{}
	}
	k := dtKey{g.seed, x, z}
	shMu.Lock()
	t, ok := shCache[k]
	shMu.Unlock()
	if ok {
		return t
	}
	r := newJigsawRNG(g.seed, x^0x5A000000, z)
	p := scatteredPiece{X: x, Z: z, W: swampHutWidth, D: swampHutDepth, Dir: r.intn(4), Exists: true}
	p.Y = g.averageGround(p)
	t = SwampHut{p}
	shMu.Lock()
	shCache[k] = t
	shMu.Unlock()
	return t
}

// Home is where the witch and the cat stand (local 2,2,5).
func (t SwampHut) Home() (int, int, int) { return t.world(2, 2, 5) }

// stampSwampHuts writes the parts of any hut overlapping this chunk.
func (g *Generator) stampSwampHuts(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(swampHutCell) {
		t := g.SwampHutIn(baseX+8+off[0], baseZ+8+off[1])
		if !t.Exists || !t.overlaps(baseX, baseZ) {
			continue
		}
		s := &pieceStamp{p: t.scatteredPiece, ch: ch, baseX: baseX, baseZ: baseZ}
		s.buildSwampHut()
	}
}

// buildSwampHut is SwampHutPiece.postProcess, write for write.
func (s *pieceStamp) buildSwampHut() {
	planks := blockBase("spruce_planks")
	log := OakLog
	fence := blockID("oak_fence")
	s.box(1, 1, 1, 5, 1, 7, planks, planks)
	s.box(1, 4, 2, 5, 4, 7, planks, planks)
	s.box(2, 1, 0, 4, 1, 0, planks, planks)
	s.box(2, 2, 2, 3, 3, 2, planks, planks)
	s.box(1, 2, 3, 1, 3, 6, planks, planks)
	s.box(5, 2, 3, 5, 3, 6, planks, planks)
	s.box(2, 2, 7, 4, 3, 7, planks, planks)
	s.box(1, 0, 2, 1, 3, 2, log, log)
	s.box(5, 0, 2, 5, 3, 2, log, log)
	s.box(1, 0, 7, 1, 3, 7, log, log)
	s.box(5, 0, 7, 5, 3, 7, log, log)
	s.airBox(2, 2, 3, 4, 3, 6) // the room itself (vanilla leaves terrain that pokes in; the witch needs her floor)
	s.place(fence, 2, 3, 2)
	s.place(fence, 3, 3, 7)
	s.place(Air, 1, 3, 4)
	s.place(Air, 5, 3, 4)
	s.place(Air, 5, 3, 5)
	s.place(blockBase("potted_red_mushroom"), 1, 3, 5)
	s.place(blockBase("crafting_table"), 3, 2, 6)
	s.place(blockBase("cauldron"), 4, 2, 6)
	s.place(fence, 1, 2, 1)
	s.place(fence, 5, 2, 1)
	stN := withProps("spruce_stairs", "facing", "north")
	stE := withProps("spruce_stairs", "facing", "east")
	stW := withProps("spruce_stairs", "facing", "west")
	stS := withProps("spruce_stairs", "facing", "south")
	s.box(0, 4, 1, 6, 4, 1, stN, stN)
	s.box(0, 4, 2, 0, 4, 7, stE, stE)
	s.box(6, 4, 2, 6, 4, 7, stW, stW)
	s.box(0, 4, 8, 6, 4, 8, stS, stS)
	s.place(withProps("spruce_stairs", "facing", "north", "shape", "outer_right"), 0, 4, 1)
	s.place(withProps("spruce_stairs", "facing", "north", "shape", "outer_left"), 6, 4, 1)
	s.place(withProps("spruce_stairs", "facing", "south", "shape", "outer_left"), 0, 4, 8)
	s.place(withProps("spruce_stairs", "facing", "south", "shape", "outer_right"), 6, 4, 8)
	for z := 2; z <= 7; z += 5 {
		for x := 1; x <= 5; x += 4 {
			s.fillDown(log, x, -1, z)
		}
	}
}

// Contains reports whether a block lies inside the hut's piece (7 wide, 7
// high, 9 deep before rotation) — StructureManager.getStructureWithPieceAt,
// which CatSpawner asks of #cats_spawn_in structures.
func (t SwampHut) Contains(x, y, z int) bool {
	if !t.Exists {
		return false
	}
	sx, sz := t.W, t.D
	if t.Dir >= 2 {
		sx, sz = t.D, t.W
	}
	return x >= t.X && x < t.X+sx && z >= t.Z && z < t.Z+sz && y >= t.Y && y < t.Y+7
}
