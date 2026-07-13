package server

import (
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Mount inventories, on the vanilla model: sneak + right-click a horse-family
// mob opens its screen (saddle slot, body-armor/carpet slot, and a chest grid
// for chested donkeys/mules — 5 columns — and llamas — strength columns).
// The screen is its own packet (horse_screen_open), laid out client-side from
// the column count; saddle/armor changes broadcast as real equipment slots
// (vanilla replaced the old saddle flag in 1.21.5). Mount inventories share
// the mobs' v1 lifetime: they don't persist across restarts.

const menuHorseColumnsDonkey = 5

// horseFamily: mounts with the inventory screen.
func horseFamily(etype int) bool {
	switch etype {
	case entityHorse, entityDonkey, entityMule, entityCamel,
		entitySkeletonHorse, entityZombieHorse, entityLlama, entityTraderLlama:
		return true
	}
	return false
}

// chestedFamily: mounts that accept a chest.
func chestedFamily(etype int) bool {
	switch etype {
	case entityDonkey, entityMule, entityLlama, entityTraderLlama:
		return true
	}
	return false
}

// horseArmorItems / carpetItems: what the body slot accepts.
var horseArmorItems = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"leather_horse_armor", "iron_horse_armor", "golden_horse_armor", "diamond_horse_armor"} {
		if id := int32(itemByName[n]); id != 0 {
			m[id] = true
		}
	}
	return m
}()

var carpetItems = func() map[int32]bool {
	m := map[int32]bool{}
	for name, id := range itemByName {
		if strings.HasSuffix(name, "_carpet") && !strings.Contains(name, "moss") {
			m[int32(id)] = true
		}
	}
	return m
}()

// horseColumns is the mount's chest-grid width (vanilla getInventoryColumns).
func horseColumns(m *mob) int {
	if !m.chested {
		return 0
	}
	if m.etype == entityLlama || m.etype == entityTraderLlama {
		return int(m.strength)
	}
	return menuHorseColumnsDonkey
}

// tryHorseScreen handles sneak-interacts and chest-equips on the horse
// family. Returns true if the interaction was consumed.
func (h *hub) tryHorseScreen(players map[int32]*tracked, t *tracked, m *mob, sneak bool) bool {
	if !horseFamily(m.etype) || m.dying > 0 || m.baby {
		return false
	}
	if sneak {
		h.openHorseScreen(players, t, m)
		return true
	}
	// Chest-equip: a held chest on an unchested donkey/mule/llama.
	if heldStack(t).item == int32(itemByName["chest"]) && chestedFamily(m.etype) && !m.chested {
		if m.strength == 0 {
			m.strength = int8(1 + h.rng.Intn(3)) // llama columns; harmless for donkeys
			if h.rng.Intn(20) == 0 {
				m.strength = int8(1 + h.rng.Intn(5)) // the rare strong llama
			}
		}
		m.chested = true
		m.chest = make([]invStack, horseColumns(m)*3)
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.playSound(players, "minecraft:entity.donkey.chest", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	return false
}

// openHorseScreen opens the mount window: its own open packet, then the
// contents (2 equipment slots + chest grid + player inventory).
func (h *hub) openHorseScreen(players map[int32]*tracked, t *tracked, m *mob) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.reclaimEnchant(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind = h.nextWin, winHorse
	t.horseEID = m.eid

	t.p.trySendEv(attachproto.HorseScreen{ID: int32(t.winID), Columns: int32(horseColumns(m)), EID: m.eid})
	h.sendHorseWindow(t, m)
}

// sendHorseWindow pushes the mount window contents.
func (h *hub) sendHorseWindow(t *tracked, m *mob) {
	t.inv.stateId++
	n := horseColumns(m) * 3
	slots := make([]attachproto.ItemStack, 0, 2+n+36)
	slots = append(slots, stackEv(m.saddleSt), stackEv(m.armorSt))
	for i := 0; i < n; i++ {
		slots = append(slots, stackEv(m.chest[i]))
	}
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// horseSlotPtr resolves a mount-window slot (0 saddle, 1 body, 2.. chest,
// then the player inventory).
func (h *hub) horseSlotPtr(t *tracked, slot int16) (*invStack, int) {
	m := h.mobs[t.horseEID]
	if m == nil {
		return nil, -1
	}
	n := int16(horseColumns(m) * 3)
	switch {
	case slot == 0:
		return &m.saddleSt, -1
	case slot == 1:
		return &m.armorSt, -1
	case slot >= 2 && slot < 2+n:
		return &m.chest[slot-2], -1
	case slot >= 2+n && slot < 2+n+27:
		return &t.inv.slots[slot-(2+n)+9], -1
	case slot >= 2+n+27 && slot < 2+n+36:
		return &t.inv.slots[slot-(2+n)-27], int(slot - (2 + n) - 27)
	}
	return nil, -1
}

// horseEquipSync re-derives the saddled flag and broadcasts the mount's
// equipment (saddle + body armor) after a menu edit.
func (h *hub) horseEquipSync(players map[int32]*tracked, m *mob) {
	// AUTHORITY: the equipment slots only hold what belongs there.
	if m.saddleSt.item != 0 && m.saddleSt.item != itemSaddle {
		m.saddleSt = invStack{}
	}
	if m.armorSt.item != 0 && !horseArmorItems[m.armorSt.item] && !carpetItems[m.armorSt.item] {
		m.armorSt = invStack{}
	}
	m.saddled = m.saddleSt.item != 0
	var eq attachproto.Equipment
	eq.EID = m.eid
	eq.Slots[attachproto.EquipBody] = stackEv(m.armorSt)
	eq.Slots[attachproto.EquipSaddle] = stackEv(m.saddleSt)
	eq.SendSaddle = true
	h.toNearbyEv(players, m.dim, m.x, m.z, eq)
}

// spillHorse drops a dead mount's inventory (saddle, armor, chest + contents).
func (h *hub) spillHorse(players map[int32]*tracked, m *mob) {
	drop := func(st invStack) {
		if st.item == 0 || st.count <= 0 {
			return
		}
		if it := h.spawnItem(players, st.item, st.count, m.x, m.y+0.5, m.z); it != nil {
			it.dmg, it.ench, it.mapID = st.dmg, st.ench, st.mapID
			it.pats = st.pats
			it.trimMat, it.trimPat = st.trimMat, st.trimPat
		}
	}
	drop(m.saddleSt)
	drop(m.armorSt)
	if m.chested {
		drop(invStack{item: int32(itemByName["chest"]), count: 1})
		for _, st := range m.chest {
			drop(st)
		}
	}
}
