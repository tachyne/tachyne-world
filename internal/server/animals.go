package server

import (
	"math"
	"strings"

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

	loveTicks     = 600   // 30 s of courting after being fed (vanilla)
	breedCooldown = 6000  // 5 min before a parent can breed again (vanilla)
	growUpTicks   = 24000 // babies take 20 min to grow (vanilla)
	breedRange    = 8.0   // courting partners find each other within this

	eggLayMin = 6000 // chickens lay every 5-10 min (vanilla)
	eggLayMax = 12000

	woolRegrowIn = 40 // sheared sheep: 1-in-N chance per second to regrow (~40 s)

	passiveCap        = 40  // world population target for passive mobs
	passiveSpawnEvery = 600 // ticks between wild-spawn attempts (30 s)
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
	m := h.spawnMob(players, etype, float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
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

// sheepMeta builds the sheep fleece byte (color 0 = white + sheared bit).
func sheepMeta(eid int32, sheared bool) []byte {
	var v byte
	if sheared {
		v = 0x10
	}
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexSheep)
	b = protocol.AppendVarInt(b, 0) // type: byte
	b = protocol.AppendU8(b, v)
	return protocol.AppendU8(b, itemMetaEnd)
}

// feedAnimal handles a right-click with this species' love-food: consume one
// and start courting (adults only, off cooldown).
func (h *hub) feedAnimal(players map[int32]*tracked, t *tracked, m *mob) bool {
	food := loveFood(m.etype)
	if food == 0 || heldStack(t).item != food || m.baby || m.loveTicks > 0 || m.breedCD > 0 {
		return false
	}
	if t.gamemode == gmSurvival {
		s := &t.inv.slots[t.p.heldSlot()]
		if s.count--; s.count == 0 {
			*s = invStack{}
		}
		h.sendSlot(t, t.p.heldSlot())
	}
	m.loveTicks = loveTicks
	m.lovedBy = t.p.eid
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, statusInLove))
	h.playSound(players, "minecraft:entity.generic.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// shearSheep handles shears on an unsheared adult sheep: wool pops off.
func (h *hub) shearSheep(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entitySheep || m.sheared || m.baby || heldStack(t).item != itemShears {
		return false
	}
	m.sheared = true
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sheepMeta(m.eid, true)))
	h.spawnItem(players, itemWhiteWool, 1+h.rng.Intn(3), m.x, m.y, m.z)
	h.playSound(players, "minecraft:entity.sheep.shear", sndNeutral, m.x, m.y, m.z, 1, 1)
	if t.gamemode == gmSurvival {
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
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, false)))
			}
		}
		if m.etype == entitySheep && m.sheared && h.rng.Intn(woolRegrowIn) == 0 {
			m.sheared = false
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sheepMeta(m.eid, false)))
		}
		if m.etype == entityChicken && !m.baby {
			if m.eggIn -= survivalTickN; m.eggIn <= 0 {
				m.eggIn = eggLayMin + h.rng.Intn(eggLayMax-eggLayMin)
				h.spawnItem(players, itemEgg, 1, m.x, m.y, m.z)
				h.playSound(players, "minecraft:entity.chicken.egg", sndNeutral, m.x, m.y, m.z, 1, 1)
			}
		}
		if m.loveTicks <= 0 {
			continue
		}
		m.loveTicks -= survivalTickN
		for _, o := range h.mobs {
			if o == m || o.etype != m.etype || o.loveTicks <= 0 || o.dying > 0 {
				continue
			}
			if math.Hypot(o.x-m.x, o.z-m.z) > breedRange {
				continue
			}
			m.loveTicks, o.loveTicks = 0, 0
			m.breedCD, o.breedCD = breedCooldown, breedCooldown
			baby := h.spawnAnimal(players, m.etype, int(m.x), int(m.z))
			baby.baby, baby.growLeft = true, growUpTicks
			h.toNearbyEv(players, 0, baby.x, baby.z, metaEv(babyMeta(baby.eid, true)))
			h.spawnXPOrb(players, 1+h.rng.Intn(7), m.x, m.y, m.z) // breeding XP (vanilla 1-7)
			breeder := players[m.lovedBy]
			if breeder == nil {
				breeder = players[o.lovedBy]
			}
			if breeder != nil {
				h.advance(players, breeder, "bred_animals", advMatch{entity: advEntityName[m.etype]})
				h.incCustom(breeder, "animals_bred", 1)
			}
			break
		}
	}
}

// wildSpawn tops up the passive population: small groups on grass near a
// player, up to the world cap (the boot herds only cover spawn).
func (h *hub) wildSpawn(players map[int32]*tracked) {
	if len(players) == 0 || !h.rules.DoMobSpawning {
		return
	}
	passives := 0
	for _, m := range h.mobs {
		if !m.hostile {
			passives++
		}
	}
	if passives >= passiveCap {
		return
	}
	var pick *tracked
	for _, t := range players {
		pick = t
		break
	}
	ang := h.rng.Float64() * 2 * math.Pi
	dist := spawnMinDist + h.rng.Intn(spawnMaxDist-spawnMinDist)
	cx := int(pick.x) + int(math.Cos(ang)*float64(dist))
	cz := int(pick.z) + int(math.Sin(ang)*float64(dist))
	if !h.ownedBlock(cx, cz) {
		return // don't spawn outside this pod's region
	}
	if !h.spawnableAnimal(cx, cz) {
		return
	}
	species := h.biomeAnimal(cx, cz)
	occupied := map[[2]int]bool{}
	for i := 0; i < 2+h.rng.Intn(3); i++ { // a family of 2-4
		x, z := h.spreadSpawn(cx, cz, occupied)
		h.spawnAnimal(players, species, x, z)
	}
}

// biomeAnimal picks a passive species appropriate to the column's biome
// (vanilla's per-biome spawn lists, condensed): the common farm four everywhere,
// plus biome signatures — rabbits/nothing exotic in deserts, polar bears and
// wolves in the snow, goats on the peaks, horses/donkeys on the plains.
func (h *hub) biomeAnimal(x, z int) int {
	base := []int{entityCow, entityChicken, entityPig, entitySheep}[h.rng.Intn(4)]
	switch b := h.world.BiomeAt(x, z); {
	case isColdBiome(b):
		return []int{entityPolarBear, entityWolf, entityRabbit, entityFox, base}[h.rng.Intn(5)]
	case isDesertBiome(b):
		return []int{entityRabbit, entityCamel, base}[h.rng.Intn(3)]
	case strings.Contains(b, "windswept") || strings.Contains(b, "slopes") || strings.Contains(b, "peak"):
		return []int{entityGoat, entityLlama, base}[h.rng.Intn(3)]
	case strings.Contains(b, "plains"):
		return []int{entityHorse, entityDonkey, entityWolf, base, base}[h.rng.Intn(5)]
	}
	return base
}
