package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Chorus, ported from ChorusFlowerBlock.randomTick.
//
// A flower either grows straight up, branches sideways, or dies. It grows up
// when it stands on end stone, on air, or on a short enough pillar — the
// pillar check walks up to four segments down and rolls against the height, so
// tall pillars stop climbing and start branching. Every cell it leaves behind
// becomes a chorus PLANT whose six connection flags are recomputed from its
// neighbours, which is what makes the stems visually join up.
//
// Chorus only grows in the End, which had never ticked at all before the
// simulation was made per-dimension.

const chorusDeadAge = 5

var (
	chorusFlowerBase = worldgen.BlockBase("chorus_flower") // age 0..5
	chorusPlantBase  = worldgen.BlockBase("chorus_plant")
	endStoneBlock    = worldgen.BlockBase("end_stone")
)

func isChorusFlower(s uint32) bool {
	return s >= chorusFlowerBase && s <= chorusFlowerBase+chorusDeadAge
}
func isChorusPlant(s uint32) bool {
	lo, hi := worldgen.BlockRange("chorus_plant")
	return s >= lo && s <= hi
}
func chorusAge(s uint32) int { return int(s - chorusFlowerBase) }

// chorusPlantAt builds a chorus-plant state wired to whatever it touches
// (ChorusPlantBlock.getStateWithConnections): it joins to other chorus blocks
// on any side, and downward to end stone.
func (h *hub) chorusPlantAt(dim, x, y, z int) uint32 {
	info, ok := worldgen.InfoForState(chorusPlantBase)
	if !ok {
		return chorusPlantBase
	}
	s := chorusPlantBase
	joins := func(nx, ny, nz int, down bool) bool {
		n := h.worldFor(dim).At(nx, ny, nz)
		if isChorusPlant(n) || isChorusFlower(n) {
			return true
		}
		return down && n == endStoneBlock
	}
	for _, d := range []struct {
		name       string
		dx, dy, dz int
		down       bool
	}{
		{"up", 0, 1, 0, false}, {"down", 0, -1, 0, true},
		{"north", 0, 0, -1, false}, {"south", 0, 0, 1, false},
		{"west", -1, 0, 0, false}, {"east", 1, 0, 0, false},
	} {
		v := "false"
		if joins(x+d.dx, y+d.dy, z+d.dz, d.down) {
			v = "true"
		}
		s = worldgen.SetProperty(info, s, d.name, v)
	}
	return s
}

// chorusNeighboursEmpty is allNeighborsEmpty: every horizontal neighbour must
// be air, optionally ignoring the direction we came from.
func (h *hub) chorusNeighboursEmpty(dim, x, y, z int, ignoreDX, ignoreDZ int) bool {
	for _, d := range horizNeighbors {
		if d.x == ignoreDX && d.z == ignoreDZ {
			continue
		}
		if h.worldFor(dim).At(x+d.x, y, z+d.z) != worldgen.Air {
			return false
		}
	}
	return true
}

// tickChorus runs one ChorusFlowerBlock.randomTick.
func (h *hub) tickChorus(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isChorusFlower(state) {
		return false
	}
	age := chorusAge(state)
	if age >= chorusDeadAge { // already dead
		return true
	}
	if h.worldFor(dim).At(x, y+1, z) != worldgen.Air {
		return true
	}

	below := h.worldFor(dim).At(x, y-1, z)
	growUp, onPillarOverStone := false, false
	switch {
	case below == endStoneBlock || below == worldgen.Air:
		growUp = true
	case isChorusPlant(below):
		// Walk down the pillar; a taller one is likelier to branch instead.
		height := 1
		for i := 0; i < 4; i++ {
			s := h.worldFor(dim).At(x, y-height-1, z)
			if !isChorusPlant(s) {
				if s == endStoneBlock {
					onPillarOverStone = true
				}
				break
			}
			height++
		}
		limit := 4
		if onPillarOverStone {
			limit = 5
		}
		if height < 2 || height <= h.rng.Intn(limit) {
			growUp = true
		}
	}

	if growUp && h.chorusNeighboursEmpty(dim, x, y+1, z, 0, 0) &&
		h.worldFor(dim).At(x, y+2, z) == worldgen.Air {
		h.setBlockAt(players, dim, blockPos{x, y, z}, h.chorusPlantAt(dim, x, y, z))
		h.setBlockAt(players, dim, blockPos{x, y + 1, z}, chorusFlowerBase+uint32(age))
		return true
	}
	if age >= 4 { // too old to branch
		h.setBlockAt(players, dim, blockPos{x, y, z}, chorusFlowerBase+chorusDeadAge)
		return true
	}

	attempts := h.rng.Intn(4)
	if onPillarOverStone {
		attempts++
	}
	branched := false
	for i := 0; i < attempts; i++ {
		d := horizNeighbors[h.rng.Intn(len(horizNeighbors))]
		tx, tz := x+d.x, z+d.z
		if h.worldFor(dim).At(tx, y, tz) == worldgen.Air &&
			h.worldFor(dim).At(tx, y-1, tz) == worldgen.Air &&
			h.chorusNeighboursEmpty(dim, tx, y, tz, -d.x, -d.z) {
			h.setBlockAt(players, dim, blockPos{tx, y, tz}, chorusFlowerBase+uint32(age+1))
			branched = true
		}
	}
	if branched {
		h.setBlockAt(players, dim, blockPos{x, y, z}, h.chorusPlantAt(dim, x, y, z))
	} else {
		h.setBlockAt(players, dim, blockPos{x, y, z}, chorusFlowerBase+chorusDeadAge)
	}
	return true
}

// chorusPlantSupported is ChorusPlantBlock.canSurvive: a plant segment holds
// if end stone or another plant is directly beneath it, or if it hangs off a
// horizontal neighbour that is itself rooted — and, in that second case, only
// when nothing sits directly above AND below it. That last clause is what
// stops a chorus tree from being a solid column.
func (h *hub) chorusPlantSupported(dim, x, y, z int) bool {
	w := h.worldFor(dim)
	below := w.At(x, y-1, z)
	above := w.At(x, y+1, z)
	sandwiched := above != worldgen.Air && below != worldgen.Air

	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		n := w.At(x+d[0], y, z+d[1])
		if !isChorusPlant(n) {
			continue
		}
		if sandwiched {
			return false
		}
		if under := w.At(x+d[0], y-1, z+d[1]); isChorusPlant(under) || under == endStoneBlock {
			return true
		}
	}
	return isChorusPlant(below) || below == endStoneBlock
}

// tickChorusPlant breaks an unsupported plant segment. Cutting the base of a
// chorus tree pops the whole thing, because each broken segment schedules its
// neighbours and they find themselves unsupported in turn.
func (h *hub) tickChorusPlant(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isChorusPlant(state) || h.chorusPlantSupported(dim, x, y, z) {
		return false
	}
	pos := blockPos{x, y, z}
	h.setBlockAt(players, dim, pos, worldgen.Air)
	h.dropLoose(players, dim, pos, state)
	return true
}
