package server

import (
	"math"
	"sort"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Entity.move's collision for a mob's box: the box is swept against the
// collision shapes of the blocks around it one axis at a time — up or down
// first, then the larger of the two horizontal moves, then the other
// (Entity.collideWithShapes, Direction.axisStepOrder) — and a walker that
// meets something on the ground tries to step up onto it, to its
// STEP_HEIGHT (Entity.collide's step-up). The shapes are the real per-state
// collision boxes (collisionShape): a slab is half a block, a fence a block
// and a half, a carpet a sixteenth.

// collideEps is Shapes' EPSILON: a move shorter than it is no move, and
// boxes closer than it on the other axes do not meet.
const collideEps = 1e-7

// cbounds is a world box: min x, y, z then max x, y, z.
type cbounds [6]float64

func (b cbounds) move(dx, dy, dz float64) cbounds {
	return cbounds{b[0] + dx, b[1] + dy, b[2] + dz, b[3] + dx, b[4] + dy, b[5] + dz}
}

// expandTowards is AABB.expandTowards: the box grown in the direction of a
// move, on each axis.
func (b cbounds) expandTowards(dx, dy, dz float64) cbounds {
	d := [3]float64{dx, dy, dz}
	for a := 0; a < 3; a++ {
		if d[a] < 0 {
			b[a] += d[a]
		} else {
			b[a+3] += d[a]
		}
	}
	return b
}

// mobAABB is the mob's box at its position (feet at m.y).
func mobAABB(m *mob) cbounds {
	b := m.box()
	hw := b.w / 2
	return cbounds{m.x - hw, m.y, m.z - hw, m.x + hw, m.y + b.h, m.z + hw}
}

// collider is what a mob collides with, gathered once per move.
type collider struct {
	boxes []cbounds
}

// gather collects the world boxes of every block collision shape meeting r.
// Cells one below the box are read too: a fence, a wall or a gate reaches
// half a block into the cell above it.
func (c *collider) gather(w *world.World, m *mob, r cbounds) {
	c.boxes = c.boxes[:0]
	x0, x1 := floorInt(r[0]-collideEps), floorInt(r[3]+collideEps)
	y0, y1 := floorInt(r[1]-collideEps)-1, floorInt(r[4]+collideEps)
	z0, z1 := floorInt(r[2]-collideEps), floorInt(r[5]+collideEps)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				s := w.At(x, y, z)
				if s == worldgen.Air {
					continue
				}
				shape := mobCollisionShape(m, s, y)
				for _, sb := range shape {
					wb := cbounds{sb[0] + float64(x), sb[1] + float64(y), sb[2] + float64(z),
						sb[3] + float64(x), sb[4] + float64(y), sb[5] + float64(z)}
					if wb[3] >= r[0]-collideEps && wb[0] <= r[3]+collideEps &&
						wb[4] >= r[1]-collideEps && wb[1] <= r[4]+collideEps &&
						wb[5] >= r[2]-collideEps && wb[2] <= r[5]+collideEps {
						c.boxes = append(c.boxes, wb)
					}
				}
			}
		}
	}
}

// powderSnowFull is a full block of powder snow under a walker that can
// stand on it, and the falling shape (0.9 high) under a body that has
// fallen more than two and a half blocks.
var (
	powderSnowFull    = []cbox{{0, 0, 0, 1, 1, 1}}
	powderSnowFalling = []cbox{{0, 0, 0, 1, 0.9, 1}}
)

// mobCollisionShape is getCollisionShape under the mob's own collision
// context: the state's boxes, but for powder snow, which holds up only a
// walker that can walk on it while it is above the block and not
// descending, and catches anything falling hard (PowderSnowBlock).
func mobCollisionShape(m *mob, s uint32, y int) []cbox {
	if !isPowderSnow(s) {
		return collisionShape(s)
	}
	if m.airFall > 2.5 {
		return powderSnowFalling
	}
	if canWalkOnPowderSnow(m) && m.y > float64(y)+1-1e-5 {
		return powderSnowFull
	}
	return nil
}

// clipAxis is Shapes.collide on one axis: how far of d the box b can move
// along axis before it meets one of the boxes.
func clipAxis(axis int, b cbounds, boxes []cbounds, d float64) float64 {
	if math.Abs(d) < collideEps {
		return 0
	}
	o1, o2 := (axis+1)%3, (axis+2)%3
	for _, s := range boxes {
		if s[o1+3] <= b[o1]+collideEps || s[o1] >= b[o1+3]-collideEps ||
			s[o2+3] <= b[o2]+collideEps || s[o2] >= b[o2+3]-collideEps {
			continue
		}
		if d > 0 {
			if s[axis] >= b[axis+3]-collideEps {
				d = math.Min(d, s[axis]-b[axis+3])
			}
		} else if s[axis+3] <= b[axis]+collideEps {
			d = math.Max(d, s[axis+3]-b[axis])
		}
	}
	if math.Abs(d) < collideEps {
		return 0
	}
	return d
}

// collideWithShapes is Entity.collideWithShapes: the move resolved axis by
// axis — Y, then the larger horizontal, then the other.
func collideWithShapes(dx, dy, dz float64, b cbounds, boxes []cbounds) (float64, float64, float64) {
	if len(boxes) == 0 {
		return dx, dy, dz
	}
	var r [3]float64
	order := [3]int{1, 0, 2}
	if math.Abs(dx) < math.Abs(dz) {
		order = [3]int{1, 2, 0}
	}
	d := [3]float64{dx, dy, dz}
	for _, a := range order {
		if d[a] != 0 {
			r[a] = clipAxis(a, b.move(r[0], r[1], r[2]), boxes, d[a])
		}
	}
	return r[0], r[1], r[2]
}

// mobCollide is Entity.collide for a mob: the move it can make from where
// it is, with the step-up onto a ledge no taller than its STEP_HEIGHT when
// it is on the ground (or lands this tick) and was stopped across.
func (h *hub) mobCollide(w *world.World, m *mob, c *collider, dx, dy, dz float64) (float64, float64, float64) {
	b := mobAABB(m)
	if dx == 0 && dy == 0 && dz == 0 {
		return 0, 0, 0
	}
	c.gather(w, m, b.expandTowards(dx, dy, dz))
	mx, my, mz := collideWithShapes(dx, dy, dz, b, c.boxes)
	xColl, yColl, zColl := mx != dx, my != dy, mz != dz
	landing := yColl && dy < 0
	step := m.stepHeight()
	if step <= 0 || !(landing || m.onGround) || !(xColl || zColl) {
		return mx, my, mz
	}
	grounded := b
	if landing {
		grounded = b.move(0, my, 0)
	}
	up := grounded.expandTowards(dx, step, dz)
	if !landing {
		up = up.expandTowards(0, -1e-5, 0)
	}
	var cs collider
	cs.gather(w, m, up)
	skip := float32(my)
	cands := make([]float64, 0, 4)
	for _, s := range cs.boxes {
		for _, coord := range [2]float64{s[1], s[4]} {
			rel := float32(coord - grounded[1])
			if rel < 0 || rel == skip || float64(rel) > step {
				continue
			}
			dup := false
			for _, c := range cands {
				if float32(c) == rel {
					dup = true
					break
				}
			}
			if !dup {
				cands = append(cands, float64(rel))
			}
		}
	}
	sort.Float64s(cands)
	for _, cand := range cands {
		sx, sy, sz := collideWithShapes(dx, cand, dz, grounded, cs.boxes)
		if sx*sx+sz*sz > mx*mx+mz*mz {
			return sx, sy - (b[1] - grounded[1]), sz
		}
	}
	return mx, my, mz
}

// stepHeight is the mob's STEP_HEIGHT attribute (0.6 unless its kind or a
// modifier says otherwise).
func (m *mob) stepHeight() float64 {
	return m.mobAttrs().Value(attr.StepHeight)
}
