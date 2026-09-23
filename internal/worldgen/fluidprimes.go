package worldgen

// Fluids generation leaves able to run. Vanilla schedules a fluid tick when
// it places a spring (SpringFeature: setBlock, then scheduleTick) and marks
// other generated fluid for post-processing, so a spring starts running the
// moment its chunk exists. tachyne generates the source and never ticked it:
// a spring stayed one still block jutting out of a cave wall, which reads as
// water or lava floating in mid-air.

// UnstableFluids lists a generated chunk's fluid sources that have open air
// beside or below them (in this chunk), in world coordinates: the cells
// vanilla would tick on load. The chunk sits at (cx, cz).
func UnstableFluids(ch *Chunk, cx, cz int32) [][3]int {
	var out [][3]int
	secs := len(ch.Sections)
	at := func(lx, y, lz int) uint32 {
		if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 || y < MinY || y >= MinY+secs*16 {
			return Stone // outside this chunk: not an opening
		}
		return ch.Sections[(y-MinY)/16][((y-MinY)%16*16+lz)*16+lx]
	}
	for s := 0; s < secs; s++ {
		sec := &ch.Sections[s]
		has := false
		for _, v := range sec {
			if v == Water || v == Lava {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		for i, v := range sec {
			if v != Water && v != Lava {
				continue
			}
			lx, lz, y := i%16, (i/16)%16, MinY+s*16+i/256
			for _, d := range [5][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
				if at(lx+d[0], y+d[1], lz+d[2]) == Air {
					out = append(out, [3]int{int(cx)*16 + lx, y, int(cz)*16 + lz})
					break
				}
			}
		}
	}
	return out
}
