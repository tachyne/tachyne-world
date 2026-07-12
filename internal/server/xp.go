package server

import (
	"encoding/binary"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Experience: orbs drop from player-killed mobs and mined ore, drift into a
// nearby player's total, and drive the client's XP bar via set_experience.
// Levels follow the vanilla curve; dying scatters 7×level (capped) as one orb
// at the death spot and zeroes the bar. XP persists with the inventory.

const (
	orbDespawnTicks = 6000 // 5 minutes, like dropped items
	orbPickupDist   = 1.5
	orbDriftRange   = 8.0 // orbs fly toward a player inside this range…
	orbDriftSpeed   = 0.3 // …at this many blocks/tick

	hostileXP    = 5   // vanilla: zombie/skeleton/spider/creeper all yield 5
	deathXPCap   = 100 // vanilla: a dying player drops 7×level, at most 100
	deathXPLevel = 7
)

var (
	entityXPOrb = entityID("experience_orb") // minecraft:entity_type "experience_orb" (1.21.5)
)

type xpOrb struct {
	dim        int
	eid        int32
	uuid       [16]byte
	x, y, z    float64
	sx, sy, sz float64 // last broadcast position (relative-move baseline)
	value      int
	born       uint64
}

// xpToNext is the vanilla cost of the NEXT level from `level`.
func xpToNext(level int) int {
	switch {
	case level >= 31:
		return 9*level - 158
	case level >= 16:
		return 5*level - 38
	}
	return 2*level + 7
}

// totalXP is the vanilla cumulative experience for a level+points position
// (the set_experience total field; also what a death would have banked).
func totalXP(level, points int) int {
	l := float64(level)
	switch {
	case level >= 32:
		return int(4.5*l*l-162.5*l+2220) + points
	case level >= 17:
		return int(2.5*l*l-40.5*l+360) + points
	}
	return int(l*l+6*l) + points
}

// addXP banks points into a player's bar, rolling levels up the vanilla curve.
func (h *hub) addXP(t *tracked, points int) {
	if points <= 0 {
		return
	}
	t.xpPoints += points
	for t.xpPoints >= xpToNext(t.xpLevel) {
		t.xpPoints -= xpToNext(t.xpLevel)
		t.xpLevel++
	}
	h.sendExperience(t)
}

// sendExperience pushes the bar/level to the client.
func (h *hub) sendExperience(t *tracked) {
	t.p.trySendEv(attachproto.XP{
		Progress: float32(t.xpPoints) / float32(xpToNext(t.xpLevel)),
		Level:    int32(t.xpLevel),
		Total:    int32(totalXP(t.xpLevel, t.xpPoints)),
	})
}

// spawnXPOrb drops an experience orb at (x,y,z), resting it on the local floor.
func (h *hub) spawnXPOrb(players map[int32]*tracked, value int, x, y, z float64) {
	h.spawnXPOrbIn(players, 0, value, x, y, z)
}

// spawnXPOrbIn drops experience into an explicit dimension.
func (h *hub) spawnXPOrbIn(players map[int32]*tracked, dim, value int, x, y, z float64) {
	if value <= 0 {
		return
	}
	y = float64(h.worldFor(dim).DropY(int(x), int(math.Ceil(y)), int(z)))
	eid := h.allocEID()
	o := &xpOrb{eid: eid, dim: dim, x: x, y: y, z: z, sx: x, sy: y, sz: z, value: value, born: h.tick.Load()}
	binary.BigEndian.PutUint32(o.uuid[12:], uint32(eid))
	h.orbs[eid] = o
	h.toNearbyEv(players, dim, x, z, entAdd(eid, entityXPOrb, o.uuid, x, y, z, 0, 0))
}

// updateOrbs collects orbs into nearby survival players and expires the rest.
func (h *hub) updateOrbs(players map[int32]*tracked) {
	now := h.tick.Load()
	for eid, o := range h.orbs {
		if now-o.born >= orbDespawnTicks {
			delete(h.orbs, eid)
			h.toNearbyEv(players, o.dim, o.x, o.z, entGone(eid))
			continue
		}
		// Orbs drift toward the nearest living survival player (vanilla magnetism).
		if near := h.nearestHuntable(players, o.dim, o.x, o.z, orbDriftRange); near != nil {
			dx, dy, dz := near.x-o.x, (near.y+0.5)-o.y, near.z-o.z
			if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d > 1e-6 {
				step := math.Min(orbDriftSpeed, d)
				o.x += dx / d * step
				o.y += dy / d * step
				o.z += dz / d * step
				if o.x != o.sx || o.y != o.sy || o.z != o.sz {
					o.sx, o.sy, o.sz = o.x, o.y, o.z
					h.toNearbyEv(players, o.dim, o.x, o.z, entMove(eid, o.x, o.y, o.z, 0, 0, false))
				}
			}
		}
		for _, t := range players {
			if t.gamemode != gmSurvival || t.dead || t.dim != o.dim {
				continue
			}
			if math.Abs(o.x-t.x) > orbPickupDist || math.Abs(o.z-t.z) > orbPickupDist || math.Abs(o.y-t.y) > orbPickupDist {
				continue
			}
			lvl := t.xpLevel
			h.addXP(t, o.value)
			delete(h.orbs, eid)
			snd, pitch := "minecraft:entity.experience_orb.pickup", 0.8+h.rng.Float32()*0.8
			if t.xpLevel > lvl {
				snd, pitch = "minecraft:entity.player.levelup", 1
			}
			h.playSound(players, snd, sndPlayer, o.x, o.y, o.z, 0.6, pitch)
			h.toNearbyEv(players, o.dim, o.x, o.z, attachproto.Collect{Collected: eid, Collector: t.p.eid, Count: 1})
			h.toNearbyEv(players, o.dim, o.x, o.z, entGone(eid))
			break
		}
	}
}

// xpForMob is what a player-killed mob yields — vanilla xpReward values from
// vanilla's values: Monster base 5, Animal 1+rand(3), Blaze constructor
// override 10, Slime.setSize xpReward=size, baby zombies ×2.5; villagers and
// iron golems pay nothing. Previously only 4 hostile species paid at all.
func xpForMob(m *mob, rng func(int) int) int {
	switch m.etype {
	case entityCow, entityChicken, entityPig, entitySheep:
		return 1 + rng(3) // Animal: 1-3
	case entitySlime, entityMagmaCube:
		return m.size // Slime: xpReward = size (4/2/1 as it splits down)
	case entityBlaze:
		return 10
	case entityIronGolem, entityVillager:
		return 0
	}
	if d := speciesOf(m.etype); d != nil { // roster species: from the table
		switch {
		case d.xp == xpNone:
			return 0
		case d.xp > 0:
			return d.xp
		case d.arch == archPassive || d.arch == archSkittish || d.arch == archWater || d.arch == archFlyer:
			return 1 + rng(3) // Animal category: 1-3
		}
		return hostileXP // Monster base 5
	}
	if m.hostile || m.neutral {
		xp := hostileXP // Monster base 5 (zombie family, skeletons, spiders, …)
		if m.baby {
			xp = int(float64(xp) * 2.5) // Zombie: babies pay 2.5×
		}
		return xp
	}
	return 0
}

// xpForBlock is mining experience (vanilla: only ores that drop the resource
// itself pay XP at the pick; iron/gold/copper pay at the furnace instead).
func xpForBlock(state uint32, rng func(int) int) int {
	switch state {
	case worldgen.CoalOre, worldgen.DeepslateCoalOre:
		return rng(3) // 0-2
	case worldgen.DiamondOre, worldgen.DeepslateDiamondOre:
		return 3 + rng(5) // 3-7
	}
	return 0
}

// dropDeathXP scatters a dying player's experience at the death spot (7×level,
// capped) and zeroes their bar — the other half of the survival stake.
func (h *hub) dropDeathXP(players map[int32]*tracked, t *tracked) {
	if t.xpLevel == 0 && t.xpPoints == 0 {
		return
	}
	h.spawnXPOrb(players, min(deathXPLevel*t.xpLevel, deathXPCap), t.x, t.y, t.z)
	t.xpLevel, t.xpPoints = 0, 0
	h.sendExperience(t)
}
