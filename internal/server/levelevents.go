package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Level events (ClientboundLevelEventPacket): vanilla's one-line way of
// asking every client near a spot to draw an effect it knows by number —
// the particles and, for the SOUND_* ones, the sound too. The engine
// approximated several by hand (a particle burst here, a sound there) and
// skipped the rest; these are the ids it now fires where vanilla does.
const (
	worldEventDispenserSmoke   = 2000 // PARTICLES_SHOOT_SMOKE, data: the facing's 3D index
	worldEventSpawnerSpawn     = 2004 // PARTICLES_MOBBLOCK_SPAWN
	worldEventPotionSplash     = 2002 // PARTICLES_SPELL_POTION_SPLASH, data: the liquid's rgb
	worldEventInstantSplash    = 2007 // PARTICLES_INSTANT_POTION_SPLASH, data: the liquid's rgb
	worldEventSplashSound      = 1053 // SOUND_SPELL_POTION_SPLASH: the bottle breaking (2002 is silent)
	worldEventInstantSound     = 1054 // SOUND_INSTANT_POTION_SPLASH: the same, for an instant brew
	worldEventBeeGrowth        = 2011 // PARTICLES_BEE_GROWTH, data: particle count
	worldEventTurtleEggPlace   = 2012 // PARTICLES_TURTLE_EGG_PLACEMENT
	worldEventBoneMeal         = 1505 // PARTICLES_AND_SOUND_PLANT_GROWTH, data: count
	worldEventAnvilBroken      = 1029 // SOUND_ANVIL_BROKEN
	worldEventAnvilUsed        = 1030 // SOUND_ANVIL_USED
	worldEventGrindstoneUse    = 1042 // SOUND_GRINDSTONE_USED
	worldEventSnifferEggBoost  = 3009 // PARTICLES_EGG_CRACK: a sniffer egg set on moss
	worldEventChorusGrow       = 1033 // SOUND_CHORUS_GROW
	worldEventChorusDeath      = 1034 // SOUND_CHORUS_DEATH
	worldEventDragonFireball   = 1017 // SOUND_DRAGON_FIREBALL
	worldEventBrushDone        = 3008 // PARTICLES_AND_SOUND_BRUSH_BLOCK_COMPLETE, data: block state
	worldEventEggCrack         = 3009 // PARTICLES_EGG_CRACK
	worldEventTrialSpawn       = 3011 // PARTICLES_TRIAL_SPAWNER_SPAWN, data: 1 when ominous
	worldEventTrialEject       = 3014 // ANIMATION_TRIAL_SPAWNER_EJECT_ITEM
	worldEventVaultActivate    = 3015 // ANIMATION_VAULT_ACTIVATE
	worldEventVaultDeactive    = 3016 // ANIMATION_VAULT_DEACTIVATE
	worldEventVaultEject       = 3017 // ANIMATION_VAULT_EJECT_ITEM
	worldEventCobweb           = 3018 // ANIMATION_SPAWN_COBWEB
	worldEventSkelToStray      = 1048 // SOUND_SKELETON_TO_STRAY
	worldEventEndermanTeleport = 2018 // PARTICLES_ENDERMAN_TELEPORT (26.3), data: clampedPackDifference to the landing
)

// levelEvent fires one at a block position for everyone near it.
func (h *hub) levelEvent(players map[int32]*tracked, dim int, event int32, x, y, z int, data int32) {
	h.toNearbyEv(players, dim, float64(x), float64(z), attachproto.WorldFX{Event: event, X: x, Y: y, Z: z, Data: data})
}

// clampedPackDifference is BlockUtil.clampedPackDifferenceInPosition with
// every radius 127: the offset from one block to another, each axis clamped
// to ±127 and biased by 127 into a byte, packed x<<16 | y<<8 | z.
func clampedPackDifference(x0, y0, z0, x1, y1, z1 int) int32 {
	axis := func(d int) int32 { return int32(max(-127, min(127, d))+127) & 0xFF }
	return axis(x1-x0)<<16 | axis(y1-y0)<<8 | axis(z1-z0)
}

// dir3D is Direction.get3DDataValue for a unit offset: down, up, north,
// south, west, east.
func dir3D(dx, dy, dz int) int32 {
	switch {
	case dy < 0:
		return 0
	case dy > 0:
		return 1
	case dz < 0:
		return 2
	case dz > 0:
		return 3
	case dx < 0:
		return 4
	}
	return 5
}

// boolInt32 is the 0/1 data word a level event carries for a flag.
func boolInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
