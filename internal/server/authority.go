package server

import (
	"math"
	"sync/atomic"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The last of the client-trust gaps (AUTHORITY, the standing rule):
//
//   - Suffocation: a head buried in a solid block hurts — and doubles as the
//     backstop that makes phasing INTO blocks pointless.
//   - Noclip: a move whose destination puts the hitbox inside solid blocks is
//     rejected outright (escaping FROM inside one is always allowed — sand
//     falling on a head must never wedge a player permanently).
//   - Mining time: a survival dig Finish arriving faster than the block's
//     hardness allows (with generous tool + latency slack) is reverted.
//   - Chunk streaming follows the HUB-validated position, so a rejected
//     teleport no longer downloads the map around the pretended location.

const (
	suffocateDamagePerSec = 2   // vanilla: 1 HP / 10 ticks
	digTolerance          = 0.5 // accept Finish at ≥50% of the computed time (latency+jitter)
)

// fullCube reports whether a state is a full solid cube (the suffocation /
// noclip test). Opaque-to-sky tracks vanilla's full-cube set closely: slabs,
// stairs, fences, glass and plants are all non-opaque.
func fullCube(state uint32) bool {
	return state != worldgen.Air && worldgen.SkyOpacity(state) == worldgen.Opaque
}

// insideSolid reports whether a player position has its feet or head cell
// inside a full cube — in the player's OWN dimension. (This read against the
// overworld froze all nether movement and suffocated nether players against
// phantom overworld terrain.)
func (h *hub) insideSolid(dim int, x, y, z float64) bool {
	fx, fz := int(math.Floor(x)), int(math.Floor(z))
	w := h.worldFor(dim)
	return fullCube(w.At(fx, int(math.Floor(y+0.1)), fz)) ||
		fullCube(w.At(fx, int(math.Floor(y+1.6)), fz))
}

// suffocate applies burial damage (1 Hz, from the survival tick).
func (h *hub) suffocate(players map[int32]*tracked, t *tracked) {
	fx, fz := int(math.Floor(t.x)), int(math.Floor(t.z))
	if fullCube(h.worldFor(t.dim).At(fx, int(math.Floor(t.y+1.5)), fz)) {
		h.damageExh(players, t, suffocateDamagePerSec, 0) // in_wall: no exhaustion
	}
}

// toolSpeed is a mining tool's speed multiplier (vanilla tier speeds; the
// class match is assumed — generous, since this only gates IMPOSSIBLY fast).
var toolSpeed = itemFloatMap(map[string]float64{
	"wooden_sword": 2, "wooden_shovel": 2, "wooden_pickaxe": 2, "wooden_axe": 2,
	"stone_sword": 4, "stone_shovel": 4, "stone_pickaxe": 4, "stone_axe": 4,
	"copper_sword": 5, "copper_shovel": 5, "copper_pickaxe": 5, "copper_axe": 5,
	"iron_sword": 6, "iron_shovel": 6, "iron_pickaxe": 6, "iron_axe": 6,
	"golden_sword": 12, "golden_shovel": 12, "golden_pickaxe": 12, "golden_axe": 12,
	"diamond_sword": 8, "diamond_shovel": 8, "diamond_pickaxe": 8, "diamond_axe": 8,
	"netherite_sword": 9, "netherite_shovel": 9, "netherite_pickaxe": 9, "netherite_axe": 9,
})

// minDigTicks is the fastest legitimate break time for a block with a held
// item, scaled by digTolerance. Vanilla: ticks ≈ 30×hardness/speed with the
// right tool (100× without harvest rights — we use the lenient 30 always).
func minDigTicks(state uint32, held int32) int {
	h := float64(worldgen.Hardness(state))
	if h <= 0 {
		return 0
	}
	speed := 1.0
	if s, ok := toolSpeed[held]; ok {
		speed = s
	}
	speed *= 1 + 1.5 // headroom for Efficiency & haste (not modeled server-side)
	return int(30 * h / speed * digTolerance)
}

// --- hub-validated position mirror (chunk-stream gate) ----------------------

func (p *player) setHubPos(x, z float64) {
	p.hubX.Store(math.Float64bits(x))
	p.hubZ.Store(math.Float64bits(z))
}

func (p *player) hubPos() (float64, float64) {
	return math.Float64frombits(p.hubX.Load()), math.Float64frombits(p.hubZ.Load())
}

// streamAllowed reports whether the connection's claimed position is close
// enough to the hub-validated one to stream chunks for it. A client being
// rubber-banded pretends to be far away — no world download for pretenders.
func (p *player) streamAllowed() bool {
	hx, hz := p.hubPos()
	if hx == 0 && hz == 0 {
		return true // pre-join / first event: nothing validated yet
	}
	return math.Abs(p.x-hx) < 48 && math.Abs(p.z-hz) < 48
}

var _ = atomic.Uint64{} // (documenting the intended field type on player)
