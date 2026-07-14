package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Crafter (1.21 auto-crafter): a 3×3 block that crafts its grid's recipe on a
// rising redstone edge and ejects the result out its front. Reimplemented from
// CrafterBlock. Storage reuses the 9-slot `bin` machinery, so hopper-fill,
// persistence, and the generic-3×3 loading window all come for free; the block
// adds the redstone-triggered craft + eject on top.
//
// MVP scope: no per-slot "disabled" toggle yet (that only affects hopper
// auto-fill of shaped holes; a manually loaded 3×3 or a full-grid recipe crafts
// correctly), and no live result-preview menu — those are follow-ups.

var crafterMin, crafterMax = worldgen.BlockRange("crafter") // crafting(2)×orientation(12)×triggered(2)

func isCrafter(s uint32) bool { return s >= crafterMin && s <= crafterMax }

// State layout (alphabetical props, booleans list true first):
// off = crafting_idx*24 + orientation_idx*2 + triggered_idx.
func crafterTriggered(s uint32) bool { return (s-crafterMin)%2 == 0 }
func crafterWithTriggered(s uint32, t bool) uint32 {
	b := s - (s-crafterMin)%2
	if t {
		return b
	}
	return b + 1
}
func crafterWithCrafting(s uint32, c bool) uint32 {
	off := s - crafterMin
	rest := off % 24
	if c {
		return crafterMin + rest // crafting index 0 = true
	}
	return crafterMin + 24 + rest
}

// crafterFront maps the orientation to the ejection direction (the first token
// of the orientation name — down_east → down, north_up → north).
func crafterFront(s uint32) (int, int, int) {
	switch (s - crafterMin) / 2 % 12 {
	case 0, 1, 2, 3: // down_*
		return 0, -1, 0
	case 4, 5, 6, 7: // up_*
		return 0, 1, 0
	case 8: // west_up
		return -1, 0, 0
	case 9: // east_up
		return 1, 0, 0
	case 10: // north_up
		return 0, 0, -1
	}
	return 0, 0, 1 // south_up
}

// updateCrafter tracks the redstone edge; a rising edge crafts once.
func (h *hub) updateCrafter(players map[int32]*tracked, pos blockPos, state uint32) {
	// A crafting=true state is the animation flag — settle it back to false.
	if (state-crafterMin)/24 == 0 {
		state = crafterWithCrafting(state, false)
		h.setBlock(players, pos, state)
	}
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if powered == crafterTriggered(state) {
		return
	}
	state = crafterWithTriggered(state, powered)
	if powered {
		state = crafterWithCrafting(state, true) // brief craft animation
		h.setBlock(players, pos, state)
		h.crafterCraft(players, pos, state)
		h.schedule(pos, 4) // clear the animation shortly after
		return
	}
	h.setBlock(players, pos, state)
}

// crafterCraft matches the 3×3 grid and, on a hit, consumes one of each
// ingredient and ejects the result.
func (h *hub) crafterCraft(players map[int32]*tracked, pos blockPos, state uint32) {
	c := h.bins[pos]
	if c == nil || len(c.slots) < 9 {
		h.craftFail(players, pos)
		return
	}
	grid := make([]invStack, 9)
	copy(grid, c.slots[:9])
	item, count := matchRecipe(grid, 3)
	if item == 0 || count == 0 {
		h.craftFail(players, pos)
		return
	}
	for i := 0; i < 9; i++ {
		if c.slots[i].item != 0 && c.slots[i].count > 0 {
			if c.slots[i].count--; c.slots[i].count <= 0 {
				c.slots[i] = invStack{}
			}
		}
	}
	h.ejectCrafted(players, pos, state, item, count)
	h.refreshBinViewers(players, pos)
	h.playSound(players, "minecraft:block.crafter.craft", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

func (h *hub) craftFail(players map[int32]*tracked, pos blockPos) {
	h.playSound(players, "minecraft:block.crafter.fail", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

// ejectCrafted pushes the result into the container the crafter faces, or drops
// it into the world if none accepts it (CrafterBlockEntity output behaviour).
func (h *hub) ejectCrafted(players map[int32]*tracked, pos blockPos, state uint32, item int32, count int) {
	dx, dy, dz := crafterFront(state)
	target := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	if dst := h.containerSlots(target); dst != nil {
		if left := binInsert(dst, invStack{item: item, count: count}); left == 0 {
			h.refreshBinViewers(players, target)
			return
		} else if left < count {
			h.refreshBinViewers(players, target)
			count = left
		}
	}
	fx := float64(pos.x) + 0.5 + float64(dx)*0.7
	fy := float64(pos.y) + 0.5 + float64(dy)*0.7
	fz := float64(pos.z) + 0.5 + float64(dz)*0.7
	h.spawnItem(players, item, count, fx, fy, fz)
}
