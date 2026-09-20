package server

import (
	"log"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// repairMultiface is a boot sweep over the world's edits, for the multiface
// blocks placed while the engine built them from the wrong default state.
//
// worldgen.BlockBase(name) is property index 0, and index 0 of a BOOLEAN is
// true — so the "default" glow lichen, vine, sculk vein and resin clump had
// every one of their six faces already switched on, and placing one turned on
// a seventh. The result was a full cube of lichen whose unsupported sides hung
// in the air, which is what Legion reported. Placement was fixed on
// 2026-09-20, but nothing rewrites blocks that are already in the world.
//
// The repair is deliberately the narrow one: drop the faces that nothing
// holds, and remove the block if that leaves none. That is exactly
// MultifaceBlock.updateShape — the update vanilla would have run on the next
// neighbour change — so it corrects the pieces standing in mid-air without
// guessing which face was meant on a block whose faces all happen to have
// something behind them. It is idempotent: a second run finds nothing.
//
// Naturally generated lichen and vines were never affected — worldgen builds
// them from the vanilla DEFAULT state (all faces off), not from BlockBase.
func (h *hub) repairMultiface() {
	fixed, removed := 0, 0
	for _, w := range []*world.World{h.world, h.nether, h.end} {
		if w == nil {
			continue
		}
		for _, c := range w.EditedChunks() {
			for _, e := range w.EditedBlocks(c[0], c[1]) {
				if !isMultiface(e.State) {
					continue
				}
				x, z := int(c[0])*16+e.LX, int(c[1])*16+e.LZ
				ns, any := multifaceUpdated(w, blockPos{x, e.Y, z}, e.State)
				switch {
				case !any:
					// Nothing holds it at all — the same end the live support
					// sweep gives it, minus the drop: nobody is here to catch it.
					w.SetBlock(x, e.Y, z, worldgen.Air)
					removed++
				case ns != e.State:
					w.SetBlock(x, e.Y, z, ns)
					fixed++
				}
			}
		}
	}
	if fixed > 0 || removed > 0 {
		log.Printf("multiface repair: trimmed %d block(s) to the faces something holds, removed %d with none left",
			fixed, removed)
	}
}
