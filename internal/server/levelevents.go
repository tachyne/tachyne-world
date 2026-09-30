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
	worldEventFireExtinguish   = 1009 // SOUND_EXTINGUISH_FIRE (data 0: the fire hiss)
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

	worldEventDispense         = 1000 // SOUND_DISPENSER_DISPENSE
	worldEventDispenseFail     = 1001 // SOUND_DISPENSER_FAIL
	worldEventDispenseLaunch   = 1002 // SOUND_DISPENSER_PROJECTILE_LAUNCH
	worldEventFireworkShoot    = 1004 // SOUND_FIREWORK_SHOOT: a dispensed rocket
	worldEventWindChargeShoot  = 1051 // SOUND_WIND_CHARGE_SHOOT: a dispensed wind charge
	worldEventZombieDoorKnock  = 1019 // SOUND_ZOMBIE_WOODEN_DOOR: BreakDoorGoal's blows
	worldEventZombieDoorCrash  = 1021 // SOUND_ZOMBIE_DOOR_CRASH: the door gives
	worldEventWitherBreak      = 1022 // SOUND_WITHER_BLOCK_BREAK
	worldEventWitherShoot      = 1024 // SOUND_WITHER_BOSS_SHOOT: each skull
	worldEventWitherSpawn      = 1023 // SOUND_WITHER_BOSS_SPAWN (global)
	worldEventDragonDeath      = 1028 // SOUND_DRAGON_DEATH (global)
	worldEventEndPortalOpen    = 1038 // SOUND_END_PORTAL_SPAWN (global)
	worldEventPortalTravel     = 1032 // SOUND_PORTAL_TRAVEL: to the traveller, on arrival
	worldEventBrew             = 1035 // SOUND_BREWING_STAND_BREW
	worldEventPhantomBite      = 1039 // SOUND_PHANTOM_BITE
	worldEventCrafterCraft     = 1049 // SOUND_CRAFTER_CRAFT: a crafted stack dropped out
	worldEventCrafterFail      = 1050 // SOUND_CRAFTER_FAIL: nothing matches
	worldEventWhiteSmoke       = 2010 // PARTICLES_SHOOT_WHITE_SMOKE, data: the facing's 3D index
	worldEventComposterFill    = 1500 // COMPOSTER_FILL, data: 1 when the pile rose
	worldEventLavaFizz         = 1501 // LAVA_FIZZ: the hiss and smoke of a quench
	worldEventDripstoneDrip    = 1504 // DRIPSTONE_DRIP: a drop leaves the tip for a cauldron
	worldEventEvaporate        = 2009 // PARTICLES_WATER_EVAPORATING: a wet sponge dries in the Nether
	worldEventSmashAttack      = 2013 // PARTICLES_SMASH_ATTACK, data: the dust's size
	worldEventDestroyParticles = 2014 // PARTICLES_DESTROY_BLOCK: a straw bed's break, silent
	worldEventGatewaySpawn     = 3000 // ANIMATION_END_GATEWAY_SPAWN

	worldEventDestroyProgress      = 2019 // PARTICLES_DESTROY_PROGRESS, data: the face struck
	worldEventDestroyProgressSound = 2020 // PARTICLES_AND_SOUND_DESTROY_PROGRESS: every fourth tick of a dig
	worldEventTrialDetect          = 3013 // PARTICLES_TRIAL_SPAWNER_DETECT_PLAYER, data: players detected
	worldEventTrialDetectOmen      = 3019 // PARTICLES_TRIAL_SPAWNER_DETECT_PLAYER_OMINOUS
	worldEventDragonEggTeleport    = 2015 // PARTICLES_DRAGON_EGG_TELEPORT, data: packDifference(16, 8, 16) to the landing
	worldEventShulkerTeleport      = 2016 // PARTICLES_SHULKER_TELEPORT, data: packDifference(8, 8, 8) to the landing
	worldEventConsumeTeleport      = 2017 // PARTICLES_CONSUME_EFFECT_TELEPORT (chorus fruit), data: clampedPackDifference
	worldEventDripLavaCauldron     = 1046 // SOUND_DRIP_LAVA_INTO_CAULDRON
	worldEventDripWaterCauldron    = 1047 // SOUND_DRIP_WATER_INTO_CAULDRON
	worldEventFrameFill            = 1503 // END_PORTAL_FRAME_FILL: the sound and the smoke
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

// packDifference is BlockUtil.packDifferenceInPosition: each axis of the
// step from one cell to another, offset by that axis's radius, a byte each.
func packDifference(from, to blockPos, rx, ry, rz int) int32 {
	return int32((to.x-from.x+rx)&0xFF)<<16 | int32((to.y-from.y+ry)&0xFF)<<8 | int32((to.z-from.z+rz)&0xFF)
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
