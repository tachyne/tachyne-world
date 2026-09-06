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

// openChest opens the chest window at a block position for a player. A chest
// that is half of a pair opens the combined Large Chest instead.
func (h *hub) openChest(t *tracked, x, y, z int) {
	state := h.worldFor(t.dim).At(x, y, z)
	if left, right, paired := h.chestPairPositions(t.dim, x, y, z, state); paired {
		h.openDoubleChest(t, left, right)
		return
	}
	// Self-heal chests placed before pairing existed: two adjacent singles on a
	// connect side become a pair on first open. Contents are untouched.
	if left, right, ok := h.formChestPair(t.dim, x, y, z, state); ok {
		h.openDoubleChest(t, left, right)
		return
	}
	defer t.p.trySendEv(soundEv("minecraft:block.chest.open", sndBlock, float64(x)+0.5, float64(y), float64(z)+0.5, 0.5, 1))
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	pos := simPos{dim: t.dim, blockPos: blockPos{x, y, z}}
	c := h.chests[pos]
	if c == nil {
		c = &chest{}
		if pos.dim == dimOverworld {
			// Structure loot is a pure function of the OVERWORLD generator, so a
			// Nether chest at the same coordinates must not inherit a dungeon's.
			h.fillStructureChest(pos.blockPos, c)
		}
		h.chests[pos] = c
	}
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind, t.viewChest = h.nextWin, pos, winChest, c

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
func (h *hub) spillContainer(players map[int32]*tracked, dim, x, y, z int, newState uint32) {
	h.spillJukebox(players, dim, x, y, z, newState)
	h.spillCampfire(players, dim, x, y, z, newState)
	h.spillLectern(players, dim, x, y, z, newState)
	h.spillShelf(players, dim, x, y, z, newState)
	pos := simPos{dim: dim, blockPos: blockPos{x, y, z}}
	spill := func(slots []invStack) {
		for _, st := range slots {
			if st.item != 0 && st.count > 0 {
				if it := h.spawnItemIn(players, dim, st.item, st.count, float64(x)+0.5, float64(y), float64(z)+0.5); it != nil {
					it.dmg = st.dmg
					it.ench = st.ench
					it.mapID = st.mapID
					it.pats = st.pats
					it.trimMat, it.trimPat = st.trimMat, st.trimPat
					it.bookID = st.bookID
					it.boxID, it.hiveID = st.boxID, st.hiveID
					it.bundleID, it.potion, it.repairCost, it.instrument, it.name, it.lode = st.bundleID, st.potion, st.repairCost, st.instrument, st.name, st.lode
					h.refreshItemMeta(players, it) // the spawn broadcast went out bare; show the real stack
				}
			}
		}
	}
	if !isChestBlock(newState) {
		// A removed chest half releases its partner back to a single chest.
		h.unpairChestNeighbors(players, dim, x, y, z)
		if c := h.chests[pos]; c != nil {
			spill(c.slots[:])
			delete(h.chests, pos)
		}
	}
	if f := h.furnaces[pos]; f != nil {
		if _, still := furnaceKindOf(newState); !still {
			spill(f.slots[:])
			delete(h.furnaces, pos)
		}
	}
	if b := h.bins[pos]; b != nil && !isDispenser(newState) && !isDropper(newState) &&
		!isHopper(newState) && !isBrewStand(newState) && !isCrafter(newState) {
		spill(b.slots)
		delete(h.bins, pos)
	}
}

// containerOpen names what right-clicking a storage block should open. Having
// ONE function decide keeps the storage side and the interaction side from
// drifting apart — the shulker box shipped with working storage and no way to
// open it because those two lived in different files.
type containerOpen int

const (
	openNothing     containerOpen = iota
	openChestWindow               // the 27-slot chest window over block storage
	openEnderWindow               // the same window over the PLAYER's own storage
	openPotSlot                   // a decorated pot's single stack (no menu)
	openVaultSlot                 // a vault: hand over a trial key, take the reward
	openHiveHarvest               // a beehive: shears or a bottle, once it is full
)

// containerOpenFor maps a block state to the container it opens.
func containerOpenFor(state uint32) containerOpen {
	switch {
	case isChestBlock(state), isShulkerBox(state):
		return openChestWindow
	case isEnderChest(state):
		return openEnderWindow
	case isDecoratedPot(state):
		return openPotSlot
	case isVault(state):
		return openVaultSlot
	case isBeeHome(state):
		return openHiveHarvest
	}
	return openNothing
}
