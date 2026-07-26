package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Sponge. Place a dry one in water and it pulls the water out around it — up
// to 64 blocks, breadth-first, never more than 6 cells away — and turns wet.
//
// Vanilla's search takes the water-logged plants with it (kelp and seagrass
// drop rather than being stranded), and stops at anything that is not water,
// so a sponge cannot reach round a corner it has no water path to.

var (
	spongeState    = worldgen.BlockBase("sponge")
	wetSpongeState = worldgen.BlockBase("wet_sponge")
)

const (
	spongeReach = 6  // vanilla's traversal depth
	spongeMax   = 64 // …and how many cells it will take
)

// spongeDrains are the water-logged plants a sponge clears along with the
// water, rather than leaving them hanging in the air.
var spongeDrains = func() map[uint32]bool {
	out := map[uint32]bool{}
	for _, n := range []string{"kelp", "kelp_plant", "seagrass", "tall_seagrass"} {
		lo, hi, ok := worldgen.BlockRangeOK(n)
		if !ok {
			continue
		}
		for s := lo; s <= hi; s++ {
			out[s] = true
		}
	}
	return out
}()

// absorbWater is the sponge's soak. Reports whether it took anything up —
// only then does the sponge turn wet.
func (h *hub) absorbWater(players map[int32]*tracked, dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	type node struct {
		p     blockPos
		depth int
	}
	queue := []node{{pos, 0}}
	seen := map[blockPos]bool{pos: true}
	taken := 0
	for len(queue) > 0 && taken < spongeMax {
		n := queue[0]
		queue = queue[1:]
		if n.depth >= spongeReach {
			continue
		}
		for _, d := range supportNeighbours {
			np := blockPos{n.p.x + d[0], n.p.y + d[1], n.p.z + d[2]}
			if seen[np] || !h.inWorldY(np.y) {
				continue
			}
			seen[np] = true
			st := w.At(np.x, np.y, np.z)
			switch {
			case worldgen.IsWater(st):
				h.setBlockAt(players, dim, np, worldgen.Air)
			case spongeDrains[st]:
				h.setBlockAt(players, dim, np, worldgen.Air)
				h.dropLoose(players, dim, np, st)
			default:
				continue // not water: the sponge cannot reach past it
			}
			taken++
			queue = append(queue, node{np, n.depth + 1})
			if taken >= spongeMax {
				break
			}
		}
	}
	return taken > 0
}

// soakSponge is the check run when a sponge is placed or its neighbourhood
// changes: a dry sponge that finds water drinks it and turns wet.
func (h *hub) soakSponge(players map[int32]*tracked, dim int, pos blockPos) {
	if h.worldFor(dim) == nil || h.worldFor(dim).At(pos.x, pos.y, pos.z) != spongeState {
		return
	}
	if !h.absorbWater(players, dim, pos) {
		return
	}
	h.setBlockAt(players, dim, pos, wetSpongeState)
	h.playSoundDim(players, dim, "minecraft:block.sponge.absorb", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}
