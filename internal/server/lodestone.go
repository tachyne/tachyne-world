package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Lodestone compasses — CompassItem.useOn and the LodestoneTracker
// component. A compass used on a lodestone locks onto it: the lock sound
// plays, item_used_on_block fires ("Country Lode, Take Me Home"), and the
// stack gains a tracker naming the lodestone's dimension and position. A
// single compass in survival is converted in place; a stack of them (or a
// creative player's) yields one new lodestone compass and, in survival,
// consumes one plain compass.
//
// The tracker is a proper item component: it travels with the stack through
// inventories, containers and drops (stackRow columns 20-23) and reaches the
// client as minecraft:lodestone_tracker, which is what turns the needle and
// renames the item. LodestoneTracker.tick — a compass in a player's inventory
// forgets a lodestone that no longer exists — runs once a second over online
// players; a lodestone is always a placed block, so the check reads the edit
// overlay and never touches chunk generation.

// lodeTracker is the lodestone_tracker component: has = the component is
// present (the compass is a "lodestone compass"), target = it points at a
// block (false once that lodestone was removed: the needle spins).
type lodeTracker struct {
	has, target bool
	x, y, z     int32
	dim         int8
}

var (
	itemCompass    = itemByName["compass"]
	lodestoneBlock = worldgen.BlockBase("lodestone")
)

type evUseLodestone struct {
	eid     int32
	x, y, z int
	slot    int32
}

func (evUseLodestone) isHubEvent() {}

// tryCompassUse (connection side): a compass on a lodestone.
func (s *Server) tryCompassUse(p *player, x, y, z int, seq int32) bool {
	if p.heldItem() != itemCompass {
		return false
	}
	state := s.worldFor(p).Block(x, y, z)
	if state != lodestoneBlock {
		return false
	}
	s.hub.post(evUseLodestone{eid: p.eid, x: x, y: y, z: z, slot: int32(p.held)})
	s.sendBlockChange(p, x, y, z, state, seq)
	return true
}

// dimRegistryName is a dimension's registry key, as GlobalPos carries it.
func dimRegistryName(dim int) string {
	switch dim {
	case dimNether:
		return "minecraft:the_nether"
	case dimEnd:
		return "minecraft:the_end"
	}
	return "minecraft:overworld"
}

// onUseLodestone is CompassItem.useOn on the hub.
func (h *hub) onUseLodestone(players map[int32]*tracked, e evUseLodestone) {
	t := players[e.eid]
	if t == nil || t.dead || t.inv == nil || e.slot < 0 || e.slot >= 9 {
		return
	}
	held := t.inv.slots[e.slot]
	if held.item != itemCompass || held.count == 0 {
		return
	}
	state := h.worldFor(t.dim).At(e.x, e.y, e.z)
	if state != lodestoneBlock {
		return
	}
	h.playSoundDim(players, t.dim, "minecraft:item.lodestone_compass.lock", sndPlayer,
		float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
	h.advance(players, t, "item_used_on_block", advMatch{blockState: state, item: held.item})
	target := lodeTracker{has: true, target: true, x: int32(e.x), y: int32(e.y), z: int32(e.z), dim: int8(t.dim)}
	if t.gamemode != gmCreative && held.count == 1 { // replaceExistingStack
		t.inv.slots[e.slot].lode = target
		h.sendSlot(t, int(e.slot))
		return
	}
	// transmuteCopy: one new lodestone compass; survival pays one plain
	// compass for it. Wherever the inventory has room, else at the feet.
	if t.gamemode != gmCreative {
		held.count--
		t.inv.slots[e.slot] = held
		h.sendSlot(t, int(e.slot))
	}
	fresh := invStack{item: itemCompass, count: 1, lode: target}
	changed, left := t.inv.addStack(fresh)
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	if left > 0 {
		if it := h.spawnItemIn(players, t.dim, fresh.item, 1, t.x, t.y, t.z); it != nil {
			it.lode = fresh.lode
			h.refreshItemMeta(players, it)
		}
	}
}

// lodestoneTick is LodestoneTracker.tick for every compass an online player
// holds: a tracked target in the player's own dimension that is no longer a
// lodestone is forgotten (the component stays; the needle goes random).
func (h *hub) lodestoneTick(players map[int32]*tracked) {
	for _, t := range players {
		if t.inv == nil {
			continue
		}
		check := func(st *invStack) bool {
			if st.item != itemCompass || !st.lode.has || !st.lode.target || int(st.lode.dim) != t.dim {
				return false
			}
			if s, ok := h.worldFor(t.dim).EditAt(int(st.lode.x), int(st.lode.y), int(st.lode.z)); ok && s == lodestoneBlock {
				return false
			}
			st.lode = lodeTracker{has: true}
			return true
		}
		for i := range t.inv.slots {
			if check(&t.inv.slots[i]) {
				h.sendSlot(t, i)
			}
		}
		if check(&t.offhand) {
			t.inv.stateId++
			t.p.trySendEv(attachproto.WindowSlot{ID: 0, StateID: t.inv.stateId, Slot: offhandWindowSlot, Item: stackEv(t.offhand)})
		}
	}
}

// offhandWindowSlot is the offhand's index in the player inventory window.
const offhandWindowSlot = 45

// packLode / unpackLode are the tracker's persisted form: x, y, z and
// flags (has | target<<1 | dim<<2). All-zero = no tracker.
func packLode(l lodeTracker) [4]int32 {
	if !l.has {
		return [4]int32{}
	}
	flags := int32(1) | int32(l.dim)<<2
	if l.target {
		flags |= 2
	}
	return [4]int32{l.x, l.y, l.z, flags}
}

func unpackLode(r [4]int32) lodeTracker {
	if r[3]&1 == 0 {
		return lodeTracker{}
	}
	return lodeTracker{has: true, target: r[3]&2 != 0, x: r[0], y: r[1], z: r[2], dim: int8(r[3] >> 2)}
}

// lodestoneComponent encodes lodestone_tracker: optional GlobalPos
// (dimension identifier + packed position) and the tracked flag.
func lodestoneComponent(b []byte, l lodeTracker) []byte {
	b = protocol.AppendVarInt(b, componentLodestone)
	if l.target {
		b = append(b, 1)
		b = protocol.AppendString(b, dimRegistryName(int(l.dim)))
		b = protocol.AppendI64(b, packPosition(int(l.x), int(l.y), int(l.z)))
	} else {
		b = append(b, 0)
	}
	return append(b, 1) // tracked: the needle follows the target
}

// packPosition is the wire Position: x 26 bits, z 26 bits, y 12 bits.
func packPosition(x, y, z int) int64 {
	return int64(x&0x3ffffff)<<38 | int64(z&0x3ffffff)<<12 | int64(y&0xfff)
}
