package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Shulker boxes: a 27-slot chest whose contents SURVIVE being broken. That one
// property is the whole point of the block — it is how you move a base — and it
// is the only part that needs machinery a chest does not already have.
//
// Placed, a box stores in h.chests like any other container, so it inherits the
// existing window, hopper interaction and persistence. Broken, its contents move
// onto the dropped item under a boxID, exactly the indirection maps and books
// already use (invStack has to stay comparable, so it holds an id, not a slice).

var (
	shulkerBoxMin = worldgen.BlockBase("shulker_box")
	// The 16 dyed boxes are contiguous after the plain one, 6 facings each.
	shulkerBoxMax = worldgen.BlockBase("black_shulker_box") + 5
)

// isShulkerBox reports whether a state is any colour of shulker box.
func isShulkerBox(s uint32) bool { return s >= shulkerBoxMin && s <= shulkerBoxMax }

// isShulkerBoxItem reports whether an item id is a shulker box.
func isShulkerBoxItem(item int32) bool {
	_, ok := shulkerBoxItems[item]
	return ok
}

// shulkerBoxItems is every shulker-box item id, built from the block names so a
// new colour cannot be missed.
var shulkerBoxItems = func() map[int32]bool {
	names := []string{"shulker_box", "white_shulker_box", "orange_shulker_box",
		"magenta_shulker_box", "light_blue_shulker_box", "yellow_shulker_box",
		"lime_shulker_box", "pink_shulker_box", "gray_shulker_box",
		"light_gray_shulker_box", "cyan_shulker_box", "purple_shulker_box",
		"blue_shulker_box", "brown_shulker_box", "green_shulker_box",
		"red_shulker_box", "black_shulker_box"}
	set := map[int32]bool{}
	for _, n := range names {
		if id, ok := itemByName[n]; ok {
			set[int32(id)] = true
		}
	}
	return set
}()

// boxContents is the storage a broken box's stack points at. Keyed by boxID,
// persisted alongside the containers.
func (h *hub) boxContents(id int32) *chest {
	if h.boxes == nil {
		h.boxes = map[int32]*chest{}
	}
	c := h.boxes[id]
	if c == nil {
		c = &chest{}
		h.boxes[id] = c
	}
	return c
}

// stowShulkerBox moves a placed box's contents onto the item that is about to
// drop, and returns the boxID to stamp on it. Returns 0 for an empty box, which
// then drops as a plain item with nothing to remember.
func (h *hub) stowShulkerBox(pos blockPos) int32 {
	c := h.chests[pos]
	if c == nil {
		return 0
	}
	empty := true
	for _, st := range c.slots {
		if st.item != 0 && st.count > 0 {
			empty = false
			break
		}
	}
	delete(h.chests, pos) // the block is going; its storage goes with it
	if empty {
		return 0
	}
	h.nextBoxID++
	id := h.nextBoxID
	*h.boxContents(id) = *c
	return id
}

// restoreShulkerBox is the other half: a placed box takes back the contents its
// item was carrying, and the id is retired.
func (h *hub) restoreShulkerBox(pos blockPos, boxID int32) {
	if boxID == 0 {
		return
	}
	if c := h.boxes[boxID]; c != nil {
		stored := *c
		h.chests[pos] = &stored
		delete(h.boxes, boxID)
	}
}

// dropShulkerBox replaces the ordinary loot drop for a broken box: one item of
// the matching colour, carrying whatever was inside it.
func (h *hub) dropShulkerBox(players map[int32]*tracked, state uint32, pos blockPos) {
	// The baked loot table already knows which colour of box this state drops.
	ds := h.evalBlockLoot(lootCtx{state: state, rng: h.rng.Intn, randf: h.rng.Float64})
	if len(ds) == 0 {
		return
	}
	item := ds[0].item
	boxID := h.stowShulkerBox(pos)
	it := h.spawnItem(players, item, 1, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
	if it != nil {
		it.boxID = boxID
	}
}
