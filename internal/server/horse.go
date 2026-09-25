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
	case entityHorse, entityDonkey, entityMule, entityCamel, entityCamelHusk,
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
	if !horseFamily(m.etype) && !(m.etype == entityNautilus && m.tamed) || m.dying > 0 || m.baby {
		return false
	}
	tamed := m.tamed || isCamelKind(m.etype) // Camel.isTamed is always true
	if sneak && tamed {
		h.openHorseScreen(players, t, m) // HasCustomInventoryScreen: the nautilus shares the mount screen
		return true
	}
	held := heldStack(t).item
	// Horse/AbstractChestedHorse/ZombieHorse.mobInteract: anything in hand
	// that is not its food makes a wild one rear up and refuse — a saddle,
	// a chest, armour or a sword alike. Only an empty hand climbs on.
	if !tamed && m.rider == 0 && held != 0 && !isMobFood(m.etype, held) && m.etype != entitySkeletonHorse {
		h.horseMakeMad(players, m)
		return true
	}
	// Chest-equip: a held chest on an unchested donkey/mule/llama.
	if tamed && held == int32(itemByName["chest"]) && chestedFamily(m.etype) && !m.chested {
		h.equipChest(players, m)
		if isSurvival(t.gamemode) {
			h.consumeHeld(t)
		}
		return true
	}
	return false
}

// horseAngrySound is getAngrySound, species by species.
func horseAngrySound(m *mob) string {
	switch m.etype {
	case entityHorse:
		if m.baby {
			return "minecraft:entity.baby_horse.angry"
		}
		return "minecraft:entity.horse.angry"
	case entityDonkey, entityMule, entityZombieHorse, entityLlama, entityTraderLlama:
		return "minecraft:entity." + entityNameByID[m.etype] + ".angry"
	}
	return ""
}

// horseMakeMad is AbstractHorse.makeMad: unless it is already up, the horse
// rears (horsestand.go) and gives its angry call.
func (h *hub) horseMakeMad(players map[int32]*tracked, m *mob) {
	if m.standLeft > 0 {
		return
	}
	h.horseStand(players, m)
	if snd := horseAngrySound(m); snd != "" {
		h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
	}
}

// equipChest straps a chest onto a donkey, mule or llama.
func (h *hub) equipChest(players map[int32]*tracked, m *mob) {
	if m.strength == 0 {
		m.strength = int8(1 + h.rng.Intn(3)) // llama columns; harmless for donkeys
		if h.rng.Intn(20) == 0 {
			m.strength = int8(1 + h.rng.Intn(5)) // the rare strong llama
		}
	}
	m.chested = true
	m.chest = make([]invStack, horseColumns(m)*3)
	h.playSoundDim(players, m.dim, "minecraft:entity.donkey.chest", sndNeutral, m.x, m.y, m.z, 1, 1)
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
	if m.armorSt.item != 0 && !bodyArmorFor(m.etype, m.armorSt.item) {
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
		if it := h.spawnItemIn(players, m.dim, st.item, st.count, m.x, m.y+0.5, m.z); it != nil {
			it.setFrom(st)
			h.refreshItemMeta(players, it) // the spawn broadcast went out bare; show the real stack
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

// nautilusArmorItems is what a nautilus's body slot takes (its five armours).
var nautilusArmorItems = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"iron_nautilus_armor", "golden_nautilus_armor", "diamond_nautilus_armor", "netherite_nautilus_armor", "copper_nautilus_armor"} {
		if id := int32(itemByName[n]); id != 0 {
			m[id] = true
		}
	}
	return m
}()

// bodyArmorFor reports whether a mount's body slot takes the item: horse
// armour or a carpet on the horse family, nautilus armour on a nautilus.
func bodyArmorFor(etype int, item int32) bool {
	if etype == entityNautilus {
		return nautilusArmorItems[item]
	}
	return horseArmorItems[item] || carpetItems[item]
}

// evOpenMountInv is the player command OPEN_INVENTORY: pressed while
// riding, it opens the vehicle's own screen (handlePlayerCommand →
// HasCustomInventoryScreen.openCustomInventoryScreen).
type evOpenMountInv struct{ eid int32 }

func (evOpenMountInv) isHubEvent() {}

// openMountInventory: a tamed horse-family mount or nautilus opens its
// mount window (AbstractHorse.openCustomInventoryScreen: tamed, and the
// opener is aboard); a chest boat or chest minecart opens its cargo.
func (h *hub) openMountInventory(players map[int32]*tracked, t *tracked) {
	if t.ridingEID == 0 || t.inv == nil {
		return
	}
	if v := h.vehicles[t.ridingEID]; v != nil {
		if v.chest != nil {
			h.openVehicleChest(players, t, v)
		}
		return
	}
	m := h.mobs[t.ridingEID]
	if m == nil || m.dying > 0 || m.baby {
		return
	}
	switch {
	case horseFamily(m.etype) && (m.tamed || isCamelKind(m.etype)), m.etype == entityNautilus && m.tamed:
		h.openHorseScreen(players, t, m)
	}
}
