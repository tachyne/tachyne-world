package server

import "github.com/tachyne/tachyne-world/internal/world"

// The block-simulation family — redstone, comparators, fire, rails, plates,
// tripwires, dispensers — evaluates in ONE dimension at a time: rsDim, set by
// the entry point that knows which world the block lives in (the scheduled
// update, the player's click, the cart's rail, the dispenser's cell). Every
// read and write inside the family goes through these helpers, so the same
// code runs in the Nether and the End; a plain h.world/h.setBlock in that
// code would read and write the OVERWORLD at the same coordinates, which is
// what happened before 2026-09-19 (a scheduled update at Nether coordinates
// rewrote overworld blocks). Hub goroutine only, like spawnCause.

// rsWorld is the world the current block simulation runs in.
func (h *hub) rsWorld() *world.World { return h.worldFor(h.rsDim) }

// rsSet writes a block in the current simulation dimension.
func (h *hub) rsSet(players map[int32]*tracked, pos blockPos, state uint32) {
	h.setBlockAt(players, h.rsDim, pos, state)
}

// rsSchedule schedules a block update in the current simulation dimension.
func (h *hub) rsSchedule(pos blockPos, delay uint64) { h.scheduleIn(h.rsDim, pos, delay) }

// rsSound plays a sound in the current simulation dimension.
func (h *hub) rsSound(players map[int32]*tracked, name string, category int32, x, y, z float64, volume, pitch float32) {
	h.playSoundDim(players, h.rsDim, name, category, x, y, z, volume, pitch)
}

// rsKey is the current simulation dimension's key for the block-keyed state
// the family carries beside the world itself — a repeater's pending flip, an
// observer's pulse, a pressed plate, a fire's age. Those maps were keyed by
// position alone, so the same coordinates in two dimensions shared one entry:
// a nether fire aged an overworld fire, and a repeater in the End cancelled
// one at home.
func (h *hub) rsKey(pos blockPos) simPos { return simPos{dim: h.rsDim, blockPos: pos} }

// inDim runs fn with the block simulation pointed at dim.
func (h *hub) inDim(dim int, fn func()) {
	old := h.rsDim
	h.rsDim = dim
	fn()
	h.rsDim = old
}
