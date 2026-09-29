package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// The rest of vanilla's mobInteract overrides: shears on a mooshroom (it
// becomes a cow and sheds five mushrooms), a snow golem (its pumpkin comes
// off) and a bogged (two mushrooms, once); an iron ingot mending an iron
// golem; flint and steel or a fire charge lighting a creeper; and a cookie
// killing a parrot.

var (
	itemIronIngot     = int32(itemByName["iron_ingot"])
	itemFlintAndSteel = int32(itemByName["flint_and_steel"])
	itemCookie        = int32(itemByName["cookie"])
)

const (
	golemRepairHeal      = 25
	snowGolemPumpkinMeta = 16 // SnowGolem DATA_PUMPKIN_ID byte: 0x10 = wearing it
	boggedShearedMeta    = 16 // Bogged DATA_SHEARED boolean
)

// tryShearOther is Shearable.readyForShearing + shear for the mobs besides
// sheep: mooshrooms (MushroomCow.shear: a cow, five mushrooms of its
// colour), snow golems (the carved pumpkin drops, the head shows) and the
// bogged (its two mushrooms, then never again).
func (h *hub) tryShearOther(players map[int32]*tracked, t *tracked, m *mob) bool {
	if usedStack(t).item != itemShears || m.dying > 0 {
		return false
	}
	switch m.etype {
	case entityMooshroom:
		if m.baby {
			return false
		}
		mush := itemRedMushroom
		if m.variant == mooshroomBrown {
			mush = itemBrownMushroom
		}
		h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.mooshroom.shear", sndPlayer, m.x, m.y, m.z, 1, 1)
		for i := 0; i < 5; i++ {
			h.spawnItemIn(players, m.dim, mush, 1, m.x, m.y+1, m.z)
		}
		h.convertMob(players, m, entityCow)
	case entitySnowGolem:
		if m.sheared {
			return false
		}
		m.sheared = true
		h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.snow_golem.shear", sndPlayer, m.x, m.y, m.z, 1, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(snowGolemMeta(m)))
		h.spawnItemIn(players, m.dim, itemCarvedPumpkin, 1, m.x, m.y+1.7, m.z)
	case entityBogged:
		if m.sheared {
			return false
		}
		m.sheared = true
		h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.bogged.shear", sndPlayer, m.x, m.y, m.z, 1, 1)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, boggedShearedMeta, true)))
		for i := 0; i < 2; i++ { // shearing/bogged: two rolls, red or brown each
			mush := itemRedMushroom
			if h.rng.Intn(2) == 0 {
				mush = itemBrownMushroom
			}
			h.spawnItemIn(players, m.dim, mush, 1, m.x, m.y+1.99, m.z)
		}
	default:
		return false
	}
	if isSurvival(t.gamemode) {
		h.applyToolWear(t, t.useSlot(), 1)
	}
	return true
}

// snowGolemMeta is the golem's pumpkin byte: on (0x10) unless sheared.
func snowGolemMeta(m *mob) []byte {
	var v byte
	if !m.sheared {
		v = 0x10
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, snowGolemPumpkinMeta)
	b = protocol.AppendVarInt(b, 0) // byte
	b = protocol.AppendU8(b, v)
	return protocol.AppendU8(b, itemMetaEnd)
}

// tryRepairGolem is IronGolem.mobInteract: an iron ingot heals 25 (nothing
// happens to a golem at full health), with the repair clank.
func (h *hub) tryRepairGolem(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityIronGolem || usedStack(t).item != itemIronIngot || m.dying > 0 {
		return false
	}
	maxHP := m.maxHP()
	if m.health >= maxHP {
		return false
	}
	m.health = min(maxHP, m.health+golemRepairHeal)
	h.playSoundDim(players, m.dim, "minecraft:entity.iron_golem.repair", sndNeutral, m.x, m.y, m.z, 1, 1+(h.rng.Float32()-h.rng.Float32())*0.2)
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	return true
}

// tryIgniteCreeper is Creeper.mobInteract: flint and steel (worn one step)
// or a fire charge (spent) lights the fuse.
func (h *hub) tryIgniteCreeper(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityCreeper || m.dying > 0 {
		return false
	}
	held := usedStack(t).item
	if held != itemFlintAndSteel && held != itemFireCharge {
		return false
	}
	sound := "minecraft:item.flintandsteel.use"
	if held == itemFireCharge {
		sound = "minecraft:item.firecharge.use"
	}
	h.playSoundDim(players, m.dim, sound, sndHostile, m.x, m.y, m.z, 1, h.rng.Float32()*0.4+0.8)
	m.ignited = true // Creeper.ignite: from here the swell only goes up
	h.creeperSetSwellDir(players, m, 1)
	if isSurvival(t.gamemode) {
		if held == itemFireCharge {
			h.consumeUsed(t)
		} else {
			h.applyToolWear(t, t.useSlot(), 1)
		}
	}
	return true
}

// tryPoisonParrot is Parrot.mobInteract's #parrot_poisonous_food branch:
// a cookie is eaten, poisons the bird and kills it outright.
func (h *hub) tryPoisonParrot(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityParrot || usedStack(t).item != itemCookie || m.dying > 0 {
		return false
	}
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	h.applyMobEffect(players, m, effPoison, 0, 45)
	h.hurtMobOf(players, m, math.MaxFloat32, dtPlayerAttack)
	return true
}

// interactMob is a right-click on a mob in reach (Mob.interact): each
// species' own interaction is tried in turn, and the first that takes the
// click raises player_interacted_with_entity with the item that was in hand
// before it (PlayerInteractTrigger reads the stack as it was).
func (h *hub) interactMob(players map[int32]*tracked, t *tracked, m *mob, sneak bool) bool {
	held := usedStack(t).item
	if m.etype == entityCamelHusk {
		m.persistent = true // CamelHusk.interact: any click keeps it
	}
	if h.tryEggOffspring(players, t, m) || h.cureZombieVillager(players, t, m) || h.feedTadpole(players, t, m) || h.trySulfurCube(players, t, m) || h.tryBucketMob(players, t, m) || h.tryLeash(players, t, m) || h.tryShearEquipment(players, t, m, sneak) || h.tryNameTag(players, t, m) || h.tryDyeSheep(players, t, m) ||
		h.tryHorseScreen(players, t, m, sneak) || h.tryHappyGhast(players, t, m) ||
		h.tryCopperGolem(players, t, m) || h.tryMilk(players, t, m) ||
		h.tryFlowerMooshroom(players, t, m) || h.tryMilkStew(players, t, m) || h.tryMount(players, t, m) ||
		h.tryBrush(players, t, m) || h.tryWolfArmor(players, t, m) || h.tryAllay(players, t, m) || h.tryBarter(players, t, m) || h.tryFeedDolphin(players, t, m) || h.tryIgniteCreeper(players, t, m) || h.tryRepairGolem(players, t, m) || h.tryPoisonParrot(players, t, m) || h.tryShearOther(players, t, m) || h.tryTame(players, t, m) || h.shearSheep(players, t, m) || h.feedAnimal(players, t, m) {
		im := advMatch{entity: advEntityName[m.etype], baby: m.baby, item: held, variant: advVariantName(m)}
		if m.armorSt.count > 0 { // the entity as the click left it (repair_wolf_armor asks for a mended coat)
			im.body, im.bodyDmg = m.armorSt.item, m.armorSt.dmg
		}
		h.advance(players, t, "player_interacted_with_entity", im)
		return true
	}
	return false
}

// onInteractMob is Player.interactOn for a right-click on an entity, in the
// hand t.useOffhand names.
func (h *hub) onInteractMob(players map[int32]*tracked, t *tracked, e evInteractMob) {
	if h.fixtureOutOfReach(t, e.target) {
		return // handleInteract: beyond isWithinEntityInteractionRange(…, 3.0)
	}
	if st := h.armorStands[e.target]; st != nil {
		h.interactStand(players, t, st)
		return
	}
	if k := h.knots[e.target]; k != nil {
		h.interactKnot(players, t, k, e.sneak)
		return
	}
	if f := h.itemFrames[e.target]; f != nil {
		h.interactFrame(players, t, f)
		return
	}
	if v := h.vehicles[e.target]; v != nil {
		if !v.isBoat() {
			h.interactCart(players, t, v)
			return
		}
		if e.sneak && v.chest != nil { // ChestBoat: sneak-click opens the cargo
			h.openVehicleChest(players, t, v)
			return
		}
		h.mountVehicle(players, t, v)
		return
	}
	m := h.mobs[e.target]
	if m == nil || m.dying != 0 || !mobInReach(t, m) {
		return
	}
	if m.etype == entityVillager || m.etype == entityWanderingTrader {
		h.openTrades(t, m)
		return
	}
	h.interactMob(players, t, m, e.sneak)
}
