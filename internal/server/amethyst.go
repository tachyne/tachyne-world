package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Budding amethyst, ported from BuddingAmethystBlock.randomTick: one random
// tick in five, pick one of the six faces, and advance whatever is on it —
// air becomes a small bud, small becomes medium, medium large, large a full
// cluster. A bud only advances if it already faces the way it grew, so a bud
// on one face never counts for another.

var (
	buddingAmethyst = worldgen.BlockBase("budding_amethyst")

	// The cluster chain, in growth order. A face holding stage N advances to
	// stage N+1; air (or full water) starts the chain at stage 0.
	amethystChain = []string{
		"small_amethyst_bud", "medium_amethyst_bud",
		"large_amethyst_bud", "amethyst_cluster",
	}

	// facingFor maps a growth direction to the property value the bud carries.
	amethystDirs = []struct {
		dx, dy, dz int
		facing     string
	}{
		{0, 1, 0, "up"}, {0, -1, 0, "down"},
		{0, 0, -1, "north"}, {0, 0, 1, "south"},
		{-1, 0, 0, "west"}, {1, 0, 0, "east"},
	}
)

// amethystStage reports which link of the chain a state is, and which way it
// faces. stage -1 means "not part of the chain".
func amethystStage(state uint32) (stage int, facing string) {
	for i, name := range amethystChain {
		lo, hi := worldgen.BlockRange(name)
		if state < lo || state > hi {
			continue
		}
		info, ok := worldgen.InfoForState(state)
		if !ok {
			return i, ""
		}
		return i, worldgen.GetProperty(info, state, "facing")
	}
	return -1, ""
}

// amethystAt builds the state for a chain stage facing a direction, carrying
// the waterlogged flag from whatever it replaces.
func amethystAt(stage int, facing string, waterlogged bool) (uint32, bool) {
	if stage < 0 || stage >= len(amethystChain) {
		return 0, false
	}
	base := worldgen.BlockBase(amethystChain[stage])
	info, ok := worldgen.InfoForState(base)
	if !ok {
		return 0, false
	}
	s := worldgen.SetProperty(info, base, "facing", facing)
	wl := "false"
	if waterlogged {
		wl = "true"
	}
	return worldgen.SetProperty(info, s, "waterlogged", wl), true
}

// tickAmethyst runs one BuddingAmethystBlock.randomTick.
func (h *hub) tickAmethyst(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if state != buddingAmethyst {
		return false
	}
	if h.rng.Intn(5) != 0 {
		return true
	}
	d := amethystDirs[h.rng.Intn(len(amethystDirs))]
	nx, ny, nz := x+d.dx, y+d.dy, z+d.dz
	at := h.worldFor(dim).At(nx, ny, nz)

	water := worldgen.IsWater(at)
	next := -1
	switch {
	case at == worldgen.Air || water:
		next = 0 // canClusterGrowAtState: air or a full water source
	default:
		// Only a bud already facing the way we are growing advances.
		if stage, facing := amethystStage(at); stage >= 0 && facing == d.facing &&
			stage+1 < len(amethystChain) {
			next = stage + 1
		}
	}
	if next < 0 {
		return true
	}
	if ns, ok := amethystAt(next, d.facing, water); ok {
		h.setBlockAt(players, dim, blockPos{nx, ny, nz}, ns)
	}
	return true
}
