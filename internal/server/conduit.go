package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The conduit. A player-built block entity: surround one with a frame of
// prismarine and it grants Conduit Power to swimmers in range, and at a full
// frame it hunts hostile mobs in the water around it.
//
// The Conduit Power EFFECT landed with the missing effects; this is the source
// that makes it reachable without a command.

const (
	conduitMinActive = 16  // MIN_ACTIVE_SIZE: frame blocks before it lights at all
	conduitMinKill   = 42  // MIN_KILL_SIZE: a full frame also attacks
	conduitKillRange = 8.0 // KILL_RANGE
	conduitRangeStep = 16  // effectRange = activeBlocks/7 * 16
	conduitPowerSecs = 13  // EFFECT_DURATION, refreshed while the conduit runs
	conduitAttackDmg = 4   // vanilla's conduit beam damage
)

var conduitState = worldgen.BlockBase("conduit")

// conduitFrameBlocks are the four block types a frame may be built from.
var conduitFrameBlocks = func() map[uint32]bool {
	set := map[uint32]bool{}
	for _, n := range []string{"prismarine", "prismarine_bricks", "sea_lantern", "dark_prismarine"} {
		lo, hi := worldgen.BlockRange(n)
		for s := lo; s <= hi; s++ {
			set[s] = true
		}
	}
	return set
}()

// conduitActiveBlocks counts a conduit's frame, or reports 0 if it cannot run.
//
// Two vanilla conditions, in order: the 3x3x3 around the conduit must be ALL
// water (that is why one dug into a wall goes dark), and then the frame is the
// prismarine sitting on the 5x5x5 shell's edge-centre lines — the shape that
// makes the familiar open cage rather than a solid box.
func (h *hub) conduitActiveBlocks(dim int, pos blockPos) int {
	w := h.worldFor(dim)
	for ox := -1; ox <= 1; ox++ {
		for oy := -1; oy <= 1; oy++ {
			for oz := -1; oz <= 1; oz++ {
				if ox == 0 && oy == 0 && oz == 0 {
					continue // the conduit itself
				}
				if !worldgen.IsWater(w.At(pos.x+ox, pos.y+oy, pos.z+oz)) {
					return 0
				}
			}
		}
	}
	n := 0
	for ox := -2; ox <= 2; ox++ {
		for oy := -2; oy <= 2; oy++ {
			for oz := -2; oz <= 2; oz++ {
				ax, ay, az := abs(ox), abs(oy), abs(oz)
				if ax <= 1 && ay <= 1 && az <= 1 {
					continue
				}
				// The frame positions: on one of the three axis-aligned rings.
				onRing := (ox == 0 && (ay == 2 || az == 2)) ||
					(oy == 0 && (ax == 2 || az == 2)) ||
					(oz == 0 && (ax == 2 || ay == 2))
				if !onRing {
					continue
				}
				if conduitFrameBlocks[w.At(pos.x+ox, pos.y+oy, pos.z+oz)] {
					n++
				}
			}
		}
	}
	if n < conduitMinActive {
		return 0
	}
	return n
}

// noteConduitBlock keeps the conduit registry in step with the world. Conduits
// are ONLY ever player-built — no structure generates one — so remembering
// where they were placed is both cheaper and more complete than scanning
// blocks around players looking for them.
func (h *hub) noteConduitBlock(dim int, pos blockPos, state uint32) {
	key := simPos{dim: dim, blockPos: pos}
	if state == conduitState {
		if h.conduits == nil {
			h.conduits = map[simPos]bool{}
		}
		h.conduits[key] = true
		return
	}
	delete(h.conduits, key)
}

// updateConduits runs every known conduit that has a player near enough to
// care. Anything whose block has gone is forgotten as it is found.
func (h *hub) updateConduits(players map[int32]*tracked) {
	for key := range h.conduits {
		if h.worldFor(key.dim).At(key.x, key.y, key.z) != conduitState {
			delete(h.conduits, key) // mined out, or the position was never one
			continue
		}
		near := false
		for _, t := range players {
			if t.dim == key.dim && !t.dead &&
				dist3(t.x, t.y, t.z, float64(key.x), float64(key.y), float64(key.z)) <= conduitWakeRange {
				near = true
				break
			}
		}
		if near {
			h.runConduit(players, key.dim, key.blockPos)
		}
	}
}

// conduitWakeRange is how close a player must be for a conduit to bother
// running. Generous: the effect itself reaches 96 blocks at a full frame.
const conduitWakeRange = 128.0

// runConduit applies one conduit's effects for this cycle.
func (h *hub) runConduit(players map[int32]*tracked, dim int, pos blockPos) {
	active := h.conduitActiveBlocks(dim, pos)
	if active == 0 {
		return
	}
	rangeBlocks := float64(active/7) * conduitRangeStep
	for _, t := range players {
		if t.dim != dim || t.dead || t.gamemode != gmSurvival {
			continue
		}
		if dist3(t.x, t.y, t.z, float64(pos.x), float64(pos.y), float64(pos.z)) > rangeBlocks {
			continue
		}
		// Vanilla requires the player to be in water or rain to benefit.
		if !h.inWater(dim, t.x, t.y, t.z) && !h.raining {
			continue
		}
		h.applyEffect(players, t, effConduitPower, 0, conduitPowerSecs)
	}
	if active < conduitMinKill {
		return
	}
	// A full frame hunts: one hostile in the water nearby takes a beam.
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 || !m.hostile {
			continue
		}
		if dist3(m.x, m.y, m.z, float64(pos.x), float64(pos.y), float64(pos.z)) > conduitKillRange {
			continue
		}
		if !h.inWater(dim, m.x, m.y, m.z) {
			continue
		}
		h.hurtMobEffect(players, m, conduitAttackDmg)
		h.playSound(players, "minecraft:block.conduit.attack.target", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
		return // vanilla attacks one target per cycle
	}
}
