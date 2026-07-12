package server

import (
	"bytes"

	"encoding/binary"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Dropped-item entities: the visible item stacks that pop out when a block is
// broken or a plant loses its support. They are hub-owned like mobs, shown to
// nearby players via Spawn Entity + the item's metadata, and despawn after the
// vanilla five minutes. Pickup/inventory is a survival-mechanics follow-up; for
// now they render, rest on the ground, and time out.

const (
	itemDespawnTicks   = 6000 // 5 minutes — vanilla item lifetime
	itemMetaIndexStack = 8    // entity-metadata index of an item entity's stack
	itemMetaTypeSlot   = 7    // metadata value type id for Slot/ItemStack (1.21.5)
	itemMetaEnd        = 0xff // metadata list terminator
)

var (
	entityItem = entityID("item") // minecraft:entity_type "item" network ID (1.21.5)
)

type itemEntity struct {
	dim           int // dimension the drop lives in
	eid           int32
	uuid          [16]byte
	x, y, z       float64
	item          int32
	count         int
	dmg           int          // durability damage carried by the dropped stack
	ench          [2]enchApply // enchantments carried by the dropped stack
	mapID         int32        // filled_map identity carried by the dropped stack
	born          uint64       // world tick spawned (for despawn)
	noPickupUntil uint64       // absolute tick pickup unlocks (tosses get a longer hold;
	//                      NEVER fake this by moving born forward — a future born
	//                      underflows the unsigned despawn age and vanishes the item)
}

// spawnItem creates a dropped-item entity at (x,y,z) and shows it to nearby
// players. Returns nil for an empty drop.
func (h *hub) spawnItem(players map[int32]*tracked, item int32, count int, x, y, z float64) *itemEntity {
	return h.spawnItemIn(players, 0, item, count, x, y, z)
}

// spawnItemIn drops into an explicit dimension.
func (h *hub) spawnItemIn(players map[int32]*tracked, dim int, item int32, count int, x, y, z float64) *itemEntity {
	if item == 0 || count <= 0 {
		return nil
	}
	// Fall from the spawn point to the local floor, so a drop mined underground
	// rests in the tunnel (not teleported to the world surface) and a popped
	// plant's drop doesn't hang in the air.
	y = float64(h.worldFor(dim).DropY(int(x), int(math.Ceil(y)), int(z)))

	eid := h.allocEID()
	now := h.tick.Load()
	it := &itemEntity{eid: eid, dim: dim, x: x, y: y, z: z, item: item, count: count, born: now, noPickupUntil: now + pickupDelay}
	binary.BigEndian.PutUint32(it.uuid[12:], uint32(eid))
	h.items[eid] = it

	h.toNearbyEv(players, dim, x, z, entAdd(eid, entityItem, it.uuid, x, y, z, 0, 0))
	h.toNearbyEv(players, dim, x, z, metaEv(itemMetadata(eid, item, count)))
	h.bus.publish("item_drop", map[string]any{"eid": eid, "item": item, "count": count, "x": x, "y": y, "z": z})
	return it
}

// spawnBlockDrop places a broken block's loot. Items have no horizontal
// physics (they rest via a straight column drop), so breaking the END block of
// a ledge — nothing below the broken cell — would sink the drop to the bottom
// of the cliff. When the drop would fall more than a couple of blocks, prefer
// an adjacent column whose surface sits at the break level (vanilla physics
// would bounce the item onto it); only genuinely fall when no neighbour
// catches it.
func (h *hub) spawnBlockDrop(players map[int32]*tracked, item int32, count int, x, y, z int) {
	if y-h.world.DropY(x, y, z) <= 2 {
		h.spawnItem(players, item, count, float64(x)+0.5, float64(y), float64(z)+0.5)
		return
	}
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		nx, nz := x+d[0], z+d[1]
		if rest := h.world.DropY(nx, y+1, nz); rest == y+1 || rest == y {
			h.spawnItem(players, item, count, float64(nx)+0.5, float64(rest), float64(nz)+0.5)
			return
		}
	}
	h.spawnItem(players, item, count, float64(x)+0.5, float64(y), float64(z)+0.5)
}

// updateItems despawns dropped items past their lifetime and merges nearby
// identical stacks (vanilla: ground items within ~0.5 blocks combine).
func (h *hub) updateItems(players map[int32]*tracked) {
	now := h.tick.Load()
	for eid, it := range h.items {
		if now-it.born >= itemDespawnTicks {
			delete(h.items, eid)
			h.toNearbyEv(players, it.dim, it.x, it.z, entGone(eid))
			continue
		}
		for oid, other := range h.items {
			if oid == eid || other.item != it.item || other.dmg != 0 || it.dmg != 0 ||
				other.ench != it.ench || other.mapID != it.mapID ||
				it.count+other.count > stackCap(it.item) {
				continue
			}
			dx, dy, dz := other.x-it.x, other.y-it.y, other.z-it.z
			if dx*dx+dy*dy+dz*dz > 1 {
				continue
			}
			it.count += other.count // absorb the newer into this one
			delete(h.items, oid)
			h.toNearbyEv(players, other.dim, other.x, other.z, entGone(oid))
			h.toNearbyEv(players, it.dim, it.x, it.z, metaEv(itemMetadata(eid, it.item, it.count)))
		}
	}
}

// appendSlot encodes a Slot/ItemStack (1.21.5 component format). count 0 = empty.
func appendSlot(b []byte, item int32, count int) []byte {
	return appendStack(b, invStack{item: item, count: count})
}

// Structured-component ids we attach to Slots, in CANONICAL (770) numbering.
// damage renders the durability bar (id identical 1.21.5→26.2); enchantments
// renders the glint + tooltip (the chain renumbers it to 13 for 774+ clients
// — see protocol.copyFullSlot).
const (
	componentDamage       = 3
	componentEnchantments = 10
	componentStoredEnch   = 34 // books; remapped per version by the chain
	componentCustomName   = 5  // anvil renames (NBT text); remapped per version
	componentLore         = 8  // plugin-UI item lore (list of NBT texts); remapped per version
	componentMapID        = 37 // filled_map's map number; remapped per version
)

// appendStack encodes a Slot, attaching the damage component when the stack
// has taken durability wear and the enchantments component when enchanted.
func appendStack(b []byte, st invStack) []byte {
	b = protocol.AppendVarInt(b, int32(st.count))
	if st.count == 0 {
		return b
	}
	b = protocol.AppendVarInt(b, st.item)
	return append(b, stackComponents(st)...)
}

// stackEv is the domain form of a stack: id + count + the same component
// bytes appendStack writes (canonical wire form, opaque scaffolding).
func stackEv(st invStack) attachproto.ItemStack {
	if st.count == 0 {
		return attachproto.ItemStack{}
	}
	return attachproto.ItemStack{ID: st.item, Count: int32(st.count), Components: stackComponents(st)}
}

// stackNumEv is stackEv for a bare (item, count) pair.
func stackNumEv(item int32, count int) attachproto.ItemStack {
	return stackEv(invStack{item: item, count: count})
}

// metaEv wraps a canonical entity-metadata body (eid-prefixed, as every
// metadata builder here produces) into the typed event: eid split out, the
// list bytes opaque (typed later — same story as ItemStack.Components).
func metaEv(body []byte) attachproto.EntityMeta {
	r := bytes.NewReader(body)
	eid, _ := protocol.ReadVarInt(r)
	return attachproto.EntityMeta{EID: eid, Meta: body[len(body)-r.Len():]}
}

// stackComponents encodes what follows the item id in a Slot: the add/remove
// component counts and the component entries.
func stackComponents(st invStack) []byte {
	var b []byte
	var enchN int32
	for _, e := range st.ench {
		if e.lvl > 0 {
			enchN++
		}
	}
	comps := int32(0)
	if st.dmg > 0 {
		comps++
	}
	if enchN > 0 {
		comps++
	}
	if st.name != "" {
		comps++
	}
	if st.mapID != 0 {
		comps++
	}
	b = protocol.AppendVarInt(b, comps) // components to add
	b = protocol.AppendVarInt(b, 0)     // components to remove
	if st.dmg > 0 {
		b = protocol.AppendVarInt(b, componentDamage)
		b = protocol.AppendVarInt(b, int32(st.dmg))
	}
	if st.mapID != 0 {
		b = protocol.AppendVarInt(b, componentMapID)
		b = protocol.AppendVarInt(b, st.mapID)
	}
	if enchN > 0 {
		// Books carry STORED enchantments (what the anvil applies); everything
		// else carries live ones. Same wire shape, different component.
		comp := int32(componentEnchantments)
		if st.item == itemEnchantedBook {
			comp = componentStoredEnch
		}
		b = protocol.AppendVarInt(b, comp)
		b = protocol.AppendVarInt(b, enchN)
		for _, e := range st.ench {
			if e.lvl > 0 {
				b = protocol.AppendVarInt(b, int32(e.id))
				b = protocol.AppendVarInt(b, int32(e.lvl))
			}
		}
	}
	if st.name != "" {
		b = protocol.AppendVarInt(b, componentCustomName)
		b = append(b, chatNBT(st.name)...)
	}
	return b
}

// itemMetadata builds set_entity_metadata (0x5c) setting an item entity's stack
// (index 8, Slot type) and terminating the list.
func itemMetadata(eid int32, item int32, count int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, itemMetaIndexStack)
	b = protocol.AppendVarInt(b, itemMetaTypeSlot)
	b = appendSlot(b, item, count)
	b = protocol.AppendU8(b, itemMetaEnd)
	return b
}
