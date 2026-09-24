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
	count      int // how many orbs of that value this one entity stands for
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

// orbValues is the ladder ExperienceOrb.getExperienceValue walks down: an
// award is paid out in the largest denominations that fit, which is why a
// 100-point kill leaves a handful of fat orbs rather than a hundred motes.
var orbValues = [...]int{2477, 1237, 617, 307, 149, 73, 37, 17, 7, 3, 1}

// orbDenomination is the largest ladder value that fits in n.
func orbDenomination(n int) int {
	for _, v := range orbValues {
		if n >= v {
			return v
		}
	}
	return 1
}

// orbMergeGroups is vanilla's ORB_GROUPS_PER_AREA. Orbs only merge with orbs
// whose entity id is congruent to theirs modulo this, which keeps a big award
// spread over a few groups instead of collapsing into one entity — the pile
// still looks like a pile.
const orbMergeGroups = 40

// spawnXPOrbIn drops experience into an explicit dimension, following
// ExperienceOrb.awardWithDirection: split the award down the value ladder and
// fold each piece into an orb that is already lying there when one will take
// it, so a mob farm does not accumulate thousands of entities.
func (h *hub) spawnXPOrbIn(players map[int32]*tracked, dim, value int, x, y, z float64) {
	if value <= 0 {
		return
	}
	y = float64(h.worldFor(dim).DropY(int(x), int(math.Ceil(y)), int(z)))
	for value > 0 {
		piece := orbDenomination(value)
		value -= piece
		if h.mergeIntoNearbyOrb(dim, piece, x, y, z) {
			continue
		}
		eid := h.allocEID()
		o := &xpOrb{eid: eid, dim: dim, x: x, y: y, z: z, sx: x, sy: y, sz: z,
			value: piece, count: 1, born: h.tick.Load()}
		binary.BigEndian.PutUint32(o.uuid[12:], uint32(eid))
		h.orbs[eid] = o // syncTracking shows it to whoever is near enough
	}
}

// mergeIntoNearbyOrb is ExperienceOrb.tryMergeToExisting: an orb of the same
// value inside a one-block box around the drop point, in a randomly chosen
// group, takes the new orb as another count and has its despawn clock reset.
func (h *hub) mergeIntoNearbyOrb(dim, value int, x, y, z float64) bool {
	group := int32(h.rng.Intn(orbMergeGroups))
	for _, o := range h.orbs {
		if o.dim != dim || o.value != value {
			continue
		}
		if (o.eid-group)%orbMergeGroups != 0 {
			continue
		}
		if math.Abs(o.x-x) > 0.5 || math.Abs(o.y-y) > 0.5 || math.Abs(o.z-z) > 0.5 {
			continue
		}
		o.count++
		o.born = h.tick.Load() // the merged orb starts its six minutes again
		return true
	}
	return false
}

// scanForOrbMerges is the once-a-second sweep ExperienceOrb.tick runs: orbs
// that have rolled together since they landed collapse into one entity.
func (h *hub) scanForOrbMerges(players map[int32]*tracked, o *xpOrb) {
	for eid, other := range h.orbs {
		if other == o || other.dim != o.dim || other.value != o.value {
			continue
		}
		if (other.eid-o.eid)%orbMergeGroups != 0 {
			continue
		}
		if math.Abs(other.x-o.x) > 0.5 || math.Abs(other.y-o.y) > 0.5 || math.Abs(other.z-o.z) > 0.5 {
			continue
		}
		o.count += other.count
		if other.born < o.born {
			o.born = other.born // vanilla keeps the LOWER age of the two
		}
		delete(h.orbs, eid)
		h.entityGone(players, other.dim, eid)
	}
}

// updateOrbs collects orbs into nearby survival players and expires the rest.
func (h *hub) updateOrbs(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, t := range players {
		if t.xpTakeDelay > 0 {
			t.xpTakeDelay-- // Player.takeXpDelay: one orb every other tick
		}
	}
	for eid, o := range h.orbs {
		if now-o.born >= orbDespawnTicks {
			delete(h.orbs, eid)
			h.entityGone(players, o.dim, eid)
			continue
		}
		if (now-o.born)%20 == 1 {
			h.scanForOrbMerges(players, o)
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
			if t.gamemode != gmSurvival || t.dead || t.dim != o.dim || t.xpTakeDelay > 0 {
				continue
			}
			if math.Abs(o.x-t.x) > orbPickupDist || math.Abs(o.z-t.z) > orbPickupDist || math.Abs(o.y-t.y) > orbPickupDist {
				continue
			}
			lvl := t.xpLevel
			t.xpTakeDelay = 2
			// Mending first: the orb repairs damaged gear before any of it
			// reaches the bar, and only the remainder is banked.
			if left := h.mendingRepair(t, o.value); left > 0 {
				h.addXP(t, left)
			}
			snd, pitch := "minecraft:entity.experience_orb.pickup", 0.8+h.rng.Float32()*0.8
			if t.xpLevel > lvl {
				snd, pitch = "minecraft:entity.player.levelup", 1
			}
			h.playSoundDim(players, o.dim, snd, sndPlayer, o.x, o.y, o.z, 0.6, pitch)
			h.toTracking(players, eid, o.dim, o.x, o.z, attachproto.Collect{Collected: eid, Collector: t.p.eid, Count: 1})
			// A merged orb pays out one of its stack per touch; it only goes
			// away once the last one has been taken.
			if o.count--; o.count > 0 {
				break
			}
			delete(h.orbs, eid)
			h.entityGone(players, o.dim, eid)
			break
		}
	}
}

// xpForMob is what a player-killed mob yields — vanilla xpReward values from
// vanilla's values: Monster base 5, Animal 1+rand(3), Blaze constructor
// override 10, Slime.setSize xpReward=size, baby zombies ×2.5; villagers and
// iron golems pay nothing. Previously only 4 hostile species paid at all.
func xpForMob(m *mob, rng func(int) int) int {
	return xpBaseForMob(m, rng) + equipmentXPBonus(m, rng)
}

// equipmentXPBonus is the loop at the end of Mob.getBaseExperienceReward: a
// mob that dies wearing or holding something pays 1-3 extra for each piece.
// It is why a skeleton in full iron is worth several times a bare one, and
// the engine paid the bare rate for all of them.
//
// A mob worth nothing to begin with (a villager, an iron golem) stays worth
// nothing: vanilla's loop only runs when the base reward is above zero.
func equipmentXPBonus(m *mob, rng func(int) int) int {
	if xpBaseForMob(m, func(int) int { return 0 }) <= 0 {
		return 0
	}
	n := 0
	if m.held != 0 {
		n += 1 + rng(3)
	}
	for _, g := range m.gear {
		if g.item != 0 {
			n += 1 + rng(3)
		}
	}
	return n
}

// xpBaseForMob is the species' own reward, before anything it is carrying.
func xpBaseForMob(m *mob, rng func(int) int) int {
	switch m.etype {
	case entityCow, entityChicken, entityPig, entitySheep:
		if m.jockey {
			return 10 // Chicken.getBaseExperienceReward for a jockey chicken
		}
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
	// Vanilla per-ore experience (Blocks.java UniformInt ranges on the ore
	// block definitions). Redstone ore has a `lit` property, so both its states
	// (base = lit, base+1 = unlit) must award — a mined ore is often lit.
	switch state {
	case worldgen.CoalOre, worldgen.DeepslateCoalOre:
		return rng(3) // 0-2
	case worldgen.DiamondOre, worldgen.DeepslateDiamondOre,
		worldgen.EmeraldOre, worldgen.DeepslateEmeraldOre:
		return 3 + rng(5) // 3-7
	case worldgen.LapisOre, worldgen.DeepslateLapisOre,
		worldgen.NetherQuartzOre:
		return 2 + rng(4) // 2-5
	case worldgen.RedstoneOre, worldgen.RedstoneOre - 1,
		worldgen.DeepslateRedstoneOre, worldgen.DeepslateRedstoneOre - 1:
		return 1 + rng(5) // 1-5
	case worldgen.NetherGoldOre:
		return rng(2) // 0-1
	}
	if state == spawnerBlock { // SpawnerBlock.spawnAfterBreak: 15 + rand(15) + rand(15)
		return 15 + rng(15) + rng(15)
	}
	for _, r := range sculkXPRanges { // sculk sensor/calibrated sensor/shrieker/catalyst: 5
		if state >= r[0] && state <= r[1] {
			return 5
		}
	}
	return 0
}

// sculkXPRanges are the sculk blocks whose spawnAfterBreak drops experience.
var sculkXPRanges = blockRange("sculk_sensor", "calibrated_sculk_sensor", "sculk_shrieker", "sculk_catalyst")

// infestedBlocks are the InfestedBlock states: mined, they spawn a silverfish.
var infestedBlocks = blockRange("infested_stone", "infested_cobblestone", "infested_stone_bricks",
	"infested_mossy_stone_bricks", "infested_cracked_stone_bricks", "infested_chiseled_stone_bricks", "infested_deepslate")

func isInfested(state uint32) bool {
	for _, r := range infestedBlocks {
		if state >= r[0] && state <= r[1] {
			return true
		}
	}
	return false
}

// dropDeathXP scatters a dying player's experience at the death spot (7×level,
// capped) and zeroes their bar — the other half of the survival stake.
func (h *hub) dropDeathXP(players map[int32]*tracked, t *tracked) {
	if t.xpLevel == 0 && t.xpPoints == 0 {
		return
	}
	// In the dimension the player died in — the overworld default dropped a
	// Nether or End death's XP into a world the player was not in.
	h.spawnXPOrbIn(players, t.dim, min(deathXPLevel*t.xpLevel, deathXPCap), t.x, t.y, t.z)
	t.xpLevel, t.xpPoints = 0, 0
	h.sendExperience(t)
}
