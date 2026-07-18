package worldgen

// Jigsaw structure assembly, ported from JigsawPlacement + JigsawBlock. A
// structure grows from a start pool: each placed piece exposes jigsaw blocks;
// for each, a connecting piece is drawn from the block's target pool and rotated
// so its matching jigsaw block sits adjacent and faces back (JigsawBlock.canAttach),
// provided its bounding box does not overlap an already-placed piece. Expansion
// stops at the structure's size (max depth).

// PlacedPiece is one template placed in the world at a rotation.
type PlacedPiece struct {
	Tmpl            *Template
	OX, OY, OZ, Rot int
	x1, y1, z1      int // exclusive max corner (world)
}

var dirDelta = map[string][3]int{
	"north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
	"up": {0, 1, 0}, "down": {0, -1, 0},
}

func oppDir(d string) string {
	switch d {
	case "north":
		return "south"
	case "south":
		return "north"
	case "east":
		return "west"
	case "west":
		return "east"
	case "up":
		return "down"
	case "down":
		return "up"
	}
	return d
}

// rotDir rotates a horizontal direction clockwise by rot quarter turns; up/down
// are unchanged.
func rotDir(d string, rot int) string {
	order := []string{"north", "east", "south", "west"}
	for i, o := range order {
		if o == d {
			return order[(i+rot)&3]
		}
	}
	return d
}

// rotatedSize is a template's world footprint at a rotation (x,z swap on odd).
func (t *Template) rotatedSize(rot int) (int, int, int) {
	if rot&1 == 1 {
		return t.Size[2], t.Size[1], t.Size[0]
	}
	return t.Size[0], t.Size[1], t.Size[2]
}

// bbox returns a piece's world AABB as [min, max) corners.
func (p *PlacedPiece) bbox() (int, int, int, int, int, int) {
	return p.OX, p.OY, p.OZ, p.x1, p.y1, p.z1
}

// overlaps reports whether two half-open AABBs intersect (adjacent pieces that
// merely share a face do not).
func overlaps(ax0, ay0, az0, ax1, ay1, az1, bx0, by0, bz0, bx1, by1, bz1 int) bool {
	return ax0 < bx1 && bx0 < ax1 && ay0 < by1 && by0 < ay1 && az0 < bz1 && bz0 < az1
}

// worldJigsaw returns a placed piece's jigsaw block in world space: position,
// front, top.
func (p *PlacedPiece) worldJigsaw(j jigsawBlock) (int, int, int, string, string) {
	rx, ry, rz := p.Tmpl.rotatePos(j.Pos[0], j.Pos[1], j.Pos[2], p.Rot)
	return p.OX + rx, p.OY + ry, p.OZ + rz, rotDir(j.Front, p.Rot), rotDir(j.Top, p.Rot)
}

type queued struct {
	piece *PlacedPiece
	jig   jigsawBlock
	depth int
}

// AssembleJigsaw grows a jigsaw structure from startPool with its start piece's
// min corner at (sx,sy,sz). Deterministic given the prng. Returns every placed
// piece (for stamping + chest routing).
func (g *Generator) AssembleJigsaw(startPool string, sx, sy, sz int, prng *jigsawRNG, maxDepth int) []PlacedPiece {
	sp := pools[startPool]
	if sp == nil || len(sp.Elements) == 0 {
		return nil
	}
	start := templates[weightedPick(sp, prng)]
	if start == nil {
		return nil
	}
	sw, sh, sd := start.rotatedSize(0)
	pieces := []*PlacedPiece{{Tmpl: start, OX: sx, OY: sy, OZ: sz, Rot: 0,
		x1: sx + sw, y1: sy + sh, z1: sz + sd}}

	q := make([]queued, 0, 16)
	for _, j := range start.Jigsaws {
		q = append(q, queued{pieces[0], j, 0})
	}

	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if cur.depth >= maxDepth {
			continue
		}
		pool := pools[cur.jig.Pool]
		if pool == nil || len(pool.Elements) == 0 {
			continue
		}
		sxp, syp, szp, sFront, sTop := cur.piece.worldJigsaw(cur.jig)
		// The connecting piece's matching jigsaw must sit one block along sFront.
		tx, ty, tz := sxp+dirDelta[sFront][0], syp+dirDelta[sFront][1], szp+dirDelta[sFront][2]
		want := oppDir(sFront)

		placed := false
		for _, loc := range weightedOrder(pool, prng) {
			cand := templates[loc]
			if cand == nil {
				continue
			}
			for _, cj := range cand.Jigsaws {
				for rot := 0; rot < 4 && !placed; rot++ {
					cjFront := rotDir(cj.Front, rot)
					// canAttach: fronts opposed, top aligned (unless rollable), names match.
					if cjFront != want || cur.jig.Target != cj.Name {
						continue
					}
					if cur.jig.Joint != "rollable" && rotDir(cj.Top, rot) != sTop {
						continue
					}
					// Place so cand's cj lands at (tx,ty,tz).
					crx, cry, crz := cand.rotatePos(cj.Pos[0], cj.Pos[1], cj.Pos[2], rot)
					ox, oy, oz := tx-crx, ty-cry, tz-crz
					cw, chh, cdd := cand.rotatedSize(rot)
					nx1, ny1, nz1 := ox+cw, oy+chh, oz+cdd
					clash := false
					for _, p := range pieces {
						if p == cur.piece {
							continue // vanilla: a child may overlap the piece it attaches to
						}
						bx0, by0, bz0, bx1, by1, bz1 := p.bbox()
						if overlaps(ox, oy, oz, nx1, ny1, nz1, bx0, by0, bz0, bx1, by1, bz1) {
							clash = true
							break
						}
					}
					if clash {
						continue
					}
					np := &PlacedPiece{Tmpl: cand, OX: ox, OY: oy, OZ: oz, Rot: rot,
						x1: nx1, y1: ny1, z1: nz1}
					pieces = append(pieces, np)
					for _, nj := range cand.Jigsaws {
						if nj.Pos == cj.Pos {
							continue // the just-connected block
						}
						q = append(q, queued{np, nj, cur.depth + 1})
					}
					placed = true
				}
				if placed {
					break
				}
			}
			if placed {
				break
			}
		}
	}

	out := make([]PlacedPiece, len(pieces))
	for i, p := range pieces {
		out[i] = *p
	}
	return out
}

// StampPieces stamps every piece's template into the chunk (per-chunk clipped)
// and returns the world positions of all chests.
func (g *Generator) StampPieces(ch *Chunk, cx, cz int32, pieces []PlacedPiece) [][3]int {
	var chests [][3]int
	for i := range pieces {
		p := &pieces[i]
		chests = append(chests, p.Tmpl.StampTemplate(ch, cx, cz, p.OX, p.OY, p.OZ, p.Rot)...)
	}
	return chests
}

// ---- deterministic RNG + weighted selection ---------------------------------

// jigsawRNG is a tiny SplitMix64-style generator seeded per structure so a
// structure assembles identically every time its chunks regenerate.
type jigsawRNG struct{ s uint64 }

func newJigsawRNG(seed int64, x, z int) *jigsawRNG {
	return &jigsawRNG{s: uint64(seed) ^ (uint64(uint32(x)) << 32) ^ uint64(uint32(z)) ^ 0x9E3779B97F4A7C15}
}
func (r *jigsawRNG) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
func (r *jigsawRNG) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// weightedPick returns one element's location by weight.
func weightedPick(p *templatePool, r *jigsawRNG) string {
	return weightedOrder(p, r)[0]
}

// weightedOrder returns the pool's element locations in a weighted-random order
// (each element appears once; higher weight → earlier on average).
func weightedOrder(p *templatePool, r *jigsawRNG) []string {
	type wl struct {
		loc string
		key uint64
	}
	list := make([]wl, 0, len(p.Elements))
	for _, e := range p.Elements {
		w := e.Weight
		if w < 1 {
			w = 1
		}
		// Smaller key sorts first; dividing the draw by weight biases high weights early.
		list = append(list, wl{e.Location, r.next() / uint64(w)})
	}
	for i := 1; i < len(list); i++ { // insertion sort (pools are tiny)
		for j := i; j > 0 && list[j].key < list[j-1].key; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.loc
	}
	return out
}
