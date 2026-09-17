package worldgen

import "sync"

// Jungle temples — JungleTempleStructure + JungleTemplePiece: a 12×15
// mossy cobblestone pyramid on the jungle floor with a tripwire arrow trap
// in its lower corridor, a hidden chest behind a three-lever piston puzzle,
// and a main chest past the trap. One per 32×32-chunk cell (jungle_temples:
// spacing 32, separation 8), in the jungle and bamboo jungle.

const (
	jungleTempleCell  = 512
	jungleTempleWidth = 12
	jungleTempleDepth = 15
)

type JungleTemple struct {
	scatteredPiece
}

var (
	jtCache = map[dtKey]JungleTemple{}
	jtMu    sync.Mutex
	// blocks
	cobblestone      = blockBase("cobblestone")
	mossyCobblestone = blockBase("mossy_cobblestone")
	chiseledBricks   = blockBase("chiseled_stone_bricks")
)

func isJungleBiome(name string) bool {
	return name == "minecraft:jungle" || name == "minecraft:bamboo_jungle"
}

// JungleTempleIn returns the temple whose cell contains (wx,wz), if its site
// is jungle.
func (g *Generator) JungleTempleIn(wx, wz int) JungleTemple {
	if g.nether || g.end {
		return JungleTemple{}
	}
	ox, oz := cellOrigin(wx, jungleTempleCell), cellOrigin(wz, jungleTempleCell)
	x := ox + int(hash01(g.seed, ox, oz, 0x3A01)*float64(jungleTempleCell-8*16))
	z := oz + int(hash01(g.seed, ox, oz, 0x3A02)*float64(jungleTempleCell-8*16))
	if !isJungleBiome(g.BiomeName(x+6, z+7)) {
		return JungleTemple{}
	}
	k := dtKey{g.seed, x, z}
	jtMu.Lock()
	t, ok := jtCache[k]
	jtMu.Unlock()
	if ok {
		return t
	}
	r := newJigsawRNG(g.seed, x^0x3A000000, z)
	p := scatteredPiece{X: x, Z: z, W: jungleTempleWidth, D: jungleTempleDepth, Dir: r.intn(4), Exists: true}
	p.Y = g.averageGround(p)
	t = JungleTemple{p}
	jtMu.Lock()
	jtCache[k] = t
	jtMu.Unlock()
	return t
}

// Chests are the main chest (past the trap) and the hidden one (behind the
// levers); Dispensers the two arrow traps.
func (t JungleTemple) Chests() [2][3]int {
	x1, y1, z1 := t.world(8, -3, 3)
	x2, y2, z2 := t.world(9, -3, 10)
	return [2][3]int{{x1, y1, z1}, {x2, y2, z2}}
}

func (t JungleTemple) Dispensers() [2][3]int {
	x1, y1, z1 := t.world(3, -2, 1)
	x2, y2, z2 := t.world(9, -2, 3)
	return [2][3]int{{x1, y1, z1}, {x2, y2, z2}}
}

// stampJungleTemples writes the parts of any temple overlapping this chunk.
func (g *Generator) stampJungleTemples(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(jungleTempleCell) {
		t := g.JungleTempleIn(baseX+8+off[0], baseZ+8+off[1])
		if !t.Exists || !t.overlaps(baseX, baseZ) {
			continue
		}
		s := &pieceStamp{p: t.scatteredPiece, ch: ch, baseX: baseX, baseZ: baseZ}
		r := newJigsawRNG(g.seed, t.X^0x3A100000, t.Z)
		mossy := func() uint32 { // MossStoneSelector: cobblestone two in five
			if r.intn(10) < 4 {
				return cobblestone
			}
			return mossyCobblestone
		}
		s.buildJungleTemple(mossy)
	}
}

// buildJungleTemple is JungleTemplePiece.postProcess, write for write.
func (s *pieceStamp) buildJungleTemple(stone func() uint32) {
	w, d := jungleTempleWidth, jungleTempleDepth
	s.boxPick(0, -4, 0, w-1, 0, d-1, stone)
	s.boxPick(2, 1, 2, 9, 2, 2, stone)
	s.boxPick(2, 1, 12, 9, 2, 12, stone)
	s.boxPick(2, 1, 3, 2, 2, 11, stone)
	s.boxPick(9, 1, 3, 9, 2, 11, stone)
	s.boxPick(1, 3, 1, 10, 6, 1, stone)
	s.boxPick(1, 3, 13, 10, 6, 13, stone)
	s.boxPick(1, 3, 2, 1, 6, 12, stone)
	s.boxPick(10, 3, 2, 10, 6, 12, stone)
	s.boxPick(2, 3, 2, 9, 3, 12, stone)
	s.boxPick(2, 6, 2, 9, 6, 12, stone)
	s.boxPick(3, 7, 3, 8, 7, 11, stone)
	s.boxPick(4, 8, 4, 7, 8, 10, stone)
	s.airBox(3, 1, 3, 8, 2, 11)
	s.airBox(4, 3, 6, 7, 3, 9)
	s.airBox(2, 4, 2, 9, 5, 12)
	s.airBox(4, 6, 5, 7, 6, 9)
	s.airBox(5, 7, 6, 6, 7, 8)
	s.airBox(5, 1, 2, 6, 2, 2)
	s.airBox(5, 2, 12, 6, 2, 12)
	s.airBox(5, 5, 1, 6, 5, 1)
	s.airBox(5, 5, 13, 6, 5, 13)
	s.place(Air, 1, 5, 5)
	s.place(Air, 10, 5, 5)
	s.place(Air, 1, 5, 9)
	s.place(Air, 10, 5, 9)
	for z := 0; z <= 14; z += 14 {
		s.boxPick(2, 4, z, 2, 5, z, stone)
		s.boxPick(4, 4, z, 4, 5, z, stone)
		s.boxPick(7, 4, z, 7, 5, z, stone)
		s.boxPick(9, 4, z, 9, 5, z, stone)
	}
	s.boxPick(5, 6, 0, 6, 6, 0, stone)
	for x := 0; x <= 11; x += 11 {
		for z := 2; z <= 12; z += 2 {
			s.boxPick(x, 4, z, x, 5, z, stone)
		}
		s.boxPick(x, 6, 5, x, 6, 5, stone)
		s.boxPick(x, 6, 9, x, 6, 9, stone)
	}
	s.boxPick(2, 7, 2, 2, 9, 2, stone)
	s.boxPick(9, 7, 2, 9, 9, 2, stone)
	s.boxPick(2, 7, 12, 2, 9, 12, stone)
	s.boxPick(9, 7, 12, 9, 9, 12, stone)
	s.boxPick(4, 9, 4, 4, 9, 4, stone)
	s.boxPick(7, 9, 4, 7, 9, 4, stone)
	s.boxPick(4, 9, 10, 4, 9, 10, stone)
	s.boxPick(7, 9, 10, 7, 9, 10, stone)
	s.boxPick(5, 9, 7, 6, 9, 7, stone)
	stE := withProps("cobblestone_stairs", "facing", "east")
	stW := withProps("cobblestone_stairs", "facing", "west")
	stS := withProps("cobblestone_stairs", "facing", "south")
	stN := withProps("cobblestone_stairs", "facing", "north")
	s.place(stN, 5, 9, 6)
	s.place(stN, 6, 9, 6)
	s.place(stS, 5, 9, 8)
	s.place(stS, 6, 9, 8)
	for x := 4; x <= 7; x++ {
		s.place(stN, x, 0, 0)
	}
	s.place(stN, 4, 1, 8)
	s.place(stN, 4, 2, 9)
	s.place(stN, 4, 3, 10)
	s.place(stN, 7, 1, 8)
	s.place(stN, 7, 2, 9)
	s.place(stN, 7, 3, 10)
	s.boxPick(4, 1, 9, 4, 1, 9, stone)
	s.boxPick(7, 1, 9, 7, 1, 9, stone)
	s.boxPick(4, 1, 10, 7, 2, 10, stone)
	s.boxPick(5, 4, 5, 6, 4, 5, stone)
	s.place(stE, 4, 4, 5)
	s.place(stW, 7, 4, 5)
	for i := 0; i < 4; i++ {
		s.place(stS, 5, 0-i, 6+i)
		s.place(stS, 6, 0-i, 6+i)
		s.airBox(5, 0-i, 7+i, 6, 0-i, 9+i)
	}
	s.airBox(1, -3, 12, 10, -1, 13)
	s.airBox(1, -3, 1, 3, -1, 13)
	s.airBox(1, -3, 1, 9, -1, 5)
	for z := 1; z <= 13; z += 2 {
		s.boxPick(1, -3, z, 1, -2, z, stone)
	}
	for z := 2; z <= 12; z += 2 {
		s.boxPick(1, -1, z, 3, -1, z, stone)
	}
	s.boxPick(2, -2, 1, 5, -2, 1, stone)
	s.boxPick(7, -2, 1, 9, -2, 1, stone)
	s.boxPick(6, -3, 1, 6, -3, 1, stone)
	s.boxPick(6, -1, 1, 6, -1, 1, stone)
	// The first trap: a tripwire across the corridor firing a dispenser.
	s.place(withProps("tripwire_hook", "facing", "east", "attached", "true"), 1, -3, 8)
	s.place(withProps("tripwire_hook", "facing", "west", "attached", "true"), 4, -3, 8)
	wireEW := withProps("tripwire", "east", "true", "west", "true", "attached", "true")
	s.place(wireEW, 2, -3, 8)
	s.place(wireEW, 3, -3, 8)
	rsNS := withProps("redstone_wire", "north", "side", "south", "side")
	for z := 2; z <= 7; z++ {
		s.place(rsNS, 5, -3, z)
	}
	s.place(withProps("redstone_wire", "north", "side", "west", "side"), 5, -3, 1)
	s.place(withProps("redstone_wire", "east", "side", "west", "side"), 4, -3, 1)
	s.place(mossyCobblestone, 3, -3, 1)
	s.place(withProps("dispenser", "facing", "north"), 3, -2, 1)
	s.place(withProps("vine", "south", "true"), 3, -2, 2)
	// The second trap.
	s.place(withProps("tripwire_hook", "facing", "north", "attached", "true"), 7, -3, 1)
	s.place(withProps("tripwire_hook", "facing", "south", "attached", "true"), 7, -3, 5)
	wireNS := withProps("tripwire", "north", "true", "south", "true", "attached", "true")
	s.place(wireNS, 7, -3, 2)
	s.place(wireNS, 7, -3, 3)
	s.place(wireNS, 7, -3, 4)
	s.place(withProps("redstone_wire", "east", "side", "west", "side"), 8, -3, 6)
	s.place(withProps("redstone_wire", "west", "side", "south", "side"), 9, -3, 6)
	s.place(withProps("redstone_wire", "north", "side", "south", "up"), 9, -3, 5)
	s.place(mossyCobblestone, 9, -3, 4)
	s.place(rsNS, 9, -2, 4)
	s.place(withProps("dispenser", "facing", "west"), 9, -2, 3)
	s.place(withProps("vine", "east", "true"), 8, -1, 3)
	s.place(withProps("vine", "east", "true"), 8, -2, 3)
	s.place(ChestNorth, 8, -3, 3) // the main chest
	s.place(mossyCobblestone, 9, -3, 2)
	s.place(mossyCobblestone, 8, -3, 1)
	s.place(mossyCobblestone, 4, -3, 5)
	s.place(mossyCobblestone, 5, -2, 5)
	s.place(mossyCobblestone, 5, -1, 5)
	s.place(mossyCobblestone, 6, -3, 5)
	s.place(mossyCobblestone, 7, -2, 5)
	s.place(mossyCobblestone, 7, -1, 5)
	s.place(mossyCobblestone, 8, -3, 5)
	s.boxPick(9, -1, 1, 9, -1, 5, stone)
	s.airBox(8, -3, 8, 10, -1, 10)
	s.place(chiseledBricks, 8, -2, 11)
	s.place(chiseledBricks, 9, -2, 11)
	s.place(chiseledBricks, 10, -2, 11)
	lever := withProps("lever", "facing", "north", "face", "wall")
	s.place(lever, 8, -2, 12)
	s.place(lever, 9, -2, 12)
	s.place(lever, 10, -2, 12)
	s.boxPick(8, -3, 8, 8, -3, 10, stone)
	s.boxPick(10, -3, 8, 10, -3, 10, stone)
	s.place(mossyCobblestone, 10, -2, 9)
	s.place(rsNS, 8, -2, 9)
	s.place(rsNS, 8, -2, 10)
	s.place(withProps("redstone_wire", "north", "side", "south", "side", "east", "side", "west", "side"), 10, -1, 9)
	s.place(withProps("sticky_piston", "facing", "up"), 9, -2, 8)
	s.place(withProps("sticky_piston", "facing", "west"), 10, -2, 8)
	s.place(withProps("sticky_piston", "facing", "west"), 10, -1, 8)
	s.place(withProps("repeater", "facing", "north"), 10, -2, 10)
	s.place(ChestNorth, 9, -3, 10) // the hidden chest
}
