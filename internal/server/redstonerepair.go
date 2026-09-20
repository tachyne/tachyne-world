package server

import "log"

// rescheduleRedstone is a boot sweep that asks every powered dust and every
// piston base in the world's edits to work itself out again.
//
// It exists because a signal is not recomputed from scratch on load: dust
// carries its power in its own block state, so a line left powered by a bug
// stays powered across restarts, and a piston held out by that line stays out.
// Legion hit exactly that — a lever removed without the strong-power relay
// (fixed 2026-09-20) left a line at full strength with nothing behind it, and
// the pistons it drove never retracted.
//
// Nothing is decided here: each position is simply SCHEDULED, so the ordinary
// update path recomputes it the way it would after any neighbour change, and a
// line settles over the next few ticks. A world with nothing wrong pays one
// pass over its edits and a handful of updates that change nothing.
func (h *hub) rescheduleRedstone() {
	w := h.world // dust and pistons simulate in the overworld
	if w == nil {
		return
	}
	wires, pistons := 0, 0
	for _, c := range w.EditedChunks() {
		for _, e := range w.EditedBlocks(c[0], c[1]) {
			switch {
			case isWire(e.State) && wirePower(e.State) > 0:
				wires++
			case isPistonBase(e.State):
				pistons++
			default:
				continue
			}
			h.scheduleIn(dimOverworld, blockPos{int(c[0])*16 + e.LX, e.Y, int(c[1])*16 + e.LZ}, 1)
		}
	}
	if wires > 0 || pistons > 0 {
		log.Printf("redstone sweep: re-evaluating %d powered dust cell(s) and %d piston(s)", wires, pistons)
	}
}
