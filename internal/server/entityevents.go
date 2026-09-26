package server

// Entity events (ClientboundEntityEventPacket): the one-byte cues vanilla
// broadcasts so every client near an entity animates something — vanilla
// EntityEvent's names. These are the ones the engine now fires where
// vanilla does (the older ones live beside their mechanics).
const (
	entityStatusWitchMagic  = 15 // WITCH_HAT_MAGIC: the ambient purple sparkle
	entityStatusFireworks   = 17 // FIREWORKS_EXPLODE: the burst from the rocket's own item
	entityStatusSpawnAnim   = 20 // SILVERFISH_MERGE_ANIM: Mob.spawnAnim's puff, as a spawner's mob appears
	entityStatusReelIn      = 31 // FISHING_ROD_REEL_IN: the hooked thing is pulled
	entityStatusVillagerSwt = 42 // VILLAGER_SWEAT: a villager in a raid
	entityStatusFoxEat      = 45 // FOX_EAT: the crumbs
	entityStatusTeleport    = 46 // TELEPORT: portal particles at a random teleport
	entityStatusPoof        = 60 // POOF: the death cloud when the corpse goes
	entityStatusSnifferDig  = 63 // SNIFFER_DIGGING_SOUND
	entityStatusShake       = 66 // SHAKE: the creaking's invulnerable shudder
	entityStatusDrown       = 67 // DROWN_PARTICLES: the bubbles of a last breath
	entityStatusRavagerRoar = 69 // RAVAGER_ROARED
)
