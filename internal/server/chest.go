package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Chests: right-click opens a generic_9x3 window over 27 slots of hub-owned
// storage keyed by block position. Clicks land through the same trust-apply
// machinery as every other window (winSlotPtr); contents persist via
// containerStore and spill as item drops when the chest is broken.
//
// v1 simplifications: no open/close lid animation or sound, no double chests
// (each block is its own 27 slots), and two players editing the SAME chest at
// once can desync each other's view until reopened (rare on this server).

const (
	menuGeneric9x3 = 2 // minecraft:generic_9x3 menu id (same through 26.2)
)

var (
	chestStateMin = worldgen.BlockBase("chest") // minecraft:chest block states: facing(4) x type(3) x waterlogged(2)
	chestStateMax = worldgen.BlockBase("chest") + 23
	// Copper chests (8 oxidation variants, contiguous) and trapped chests share
	// the wooden-chest 27-slot storage + window. The copper golem sorts items OUT
	// of copper chests into wooden/trapped ones (see coppergolem.go).
	copperChestMin  = worldgen.BlockBase("copper_chest")
	copperChestMax  = worldgen.BlockBase("waxed_oxidized_copper_chest") + 23
	trappedChestMin = worldgen.BlockBase("trapped_chest")
	trappedChestMax = worldgen.BlockBase("trapped_chest") + 23
)

// isChestBlock reports whether a state is any single-block chest container.
func isChestBlock(s uint32) bool {
	return (s >= chestStateMin && s <= chestStateMax) ||
		(s >= copperChestMin && s <= copperChestMax) ||
		(s >= trappedChestMin && s <= trappedChestMax)
}

func isCopperChest(s uint32) bool { return s >= copperChestMin && s <= copperChestMax }

type chest struct {
	slots [27]invStack
}

type evOpenChest struct {
	eid     int32
	x, y, z int
}

func (evOpenChest) isHubEvent() {}

// openChest opens the chest window at a block position for a player.
func (h *hub) openChest(t *tracked, x, y, z int) {
	defer t.p.trySendEv(soundEv("minecraft:block.chest.open", sndBlock, float64(x)+0.5, float64(y), float64(z)+0.5, 0.5, 1))
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	pos := blockPos{x, y, z}
	c := h.chests[pos]
	if c == nil {
		c = &chest{}
		h.dungeonLoot(pos, c) // a generated structure chest fills on first open
		h.desertTempleLoot(pos, c)
		h.ruinedPortalLoot(pos, c)
		h.pillagerOutpostLoot(pos, c)
		h.chests[pos] = c
	}
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind = h.nextWin, pos, winChest

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuGeneric9x3), Title: "Chest"})
	h.sendChestWindow(t, c)
}

// sendChestWindow refreshes the whole chest window: 27 chest slots + the
// player's main inventory + hotbar.
func (h *hub) sendChestWindow(t *tracked, c *chest) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 63)
	for i := 0; i < 27; i++ {
		slots = append(slots, stackEv(c.slots[i]))
	}
	for i := 9; i <= 35; i++ { // main inventory: window 27-53
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ { // hotbar: window 54-62
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// spillContainer runs after a block change: if the position held furnace/chest
// storage and the block there is no longer that container, the contents
// scatter as item drops and the state is forgotten. Anyone still viewing it
// gets a resync on their next click (stale window id path).
func (h *hub) spillContainer(players map[int32]*tracked, x, y, z int, newState uint32) {
	h.spillJukebox(players, x, y, z, newState)
	h.spillCampfire(players, x, y, z, newState)
	pos := blockPos{x, y, z}
	spill := func(slots []invStack) {
		for _, st := range slots {
			if st.item != 0 && st.count > 0 {
				if it := h.spawnItem(players, st.item, st.count, float64(x)+0.5, float64(y), float64(z)+0.5); it != nil {
					it.dmg = st.dmg
					it.ench = st.ench
				}
			}
		}
	}
	if c := h.chests[pos]; c != nil && !isChestBlock(newState) {
		spill(c.slots[:])
		delete(h.chests, pos)
	}
	if f := h.furnaces[pos]; f != nil {
		if _, still := furnaceKindOf(newState); !still {
			spill(f.slots[:])
			delete(h.furnaces, pos)
		}
	}
	if b := h.bins[pos]; b != nil && !isDispenser(newState) && !isDropper(newState) &&
		!isHopper(newState) && !isBrewStand(newState) {
		spill(b.slots)
		delete(h.bins, pos)
	}
}
