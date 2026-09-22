package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Footsteps. Vanilla's Entity.move accumulates 0.6 × the distance moved
// and, each time that passes the next whole number while the entity stands
// on a block, plays a step sound: the block's own sound type at 0.15 of its
// volume (a carpet, snow layer or the sprouts it wades through play theirs
// and a muffled copy of the block beneath), or the mob's own footfall for
// the species that have one. A mob moving through water splashes instead.

// silentSound is SoundEvents.EMPTY — a registered event that plays nothing.
const silentSound = "minecraft:intentionally_empty"

// stepSound is a sound type's step voice: volume, pitch, event name.
type stepSound struct {
	volume, pitch float32
	name          string
}

// mobStepVoices are the species whose playStepSound is their own sound
// rather than the block's. An empty name is a species that makes no step
// sound at all (fliers, fish, the allay).
var mobStepVoices = map[string]stepSound{
	"polar_bear": {0.15, 1, "minecraft:entity.polar_bear.step"}, "goat": {0.15, 1, "minecraft:entity.goat.step"},
	"sniffer": {0.15, 1, "minecraft:entity.sniffer.step"}, "parrot": {0.15, 1, "minecraft:entity.parrot.step"},
	"armadillo": {0.15, 1, "minecraft:entity.armadillo.step"}, "sheep": {0.15, 1, "minecraft:entity.sheep.step"},
	"pig": {0.15, 1, "minecraft:entity.pig.step"}, "wolf": {0.15, 1, "minecraft:entity.wolf.step"},
	"chicken": {0.15, 1, "minecraft:entity.chicken.step"}, "cow": {0.15, 1, "minecraft:entity.cow.step"},
	"mooshroom": {0.15, 1, "minecraft:entity.cow.step"}, "frog": {0.15, 1, "minecraft:entity.frog.step"},
	"iron_golem": {1, 1, "minecraft:entity.iron_golem.step"}, "llama": {0.15, 1, "minecraft:entity.llama.step"},
	"trader_llama": {0.15, 1, "minecraft:entity.llama.step"}, "panda": {0.15, 1, "minecraft:entity.panda.step"},
	"zoglin": {0.15, 1, "minecraft:entity.zoglin.step"}, "endermite": {0.15, 1, "minecraft:entity.endermite.step"},
	"ravager": {0.15, 1, "minecraft:entity.ravager.step"}, "silverfish": {0.15, 1, "minecraft:entity.silverfish.step"},
	"spider": {0.15, 1, "minecraft:entity.spider.step"}, "cave_spider": {0.15, 1, "minecraft:entity.spider.step"},
	"piglin": {0.15, 1, "minecraft:entity.piglin.step"}, "piglin_brute": {0.15, 1, "minecraft:entity.piglin_brute.step"},
	"skeleton": {0.15, 1, "minecraft:entity.skeleton.step"}, "stray": {0.15, 1, "minecraft:entity.stray.step"},
	"bogged": {0.15, 1, "minecraft:entity.bogged.step"}, "parched": {0.15, 1, "minecraft:entity.parched.step"},
	"wither_skeleton": {0.15, 1, "minecraft:entity.wither_skeleton.step"}, "creaking": {0.15, 1, "minecraft:entity.creaking.step"},
	"hoglin": {0.15, 1, "minecraft:entity.hoglin.step"}, "zombie": {0.15, 1, "minecraft:entity.zombie.step"},
	"husk": {0.15, 1, "minecraft:entity.husk.step"}, "drowned": {0.15, 1, "minecraft:entity.drowned.step"},
	"zombie_villager": {0.15, 1, "minecraft:entity.zombie_villager.step"}, "zombified_piglin": {0.15, 1, "minecraft:entity.zombie.step"},
	"warden": {10, 1, "minecraft:entity.warden.step"}, "camel": {1, 1, "minecraft:entity.camel.step"},
	"camel_husk": {0.4, 1, "minecraft:entity.camel_husk.step"}, "strider": {1, 1, "minecraft:entity.strider.step"},
	"turtle": {0.15, 1, "minecraft:entity.turtle.shamble"}, "copper_golem": {1, 1, "minecraft:entity.copper_golem.step"},
	"allay": {}, "bee": {}, "cod": {}, "salmon": {}, "pufferfish": {}, "tropical_fish": {}, "tadpole": {},
	"nautilus": {}, "zombie_nautilus": {}, "happy_ghast": {},
}

// insideStepBlocks and combinationStepBlocks are #inside_step_sound_blocks
// and #combination_step_sound_blocks: a block the mob stands IN whose step
// sound replaces (or joins, muffling) the block it stands on.
var (
	insideStepBlocks = map[string]bool{"powder_snow": true, "sculk_vein": true, "glow_lichen": true, "lily_pad": true,
		"small_amethyst_bud": true, "pink_petals": true, "wildflowers": true, "leaf_litter": true}
	combinationStepBlocks = func() map[string]bool {
		m := map[string]bool{"moss_carpet": true, "pale_moss_carpet": true, "snow": true, "nether_sprouts": true,
			"warped_roots": true, "crimson_roots": true, "resin_clump": true}
		for _, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
			"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
			m[c+"_carpet"] = true
		}
		return m
	}()
	camelSandBlocks = map[string]bool{"sand": true, "red_sand": true, "suspicious_sand": true}
)

// blockStepSound is a block state's sound type's step voice.
func blockStepSound(state uint32) stepSound {
	name, _ := worldgen.StateName(state)
	t := blockSoundType[name]
	if t == "" {
		t = "STONE"
	}
	return soundTypeSteps[t]
}

// blockPlaceSound is BlockItem.place's voice for a freshly placed block: the
// sound type's place event, at half again its volume and 0.8 of its pitch.
// The empty event means the block places in silence (a dried ghast, a cactus
// flower) and comes back with no name at all.
func blockPlaceSound(state uint32) (name string, volume, pitch float32) {
	n, _ := worldgen.StateName(state)
	t := blockSoundType[n]
	if t == "" {
		t = "STONE"
	}
	if name = soundTypePlaces[t]; name == silentSound {
		return "", 0, 0
	}
	s := soundTypeSteps[t]
	return name, (s.volume + 1) / 2, s.pitch * 0.8
}

// mobFootsteps is Entity.move's step-sound half for one movement: the
// distance accumulates and each whole unit past the last step plays.
func (h *hub) mobFootsteps(players map[int32]*tracked, m *mob, dx, dy, dz float64) {
	// Entering water splashes, whatever the step cadence is doing — a mob
	// falling in makes the noise on the way past, not on its next footfall.
	// Water-dwellers are exempt: a fish is never "entering" water.
	if !m.swims {
		if wet := h.inWater(m.dim, m.x, m.y, m.z); wet != m.wasWet {
			if wet {
				h.playSplash(players, m.dim, m.x, m.y, m.z,
					dx/mobMoveInterval, dy/mobMoveInterval, dz/mobMoveInterval, false)
			}
			m.wasWet = wet
		}
	}
	if m.flies || m.mount != 0 {
		return // no ground, or a passenger (vanilla emits nothing for passengers)
	}
	m.moveDist += float32(math.Hypot(dx, dz) * 0.6)
	if m.moveDist <= m.nextStep {
		return
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	fx, fz := int(math.Floor(m.x)), int(math.Floor(m.z))
	onY := int(math.Floor(m.y - 1e-5))
	below := w.At(fx, onY, fz)
	if below == worldgen.Air {
		return // nothing underfoot: the next block heard counts this step
	}
	if !worldgen.Collides(below) && !h.inWater(m.dim, m.x, m.y, m.z) {
		return // falling past something soft
	}
	m.nextStep = float32(int(m.moveDist) + 1)
	cat := int32(sndNeutral)
	if m.hostile {
		cat = sndHostile
	}
	if h.inWater(m.dim, m.x, m.y, m.z) { // Entity.waterSwimSound
		v := float32(math.Min(1, math.Sqrt(dx*dx*0.2+dy*dy+dz*dz*0.2)/mobMoveInterval*0.35))
		h.playSoundDim(players, m.dim, "minecraft:entity.generic.swim", cat, m.x, m.y, m.z, v, 1+(h.rng.Float32()-h.rng.Float32())*0.4)
		return
	}
	for _, s := range footstepsFor(w, m, fx, onY, fz) {
		h.playSoundDim(players, m.dim, s.name, cat, m.x, m.y, m.z, s.volume, s.pitch)
	}
}

// footstepsFor chooses what one footfall plays: the species' own step, or
// the block's — the block the mob stands in when that is a carpet-like or
// wade-through block (with the muffled block beneath for the carpets).
func footstepsFor(w *world.World, m *mob, fx, onY, fz int) []stepSound {
	name := entityNameByID[m.etype]
	if v, ok := mobStepVoices[name]; ok {
		if v.name == "" {
			return nil
		}
		below := w.At(fx, onY, fz)
		switch name {
		case "camel": // Camel: sand and concrete powder underfoot
			if bn, _ := worldgen.StateName(below); camelSandBlocks[bn] || worldgen.IsConcretePowder(below) {
				v.name = "minecraft:entity.camel.step_sand"
			}
		case "strider":
			if worldgen.IsLava(w.At(fx, int(math.Floor(m.y)), fz)) {
				v.name = "minecraft:entity.strider.step_lava"
			}
		case "turtle":
			if m.baby {
				v.name = "minecraft:entity.turtle.shamble_baby"
			}
		}
		return []stepSound{v}
	}
	below := w.At(fx, onY, fz)
	in := w.At(fx, onY+1, fz)
	inName, _ := worldgen.StateName(in)
	switch {
	case insideStepBlocks[inName]:
		s := blockStepSound(in)
		return []stepSound{{s.volume * 0.15, s.pitch, s.name}}
	case combinationStepBlocks[inName]:
		s, u := blockStepSound(in), blockStepSound(below)
		return []stepSound{{s.volume * 0.15, s.pitch, s.name}, {u.volume * 0.05, u.pitch * 0.8, u.name}}
	}
	s := blockStepSound(below)
	return []stepSound{{s.volume * 0.15, s.pitch, s.name}}
}
