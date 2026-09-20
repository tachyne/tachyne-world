package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The decorated pot: a container holding exactly ONE stack
// (ContainerSingleItem). It is not a chest with a smaller window — vanilla
// gives it no menu at all. A right click with something puts ONE of it in,
// a right click with anything else (or an empty hand) only rocks the pot;
// what is inside comes back when the pot is broken, and not before.

var decoratedPotMin, decoratedPotMax = worldgen.BlockRange("decorated_pot")

// Wobble styles (DecoratedPotBlockEntity.WobbleStyle, block event action 1):
// the pot leans in when it takes something, back when it refuses.
const (
	potWobblePositive = 0
	potWobbleNegative = 1
)

// isDecoratedPot reports whether a state is a decorated pot.
func isDecoratedPot(s uint32) bool { return s >= decoratedPotMin && s <= decoratedPotMax }

// usePot is DecoratedPotBlock.useItemOn: one item goes in if the pot is
// empty or already holds the same thing with room left; anything else is a
// refusal. Reports whether the pot handled the interaction.
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
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5

	fits := stored.item == 0 || (potStacks(stored, held) && stored.count < stackCap(stored.item))
	if held.item == 0 || held.count == 0 || !fits {
		// useWithoutItem: the pot rocks back and refuses. Nothing comes out —
		// the only way to empty a pot is to break it.
		h.playSound(players, "minecraft:block.decorated_pot.insert_fail", sndBlock, cx, cy, cz, 1, 1)
		h.potWobble(players, key, potWobbleNegative)
		return true
	}
	one := held
	one.count = 1
	if stored.item == 0 {
		stored = one
	} else {
		stored.count++
	}
	h.pots[key] = stored
	slot := t.p.heldSlot()
	if t.gamemode != gmCreative {
		if t.inv.slots[slot].count--; t.inv.slots[slot].count <= 0 {
			t.inv.slots[slot] = invStack{}
		}
		h.sendSlot(t, slot)
	}
	h.incStat(t, attachproto.StatUsed, held.item, 1)
	// The insert sound rises with how full the pot is, so you can hear a pot
	// filling up without opening anything.
	fill := float32(stored.count) / float32(stackCap(stored.item))
	h.playSound(players, "minecraft:block.decorated_pot.insert", sndBlock, cx, cy, cz, 1, 0.7+0.5*fill)
	// DecoratedPotBlock.useItemOn: seven dust motes puff off the rim. Sent in
	// the pot's own dimension, not the overworld.
	h.toNearbyEv(players, key.dim, cx, cz, attachproto.Particles{
		PID: particleDustPlume, X: cx, Y: float64(key.y) + 1.2, Z: cz, Count: potPlumeCount})
	h.potWobble(players, key, potWobblePositive)
	return true
}

const (
	particleDustPlume = 104 // canonical-770 minecraft:dust_plume
	potPlumeCount     = 7   // sendParticles(..., 7, 0, 0, 0, 0)
)

// potWobble sends the pot's block event; every client animates the lean.
func (h *hub) potWobble(players map[int32]*tracked, pos simPos, style uint8) {
	id, ok := worldgen.BlockRegistryID("decorated_pot")
	if !ok {
		return
	}
	h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 1, Param: style, Block: int32(id)})
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
		if it := h.spawnItemIn(players, dim, st.item, st.count,
			float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5); it != nil {
			it.dmg, it.ench, it.name = st.dmg, st.ench, st.name
			h.refreshItemMeta(players, it)
		}
	}
}

// potStacks is ItemStack.isSameItemSameComponents for what a pot cares
// about: the same item, same damage, same enchantments, same name, same
// extras — a pot will not mix two different swords into one pile.
func potStacks(a, b invStack) bool {
	return a.item == b.item && a.dmg == b.dmg && a.ench == b.ench &&
		a.name == b.name && a.sameExtras(b) && a.potion == b.potion
}

// potInsert is the hopper's side of the pot (ContainerSingleItem accepts one
// item at a time from above). Reports whether the item went in.
func (h *hub) potInsert(pos simPos, st invStack) bool {
	if h.pots == nil {
		h.pots = map[simPos]invStack{}
	}
	stored := h.pots[pos]
	switch {
	case stored.item == 0:
		one := st
		one.count = 1
		h.pots[pos] = one
	case potStacks(stored, st) && stored.count < stackCap(stored.item):
		stored.count++
		h.pots[pos] = stored
	default:
		return false
	}
	return true
}

// potExtract is the hopper underneath: one item out of the pot at a time.
func (h *hub) potExtract(pos simPos) (invStack, bool) {
	stored, ok := h.pots[pos]
	if !ok || stored.item == 0 || stored.count <= 0 {
		return invStack{}, false
	}
	one := stored
	one.count = 1
	if stored.count--; stored.count <= 0 {
		delete(h.pots, pos)
	} else {
		h.pots[pos] = stored
	}
	return one, true
}

// evUsePot asks the hub to run a right-click on a decorated pot.
type evUsePot struct {
	eid     int32
	x, y, z int
}

func (evUsePot) isHubEvent() {}
