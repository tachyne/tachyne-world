package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bubble columns (BubbleColumnBlock, SoulSandBlock, MagmaBlock). Source
// water resting on soul sand becomes an updraft column, on a magma block a
// whirlpool, and the column climbs through every source-water cell above it.
// Take the source block away and the whole column falls back to plain water.
// The lift and drag on a player are the client's own physics once the block
// is there; the engine keeps the block right.

// bubbleSourceDelay is the 20-tick tick soul sand and magma schedule when
// water arrives above them (or when they are placed).
const bubbleSourceDelay = 20

// bubbleCanExistIn is canExistIn: a column cell, or a full water source.
func bubbleCanExistIn(s uint32) bool {
	return worldgen.IsBubbleColumn(s) || s == worldgen.WaterBase
}

// bubbleColumnFor is getColumnState: what the cell above `below` should be.
func bubbleColumnFor(below uint32) uint32 {
	switch {
	case worldgen.IsBubbleColumn(below):
		return below
	case below == worldgen.SoulSand:
		return worldgen.BubbleColumnUp
	case below == magmaBlockState:
		return worldgen.BubbleColumnDrag
	}
	return worldgen.WaterBase
}

// isBubbleSource reports the two blocks that drive a column.
func isBubbleSource(s uint32) bool { return s == worldgen.SoulSand || s == magmaBlockState }

// updateBubbleColumn is updateColumn: rewrites the cell at pos from what is
// under it and walks the column upward through every cell that can hold
// one. Returns whether the cell at pos is a column afterwards.
func (h *hub) updateBubbleColumn(players map[int32]*tracked, dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	if !bubbleCanExistIn(w.Block(pos.x, pos.y, pos.z)) {
		return false
	}
	col := bubbleColumnFor(w.Block(pos.x, pos.y-1, pos.z))
	for p := pos; h.inWorldYIn(dim, p.y) && bubbleCanExistIn(w.Block(p.x, p.y, p.z)); p.y++ {
		if w.Block(p.x, p.y, p.z) != col {
			h.setBlockAt(players, dim, p, col)
		}
	}
	return worldgen.IsBubbleColumn(col)
}

// tickBubbleSource handles an update on soul sand or magma: a neighbour
// change with water above schedules the 20-tick tick; the tick itself
// raises the column. Returns whether the block was a bubble source.
func (h *hub) tickBubbleSource(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	if !isBubbleSource(state) {
		return false
	}
	above := blockPos{pos.x, pos.y + 1, pos.z}
	if !h.inWorldYIn(dim, above.y) || !bubbleCanExistIn(h.worldFor(dim).Block(above.x, above.y, above.z)) {
		return true
	}
	key := simPos{dim: dim, blockPos: pos}
	if due, ok := h.bubbleDue[key]; ok && h.tick.Load() >= due {
		delete(h.bubbleDue, key)
		h.updateBubbleColumn(players, dim, above)
		return true
	} else if ok {
		return true // already scheduled
	}
	if h.bubbleDue == nil {
		h.bubbleDue = map[simPos]uint64{}
	}
	h.bubbleDue[key] = h.tick.Load() + bubbleSourceDelay
	h.scheduleIn(dim, pos, bubbleSourceDelay)
	return true
}
