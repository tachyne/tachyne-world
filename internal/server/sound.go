package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Sounds + particles. Sounds are sent INLINE BY NAME (soundId 0 + identifier)
// — the version-proof form of the sound holder, so no per-version sound
// registry remap is ever needed; unknown names are ignored by the client.
// Particles DO carry version-specific type ids; the chain remaps the small
// payload-free set we emit (protocol.remapParticleID).

const (

	// Sound categories (soundSource enum, stable across versions).
	sndRecord  = 2 // records/jukebox source (note blocks + discs)
	sndBlock   = 4
	sndHostile = 5
	sndNeutral = 6
	sndPlayer  = 7

	// Canonical (770) ids of the payload-free particles we emit.
	particleCrit             = 5
	particleExplosionEmitter = 21
	particlePoof             = 56
	particleBubble           = 3  // stable across every served version
	particleFishing          = 30 // the bobber wake (31 on 773+, 38 on 776)
	particleSplash           = 67 // (68 on 773+, 70 on 775, 77 on 776)

	worldEventBlockBreak = 2001
	particleNote         = 55 // canonical 770 particle id (remapped per client)
)

// soundEv builds the positioned-sound event (by name — version-proof).
func soundEv(name string, category int32, x, y, z float64, volume, pitch float32) attachproto.Sound {
	return attachproto.Sound{Name: name, Category: category, X: x, Y: y, Z: z, Volume: volume, Pitch: pitch}
}

// playSound plays a named sound at a position for everyone tracking it.
func (h *hub) playSound(players map[int32]*tracked, name string, category int32, x, y, z float64, volume, pitch float32) {
	h.playSoundDim(players, 0, name, category, x, y, z, volume, pitch)
}

// playSoundDim is playSound routed to one dimension's players.
func (h *hub) playSoundDim(players map[int32]*tracked, dim int, name string, category int32, x, y, z float64, volume, pitch float32) {
	h.toNearbyEv(players, dim, x, z, soundEv(name, category, x, y, z, volume, pitch))
}

// hurtPitch is the vanilla-feel randomized pitch for hurt/ambient sounds.
func (h *hub) hurtPitch() float32 { return 0.9 + h.rng.Float32()*0.2 }

// spawnParticles shows a payload-free particle burst to everyone tracking it.
func (h *hub) spawnParticles(players map[int32]*tracked, pid int32, x, y, z float64, spread, speed float32, count int32) {
	h.toNearbyEv(players, 0, x, z, attachproto.Particles{PID: pid, X: x, Y: y, Z: z, Spread: spread, Speed: speed, Count: count})
}

// blockBreakEvent builds the world-event 2001 FX — break particles + sound
// for a block state, rendered by the client from the state alone.
func blockBreakEvent(x, y, z int, state uint32) attachproto.WorldFX {
	return attachproto.WorldFX{Event: worldEventBlockBreak, X: x, Y: y, Z: z, Data: int32(state)}
}

// mobSounds names a mob type's hurt/death/ambient sounds ("" = silent type).
func mobSounds(etype int) (hurt, death, ambient string) {
	switch etype {
	case entityZombie:
		return "minecraft:entity.zombie.hurt", "minecraft:entity.zombie.death", "minecraft:entity.zombie.ambient"
	case entitySkeleton:
		return "minecraft:entity.skeleton.hurt", "minecraft:entity.skeleton.death", "minecraft:entity.skeleton.ambient"
	case entitySpider:
		return "minecraft:entity.spider.hurt", "minecraft:entity.spider.death", "minecraft:entity.spider.ambient"
	case entityCreeper:
		return "minecraft:entity.creeper.hurt", "minecraft:entity.creeper.death", "" // creepers are silent stalkers
	case entityCow:
		return "minecraft:entity.cow.hurt", "minecraft:entity.cow.death", "minecraft:entity.cow.ambient"
	case entityChicken:
		return "minecraft:entity.chicken.hurt", "minecraft:entity.chicken.death", "minecraft:entity.chicken.ambient"
	case entityPig:
		return "minecraft:entity.pig.hurt", "minecraft:entity.pig.death", "minecraft:entity.pig.ambient"
	case entitySheep:
		return "minecraft:entity.sheep.hurt", "minecraft:entity.sheep.death", "minecraft:entity.sheep.ambient"
	}
	// Roster species: derive the sound-event names from the registry name (or a
	// borrowed voice). The client silently ignores any name it doesn't know, so
	// species without a matching event are simply quiet.
	if d := speciesOf(etype); d != nil {
		voice := d.name
		if d.soundAs != "" {
			voice = d.soundAs
		}
		prefix := "minecraft:entity." + voice + "."
		amb := prefix + "ambient"
		if d.quiet {
			amb = ""
		}
		return prefix + "hurt", prefix + "death", amb
	}
	return "", "", ""
}

// mobAmbience gives loaded mobs their idle voices: each mob has a small chance
// per second to vocalize (zombie groans in the night, cows moo). Runs at 1 Hz.
func (h *hub) mobAmbience(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.dying > 0 || h.rng.Intn(12) != 0 {
			continue
		}
		if _, _, ambient := mobSounds(m.etype); ambient != "" {
			cat := int32(sndNeutral)
			if m.hostile {
				cat = sndHostile
			}
			h.playSound(players, ambient, cat, m.x, m.y, m.z, 1, h.hurtPitch())
		}
	}
}
