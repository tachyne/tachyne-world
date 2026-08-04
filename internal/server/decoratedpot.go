package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The decorated pot: a container holding exactly ONE stack
// (ContainerSingleItem). Right-click with something to put it in, empty-handed
// to take it back — there is no menu, which is why it needs a little code of
// its own rather than reusing the chest window.

var decoratedPotMin, decoratedPotMax = worldgen.BlockRange("decorated_pot")

// isDecoratedPot reports whether a state is a decorated pot.
func isDecoratedPot(s uint32) bool { return s >= decoratedPotMin && s <= decoratedPotMax }

// usePot is the right-click: insert what is held, or take what is stored.
// Reports whether the pot handled the interaction.
func (h *hub) usePot(players map[int32]*tracked, t *tracked, pos blockPos) bool {
	if t.inv == nil {
		return false
	}
	if h.pots == nil {
		h.pots = map[simPos]invStack{}
	}
	key := simPos{dim: t.dim, blockPos: pos}
	held := heldStack(t)
	stored := h.pots[key]

	switch {
	case held.item != 0 && held.count > 0 && stored.item == 0:
		// Vanilla takes the whole held stack; the pot holds it as one.
		h.pots[key] = held
		slot := t.p.heldSlot()
		t.inv.slots[slot] = invStack{}
		h.sendSlot(t, slot)
		h.playSound(players, "minecraft:block.decorated_pot.insert", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	case stored.item != 0:
		// Empty-handed, or holding something else: the contents come out.
		delete(h.pots, key)
		changed, left := t.inv.addStack(stored)
		for _, sl := range changed {
			h.sendSlot(t, sl)
		}
		if left > 0 {
			h.spawnItem(players, stored.item, left, t.x, t.y, t.z)
		}
		h.playSound(players, "minecraft:block.decorated_pot.insert_fail", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	default:
		return false // nothing held and nothing inside
	}
	return true
}

// spillPot scatters a pot's contents when the block goes.
func (h *hub) spillPot(players map[int32]*tracked, dim int, pos blockPos, newState uint32) {
	if isDecoratedPot(newState) {
		return
	}
	key := simPos{dim: dim, blockPos: pos}
	st, ok := h.pots[key]
	if !ok {
		return
	}
	delete(h.pots, key)
	if st.item != 0 && st.count > 0 {
		h.spawnItem(players, st.item, st.count,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
	}
}

// evUsePot asks the hub to run a right-click on a decorated pot.
type evUsePot struct {
	eid     int32
	x, y, z int
}

func (evUsePot) isHubEvent() {}
