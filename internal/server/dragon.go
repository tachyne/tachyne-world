package server

import (
	"encoding/binary"
	"log"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ender dragon fight. The dragon is a hub-driven flyer (it skips the
// standard grounded mob physics): it circles the pillar ring, periodically
// swoops at a player, and heals while any end crystal survives. Crystals sit
// on the pillar tops and detonate when hit. Killing the dragon showers XP,
// opens the exit portal (with the egg), and drops the elytra beside it.

const (
	dragonHealth   = 200
	dragonSpeed    = 0.7
	dragonContact  = 8
	dragonHealRate = 2 // HP/s while any crystal lives
)

var (
	entityEnderDragon = entityID("ender_dragon")
	entityEndCrystal  = entityID("end_crystal")

	itemElytra = itemByName["elytra"]
)

type crystal struct {
	eid     int32
	uuid    [16]byte
	x, y, z float64
}

// enterEnd is called when a player arrives in dim 2: first arrival stages
// the fight (unless the dragon is already defeated).
func (h *hub) enterEnd(players map[int32]*tracked, arriving *tracked) {
	if h.rules.DragonDefeated || h.dragon != nil || h.end == nil {
		return
	}
	// Built by hand (not spawnMobIn): its radius broadcast could reach the
	// arriving player, whose copy must come from the view swap alone.
	eid := h.allocEID()
	m := &mob{eid: eid, etype: entityEnderDragon, dim: 2, health: dragonHealth,
		behavior: idleBehavior{},
		x:        0.5, y: float64(worldgen.EndSurfaceY + 25), z: 0.5,
		sx: 0.5, sy: float64(worldgen.EndSurfaceY + 25), sz: 0.5}
	binary.BigEndian.PutUint32(m.uuid[12:], uint32(eid))
	h.mobs[eid] = m
	m.hostile = true
	m.health = dragonHealth
	m.behavior = idleBehavior{} // movement is updateDragon's, not steer()'s
	h.dragon = m
	// spawnMobIn's broadcast is radius-culled; the boss must reach the whole
	// island. Skip the arriving player: the dimension-switch view swap sends
	// them every dim-2 entity exactly once — a duplicate spawn for the same
	// entity id is undefined client behavior.
	for _, t := range players {
		if t.dim == 2 && t != arriving {
			t.p.trySendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, 0, 0))
		}
	}
	log.Printf("end: dragon staged eid=%d at (%.0f,%.0f,%.0f)", m.eid, m.x, m.y, m.z)
	for i := 0; i < worldgen.EndPillars; i++ {
		px := worldgen.EndPillarRing * cosTurn(float64(i)/worldgen.EndPillars)
		pz := worldgen.EndPillarRing * sinTurn(float64(i)/worldgen.EndPillars)
		c := &crystal{eid: h.allocEID(), x: px + 0.5, y: float64(worldgen.EndPillarTop(i)), z: pz + 0.5}
		binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
		h.crystals[c.eid] = c
		for _, t := range players {
			if t.dim == 2 && t != arriving {
				t.p.trySendEv(entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
			}
		}
	}
}

// updateDragon flies the circuit, swoops, heals, and bites.
func (h *hub) updateDragon(players map[int32]*tracked) {
	m := h.dragon
	if m == nil || m.dying > 0 {
		return
	}
	now := h.tick.Load()
	// Waypoint: circle the ring; every ~12s, swoop at a random End player.
	if now >= h.dragonNextAt {
		h.dragonNextAt = now + 240
		h.dragonSwoop = nil
		for _, t := range players {
			if t.dim == 2 && !t.dead && t.gamemode == gmSurvival {
				h.dragonSwoop = t
				break
			}
		}
	}
	var tx, ty, tz float64
	if h.dragonSwoop != nil && !h.dragonSwoop.dead && h.dragonSwoop.dim == 2 {
		tx, ty, tz = h.dragonSwoop.x, h.dragonSwoop.y+1, h.dragonSwoop.z
	} else {
		ang := float64(now%1200) / 1200 // one lap per minute
		tx = worldgen.EndPillarRing * 1.2 * cosTurn(ang)
		tz = worldgen.EndPillarRing * 1.2 * sinTurn(ang)
		ty = float64(worldgen.EndSurfaceY + 28)
	}
	dx, dy, dz := tx-m.x, ty-m.y, tz-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d > 1e-6 {
		step := math.Min(dragonSpeed, d)
		m.x += dx / d * step
		m.y += dy / d * step
		m.z += dz / d * step
		m.yaw = float32(math.Atan2(dz, dx)*180/math.Pi) - 90
	}
	// Contact damage to End players in reach.
	for _, t := range players {
		if t.dim != 2 || t.dead || t.gamemode != gmSurvival || now < t.graceUntil {
			continue
		}
		if dist3(t.x, t.y, t.z, m.x, m.y, m.z) < 4 {
			h.damage(players, t, t.armorReduce(dragonContact))
			h.knockback(t, m.x, m.z)
		}
	}
	// Crystal healing.
	if now%20 == 0 && len(h.crystals) > 0 && m.health < dragonHealth {
		m.health = min(dragonHealth, m.health+dragonHealRate)
	}
	if now%200 == 0 { // ~10s heartbeat while the fight is being debugged
		log.Printf("end: dragon at (%.1f,%.1f,%.1f) hp=%d players-in-end=%d", m.x, m.y, m.z, m.health, func() (n int) {
			for _, t := range players {
				if t.dim == 2 {
					n++
				}
			}
			return
		}())
	}
	// Broadcast: the same relative-move packets every other mob uses (the only
	// entity-movement encoding real clients have verified — sync_entity_position
	// was our sole use of that packet and 26.x clients never rendered a dragon
	// driven by it). NoSync carries that constraint into the event: renderers
	// stay relative forever, saturating + converging on any oversized delta.
	if m.x != m.sx || m.y != m.sy || m.z != m.sz {
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		mv := entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, false)
		mv.NoSync = true
		h.toDimEv(players, 2, mv)
	}
}

// hitCrystal detonates an end crystal (any player hit or arrow).
func (h *hub) hitCrystal(players map[int32]*tracked, eid int32) bool {
	c := h.crystals[eid]
	if c == nil {
		return false
	}
	delete(h.crystals, eid)
	h.toDimEv(players, 2, entGone(eid))
	h.playSoundDim(players, 2, "minecraft:entity.generic.explode", sndBlock, c.x, c.y, c.z, 1, 1)
	h.spawnParticles(players, particleExplosionEmitter, c.x, c.y, c.z, 1, 0.5, 4)
	for _, t := range players { // the blast bites anyone on the pillar
		if t.dim == 2 && !t.dead && t.gamemode == gmSurvival &&
			dist3(t.x, t.y, t.z, c.x, c.y, c.z) < 5 {
			h.damage(players, t, t.armorReduce(6))
		}
	}
	return true
}

// dragonDefeated: XP shower, exit portal + egg, the elytra, eternal glory.
func (h *hub) dragonDefeated(players map[int32]*tracked) {
	h.dragon = nil
	h.rules.DragonDefeated = true
	h.saveRules()
	cx, cy := 0, worldgen.EndSurfaceY
	for h.end.At(cx, cy, 0) != worldgen.Air && cy < worldgen.EndSurfaceY+8 {
		cy++
	}
	// Exit portal: a bedrock dais with end-portal blocks and the egg on top.
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx*dx+dz*dz > 5 {
				continue
			}
			h.setBlockIn(players, 2, blockPos{dx, cy - 1, dz}, worldgen.Bedrock)
			if dx*dx+dz*dz <= 2 && !(dx == 0 && dz == 0) {
				h.setBlockIn(players, 2, blockPos{dx, cy, dz}, worldgen.EndPortalBlock)
			}
		}
	}
	h.setBlockIn(players, 2, blockPos{0, cy, 0}, worldgen.Bedrock)
	h.setBlockIn(players, 2, blockPos{0, cy + 1, 0}, worldgen.DragonEgg)
	h.spawnXPOrbIn(players, 2, 1500, 2.5, float64(cy+1), 0.5)
	h.spawnItemIn(players, 2, itemElytra, 1, 3.5, float64(cy+1), 3.5)
	for _, t := range players {
		t.p.trySendEv(chatEv("The Ender Dragon has fallen!"))
	}
}

// setBlockIn writes + broadcasts a block in an explicit dimension.
func (h *hub) setBlockIn(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	h.worldFor(dim).SetBlock(pos.x, pos.y, pos.z, state)
	body := blockSetEv(pos.x, pos.y, pos.z, state)
	for _, t := range players {
		if t.dim == dim {
			t.p.trySendEv(body)
		}
	}
}

// cosTurn/sinTurn: real trig on turn fractions (worldgen's approx is private).
func cosTurn(t float64) float64 { return math.Cos(2 * math.Pi * t) }
func sinTurn(t float64) float64 { return math.Sin(2 * math.Pi * t) }

// evEndRefresh re-sends the End's boss entities to one player (/refresh).
type evEndRefresh struct{ eid int32 }

func (evEndRefresh) isHubEvent() {}

func (h *hub) onEndRefresh(players map[int32]*tracked, eid int32) {
	t := players[eid]
	if t == nil || t.dim != 2 {
		return
	}
	if m := h.dragon; m != nil {
		t.p.sendEv(entGone(m.eid))
		t.p.sendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
	}
	for _, c := range h.crystals {
		t.p.sendEv(entGone(c.eid))
		t.p.sendEv(entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
	}
}
