package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Hostile pack 2: biome variants (husk/stray/drowned), slimes that split,
// neutral endermen that teleport, and potion-lobbing witches. All ride the
// existing mob framework; only species quirks live here.

const (
	endermanHealth = 40
	endermanDamage = 7
	witchHealth    = 26
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

	entityPearlProj  = entityID("ender_pearl")      // thrown ender pearl
	entitySplashProj = entityID("splash_potion")    // witch's splash potion
	entityLingerProj = entityID("lingering_potion") // a thrown lingering potion: its own entity type

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
	case entityHusk: // desert zombie: immune to daylight (not in #burn_in_daylight)
		m.setFollowRange(35) // zombie-family FOLLOW_RANGE (vanilla 1.21.5)
		m.setBaseArmor(2)    // zombie-family base ARMOR
		h.rollReinforcements(m)
		h.rollZombieBaby(players, m)
	case entityStray, entityDrowned: // cold skeleton / wet zombie: burn like their cousins
		if m.etype == entityStray {
			m.behavior = rangedBehavior{}
			h.toTracking(players, m.eid, m.dim, m.x, m.z, skeletonEquip(m.eid))
		} else {
			m.setFollowRange(35) // drowned are zombies too
			m.setBaseArmor(2)
			h.rollReinforcements(m)
			h.rollZombieBaby(players, m)
			// Drowned.populateDefaultEquipmentSlots: one in ten spawns armed,
			// ten of sixteen of those with a trident, the rest a fishing rod.
			if !m.baby && h.rng.Float64() > 0.9 {
				if h.rng.Intn(16) < 10 {
					m.trident = true
					// DrownedTridentAttackGoal: close to ten blocks and throw,
					// standing its ground — no skeleton kiting.
					m.behavior = holdRangedBehavior{radius: tridentRange}
					h.toTracking(players, m.eid, m.dim, m.x, m.z, mobEquip(m.eid, itemTrident))
				} else {
					m.held = int32(itemFishingRod)
					h.toTracking(players, m.eid, m.dim, m.x, m.z, mobEquip(m.eid, m.held))
				}
			}
		}
	case entitySlime:
		m.size = 4
		m.applyCubeSize()
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(slimeMeta(m.eid, m.size)))
	case entityEnderman:
		m.neutral = true     // holds its peace until hit
		m.setFollowRange(64) // EnderMan FOLLOW_RANGE (vanilla 1.21.5)
	case entityWitch:
		m.behavior = holdRangedBehavior{radius: witchRange} // RangedAttackGoal(1.0, 60, 10): walks in, then stands and throws
	default:
		return false
	}
	return true
}

// slimeSpeed derives a cube's per-step speed from vanilla's size-scaled
// MOVEMENT_SPEED (AbstractCubeMob.setSize: 0.2 + 0.1×size). Magma cubes take
// the same formula — their 0.2 in createAttributes is superseded the moment
// setSize runs, which it always does.
func slimeSpeed(size int) float64 {
	return (0.2 + 0.1*float64(size)) * attrToStep
}

// applyCubeSize derives everything a slime or magma cube's size determines,
// following AbstractCubeMob.setSize plus the two subclasses' overrides: health
// is size squared, movement speed scales with size, attack damage IS the size,
// and a magma cube additionally armours up by 3 per size step.
//
// Every place that assigns a size has to call this — which is the point of
// having it in one function rather than four half-copies.
func (m *mob) applyCubeSize() {
	if m.etype == entitySulfurCube {
		// SulfurCube.setCubeMobHealth: four per size, not size squared; and
		// it has no attack at all (canDealDamage is false).
		m.setMaxHP(4 * m.size)
		m.health = m.maxHP()
		m.setMoveSpeed(slimeSpeed(m.size))
		m.setAttackDamage(0)
		return
	}
	m.setMaxHP(m.size * m.size)
	m.health = m.maxHP()
	m.setMoveSpeed(slimeSpeed(m.size))
	m.setAttackDamage(float64(m.size))
	if m.etype == entityMagmaCube {
		m.setBaseArmor(float64(m.size) * 3)
	}
}

// slimeHop is vanilla SlimeMoveControl adapted to our step model: a slime
// travels ONLY mid-bound and sits still between hops. jumpDelay is
// rand(20)+10 ticks (a magma cube's four times that), ÷3 while hunting; each launch rides a jump impulse to
// the client so the arc animates (jump power 0.42, pure visual).
func (h *hub) slimeHop(players map[int32]*tracked, m *mob) {
	m.slimeHeadingLeft -= mobMoveInterval
	if m.hopTicks > 0 {
		m.hopTicks-- // sailing: keep the launch heading
		return
	}
	m.vx, m.vz = 0, 0 // grounded between bounds: slimes don't slide
	if m.hopDelay--; m.hopDelay > 0 {
		return
	}
	delay := 10 + h.rng.Intn(20) // Slime.getJumpDelay
	if m.etype == entityMagmaCube {
		delay *= 4 // MagmaCube.getJumpDelay: four times the wait
	}
	delay /= mobMoveInterval
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
		// SlimeRandomDirectionGoal: one heading, held for 40 to 100 ticks.
		if m.slimeHeadingLeft <= 0 {
			m.slimeHeadingLeft = 40 + h.rng.Intn(60)
			m.slimeHeading = float64(h.rng.Intn(360)) * math.Pi / 180
		}
		dx, dz = math.Cos(m.slimeHeading), math.Sin(m.slimeHeading)
	}
	if d := math.Hypot(dx, dz); d > 1e-6 {
		m.vx, m.vz = dx/d*m.moveSpeed(), dz/d*m.moveSpeed()
	}
	vy := 0.42
	if m.etype == entityMagmaCube {
		vy += float64(m.size) * 0.1 // MagmaCube.jumpFromGround: bigger cubes jump higher
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Velocity{
		EID: m.eid, VX: m.vx / mobMoveInterval, VY: vy, VZ: m.vz / mobMoveInterval})
	h.playSoundDim(players, m.dim, "minecraft:entity.slime.jump", sndHostile, m.x, m.y, m.z, 0.4, 0.8+h.rng.Float32()*0.4)
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
			s = h.spawnHostileIn(players, m.etype, m.dim, int(m.x)+h.rng.Intn(3)-1, int(m.z)+h.rng.Intn(3)-1)
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
		s.applyCubeSize()
		h.toTracking(players, s.eid, s.dim, s.x, s.z, metaEv(slimeMeta(s.eid, s.size)))
	}
}

// endermanTeleport is Enderman.teleport(): a random point within 32 blocks
// on each horizontal axis and 32 up or down, landed by randomTeleport's
// rule (endermanTeleportTo). One try; the callers that must get away
// (a projectile, a splash) retry with endermanTeleportHard.
func (h *hub) endermanTeleport(players map[int32]*tracked, m *mob) bool {
	x := m.x + (h.rng.Float64()-0.5)*64
	y := m.y + float64(h.rng.Intn(64)-32)
	z := m.z + (h.rng.Float64()-0.5)*64
	return h.endermanTeleportTo(players, m, x, y, z)
}

// endermanTeleportHard is repeatedlyTryToTeleport: up to 64 tries.
func (h *hub) endermanTeleportHard(players map[int32]*tracked, m *mob) {
	for i := 0; i < 64; i++ {
		if h.endermanTeleport(players, m) {
			return
		}
	}
}

// throwPearl handles a player's ender-pearl right-click: the pearl flies, and
// where it shatters the thrower lands (paying the vanilla 5 HP). The item's
// use_cooldown (a second) gates the next throw and is shown on the client.
func (h *hub) throwPearl(players map[int32]*tracked, t *tracked) {
	if h.onCooldown(t, itemEnderPearl) {
		return
	}
	if s := usedStack(t); s.item != itemEnderPearl || s.count <= 0 {
		return // EnderpearlItem.use throws the stack in the hand used
	}
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	vx, vy, vz := h.throwFromRotation(t, 0, 1.5, throwUncertainty) // EnderpearlItem.use
	a := h.launchProjectileIn(players, entityPearlProj, t.dim, t.x, t.y+1.5, t.z, vx, vy, vz)
	a.shooter, a.breaks, a.pearl = t.p.eid, true, true
	a.noHitUntil = h.tick.Load() + arrowNoSelfHT
	a.playerShot = true
	h.playSoundDim(players, t.dim, "minecraft:entity.ender_pearl.throw", sndPlayer, t.x, t.y, t.z, 0.5, 0.6+h.rng.Float32()*0.4)
	h.setCooldown(t, itemEnderPearl, pearlCooldown)
}

// pearlLand teleports the thrower to the shatter point (vanilla: 5 HP toll).
func (h *hub) pearlLand(players map[int32]*tracked, a *arrowEntity) {
	t := players[a.shooter]
	if t == nil || t.dead {
		return
	}
	// The drop height comes from the dimension the pearl was thrown in: this
	// read the overworld, so a pearl thrown in the Nether or the End landed
	// its thrower at whatever height the overworld happened to have there.
	w := h.worldFor(a.dim)
	if w == nil {
		w = h.world
	}
	t.x, t.y, t.z = a.x, float64(w.DropY(int(a.x), int(math.Ceil(a.y)), int(a.z))), a.z
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	h.playSoundDim(players, a.dim, "minecraft:entity.enderman.teleport", sndPlayer, t.x, t.y, t.z, 1, 1)
	h.vibAt(t.dim, freqTeleport, t.x, t.y, t.z, t.p.eid)
	h.damageOf(players, t, pearlDamage, dtEnderPearl)
	h.pearlEndermite(players, a) // one pearl in twenty leaves an endermite behind
}

// itemCarvedPumpkin resolves from the generated registry (a hardcoded 345
// drifted to spruce_fence in the 1.21.11 id migration — the enderman
// disguise silently stopped working).
var itemCarvedPumpkin = int32(itemByName["carved_pumpkin"])
var itemJackOLantern = int32(itemByName["jack_o_lantern"])

// setClimbing syncs a spider's climbing flag, and only when it changed — the
// client renders a climbing spider clinging to the wall, and re-sending an
// unchanged flag every update would be pure noise.
//
// Spider.setClimbing writes bit 0x01 of the spider's own flags byte. That byte
// is Spider's first synced field, so it sits at index 16 (Entity 8 +
// LivingEntity 7 + Mob 1) — the same index in 1.21.5 and 26.2, because Spider
// is not ageable and the 26.x insertion that shifted everything is in the
// AgeableMob chain.
func (h *hub) setClimbing(players map[int32]*tracked, m *mob, on bool) {
	if m.climbing == on {
		return
	}
	m.climbing = on
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(spiderClimbMeta(m.eid, on)))
}

const metaIndexSpiderFlags = 16

func spiderClimbMeta(eid int32, climbing bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexSpiderFlags)
	b = protocol.AppendVarInt(b, 0) // type 0: byte
	var flags byte
	if climbing {
		flags = 0x01 // Spider.setClimbing: flags | 1
	}
	b = protocol.AppendU8(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}

// climbsWalls and isAmphibious are capabilities of a SPECIES, not state set by
// whichever spawn path happened to create the mob — the same lesson the leash
// code learned when a zombie spawned outside spawnHostileY read as peaceful.
//
// Spider.onClimbable: a spider goes up whatever it walks into. Cave spiders
// inherit it.
func climbsWalls(etype int) bool {
	return etype == entitySpider || etype == entityCaveSpider
}

// Drowned.travelInWater: a drowned walks on land and SWIMS in water. It is the
// only mob that does both — m.swims means water-BOUND, which a fish is and a
// drowned is not.
func isAmphibious(etype int) bool { return etype == entityDrowned }

// endermanTeleportTo is Enderman.teleport(x,y,z) over LivingEntity.
// randomTeleport: from the point it drops to the first block entities can
// teleport onto (#entities_can_teleport_to, i.e. anything that blocks
// motion), keeping the point's height above it; the ground must not be in
// #enderman_does_not_teleport_to, the whole 2.9-block box must be free of
// blocks and liquid, and the block under it must not be water. Otherwise
// the enderman stays put. It never goes looking for the surface: an
// enderman in a cave blinks about the cave, one in the sun may land in
// shade — which is how daylight clears them.
func (h *hub) endermanTeleportTo(players map[int32]*tracked, m *mob, x, y, z float64) bool {
	w := h.worldFor(m.dim)
	if w == nil || m.mount != 0 { // isPassenger
		return false
	}
	bx, bz := floorInt(x), floorInt(z)
	py := floorInt(y)
	if !h.inWorldYIn(m.dim, py) {
		return false
	}
	// LivingEntity.randomTeleport: level.hasChunkAt(target) — never into a
	// chunk that is not loaded. Reading one generated it on the spot, and the
	// enderman landed outside the loaded area.
	if !w.Loaded(int32(bx>>4), int32(bz>>4)) {
		return false
	}
	for py > worldgen.MinY {
		py--
		ground := w.At(bx, py, bz)
		if !inRanges2(ground, canTeleportOnto) {
			y-- // the point sinks with the search, a cell per miss
			continue
		}
		if inRanges2(ground, endermanNoTeleport) {
			return false
		}
		b := m.box()
		hw := b.w / 2
		for cx := floorInt(x - hw); cx <= floorInt(x+hw-1e-7); cx++ {
			for cz := floorInt(z - hw); cz <= floorInt(z+hw-1e-7); cz++ {
				for cy := floorInt(y); cy <= floorInt(y+b.h-1e-7); cy++ {
					if s := w.At(cx, cy, cz); worldgen.Collides(s) || worldgen.IsFluid(s) {
						return false
					}
				}
			}
		}
		if worldgen.IsWater(w.At(bx, floorInt(y)-1, bz)) { // canRandomlyTeleportTo
			return false
		}
		ox, oy, oz := m.x, m.y, m.z
		m.x, m.y, m.z = x, y, z
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		h.playSoundDim(players, m.dim, "minecraft:entity.enderman.teleport", sndHostile, ox, oy, oz, 1, 1)
		h.playSoundDim(players, m.dim, "minecraft:entity.enderman.teleport", sndHostile, m.x, m.y, m.z, 1, 1)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTeleport))
		h.vibAt(m.dim, freqTeleport, ox, oy, oz, m.eid)
		return true
	}
	return false
}

var (
	canTeleportOnto    = worldgen.BlockTag("entities_can_teleport_to")
	endermanNoTeleport = worldgen.BlockTag("enderman_does_not_teleport_to")
)
