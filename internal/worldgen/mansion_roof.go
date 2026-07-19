package worldgen

// createRoof — vanilla MansionPiecePlacer.createRoof, transcribed. `g2` is the
// next floor's grid (nil for the top): where a house cell has a room above it,
// the flat roof is replaced by small walls; open edges get roof fronts/corners.

func (p *mansionPlacer) createRoof(base [3]int, rot int, g, g2 *simpleGrid) {
	isH := gridIsHouse
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			pos := rel(base, mrotDir(rot, mSouth), 8+(y-p.startY)*8)
			pos = rel(pos, mrotDir(rot, mEast), (x-p.startX)*8)
			above2 := g2 != nil && isH(g2, x, y)
			if !isH(g, x, y) || above2 {
				continue
			}
			p.add("roof", above(pos, 3), rot, mirNone)
			if !isH(g, x+1, y) {
				p.add("roof_front", rel(pos, mrotDir(rot, mEast), 6), rot, mirNone)
			}
			if !isH(g, x-1, y) {
				q := rel(rel(pos, mrotDir(rot, mEast), 0), mrotDir(rot, mSouth), 7)
				p.add("roof_front", q, rotCompose(rot, 2), mirNone)
			}
			if !isH(g, x, y-1) {
				p.add("roof_front", rel(pos, mrotDir(rot, mWest), 1), rotCompose(rot, 3), mirNone)
			}
			if !isH(g, x, y+1) {
				q := rel(rel(pos, mrotDir(rot, mEast), 6), mrotDir(rot, mSouth), 6)
				p.add("roof_front", q, rotCompose(rot, 1), mirNone)
			}
		}
	}
	if g2 != nil {
		for y := 0; y < g.h; y++ {
			for x := 0; x < g.w; x++ {
				pos := rel(base, mrotDir(rot, mSouth), 8+(y-p.startY)*8)
				pos = rel(pos, mrotDir(rot, mEast), (x-p.startX)*8)
				above2 := isH(g2, x, y)
				if !isH(g, x, y) || !above2 {
					continue
				}
				if !isH(g, x+1, y) {
					p.add("small_wall", rel(pos, mrotDir(rot, mEast), 7), rot, mirNone)
				}
				if !isH(g, x-1, y) {
					q := rel(rel(pos, mrotDir(rot, mWest), 1), mrotDir(rot, mSouth), 6)
					p.add("small_wall", q, rotCompose(rot, 2), mirNone)
				}
				if !isH(g, x, y-1) {
					q := rel(rel(pos, mrotDir(rot, mWest), 0), mrotDir(rot, mNorth), 1)
					p.add("small_wall", q, rotCompose(rot, 3), mirNone)
				}
				if !isH(g, x, y+1) {
					q := rel(rel(pos, mrotDir(rot, mEast), 6), mrotDir(rot, mSouth), 7)
					p.add("small_wall", q, rotCompose(rot, 1), mirNone)
				}
				if !isH(g, x+1, y) {
					if !isH(g, x, y-1) {
						q := rel(rel(pos, mrotDir(rot, mEast), 7), mrotDir(rot, mNorth), 2)
						p.add("small_wall_corner", q, rot, mirNone)
					}
					if !isH(g, x, y+1) {
						q := rel(rel(pos, mrotDir(rot, mEast), 8), mrotDir(rot, mSouth), 7)
						p.add("small_wall_corner", q, rotCompose(rot, 1), mirNone)
					}
				}
				if isH(g, x-1, y) {
					continue
				}
				if !isH(g, x, y-1) {
					q := rel(rel(pos, mrotDir(rot, mWest), 2), mrotDir(rot, mNorth), 1)
					p.add("small_wall_corner", q, rotCompose(rot, 3), mirNone)
				}
				if !isH(g, x, y+1) {
					q := rel(rel(pos, mrotDir(rot, mWest), 1), mrotDir(rot, mSouth), 8)
					p.add("small_wall_corner", q, rotCompose(rot, 2), mirNone)
				}
			}
		}
	}
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			pos := rel(base, mrotDir(rot, mSouth), 8+(y-p.startY)*8)
			pos = rel(pos, mrotDir(rot, mEast), (x-p.startX)*8)
			above2 := g2 != nil && isH(g2, x, y)
			if !isH(g, x, y) || above2 {
				continue
			}
			if !isH(g, x+1, y) {
				q := rel(pos, mrotDir(rot, mEast), 6)
				if !isH(g, x, y+1) {
					p.add("roof_corner", rel(q, mrotDir(rot, mSouth), 6), rot, mirNone)
				} else if isH(g, x+1, y+1) {
					p.add("roof_inner_corner", rel(q, mrotDir(rot, mSouth), 5), rot, mirNone)
				}
				if !isH(g, x, y-1) {
					p.add("roof_corner", q, rotCompose(rot, 3), mirNone)
				} else if isH(g, x+1, y-1) {
					q2 := rel(rel(pos, mrotDir(rot, mEast), 9), mrotDir(rot, mNorth), 2)
					p.add("roof_inner_corner", q2, rotCompose(rot, 1), mirNone)
				}
			}
			if isH(g, x-1, y) {
				continue
			}
			q := rel(rel(pos, mrotDir(rot, mEast), 0), mrotDir(rot, mSouth), 0)
			if !isH(g, x, y+1) {
				p.add("roof_corner", rel(q, mrotDir(rot, mSouth), 6), rotCompose(rot, 1), mirNone)
			} else if isH(g, x-1, y+1) {
				q2 := rel(rel(q, mrotDir(rot, mSouth), 8), mrotDir(rot, mWest), 3)
				p.add("roof_inner_corner", q2, rotCompose(rot, 3), mirNone)
			}
			if !isH(g, x, y-1) {
				p.add("roof_corner", q, rotCompose(rot, 2), mirNone)
				continue
			}
			if !isH(g, x-1, y-1) {
				continue
			}
			p.add("roof_inner_corner", rel(q, mrotDir(rot, mSouth), 1), rotCompose(rot, 2), mirNone)
		}
	}
}
