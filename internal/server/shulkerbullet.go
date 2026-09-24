package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The shulker bullet's flight (ShulkerBullet). It does not curve smoothly
// at its victim: it flies in legs along one axis at a time — east, then
// up, then south — each leg a cell or so long, picking the next leg when
// the current one ends, runs into something it could stand on, or lines up
// with the target on its axis. Only within two blocks of the target does it
// head straight in. It speeds up as it goes (the planned step grows 2.5 % a
// tick), and falls at 0.04 once its target is gone.

const (
	shulkerBulletStep  = 0.15  // ShulkerBullet.SPEED: the planned step, blocks/tick
	shulkerBulletGrow  = 1.025 // the planned step's growth each tick, clamped to ±1 an axis
	shulkerBulletTurn  = 0.2   // how far the motion closes on the planned step each tick
	shulkerBulletNoDir = -1
)

// Axes, for the leg the bullet must not repeat.
const (
	axisNone = iota
	axisX
	axisY
	axisZ
)

// bulletDirs is Direction in its declared order (getRandom indexes it):
// down, up, north, south, west, east.
var bulletDirs = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func bulletDirAxis(d int) int {
	switch d {
	case 0, 1:
		return axisY
	case 2, 3:
		return axisZ
	case 4, 5:
		return axisX
	}
	return axisNone
}

// bulletPlan is the bullet's course: the leg it is on, how many ticks until
// it picks another, and the step it is steering toward.
type bulletPlan struct {
	planned    bool
	dir        int // index into bulletDirs, or shulkerBulletNoDir when heading straight in
	steps      int
	tx, ty, tz float64
}

func floorCell(x, y, z float64) blockPos {
	return blockPos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}
}

// shulkerBulletSelect is selectNextMoveDirection: a new leg toward the
// target, never along avoid.
func (h *hub) shulkerBulletSelect(a *arrowEntity, avoid int, t *tracked) {
	w := h.worldFor(a.dim)
	empty := func(p blockPos) bool {
		st := w.At(p.x, p.y, p.z)
		return st == worldgen.Air || st == caveAirState || st == voidAirState
	}
	half := playerHeight(t) * 0.5
	tp := floorCell(t.x, t.y+half, t.z)
	tx, ty, tz := float64(tp.x)+0.5, float64(tp.y)+half, float64(tp.z)+0.5
	dir := shulkerBulletNoDir
	cx, cy, cz := float64(tp.x)+0.5-a.x, float64(tp.y)+0.5-a.y, float64(tp.z)+0.5-a.z
	if cx*cx+cy*cy+cz*cz >= 4 { // not yet within two blocks of the target's cell
		cur := floorCell(a.x, a.y, a.z)
		var options []int
		if avoid != axisX {
			if cur.x < tp.x && empty(blockPos{cur.x + 1, cur.y, cur.z}) {
				options = append(options, 5)
			} else if cur.x > tp.x && empty(blockPos{cur.x - 1, cur.y, cur.z}) {
				options = append(options, 4)
			}
		}
		if avoid != axisY {
			if cur.y < tp.y && empty(blockPos{cur.x, cur.y + 1, cur.z}) {
				options = append(options, 1)
			} else if cur.y > tp.y && empty(blockPos{cur.x, cur.y - 1, cur.z}) {
				options = append(options, 0)
			}
		}
		if avoid != axisZ {
			if cur.z < tp.z && empty(blockPos{cur.x, cur.y, cur.z + 1}) {
				options = append(options, 3)
			} else if cur.z > tp.z && empty(blockPos{cur.x, cur.y, cur.z - 1}) {
				options = append(options, 2)
			}
		}
		dir = h.rng.Intn(6)
		if len(options) == 0 {
			for tries := 5; tries > 0; tries-- {
				s := bulletDirs[dir]
				if empty(blockPos{cur.x + s[0], cur.y + s[1], cur.z + s[2]}) {
					break
				}
				dir = h.rng.Intn(6)
			}
		} else {
			dir = options[h.rng.Intn(len(options))]
		}
		s := bulletDirs[dir]
		tx, ty, tz = a.x+float64(s[0]), a.y+float64(s[1]), a.z+float64(s[2])
	}
	p := &a.bullet
	p.planned, p.dir = true, dir
	dx, dy, dz := tx-a.x, ty-a.y, tz-a.z
	if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d == 0 {
		p.tx, p.ty, p.tz = 0, 0, 0
	} else {
		p.tx, p.ty, p.tz = dx/d*shulkerBulletStep, dy/d*shulkerBulletStep, dz/d*shulkerBulletStep
	}
	p.steps = 10 + h.rng.Intn(5)*10
}

// shulkerBulletSteer is the first half of ShulkerBullet.tick, before the
// move: close on the planned step toward a live target, or fall once it is
// gone. It returns the live target, or nil.
func (h *hub) shulkerBulletSteer(players map[int32]*tracked, a *arrowEntity) *tracked {
	t := players[a.homing]
	if t == nil || t.dead || t.dim != a.dim || t.gamemode == gmSpectator {
		a.vy -= projectileGravity(a.etype)
		return nil
	}
	p := &a.bullet
	if !p.planned {
		h.shulkerBulletSelect(a, axisNone, t)
	}
	clamp := func(v float64) float64 { return math.Max(-1, math.Min(1, v)) }
	p.tx, p.ty, p.tz = clamp(p.tx*shulkerBulletGrow), clamp(p.ty*shulkerBulletGrow), clamp(p.tz*shulkerBulletGrow)
	a.vx += (p.tx - a.vx) * shulkerBulletTurn
	a.vy += (p.ty - a.vy) * shulkerBulletTurn
	a.vz += (p.tz - a.vz) * shulkerBulletTurn
	return t
}

// shulkerBulletReplan is the second half, after the move: a new leg when
// this one's steps run out, when the next cell along it is something to
// stand on, or when the bullet has drawn level with the target on its axis.
func (h *hub) shulkerBulletReplan(a *arrowEntity, t *tracked) {
	p := &a.bullet
	if p.steps > 0 {
		if p.steps--; p.steps == 0 {
			h.shulkerBulletSelect(a, bulletDirAxis(p.dir), t)
		}
	}
	if p.dir == shulkerBulletNoDir {
		return
	}
	cur := floorCell(a.x, a.y, a.z)
	axis := bulletDirAxis(p.dir)
	s := bulletDirs[p.dir]
	if next := h.worldFor(a.dim).At(cur.x+s[0], cur.y+s[1], cur.z+s[2]); worldgen.IsSturdyTop(next) {
		h.shulkerBulletSelect(a, axis, t)
		return
	}
	tp := floorCell(t.x, t.y, t.z)
	if axis == axisX && cur.x == tp.x || axis == axisZ && cur.z == tp.z || axis == axisY && cur.y == tp.y {
		h.shulkerBulletSelect(a, axis, t)
	}
}

// shootDownShulkerBullet is ShulkerBullet.hurtServer: any blow — a punch, a
// sword — destroys the bullet with a hurt sound and a spray of crits.
func (h *hub) shootDownShulkerBullet(players map[int32]*tracked, a *arrowEntity) {
	h.playSoundDim(players, a.dim, "minecraft:entity.shulker_bullet.hurt", sndHostile, a.x, a.y, a.z, 1, 1)
	h.spawnParticles(players, a.dim, particleCrit, a.x, a.y, a.z, 0.2, 0, 15)
	delete(h.arrows, a.eid)
	h.entityGone(players, a.dim, a.eid)
	h.vibAt(a.dim, freqEntityDamage, a.x, a.y, a.z, 0)
}
