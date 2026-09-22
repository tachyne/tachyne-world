package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The wither's two side heads. Vanilla's centre head shoots whatever the
// boss is fighting; the side heads pick their own victims — any living thing
// in a twenty-block box, chosen at random — and fire at them on their own
// clock. Heads with nothing to shoot at lob a skull at a random point near
// the boss instead, which is what chews the arena up. All of it is skipped
// on easy, as vanilla skips it.

const (
	witherSideBox     = 20.0 // getNearbyEntities(inflate(20, 8, 20))
	witherSideBoxY    = 8.0
	witherSideMinGap  = 10 // nextHeadUpdate: 10 + rand(10) between re-evaluations
	witherSideRndGap  = 10
	witherSideFireGap = 40 // …and 40 + rand(20) after a head actually fires
	witherSideFireRnd = 20
	witherIdleUpdates = 15   // idleHeadUpdates before a head fires at nothing
	witherIdleSpreadX = 10.0 // the random point it picks then
	witherIdleSpreadY = 5.0
	witherSideRangeSq = 900.0 // a head drops a victim past thirty blocks
	witherTickEvery   = 4     // updateWithers runs on this cadence
)

// witherHeadsTick runs the two side heads for one wither.
func (h *hub) witherHeadsTick(players map[int32]*tracked, m *mob) {
	if m.spawnInvuln > 0 || m.dying != 0 {
		return
	}
	now := int(h.tick.Load())
	for i := 0; i < 2; i++ {
		if now < m.headNext[i] {
			continue
		}
		m.headNext[i] = now + witherSideMinGap + h.rng.Intn(witherSideRndGap)
		if h.rules.Difficulty != diffPeaceful && h.rules.Difficulty != diffEasy {
			if m.headIdle[i]++; m.headIdle[i] > witherIdleUpdates {
				// A bored head fires into the scenery near the boss.
				x := m.x + (h.rng.Float64()*2-1)*witherIdleSpreadX
				y := m.y + (h.rng.Float64()*2-1)*witherIdleSpreadY
				z := m.z + (h.rng.Float64()*2-1)*witherIdleSpreadX
				h.witherSkullAt(players, m, x, y, z, true) // a bored head always fires blue
				m.headIdle[i] = 0
			}
		}
		// A victim it already has, while it is alive, close and visible.
		if o := h.mobs[m.headTarget[i]]; o != nil {
			if o.dying > 0 || o.dim != m.dim ||
				dist3sq(o.x, o.y, o.z, m.x, m.y, m.z) > witherSideRangeSq {
				m.headTarget[i] = 0
			} else {
				h.witherSkullAt(players, m, o.x, o.y+0.5, o.z, false)
				m.headNext[i] = now + witherSideFireGap + h.rng.Intn(witherSideFireRnd)
				m.headIdle[i] = 0
				continue
			}
		}
		if o := h.witherPickVictim(m); o != nil {
			m.headTarget[i] = o.eid
		}
	}
}

// witherPickVictim is the side heads' target selector: a random living thing
// in the box around the boss — anything but another wither or an undead,
// which is why a wither clears a nether fortress of everything but its own.
func (h *hub) witherPickVictim(m *mob) *mob {
	var pool []*mob
	h.grid().nearby(m.dim, m.x, m.z, witherSideBox, func(o *mob) {
		if o.eid == m.eid || o.dying > 0 || o.etype == entityWither || undeadTypes[o.etype] {
			return
		}
		if math.Abs(o.y-m.y) > witherSideBoxY {
			return
		}
		pool = append(pool, o)
	})
	if len(pool) == 0 {
		return nil
	}
	return pool[h.rng.Intn(len(pool))]
}

const (
	// witherSkullBlast is WitherSkull.onHit's explosion: power one, no fire.
	witherSkullBlast = 1
	// witherSkullResistCap is what a BLUE skull holds every breakable block
	// to, so it goes through obsidian the ordinary one only scorches.
	witherSkullResistCap = 0.8
	// witherBlueOdds is the 0.001 roll on the centre head's aimed shot. The
	// side heads' bored shots are always blue, which is where most of them
	// come from.
	witherBlueOdds = 0.001
)

// witherSkullAt fires one skull from the boss toward a point. A blue skull
// (WitherSkull.setDangerous) flies slower and blasts harder.
func (h *hub) witherSkullAt(players map[int32]*tracked, m *mob, x, y, z float64, dangerous bool) {
	ux, uy, uz := aimAt(m.x, m.y+2, m.z, x, y, z)
	v := hurtingSpeed
	a := h.launchProjectileIn(players, entityWitherSkull, m.dim, m.x, m.y+2, m.z, ux*v, uy*v, uz*v)
	a.shooter, a.dmg, a.wither, a.breaks = m.eid, 8, 10, true
	a.explode, a.dangerous = witherSkullBlast, dangerous
	h.playSoundDim(players, m.dim, "minecraft:entity.wither.shoot", sndHostile, m.x, m.y, m.z, 2, 1)
}

// witherSmashTick is WitherBoss.destroyBlocksTick: twenty ticks after it is
// hurt, the wither brings down everything breakable in the box it occupies —
// which is how a wither fight ends up in a crater. Bedrock and the rest of
// #wither_immune stand.
func (h *hub) witherSmashTick(players map[int32]*tracked, m *mob) {
	if m.witherSmash <= 0 {
		return
	}
	if m.witherSmash -= witherTickEvery; m.witherSmash > 0 {
		return
	}
	m.witherSmash = 0
	if !h.rules.MobGriefing {
		return
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	broke := false
	for x := bx - 1; x <= bx+1; x++ {
		for z := bz - 1; z <= bz+1; z++ {
			for y := by; y <= by+3; y++ {
				st := h.worldFor(m.dim).At(x, y, z)
				if st == worldgen.Air || witherImmuneBlock(st) {
					continue
				}
				h.breakBlockDrop(players, m.dim, blockPos{x, y, z}, st)
				broke = true
			}
		}
	}
	if broke {
		h.playSoundDim(players, m.dim, "minecraft:entity.wither.break_block", sndHostile, m.x, m.y, m.z, 1, 1)
	}
}

// witherImmuneBlock is #minecraft:wither_immune — what a wither cannot break.
func witherImmuneBlock(st uint32) bool {
	name, ok := worldgen.StateName(st)
	if !ok {
		return true
	}
	switch name {
	case "barrier", "bedrock", "end_portal", "end_portal_frame", "end_gateway",
		"command_block", "repeating_command_block", "chain_command_block",
		"structure_block", "jigsaw", "moving_piston", "light", "reinforced_deepslate":
		return true
	}
	return false
}
