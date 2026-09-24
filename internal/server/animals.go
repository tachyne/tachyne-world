package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Farm animals: chickens, pigs and sheep join the cows — with breeding.
// Right-click an adult with its love-food and hearts appear; two courting
// animals of a species near each other make a baby that grows up in the
// vanilla 20 minutes. Sheep shear for wool and regrow it by grazing;
// chickens lay eggs, and thrown eggs sometimes hatch chicks.

const (
	chickenHealth = 4
	pigHealth     = 10
	sheepHealth   = 8

	metaIndexBaby  = 16 // ageable mobs: baby flag (bool)
	metaIndexSheep = 17 // sheep: color bits 0-3 + sheared bit 0x10 (byte)
	metaTypeBool   = 8  // metadata value type: boolean (1.21.5)
	statusInLove   = 18 // entity status: heart particles

	loveTicks        = 600   // 30 s of courting after being fed (vanilla)
	breedCooldown    = 6000  // 5 min before a parent can breed again (vanilla)
	growUpTicks      = 24000 // babies take 20 min to grow (vanilla)
	breedRange       = 8.0   // BreedGoal.PARTNER_SEARCH_RANGE: how far a partner is looked for
	breedMeetRange   = 3.0   // …and how close the two must actually be to breed (distanceToSqr < 9)
	breedCourtTicks  = 60    // BreedGoal.tick: sixty ticks together before the baby
	pandaBambooReach = 8     // PandaBreedGoal.canFindBamboo's horizontal reach

	eggLayMin = 6000 // chickens lay every 5-10 min (vanilla)
	eggLayMax = 12000
)

var (
	entityChicken = entityID("chicken")
	entityPig     = entityID("pig")
	entitySheep   = entityID("sheep")

	itemWheat      = itemByName["wheat"]
	itemCarrot     = itemByName["carrot"]
	itemFeather    = itemByName["feather"]
	itemRawChicken = itemByName["chicken"]
	itemPorkchop   = itemByName["porkchop"]
	itemMutton     = itemByName["mutton"]
	itemWhiteWool  = itemByName["white_wool"]
	itemShears     = itemByName["shears"]
)

// loveFood maps species → the item that courts it (vanilla).
func loveFood(etype int) int32 {
	switch etype {
	case entityCow, entitySheep:
		return itemWheat
	case entityPig:
		return itemCarrot
	case entityChicken:
		return itemWheatSeeds
	}
	// Roster species carry their breeding food in the table (mooshroom→wheat,
	// wolf→beef, panda→bamboo, horse→golden_carrot, …).
	if d := speciesOf(etype); d != nil && d.love != "" {
		return itemByName[d.love]
	}
	return 0
}

// spawnAnimal creates a passive species with its stats (health set by
// mobHealth) and species quirks (chickens dawdle, sheep pick a fleece).
func (h *hub) spawnAnimal(players map[int32]*tracked, etype, x, z int) *mob {
	m := h.spawnMobIn(players, etype, dimOverworld, float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
	if m == nil {
		return nil // plugin-cancelled spawn
	}
	if etype == entityChicken { // speed comes from speedFor (attr 0.25, like pigs)
		m.eggIn = eggLayMin + h.rng.Intn(eggLayMax-eggLayMin)
	}
	h.applySpecies(players, m) // roster species get their stance/quirks (no-op for the originals)
	return m
}

// babyMeta builds the ageable baby flag metadata.
func babyMeta(eid int32, baby bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexBaby)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, baby)
	return protocol.AppendU8(b, itemMetaEnd)
}

// sheepMeta builds the fleece byte for a sheep, colour included. Kept as a
// thin wrapper so callers pass the mob rather than remembering the bit layout.
func sheepMeta(m *mob, sheared bool) []byte {
	return sheepFleeceMeta(m.eid, m.color, sheared)
}

// feedAnimal handles a right-click with something this species eats
// (Animal.mobInteract and the pet/horse overrides): a hurt pet heals, a
// baby grows a tenth of its remaining time, an adult off cooldown starts
// courting. Returns whether the food was taken.
func (h *hub) feedAnimal(players map[int32]*tracked, t *tracked, m *mob) bool {
	item := heldStack(t).item
	if m.dying > 0 {
		return false
	}
	if horseFamily(m.etype) {
		return h.feedHorse(players, t, m, item)
	}
	if !isLoveFood(m.etype, item) {
		return false
	}
	if m.etype == entityNautilus && !m.tamed && !m.baby {
		return false // AbstractNautilus.isFood: a wild adult takes only its taming pufferfish (tryTame)
	}
	// A tamed wolf or nautilus eats to heal (any player); a cat only from its
	// owner. The meal heals its nutrition (twice that for a wolf or a
	// nautilus: TamableAnimal.feed's factor 2), 1 for a non-food.
	if m.tamed && (m.etype == entityWolf || m.etype == entityNautilus || (m.etype == entityCat && m.owner == t.p.eid)) && m.health < m.maxHP() {
		n := foodPoints[item]
		if n == 0 {
			n = 1
		}
		if m.etype == entityWolf || m.etype == entityNautilus {
			n *= 2
		}
		h.healMob(m, n)
		h.consumeFed(t, item)
		h.playSoundDim(players, m.dim, eatSound(m.etype), sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	if m.baby {
		// getSpeedUpSecondsWhenFeeding: a tenth of the remaining time, in whole seconds.
		h.ageUp(m, m.growLeft/200*20)
		h.consumeFed(t, item)
		h.playSoundDim(players, m.dim, eatSound(m.etype), sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	if neverLoves[m.etype] || m.loveTicks > 0 || m.breedCD > 0 || m.hasEgg { // Turtle.canFallInLove: not while carrying an egg
		return false
	}
	h.consumeFed(t, item)
	h.setInLove(players, t, m)
	h.playSoundDim(players, m.dim, eatSound(m.etype), sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// shearMob shears an unsheared adult sheep — the player-independent core (also
// used by the dispenser). Returns whether wool actually popped.
func (h *hub) shearMob(players map[int32]*tracked, m *mob) bool {
	if m.etype != entitySheep || m.sheared || m.baby {
		return false
	}
	m.sheared = true
	h.vibAt(m.dim, freqShear, m.x, m.y, m.z, m.eid)
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sheepMeta(m, true)))
	h.spawnItemIn(players, m.dim, sheepWool(m), 1+h.rng.Intn(3), m.x, m.y, m.z) // its own fleece
	h.playSoundDim(players, m.dim, "minecraft:entity.sheep.shear", sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// shearSheep handles a player using shears on an unsheared adult sheep.
func (h *hub) shearSheep(players map[int32]*tracked, t *tracked, m *mob) bool {
	if heldStack(t).item != itemShears || !h.shearMob(players, m) {
		return false
	}
	if isSurvival(t.gamemode) {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	return true
}

// updateBreeding runs at 1 Hz: pair up courting animals, grow babies, lay
// eggs, regrow wool, and top up the wild population.
func (h *hub) updateBreeding(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.dying > 0 {
			continue
		}
		if m.breedCD > 0 {
			m.breedCD -= survivalTickN
		}
		if m.baby && !m.hostile { // baby zombies never mature (vanilla)
			if m.growLeft -= survivalTickN; m.growLeft <= 0 {
				m.baby = false
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, false)))
				if m.etype == entitySulfurCube {
					h.sulfurGrewUp(players, m) // ageBoundaryReached: size 2 again
				}
				if m.etype == entityTurtle {
					// Turtle.ageBoundaryReached drops gameplay/turtle_grow —
					// the only source of turtle scute in the game.
					if scute := itemByName["turtle_scute"]; scute != 0 {
						h.spawnItemIn(players, m.dim, scute, 1, m.x, m.y+0.5, m.z)
						h.playSoundDim(players, m.dim, "minecraft:entity.turtle.egg_hatch", sndNeutral, m.x, m.y, m.z, 1, 1)
					}
				}
			}
		}
		if m.converting > 0 {
			h.tickCure(players, m)
			continue // the cure may have replaced the mob
		}
		if m.etype == entityVillager {
			// VillagerPanicTrigger: a panicking villager asks for a golem with
			// three agreeing, on the tick that is a multiple of a hundred.
			if h.tick.Load()%golemPanicEvery < survivalTickN && h.villagerPanicking(m) {
				h.spawnGolemIfNeeded(players, m, golemPanicAgree)
			}
			h.villagerGossipTick(players, m)
			h.villagerJobTick(players, m)  // workstations: validate the held one, look for a free one
			h.villagerBedTick(players, m)  // …and a bed for whoever has none
			h.villagerMeetTick(players, m) // …and a meeting bell
			// Villager.customServerAiStep: one tick in a hundred inside an active
			// raid, the sweat particles (VILLAGER_SWEAT) — this step is 20 ticks.
			if h.rng.Intn(5) == 0 && h.raidNear(m.dim, m.x, m.z) {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerSwt))
			}
		}
		if m.etype == entityNautilus {
			if m.rider != 0 {
				h.nautilusBreath(players, m)
			}
		}
		if m.etype == entityChicken && !m.baby && !m.jockey {
			if m.eggIn -= survivalTickN; m.eggIn <= 0 {
				m.eggIn = eggLayMin + h.rng.Intn(eggLayMax-eggLayMin)
				h.spawnItemIn(players, m.dim, chickenEggFor(m.variant), 1, m.x, m.y, m.z)
				h.playSoundDim(players, m.dim, "minecraft:entity.chicken.egg", sndNeutral, m.x, m.y, m.z, 1, 1)
			}
		}
		if m.loveTicks <= 0 {
			continue
		}
		m.loveTicks -= survivalTickN
		// BreedGoal.getFreePartner: the NEAREST courting animal of the same
		// kind within eight blocks, not the first one the grid hands back.
		var partner *mob
		best := breedRange * breedRange
		h.grid().nearby(m.dim, m.x, m.z, breedRange, func(o *mob) {
			if o == m || o.etype != m.etype || o.loveTicks <= 0 || o.dying > 0 {
				return
			}
			if m.etype == entitySniffer && (!snifferMates(m) || !snifferMates(o)) {
				return // Sniffer.canMate: never mid-sniff, mid-search or mid-dig
			}
			dx, dz := o.x-m.x, o.z-m.z
			if d2 := dx*dx + dz*dz; d2 < best {
				partner, best = o, d2
			}
		})
		if partner == nil {
			m.breedTime = 0
			continue
		}
		if m.etype == entityPanda && !h.pandaFindsBamboo(m) {
			// PandaBreedGoal.canUse: a panda with no bamboo in reach sulks
			// instead of breeding.
			m.breedTime = 0
			continue
		}
		// BreedGoal.tick: the pair walk TO each other at the goal's speed and
		// only breed once they have been together for sixty ticks and are
		// within three blocks. Without this, two animals fed at opposite ends
		// of a pen made a baby without ever meeting.
		if best > breedMeetRange*breedMeetRange {
			m.hasTarget, m.tx, m.tz = true, partner.x, partner.z
			m.breedTime = 0
			continue
		}
		if m.breedTime += survivalTickN; m.breedTime < breedCourtTicks {
			m.hasTarget, m.tx, m.tz = true, partner.x, partner.z
			continue
		}
		m.hasTarget = false
		partner.hasTarget, partner.breedTime = false, 0
		if o := partner; o != nil {
			m.loveTicks, o.loveTicks = 0, 0
			m.breedCD, o.breedCD = breedCooldown, breedCooldown
			var baby *mob
			if m.etype == entityTurtle {
				m.hasEgg = true // TurtleBreedGoal.breed: an egg to carry home, no hatchling yet
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(turtleMeta(m)))
			} else if m.etype == entitySniffer {
				// Sniffer.spawnChildFromBreeding: no snifflet, an egg dropped
				// where the parent stands.
				h.spawnItemIn(players, m.dim, itemByName["sniffer_egg"], 1, m.x, m.y, m.z)
				h.playSoundDim(players, m.dim, "minecraft:block.sniffer_egg.plop", sndNeutral, m.x, m.y, m.z, 1, (h.rng.Float32()-h.rng.Float32())*0.2+0.5)
			} else if m.etype == entityNautilus {
				// The calf comes where its parent swims, not on the ground
				// above; a tamed parent's is born tamed to the same owner
				// (Nautilus.getBreedOffspring).
				baby = h.spawnSpecies(players, m.etype, m.dim, m.x, m.y, m.z)
				if baby != nil && m.tamed {
					baby.tamed, baby.owner, baby.ownerUUID = true, m.owner, m.ownerUUID
				}
			} else if m.etype == entityFrog {
				// FrogAi: frogs do not have babies, they go IS_PREGNANT and
				// lay a clutch of frogspawn on the nearest water.
				m.pregnant = true
			} else {
				baby = h.spawnAnimal(players, m.etype, int(m.x), int(m.z))
			}
			if baby != nil { // a plugin may cancel the birth; the parents still cool down
				baby.baby, baby.growLeft = true, growUpTicks
				// A foal lands between its parents, which is the whole point of
				// breeding horses rather than taming whatever wanders past.
				h.breedHorseAttributes(m, o, baby)
				h.inheritVariant(baby, m, o) // an axolotl takes a parent's colour (or the rare blue)
				if baby.etype == entityFox { // FoxBreedGoal.breed: the cub trusts whoever fed each parent
					for _, eid := range []int32{m.lovedBy, o.lovedBy} {
						if t := players[eid]; t != nil {
							foxAddTrusted(baby, t.p.name)
						}
					}
				}
				if baby.etype == entitySheep { // the lamb was announced in its rolled colour
					h.toNearbyEv(players, baby.dim, baby.x, baby.z, metaEv(sheepMeta(baby, false)))
				}
				if vm := variantMeta(baby); vm != nil {
					h.toNearbyEv(players, baby.dim, baby.x, baby.z, metaEv(vm))
				}
				h.toTracking(players, baby.eid, 0, baby.x, baby.z, metaEv(babyMeta(baby.eid, true)))
			}
			h.spawnXPOrbIn(players, m.dim, 1+h.rng.Intn(7), m.x, m.y, m.z) // breeding XP (vanilla 1-7)
			breeder := players[m.lovedBy]
			if breeder == nil {
				breeder = players[o.lovedBy]
			}
			if breeder != nil {
				// The child, when one is born (a turtle, sniffer or frog
				// breeds into an egg or spawn: no offspring, only parents).
				bred := advMatch{parent: advEntityName[m.etype], partner: advEntityName[o.etype], variant: advVariantName(baby)}
				if baby != nil {
					bred.entity = advEntityName[baby.etype]
				}
				h.advance(players, breeder, "bred_animals", bred)
				h.incCustom(breeder, "animals_bred", 1)
			}
			break
		}
	}
}

// (wildSpawn/biomeAnimal retired 2026-07-11, the herd top-up 2026-09-19:
// natural spawning is the vanilla NaturalSpawner port in spawn.go and
// spawnvanilla.go, drawing from the biome data's pools.)

// chickenEggFor is the gameplay/chicken_lay table: a chicken lays the egg its
// own variant calls for — brown in the warm biomes, blue in the cold ones,
// the plain white egg everywhere else. Every chicken had been laying white.
func chickenEggFor(variant int32) int32 {
	switch variant {
	case tempWarm:
		return itemBrownEgg
	case tempCold:
		return itemBlueEgg
	}
	return itemEgg
}

// pandaFindsBamboo is PandaBreedGoal.canFindBamboo: one bamboo block anywhere
// within eight blocks horizontally and two above the panda is enough. With
// none in reach a panda will not breed, whatever it has been fed.
func (h *hub) pandaFindsBamboo(m *mob) bool {
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for dy := 0; dy < 3; dy++ {
		for dx := -pandaBambooReach; dx <= pandaBambooReach; dx++ {
			for dz := -pandaBambooReach; dz <= pandaBambooReach; dz++ {
				if inStates(w.At(bx+dx, by+dy, bz+dz), bambooStates) {
					return true
				}
			}
		}
	}
	return false
}
