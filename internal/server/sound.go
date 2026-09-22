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
	sndAmbient = 1 // ambient source (fireworks)
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
	particleNectar           = 79 // falling_nectar (80 on 773+, 82 on 775, 89 on 776)

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

// playSoundExcept is playSoundDim to everyone but one player — the one
// whose own client already played the sound on prediction (vanilla's
// Level.playSound(except, …) for doors, buttons and the like).
func (h *hub) playSoundExcept(players map[int32]*tracked, dim int, except int32, name string, category int32, x, y, z float64, volume, pitch float32) {
	ev := soundEv(name, category, x, y, z, volume, pitch)
	cx, cz := chunkFloor(x), chunkFloor(z)
	for eid, t := range players {
		if eid == except || t.dim != dim {
			continue
		}
		if abs(chunkFloor(t.x)-cx) <= viewRadius && abs(chunkFloor(t.z)-cz) <= viewRadius {
			t.p.trySendEv(ev)
		}
	}
}

// hurtPitch is the vanilla-feel randomized pitch for hurt/ambient sounds.
func (h *hub) hurtPitch() float32 { return 0.9 + h.rng.Float32()*0.2 }

// spawnParticles shows a payload-free particle burst to everyone tracking it.
func (h *hub) spawnParticles(players map[int32]*tracked, dim int, pid int32, x, y, z float64, spread, speed float32, count int32) {
	h.toNearbyEv(players, dim, x, z, attachproto.Particles{PID: pid, X: x, Y: y, Z: z, Spread: spread, Speed: speed, Count: count})
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
	case entityHusk:
		return "minecraft:entity.husk.hurt", "minecraft:entity.husk.death", "minecraft:entity.husk.ambient"
	case entityDrowned:
		return "minecraft:entity.drowned.hurt", "minecraft:entity.drowned.death", "minecraft:entity.drowned.ambient"
	case entityStray:
		return "minecraft:entity.stray.hurt", "minecraft:entity.stray.death", "minecraft:entity.stray.ambient"
	case entityEnderman:
		return "minecraft:entity.enderman.hurt", "minecraft:entity.enderman.death", "minecraft:entity.enderman.ambient"
	case entityWitch:
		return "minecraft:entity.witch.hurt", "minecraft:entity.witch.death", "minecraft:entity.witch.ambient"
	case entityBlaze:
		return "minecraft:entity.blaze.hurt", "minecraft:entity.blaze.death", "minecraft:entity.blaze.ambient"
	case entitySlime: // no ambient: a slime's voice is its squish on landing
		return "minecraft:entity.slime.hurt", "minecraft:entity.slime.death", ""
	case entityMagmaCube:
		return "minecraft:entity.magma_cube.hurt", "minecraft:entity.magma_cube.death", ""
	case entityZombifiedPiglin:
		return "minecraft:entity.zombified_piglin.hurt", "minecraft:entity.zombified_piglin.death", "minecraft:entity.zombified_piglin.ambient"
	case entityIronGolem: // IronGolem: no ambient sound
		return "minecraft:entity.iron_golem.hurt", "minecraft:entity.iron_golem.death", ""
	case entityVillager:
		return "minecraft:entity.villager.hurt", "minecraft:entity.villager.death", "minecraft:entity.villager.ambient"
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

// mobSoundsFor is mobSounds for a live mob: the species whose vanilla voice
// depends on its state (getAmbientSound/getHurtSound overrides that pick by
// water, age, held item, flight or the creaking's sway).
func (h *hub) mobSoundsFor(m *mob) (hurt, death, ambient string) {
	hurt, death, ambient = mobSounds(m.etype)
	switch m.etype {
	case entityDrowned: // Drowned: its own voice under water
		if h.inWater(m.dim, m.x, m.y, m.z) {
			hurt, death, ambient = "minecraft:entity.drowned.hurt_water", "minecraft:entity.drowned.death_water", "minecraft:entity.drowned.ambient_water"
		}
	case entitySlime, entityMagmaCube: // AbstractCubeMob: the small size has its own voice
		if m.size == 1 {
			hurt, death = hurt+"_small", death+"_small"
		}
	case entityCreaking:
		hurt = "minecraft:entity.creaking.sway" // Creaking.getHurtSound
	case entityTurtle:
		if m.baby { // Turtle: the babies' own hurt/death
			hurt, death = "minecraft:entity.turtle.hurt_baby", "minecraft:entity.turtle.death_baby"
		}
		ambient = "minecraft:entity.turtle.ambient_land" // getAmbientSound: land only, silent in water
		if h.inWater(m.dim, m.x, m.y, m.z) {
			ambient = ""
		}
	case entityAxolotl:
		ambient = "minecraft:entity.axolotl.idle_air" // getAmbientSound: isInWaterOrBubble
		if h.inWater(m.dim, m.x, m.y, m.z) {
			ambient = "minecraft:entity.axolotl.idle_water"
		}
	case entityAllay:
		ambient = "minecraft:entity.allay.ambient_without_item" // getAmbientSound: hasItemInHand
		if m.held != 0 {
			ambient = "minecraft:entity.allay.ambient_with_item"
		}
	case entityBreeze:
		ambient = "minecraft:entity.breeze.idle_ground" // getAmbientSound: onGround
		if m.brzState == brzJumping {
			ambient = "minecraft:entity.breeze.idle_air"
		}
	case entitySniffer:
		ambient = "minecraft:entity.sniffer.idle" // Sniffer.getAmbientSound
	case entityWolf:
		// A wolf's voice is its WolfSoundVariant: seven registered sets, each
		// a suffix on the entity.wolf sound names.
		v := "minecraft:entity.wolf" + wolfSoundSuffix(m)
		hurt, death, ambient = v+".hurt", v+".death", v+".ambient"
	case entityNautilus: // Nautilus: its own voice under water, the land one out of it, the baby's own
		p := "minecraft:entity.nautilus."
		if m.baby {
			p = "minecraft:entity.baby_nautilus."
		}
		land := ""
		if !h.inWater(m.dim, m.x, m.y, m.z) {
			land = "_land"
		}
		hurt, death, ambient = p+"hurt"+land, p+"death"+land, p+"ambient"+land
	}
	return hurt, death, ambient
}

// mobAmbience is Mob.baseTick's idle voice: every tick a mob rolls
// random(1000) < ambientSoundTime++, and on a hit resets the counter to
// minus its ambient interval — 80 ticks for most, 120 for animals, fish,
// golems and cats, 160 guardians, 200 turtles, 400 horses, 900 ocelots —
// so the next call cannot come sooner than that and grows likelier after.
func (h *hub) mobAmbience(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.dying > 0 {
			continue
		}
		t := m.ambientTime
		m.ambientTime++
		if int32(h.rng.Intn(1000)) >= t {
			continue
		}
		m.ambientTime = -ambientInterval(m)
		_, _, ambient := h.mobSoundsFor(m)
		if m.etype == entityWolf {
			ambient = h.wolfAmbient(m)
		}
		if ambient != "" {
			cat := int32(sndNeutral)
			if m.hostile {
				cat = sndHostile
			}
			h.playSoundDim(players, m.dim, ambient, cat, m.x, m.y, m.z, 1, h.voicePitch(m))
		}
	}
}

// voicePitch is LivingEntity.getVoicePitch: babies squeak half an octave up.
func (h *hub) voicePitch(m *mob) float32 {
	p := (h.rng.Float32() - h.rng.Float32()) * 0.2
	if m.baby {
		return p + 1.5
	}
	return p + 1
}

// ambientInterval is getAmbientSoundInterval by class.
func ambientInterval(m *mob) int32 {
	if iv, ok := ambientIntervals[entityNameByID[m.etype]]; ok {
		return iv
	}
	return 80
}

var ambientIntervals = func() map[string]int32 {
	m := map[string]int32{"turtle": 200, "ocelot": 900, "guardian": 160, "elder_guardian": 160}
	for _, n := range []string{"cow", "mooshroom", "pig", "sheep", "chicken", "rabbit", "wolf", "cat", "fox", "panda",
		"polar_bear", "bee", "goat", "frog", "axolotl", "armadillo", "sniffer", "strider", "hoglin", "parrot",
		"dolphin", "squid", "glow_squid", "nautilus", "cod", "salmon", "pufferfish", "tropical_fish", "tadpole",
		"iron_golem", "snow_golem", "copper_golem"} {
		m[n] = 120
	}
	for _, n := range []string{"horse", "donkey", "mule", "skeleton_horse", "zombie_horse", "camel", "camel_husk", "llama", "trader_llama"} {
		m[n] = 400
	}
	return m
}()

// globalSoundRange is how far ServerLevel.globalLevelEvent places a sound a
// listener is further away from than this: on the line towards it, so it still
// arrives from the right direction at full volume.
const globalSoundRange = 32.0

// playSoundGlobal is ServerLevel.globalLevelEvent — the handful of sounds
// everyone in a dimension is meant to hear wherever they are: a wither waking
// up, the dragon dying, an end portal opening. A listener out of earshot hears
// it from a point globalSoundRange blocks off in its direction, which is what
// makes it carry without being placeless. The global_sound_events gamerule
// turns that off, and then only the people nearby hear it.
func (h *hub) playSoundGlobal(players map[int32]*tracked, dim int, name string, category int32, x, y, z float64, volume, pitch float32) {
	if !h.rules.GlobalSounds {
		h.playSoundDim(players, dim, name, category, x, y, z, volume, pitch)
		return
	}
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		sx, sy, sz := x, y, z
		if d := dist3(t.x, t.y, t.z, x, y, z); d > globalSoundRange {
			sx = t.x + (x-t.x)/d*globalSoundRange
			sy = t.y + (y-t.y)/d*globalSoundRange
			sz = t.z + (z-t.z)/d*globalSoundRange
		}
		t.p.trySendEv(soundEv(name, category, sx, sy, sz, volume, pitch))
	}
}

// wolfSoundSuffixes are the seven WolfSoundVariants in registry order, as the
// suffix each puts on the entity.wolf sound names: classic carries none.
var wolfSoundSuffixes = [...]string{"", "_puglin", "_sad", "_angry", "_grumpy", "_big", "_cute"}

// wolfSoundSuffix is one wolf's set, defaulting to classic for a wolf that
// predates the field.
func wolfSoundSuffix(m *mob) string {
	if int(m.soundSet) >= 0 && int(m.soundSet) < len(wolfSoundSuffixes) {
		return wolfSoundSuffixes[m.soundSet]
	}
	return ""
}

// wolfAmbient is Wolf.getAmbientSound, which is three sounds rather than one:
// an angry wolf growls; otherwise one idle noise in three is a pant — a whine
// instead, when a tamed wolf is hurt — and the other two are the plain
// ambient. All four come from the wolf's own sound variant.
func (h *hub) wolfAmbient(m *mob) string {
	v := "minecraft:entity.wolf" + wolfSoundSuffix(m)
	if m.anger > 0 || m.targetEID != 0 {
		return v + ".growl"
	}
	if h.rng.Intn(3) == 0 {
		// vanilla's whine is a literal "below 20" on the tamed wolf's 40-point
		// bar — half health.
		if m.tamed && m.health < wolfTamedHealth/2 {
			return v + ".whine"
		}
		return v + ".pant"
	}
	return v + ".ambient"
}
