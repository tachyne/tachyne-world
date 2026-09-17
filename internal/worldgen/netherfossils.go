package worldgen

// Nether fossils — NetherFossilStructure: in the soul sand valley, one
// attempt per 2×2 chunks (nether_fossils: spacing 2, separation 1), a
// random one of vanilla's fourteen bone-block fossils turned at random,
// dropped from a random height to the first air over soul sand or solid
// ground above the lava, air cells of the template left alone.

const netherFossilCell = 32

// stampNetherFossils writes the parts of any fossil overlapping this chunk.
func (g *Generator) stampNetherFossils(ch *Chunk, cx, cz int32) {
	if TemplateByName("nether_fossils/fossil_1") == nil {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(netherFossilCell) {
		ox, oz := cellOrigin(baseX+8+off[0], netherFossilCell), cellOrigin(baseZ+8+off[1], netherFossilCell)
		x := ox + int(hash01(g.seed, ox, oz, 0xF001)*16) // the cell's first chunk (spacing − separation = 1)
		z := oz + int(hash01(g.seed, ox, oz, 0xF002)*16)
		if x+16 < baseX || x-16 > baseX+16 || z+16 < baseZ || z-16 > baseZ+16 {
			continue
		}
		if g.netherBiome(x, z) != "minecraft:soul_sand_valley" {
			continue
		}
		lo, hi := netherY(32), NetherCeiling-2
		y := lo + int(hash01(g.seed, ox, oz, 0xF003)*float64(hi-lo+1))
		col := g.netherColumn(x, z)
		at := func(py int) uint32 {
			if py < MinY || py >= MinY+len(col) {
				return Air
			}
			return col[py-MinY]
		}
		for y > NetherLavaSea {
			cur, below := at(y), at(y-1)
			if cur == Air && (below == SoulSand || (below != Air && !IsFluid(below) && Collides(below))) {
				break
			}
			y--
		}
		if y <= NetherLavaSea {
			continue
		}
		n := 1 + int(hash01(g.seed, ox, oz, 0xF004)*14)
		rot := int(hash01(g.seed, ox, oz, 0xF005) * 4)
		if t := TemplateByName("nether_fossils/fossil_" + itoaW(n)); t != nil {
			t.StampTemplateProc(ch, cx, cz, x, y, z, rot, nil, true)
		}
	}
}

func itoaW(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}
