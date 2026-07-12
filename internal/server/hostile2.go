package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// Hostile pack 2: biome variants (husk/stray/drowned), slimes that split,
// neutral endermen that teleport, and potion-lobbing witches. All ride the
// existing mob framework; only species quirks live here.

const (
	endermanHealth = 40
	endermanDamage = 7
	witchHealth    = 26
	witchDamage    = 4    // v1: her splash potion lands as instant harm
	witchRange     = 10.0 // throw distance
	witchCooldown  = 30   // mob-updates between throws (~3 s)

	metaIndexSlimeSize = 16 // slime metadata: size (VarInt; 1/2/4)

	pearlDamage = 5 // vanilla: landing costs 5 HP
)

var (
	entityDrowned  = entityID("drowned")
	entityEnderman = entityID("enderman")
	entityHusk     = entityID("husk")
	entitySlime    = entityID("slime")
	entityStray    = entityID("stray")
	entityWitch    = entityID("witch")

	entityPearlProj  = entityID("ender_pearl")   // thrown ender pearl
	entitySplashProj = entityID("splash_potion") // witch's splash potion

	itemSlimeball  = itemByName["slime_ball"]
	itemEnderPearl = itemByName["ender_pearl"]
	itemRedstone   = itemByName["redstone"]
	itemGlowstone  = itemByName["glowstone_dust"]
	itemSugar      = itemByName["sugar"]
)

// isColdBiome/isDesertBiome/isSwampBiome pick spawn variants from the
// generator's biome names.
func isColdBiome(b string) bool {
	return strings.Contains(b, "snow") || strings.Contains(b, "frozen") || strings.Contains(b, "ice")
}
func isDesertBiome(b string) bool { return strings.Contains(b, "desert") }
func isSwampBiome(b string) bool  { return strings.Contains(b, "swamp") }

// configureHostile2 applies pack-2 species quirks after spawnHostile's base
// setup. Returns false for species it doesn't know.
func (h *hub) configureHostile2(players map[int32]*tracked, m *mob) bool {
	switch m.etype {
	case entityHusk: // desert zombie: immune to daylight
		m.burns = false
		m.aggro = 35 // zombie-family FOLLOW_RANGE (vanilla 1.21.5)
		m.armor = 2  // zombie-family base ARMOR
		m.reinf = h.rollReinforcements()
		h.rollZombieBaby(players, m)
	case entityStray, entityDrowned: // cold skeleton / wet zombie: burn like their cousins
		m.burns = true
		m.burnDelay = h.rng.Intn(burnStaggerMax)
		if m.etype == entityStray {
			m.behavior = rangedBehavior{}
			h.toNearbyEv(players, m.dim, m.x, m.z, skeletonEquip(m.eid))
		} else {
			m.aggro = 35 // drowned are zombies too
			m.armor = 2
			m.reinf = h.rollReinforcements()
			h.rollZombieBaby(players, m)
		}
	case entitySlime:
		m.size = 4
		m.health = m.size * m.size
		m.speed = slimeSpeed(m.etype, m.size) // attr 0.2 + 0.1×size
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(slimeMeta(m.eid, m.size)))
	case entityEnderman:
		m.neutral = true // holds its peace until hit
		m.aggro = 64     // EnderMan FOLLOW_RANGE (vanilla 1.21.5)
	case entityWitch:
		m.behavior = rangedBehavior{} // keeps her distance like a skeleton
	default:
		return false
	}
	return true
}

// slimeSpeed derives a slime's per-step speed from vanilla's size-scaled
// attribute (Slime.setSize: MOVEMENT_SPEED = 0.2 + 0.1×size; magma cubes
// stay at their flat 0.2 base).
func slimeSpeed(etype, size int) float64 {
	if etype == entityMagmaCube {
		return 0.2 * 0.45
	}
	return (0.2 + 0.1*float64(size)) * 0.45
}

// slimeHop is vanilla SlimeMoveControl adapted to our step model: a slime
// travels ONLY mid-bound and sits still between hops. jumpDelay is
// rand(20)+10 ticks, ÷3 while hunting; each launch rides a jump impulse to
// the client so the arc animates (jump power 0.42, pure visual).
func (h *hub) slimeHop(players map[int32]*tracked, m *mob) {
	if m.hopTicks > 0 {
		m.hopTicks-- // sailing: keep the launch heading
		return
	}
	m.vx, m.vz = 0, 0 // grounded between bounds: slimes don't slide
	if m.hopDelay--; m.hopDelay > 0 {
		return
	}
	delay := (10 + h.rng.Intn(20)) / mobMoveInterval
	if m.hasTarget {
		delay /= 3
	}
	if delay < 1 {
		delay = 1
	}
	m.hopDelay = delay
	m.hopTicks = 4 // ~8 ticks of travel per bound
	var dx, dz float64
	if m.hasTarget {
		dx, dz = m.tx-m.x, m.tz-m.z
	} else {
		ang := h.rng.Float64() * 2 * math.Pi
		dx, dz = math.Cos(ang), math.Sin(ang)
	}
	if d := math.Hypot(dx, dz); d > 1e-6 {
		m.vx, m.vz = dx/d*m.speed, dz/d*m.speed
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Velocity{
		EID: m.eid, VX: m.vx / mobMoveInterval, VY: 0.42, VZ: m.vz / mobMoveInterval})
	h.playSound(players, "minecraft:entity.slime.jump", sndHostile, m.x, m.y, m.z, 0.4, 0.8+h.rng.Float32()*0.4)
}

// slimeMeta builds the slime size metadata (the client scales the cube).
func slimeMeta(eid int32, size int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexSlimeSize)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(size))
	return protocol.AppendU8(b, itemMetaEnd)
}

// splitSlime spawns the smaller halves when a slime dies (vanilla: 2-4 of
// half the size; the smallest just dies and drops slimeballs).
func (h *hub) splitSlime(players map[int32]*tracked, m *mob) {
	if m.size <= 1 {
		return
	}
	for i := 0; i < 2+h.rng.Intn(3); i++ {
		var s *mob
		if m.dim == 0 {
			s = h.spawnHostile(players, m.etype, int(m.x)+h.rng.Intn(3)-1, int(m.z)+h.rng.Intn(3)-1)
		} else { // nether magma cubes split in place, in their own world
			s = h.spawnMobIn(players, m.etype, m.dim, m.x+float64(h.rng.Intn(3)-1), m.y, m.z+float64(h.rng.Intn(3)-1))
			if s != nil {
				s.hostile, s.behavior = true, Behavior(hostileBehavior{})
			}
		}
		if s == nil {
			continue // plugin-cancelled split half
		}
		s.size = m.size / 2
		s.health = s.size * s.size
		s.speed = slimeSpeed(s.etype, s.size)
		h.toNearbyEv(players, s.dim, s.x, s.z, metaEv(slimeMeta(s.eid, s.size)))
	}
}

// endermanTeleport blinks an enderman to a random nearby spot (on being hit,
// or when rained on) with the vanilla warp sound.
func (h *hub) endermanTeleport(players map[int32]*tracked, m *mob) {
	for try := 0; try < 16; try++ {
		// Vanilla EnderMan.teleport(): ±32 blocks on each horizontal axis.
		x := int(m.x) + h.rng.Intn(65) - 32
		z := int(m.z) + h.rng.Intn(65) - 32
		if !h.world.Spawnable(x, z) {
			continue
		}
		h.playSound(players, "minecraft:entity.enderman.teleport", sndHostile, m.x, m.y, m.z, 1, 1)
		m.x, m.z = float64(x)+0.5, float64(z)+0.5
		m.y = float64(h.world.MobFeet(x, z))
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, true))
		return
	}
}

// witchThrow lobs a splash potion at the nearest huntable player (her ranged
// "melee" — the projectile lands as instant harm until brewing exists).
func (h *hub) witchThrow(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, witchRange)
	if t == nil {
		return
	}
	dx, dy, dz := t.x-m.x, (t.y+0.5)-(m.y+1.2), t.z-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	v := 1.0
	a := h.launchProjectileIn(players, entitySplashProj, m.dim, m.x, m.y+1.2, m.z, dx/d*v, dy/d*v+0.06*d, dz/d*v)
	a.shooter, a.dmg, a.breaks, a.poison = m.eid, witchDamage, true, true
	h.playSound(players, "minecraft:entity.witch.throw", sndHostile, m.x, m.y, m.z, 1, 1)
	m.attackCD = witchCooldown
}

// throwPearl handles a player's ender-pearl right-click: the pearl flies, and
// where it shatters the thrower lands (paying the vanilla 5 HP).
func (h *hub) throwPearl(players map[int32]*tracked, t *tracked) {
	slot := -1
	for i := range t.inv.slots {
		if s := &t.inv.slots[i]; s.item == itemEnderPearl && s.count > 0 {
			slot = i
			break
		}
	}
	if t.gamemode == gmSurvival {
		if slot < 0 {
			return
		}
		if s := &t.inv.slots[slot]; true {
			if s.count--; s.count == 0 {
				*s = invStack{}
			}
			h.sendSlot(t, slot)
		}
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	v := 1.5
	a := h.launchProjectileIn(players, entityPearlProj, t.dim, t.x, t.y+1.5, t.z, dx*v, dy*v, dz*v)
	a.shooter, a.breaks, a.pearl = t.p.eid, true, true
	a.noHitUntil = h.tick.Load() + arrowNoSelfHT
	a.playerShot = true
	h.playSound(players, "minecraft:entity.ender_pearl.throw", sndPlayer, t.x, t.y, t.z, 0.5, 0.6+h.rng.Float32()*0.4)
}

// pearlLand teleports the thrower to the shatter point (vanilla: 5 HP toll).
func (h *hub) pearlLand(players map[int32]*tracked, a *arrowEntity) {
	t := players[a.shooter]
	if t == nil || t.dead {
		return
	}
	t.x, t.y, t.z = a.x, float64(h.world.DropY(int(a.x), int(math.Ceil(a.y)), int(a.z))), a.z
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	h.playSound(players, "minecraft:entity.enderman.teleport", sndPlayer, t.x, t.y, t.z, 1, 1)
	h.damage(players, t, pearlDamage)
}

// itemCarvedPumpkin resolves from the generated registry (a hardcoded 345
// drifted to spruce_fence in the 1.21.11 id migration — the enderman
// disguise silently stopped working).
var itemCarvedPumpkin = int32(itemByName["carved_pumpkin"])

// staredAt implements EnderMan.isBeingStaredBy (vanilla behavior): a survival player
// within follow range whose view vector points at the enderman's eyes —
// dot(view, dir) > 1 − 0.025/d — unless they wear a carved pumpkin.
func (h *hub) staredAt(players map[int32]*tracked, m *mob) bool {
	reach := m.aggro // endermen carry FOLLOW_RANGE 64
	for _, t := range players {
		if t.dim != m.dim || t.gamemode != gmSurvival || t.dead {
			continue
		}
		if t.armor[0].item == itemCarvedPumpkin {
			continue // the disguise works
		}
		ex, ey, ez := m.x-t.x, (m.y+2.55)-(t.y+1.62), m.z-t.z // eye to eye
		d := math.Sqrt(ex*ex + ey*ey + ez*ez)
		if d < 1e-6 || d > reach {
			continue
		}
		yawR := float64(t.yaw) * math.Pi / 180
		pitchR := float64(t.pitch) * math.Pi / 180
		vx := -math.Sin(yawR) * math.Cos(pitchR)
		vy := -math.Sin(pitchR)
		vz := math.Cos(yawR) * math.Cos(pitchR)
		if (vx*ex+vy*ey+vz*ez)/d > 1-0.025/d {
			return true
		}
	}
	return false
}
