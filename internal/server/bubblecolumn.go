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

// bubbleSourceDelay is the 20-tick tick a water source schedules when it
// finds soul sand or magma under it (LiquidBlock.tryScheduleBubbleBlockColumn).
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

// bubbleScheduleTick is LiquidBlock.tryScheduleBubbleBlockColumn, run from
// the water block's onPlace, neighborChanged and updateShape (its floor
// changed): a full water source over soul sand or magma asks for its own
// block tick 20 ticks out, which raises (or drops) the column. A tick
// already pending is kept.
func (h *hub) bubbleScheduleTick(dim int, pos blockPos, state uint32) {
	if state != worldgen.WaterBase || !h.inWorldYIn(dim, pos.y-1) {
		return
	}
	if isBubbleSource(h.worldFor(dim).Block(pos.x, pos.y-1, pos.z)) {
		h.scheduleBlockTickIn(dim, pos, bubbleSourceDelay)
	}
}

// tickBubbleWater is LiquidBlock.tick, the water block's scheduled tick:
// a full source the column can occupy becomes a column from what is under
// it, and the column climbs.
func (h *hub) tickBubbleWater(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	if state == worldgen.WaterBase {
		h.updateBubbleColumn(players, dim, pos)
	}
}
