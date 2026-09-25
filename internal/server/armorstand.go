package server

import (
	"math"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Armor stands, on the vanilla model: placed from the item (bottom-center of
// the clicked cell, yaw snapped to 45°), clicked with armor to dress them
// (swap when occupied), clicked empty-handed to undress (head first), broken
// by a quick double punch — the stand and its gear pop. Metadata indices and
// the entity type are identical on every served version, so no gateway
// surgery. v1: no arms (vanilla survival default), no pose controls.

const (
	entityStatusStandWobble = 32 // vanilla entity event: armor-stand hit wobble
	standBreakWindow        = 5  // ticks: second punch inside this breaks (vanilla WOBBLE_TIME)
)

var itemArmorStand = int32(itemByName["armor_stand"])

// armorStand is one placed stand. equip is indexed by the attach Equip*
// constants (main, off, feet, legs, chest, head).
type armorStand struct {
	eid     int32
	dim     int
	x, y, z float64
	yaw     float32
	equip   [6]invStack
	lastHit uint64
	name    string  // custom_name: from a named armour-stand item or a name tag
	hurt    float64 // health lost (a stand has 20; causeDamage breaks it at 0.5)
	fire    int     // remainingFireTicks
}

// standSlotFor classifies an item into the stand's equip index (-1 = not
// wearable; hand slots need arms, which v1 stands don't have).
func standSlotFor(item int32) int {
	name := ""
	if n, ok := itemNameOf[item]; ok {
		name = n
	}
	switch {
	case strings.HasSuffix(name, "_helmet") || name == "turtle_helmet" ||
		strings.HasSuffix(name, "_head") || name == "carved_pumpkin" || strings.HasSuffix(name, "_skull"):
		return attachproto.EquipHead
	case strings.HasSuffix(name, "_chestplate") || name == "elytra":
		return attachproto.EquipChest
	case strings.HasSuffix(name, "_leggings"):
		return attachproto.EquipLegs
	case strings.HasSuffix(name, "_boots"):
		return attachproto.EquipFeet
	}
	return -1
}

type evPlaceStand struct {
	eid     int32
	x, y, z int
	yaw     float32
	off     bool // used from the offhand (the packet's InteractionHand)
}

func (evPlaceStand) isHubEvent() {}

// onPlaceStand spawns a stand at the cell's bottom center, yaw snapped.
func (h *hub) onPlaceStand(players map[int32]*tracked, e evPlaceStand) {
	t := players[e.eid]
	if t == nil {
		return
	}
	w := h.worldFor(t.dim)
	if w.At(e.x, e.y, e.z) != 0 || w.At(e.x, e.y+1, e.z) != 0 {
		return // needs two open cells
	}
	yaw := float32(math.Floor(float64(e.yaw-180+22.5)/45)) * 45
	st := &armorStand{eid: h.allocEID(), dim: t.dim,
		x: float64(e.x) + 0.5, y: float64(e.y), z: float64(e.z) + 0.5, yaw: yaw,
		name: usedStack(t).name} // createDefaultStackConfig: the item's custom_name
	h.armorStands[st.eid] = st
	if isSurvival(t.gamemode) {
		h.consumeUsed(t)
	}
	h.toNearbyEv(players, st.dim, st.x, st.z, h.standAddEv(st))
	if st.name != "" {
		h.toNearbyEv(players, st.dim, st.x, st.z, metaEv(nameMeta(st.eid, st.name)))
	}
	h.vibAt(st.dim, freqEntityPlace, st.x, st.y, st.z, t.p.eid)
	h.playSoundDim(players, st.dim, "minecraft:entity.armor_stand.place", sndBlock, st.x, st.y, st.z, 0.75, 0.8)
}

func (h *hub) standAddEv(st *armorStand) attachproto.EntityAdd {
	return attachproto.EntityAdd{EID: st.eid, Type: int32(entityByName["armor_stand"]),
		X: st.x, Y: st.y, Z: st.z, Yaw: st.yaw}
}

func (h *hub) standEquipEv(st *armorStand) attachproto.Equipment {
	var eq attachproto.Equipment
	eq.EID = st.eid
	for i, s := range st.equip {
		eq.Slots[i] = stackEv(s)
	}
	return eq
}

// sendStandsTo replays placed stands to a joining/refreshing session.
func (h *hub) sendStandsTo(t *tracked) {
	for _, st := range h.armorStands {
		if st.dim != t.dim {
			continue
		}
		t.p.trySendEv(h.standAddEv(st))
		t.p.trySendEv(h.standEquipEv(st))
		if st.name != "" {
			t.p.trySendEv(metaEv(nameMeta(st.eid, st.name)))
		}
	}
}

// interactStand dresses/undresses: a wearable held item equips (swapping any
// current piece into the hand); an empty hand takes the topmost piece.
func (h *hub) interactStand(players map[int32]*tracked, t *tracked, st *armorStand) {
	if t.inv == nil {
		return
	}
	heldSlot := t.p.heldSlot()
	held := t.inv.slots[heldSlot]
	if held.item == int32(itemByName["name_tag"]) {
		// ArmorStand.interactAt passes a name tag, and NameTagItem names any
		// living entity but a player: a named tag names the stand.
		if held.name == "" {
			return
		}
		st.name = held.name
		h.toNearbyEv(players, st.dim, st.x, st.z, metaEv(nameMeta(st.eid, st.name)))
		if isSurvival(t.gamemode) {
			if held.count--; held.count <= 0 {
				held = invStack{}
			}
			t.inv.slots[heldSlot] = held
			h.sendSlot(t, heldSlot)
		}
		return
	}
	if held.item != 0 {
		slot := standSlotFor(held.item)
		if slot < 0 {
			return
		}
		prev := st.equip[slot]
		piece := held
		piece.count = 1
		st.equip[slot] = piece
		if t.gamemode != gmCreative {
			if held.count--; held.count <= 0 {
				held = invStack{}
			}
			t.inv.slots[heldSlot] = held
			if prev.item != 0 { // the swapped-out piece comes back
				changed, leftover := t.inv.addStack(prev)
				for _, s := range changed {
					h.sendSlot(t, s)
				}
				if leftover > 0 {
					h.tossItem(players, t, prev)
				}
			}
			h.sendSlot(t, heldSlot)
		}
	} else {
		// Undress head-down (no click height in the domain event).
		order := []int{attachproto.EquipHead, attachproto.EquipChest, attachproto.EquipLegs, attachproto.EquipFeet}
		took := false
		for _, slot := range order {
			if st.equip[slot].item != 0 {
				piece := st.equip[slot]
				st.equip[slot] = invStack{}
				changed, leftover := t.inv.addStack(piece)
				for _, s := range changed {
					h.sendSlot(t, s)
				}
				if leftover > 0 {
					h.tossItem(players, t, piece)
				}
				took = true
				break
			}
		}
		if !took {
			return
		}
	}
	h.toNearbyEv(players, st.dim, st.x, st.z, h.standEquipEv(st))
	h.playSoundDim(players, t.dim, "minecraft:entity.armor_stand.hit", sndBlock, st.x, st.y, st.z, 0.3, 1)
}

// hitStand is a player's blow (Player.attack → hurtServer with player
// damage): the vanilla double punch — a lone hit wobbles, a second within
// the window breaks, a creative player's breaks at once.
func (h *hub) hitStand(players map[int32]*tracked, t *tracked, st *armorStand) {
	h.standHurt(players, st, dtPlayerAttack, t, false)
}

// breakStand removes a stand: brokenByPlayer drops the stand item (with its
// name) and brokenByAnything the gear and the break sound; a creative or
// bypassing kill drops nothing.
func (h *hub) breakStand(players map[int32]*tracked, st *armorStand, standItem, gear bool) {
	delete(h.armorStands, st.eid)
	h.entityGone(players, st.dim, st.eid)
	if !gear {
		return
	}
	h.playSoundDim(players, st.dim, "minecraft:entity.armor_stand.break", sndBlock, st.x, st.y, st.z, 1, 1)
	if !h.rules.EntityDrops {
		return // gamerule entity_drops
	}
	if standItem {
		// ArmorStand.brokenByPlayer: the item carries the stand's name.
		if it := h.spawnItemIn(players, st.dim, itemArmorStand, 1, st.x, st.y+0.5, st.z); it != nil && st.name != "" {
			it.name = st.name
			h.refreshItemMeta(players, it)
		}
	}
	for _, s := range st.equip { // brokenByAnything: each piece whole, components and all
		if s.item != 0 {
			if it := h.spawnItemIn(players, st.dim, s.item, s.count, st.x, st.y+1, st.z); it != nil {
				it.setFrom(s)
				h.refreshItemMeta(players, it)
			}
		}
	}
}
