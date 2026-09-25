package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Frogspawn is placed ON water rather than against a block face, so the
// client's own raycast — which ignores fluids — finds nothing and sends a
// plain use. The server has to do the water-surface hit itself, the way
// PlaceOnWaterBlockItem does: ray to the first water SOURCE, place in the
// block above it.
//
// A lily pad works today only because its item is an ordinary BlockItem in
// this engine's tables; frogspawn genuinely needed the surface hit.

var (
	itemFrogspawn  = itemByName["frogspawn"]
	frogspawnBlock = worldgen.BlockBase("frogspawn")
)

const frogspawnReach = 5.0

type evPlaceOnWater struct{ eid int32 }

func (evPlaceOnWater) isHubEvent() {}

// placeFrogspawn walks the look ray to the first water source and lays the
// spawn on top of it.
func (h *hub) placeFrogspawn(players map[int32]*tracked, t *tracked) {
	if t.dead || t.inv == nil || heldStack(t).item != itemFrogspawn {
		return
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	ox, oy, oz := t.x, t.y+1.5, t.z
	w := h.worldFor(t.dim)
	last := blockPos{int(math.Floor(ox)), int(math.Floor(oy)), int(math.Floor(oz))}
	for d := 0.0; d <= frogspawnReach; d += 0.1 {
		p := blockPos{int(math.Floor(ox + dx*d)), int(math.Floor(oy + dy*d)), int(math.Floor(oz + dz*d))}
		if p == last && d > 0 {
			continue
		}
		last = p
		st := w.At(p.x, p.y, p.z)
		switch {
		case st == worldgen.WaterBase:
			if w.At(p.x, p.y+1, p.z) != worldgen.Air {
				return // something is already sitting on the surface
			}
			h.setBlockLive(players, t.dim, p.x, p.y+1, p.z, frogspawnBlock)
			h.scheduleFrogspawn(t.dim, blockPos{p.x, p.y + 1, p.z})
			h.playSoundDim(players, t.dim, "minecraft:block.frogspawn.place", sndBlock,
				float64(p.x)+0.5, float64(p.y)+1.5, float64(p.z)+0.5, 1, 1)
			if isSurvival(t.gamemode) {
				h.consumeHeld(t)
			}
			return
		case worldgen.Collides(st):
			return // a solid before any water
		}
	}
}

// Frogspawn hatching (FrogspawnBlock): laid or placed spawn sits on the water
// for three to ten minutes and then bursts into two to five tadpoles. It
// needs water under it and air above — drain the pool and the spawn is gone.
const (
	frogspawnMinHatch = 3600
	frogspawnMaxHatch = 12000
)

// scheduleFrogspawn arms a newly placed clutch (FrogspawnBlock.onPlace). The
// clutch keeps its own due tick: the simulation queue also reaches the cell
// for every neighbour change, and those must not hatch it early.
func (h *hub) scheduleFrogspawn(dim int, pos blockPos) {
	delay := uint64(frogspawnMinHatch + h.rng.Intn(frogspawnMaxHatch-frogspawnMinHatch))
	if h.frogspawnDue == nil {
		h.frogspawnDue = map[simPos]uint64{}
	}
	h.frogspawnDue[simPos{dim: dim, blockPos: pos}] = h.tick.Load() + delay
	h.scheduleIn(dim, pos, delay)
}

// frogspawnSurvives is FrogspawnBlock.canSurvive (mayPlaceOn): water below —
// a waterlogged block counts, #supports_frogspawn is empty — and no fluid
// above.
func frogspawnSurvives(below, above uint32) bool {
	return worldgen.HoldsWater(below) && !worldgen.IsFluid(above) && !worldgen.IsWaterlogged(above)
}

// tickFrogspawn is FrogspawnBlock: on a neighbour's change (updateShape) a
// clutch that can no longer survive is gone; on its own due tick (tick) it
// hatches, or — no longer supported — is destroyed. Reports whether the
// position was a clutch at all.
func (h *hub) tickFrogspawn(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	if state != frogspawnBlock {
		return false
	}
	w := h.worldFor(dim)
	survives := frogspawnSurvives(w.Block(pos.x, pos.y-1, pos.z), w.Block(pos.x, pos.y+1, pos.z))
	key := simPos{dim: dim, blockPos: pos}
	due, armed := h.frogspawnDue[key]
	if !armed || h.tick.Load() < due {
		if !survives { // updateShape: the water went, and so does the spawn
			delete(h.frogspawnDue, key)
			h.setBlockAt(players, dim, pos, worldgen.Air)
		}
		return true
	}
	delete(h.frogspawnDue, key)
	// Level.destroyBlock without drops: the break effect and BLOCK_DESTROY.
	h.toNearbyEv(players, dim, float64(pos.x)+0.5, float64(pos.z)+0.5, blockBreakEvent(pos.x, pos.y, pos.z, state))
	h.setBlockAt(players, dim, pos, worldgen.Air)
	h.vib(dim, freqBlockDestroy, pos.x, pos.y, pos.z, 0)
	if !survives {
		return true
	}
	h.playSoundDim(players, dim, "minecraft:block.frogspawn.hatch", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	for i, n := 0, 2+h.rng.Intn(4); i < n; i++ {
		x := float64(pos.x) + math.Min(math.Max(h.rng.Float64(), 0.2), 0.8) // getRandomTadpolePositionOffset: a clamped roll
		z := float64(pos.z) + math.Min(math.Max(h.rng.Float64(), 0.2), 0.8)
		if tp := h.spawnMobIn(players, entityTadpole, dim, x, float64(pos.y)-0.5, z); tp != nil {
			tp.persistent = true // setPersistenceRequired: a hatched tadpole stays
			tp.yaw = float32(1 + h.rng.Intn(360))
		}
	}
	return true
}

// frogLaySpawn is TryLaySpawnOnWaterNearLand: a pregnant frog standing on
// land beside water drops its clutch on the surface next to it.
func (h *hub) frogLaySpawn(players map[int32]*tracked, m *mob) {
	if !m.pregnant || m.dying != 0 {
		return
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y)-1, floorInt(m.z)
	if worldgen.IsFluid(w.Block(bx, by+1, bz)) {
		return // vanilla lays from the bank, never from in the water
	}
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		x, z := bx+d[0], bz+d[1]
		if w.Block(x, by, z) != worldgen.WaterBase || w.Block(x, by+1, z) != worldgen.Air {
			continue
		}
		h.setBlockAt(players, m.dim, blockPos{x, by + 1, z}, frogspawnBlock)
		h.scheduleFrogspawn(m.dim, blockPos{x, by + 1, z})
		h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.frog.lay_spawn", sndBlock, m.x, m.y, m.z, 1, 1)
		m.pregnant = false
		return
	}
}
