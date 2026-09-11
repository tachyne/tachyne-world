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
	if heldStack(t).item != itemShears || m.dying > 0 {
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
		h.playSound(players, "minecraft:entity.mooshroom.shear", sndPlayer, m.x, m.y, m.z, 1, 1)
		for i := 0; i < 5; i++ {
			h.spawnItem(players, mush, 1, m.x, m.y+1, m.z)
		}
		h.convertMob(players, m, entityCow)
	case entitySnowGolem:
		if m.sheared {
			return false
		}
		m.sheared = true
		h.playSound(players, "minecraft:entity.snow_golem.shear", sndPlayer, m.x, m.y, m.z, 1, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(snowGolemMeta(m)))
		h.spawnItem(players, itemCarvedPumpkin, 1, m.x, m.y+1.7, m.z)
	case entityBogged:
		if m.sheared {
			return false
		}
		m.sheared = true
		h.playSound(players, "minecraft:entity.bogged.shear", sndPlayer, m.x, m.y, m.z, 1, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, boggedShearedMeta, true)))
		for i := 0; i < 2; i++ { // shearing/bogged: two rolls, red or brown each
			mush := itemRedMushroom
			if h.rng.Intn(2) == 0 {
				mush = itemBrownMushroom
			}
			h.spawnItem(players, mush, 1, m.x, m.y+1.99, m.z)
		}
	default:
		return false
	}
	if t.gamemode == gmSurvival {
		h.applyToolWear(t, t.p.heldSlot(), 1)
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
	if m.etype != entityIronGolem || heldStack(t).item != itemIronIngot || m.dying > 0 {
		return false
	}
	maxHP := m.maxHP()
	if m.health >= maxHP {
		return false
	}
	m.health = min(maxHP, m.health+golemRepairHeal)
	h.playSound(players, "minecraft:entity.iron_golem.repair", sndNeutral, m.x, m.y, m.z, 1, 1+(h.rng.Float32()-h.rng.Float32())*0.2)
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	return true
}

// tryIgniteCreeper is Creeper.mobInteract: flint and steel (worn one step)
// or a fire charge (spent) lights the fuse.
func (h *hub) tryIgniteCreeper(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityCreeper || m.dying > 0 {
		return false
	}
	held := heldStack(t).item
	if held != itemFlintAndSteel && held != itemFireCharge {
		return false
	}
	sound := "minecraft:item.flintandsteel.use"
	if held == itemFireCharge {
		sound = "minecraft:item.firecharge.use"
	}
	h.playSound(players, sound, sndHostile, m.x, m.y, m.z, 1, h.rng.Float32()*0.4+0.8)
	if m.fuse == 0 {
		m.fuse = creeperFuseTicks
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(creeperStateMeta(m.eid, 1)))
	}
	if t.gamemode == gmSurvival {
		if held == itemFireCharge {
			h.consumeHeld(t)
		} else {
			h.applyToolWear(t, t.p.heldSlot(), 1)
		}
	}
	return true
}

// tryPoisonParrot is Parrot.mobInteract's #parrot_poisonous_food branch:
// a cookie is eaten, poisons the bird and kills it outright.
func (h *hub) tryPoisonParrot(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityParrot || heldStack(t).item != itemCookie || m.dying > 0 {
		return false
	}
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.applyMobEffect(players, m, effPoison, 0, 45)
	h.hurtMobOf(players, m, math.MaxFloat32, dtPlayerAttack)
	return true
}
