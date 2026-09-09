package worldgen

// Capped processor rules. Vanilla's CappedProcessor shuffles a piece's
// blocks and lets its rule fire on the first `limit` that it changes, so a
// trail-ruins house turns six of its gravel blocks into suspicious gravel
// holding the common archaeology table and three more into the rare one,
// and a road or tower top two. The choice has to be made for the whole
// piece at once — a chunk-clipped stamp must agree with its neighbour — so
// the picks are taken here from the piece's surviving candidates by lowest
// position hash and carried on the piece.

// SusCell is one block a capped rule chose, with the loot it appended.
type SusCell struct {
	X, Y, Z int
	State   uint32
	Table   string // the archaeology table ("" when the rule appends none)
}

// cappedPicks resolves every capped rule of the piece's processor list in
// order: candidates are the blocks whose state, after the ordinary rules,
// matches the rule's input and that no earlier capped rule already took.
func (g *Generator) cappedPicks(p *PlacedPiece, salt uint64) []SusCell {
	rules := processors[p.Proc]
	hasCap := false
	for i := range rules {
		if rules[i].Cap > 0 {
			hasCap = true
			break
		}
	}
	if !hasCap {
		return nil
	}
	t := p.Tmpl
	type cand struct {
		pos   [3]int
		state uint32
		h     float64
	}
	var cells []cand
	for _, b := range t.Blocks {
		state := t.resolved[p.Rot&3][b[3]]
		if state == tmplSkip || (p.SkipAir && state == Air) {
			continue
		}
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], p.Rot)
		wx, wy, wz := p.OX+rx, p.OY+ry, p.OZ+rz
		state = applyRules(rules, state, wx, wy, wz, p.OX, p.OY, p.OZ)
		cells = append(cells, cand{[3]int{wx, wy, wz}, state, hash01(g.seed, wx, wz*8192+wy, salt)})
	}
	taken := map[[3]int]bool{}
	var out []SusCell
	for ri := range rules {
		rule := &rules[ri]
		if rule.Cap == 0 || rule.outState == tmplSkip {
			continue
		}
		for n := 0; n < rule.Cap; n++ {
			best := -1
			for i := range cells {
				c := &cells[i]
				if taken[c.pos] || (rule.In != "" && (c.state < rule.inLo || c.state > rule.inHi)) {
					continue
				}
				if best < 0 || c.h < cells[best].h {
					best = i
				}
			}
			if best < 0 {
				break
			}
			taken[cells[best].pos] = true
			out = append(out, SusCell{cells[best].pos[0], cells[best].pos[1], cells[best].pos[2], rule.outState, rule.Loot})
		}
	}
	return out
}

// attachCappedPicks resolves the capped rules of every piece of a structure
// (salted by piece index so two copies of one template pick differently).
func (g *Generator) attachCappedPicks(pieces []PlacedPiece, salt uint64) {
	for i := range pieces {
		pieces[i].Sus = g.cappedPicks(&pieces[i], salt+uint64(i)*7919)
	}
}
