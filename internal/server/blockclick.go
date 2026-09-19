package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Four block right-clicks the engine lacked: picking sweet berries
// (SweetBerryBushBlock.useWithoutItem, harvest/sweet_berry_bush), picking
// glow berries (CaveVines.use, harvest/cave_vine), cycling a copper golem
// statue's pose (CopperGolemStatueBlock.updatePose) and a lone dust's
// dot/cross toggle (RedStoneWireBlock.useWithoutItem).

var (
	itemSweetBerries = int32(itemByName["sweet_berries"])
	itemGlowBerries  = int32(itemByName["glow_berries"])
	statueRanges     = func() [][2]uint32 { // the eight copper golem statues: pose(4) × facing(4) × waterlogged(2)
		var out [][2]uint32
		for _, n := range []string{"copper_golem_statue", "exposed_copper_golem_statue", "weathered_copper_golem_statue", "oxidized_copper_golem_statue",
			"waxed_copper_golem_statue", "waxed_exposed_copper_golem_statue", "waxed_weathered_copper_golem_statue", "waxed_oxidized_copper_golem_statue"} {
			if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
				out = append(out, [2]uint32{lo, hi})
			}
		}
		return out
	}()
)

// isCaveVine / isGolemStatue classify the clicked block (isBerryBush lives
// in entityinside.go).
func isCaveVine(s uint32) bool {
	return (s >= caveVinesLo && s <= caveVinesHi) || (s >= caveVinesPlantLo && s <= caveVinesPlantHi)
}
func isGolemStatue(s uint32) bool {
	for _, r := range statueRanges {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}

// evClickBlock is one of these right-clicks, posted by the interaction path.
type evClickBlock struct {
	eid     int32
	x, y, z int
}

func (evClickBlock) isHubEvent() {}

// clickBlock runs the block's own use for the four kinds.
func (h *hub) clickBlock(players map[int32]*tracked, e evClickBlock) {
	t := players[e.eid]
	if t == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	w := h.worldFor(t.dim)
	state := w.At(e.x, e.y, e.z)
	fx, fy, fz := float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5
	switch {
	case isBerryBush(state):
		age := int(state - berryBase)
		if age < 2 {
			return
		}
		n := 1 + h.rng.Intn(2) // harvest/sweet_berry_bush: 1-2, and one more at age 3
		if age == 3 {
			n++
		}
		h.spawnItemIn(players, t.dim, itemSweetBerries, n, fx, fy, fz)
		h.playSoundDim(players, t.dim, "minecraft:block.sweet_berry_bush.pick_berries", sndBlock, fx, fy, fz, 1, 0.8+h.rng.Float32()*0.4)
		h.setBlockAt(players, t.dim, pos, berryBase+1)
		h.vib(t.dim, freqBlockChange, e.x, e.y, e.z, t.p.eid)
	case isCaveVine(state):
		info, ok := worldgen.InfoForState(state)
		if !ok || worldgen.GetProperty(info, state, "berries") != "true" {
			return
		}
		h.spawnItemIn(players, t.dim, itemGlowBerries, 1, fx, fy, fz) // harvest/cave_vine
		h.playSoundDim(players, t.dim, "minecraft:block.cave_vines.pick_berries", sndBlock, fx, fy, fz, 1, 0.8+h.rng.Float32()*0.4)
		h.setBlockAt(players, t.dim, pos, worldgen.SetProperty(info, state, "berries", "false"))
		h.vib(t.dim, freqBlockChange, e.x, e.y, e.z, t.p.eid)
	case isGolemStatue(state):
		if axeItems[heldStack(t).item] {
			return // an axe scrapes or strips wax instead (Item.useOn runs first)
		}
		info, ok := worldgen.InfoForState(state)
		if !ok {
			return
		}
		poses := []string{"standing", "sitting", "running", "star"}
		cur := worldgen.GetProperty(info, state, "copper_golem_pose")
		next := poses[0]
		for i, p := range poses {
			if p == cur {
				next = poses[(i+1)%len(poses)]
			}
		}
		h.playSoundDim(players, t.dim, "minecraft:entity.copper_golem_become_statue", sndBlock, fx, fy, fz, 1, 1)
		h.setBlockAt(players, t.dim, pos, worldgen.SetProperty(info, state, "copper_golem_pose", next))
		h.vib(t.dim, freqBlockChange, e.x, e.y, e.z, t.p.eid)
	case isWire(state):
		h.inDim(t.dim, func() { h.toggleWireDot(players, pos, state) })
	}
}

// toggleWireDot is RedStoneWireBlock.useWithoutItem: a cross (every arm
// out, nothing to connect to) becomes a dot, a dot becomes a cross; a dust
// with real connections is left alone.
func (h *hub) toggleWireDot(players map[int32]*tracked, pos blockPos, state uint32) {
	info, ok := worldgen.InfoForState(state)
	if !ok || h.wireHasRealArms(pos.x, pos.y, pos.z) {
		return
	}
	v := "none" // cross → dot
	if wireIsDot(state) {
		v = "side" // dot → cross
	}
	next := state
	for _, d := range horizontalDirs {
		next = worldgen.SetProperty(info, next, rsDirName[d], v)
	}
	if next != state {
		h.rsSet(players, pos, next)
	}
}

// wireIsDot reports a dust with no arms at all.
func wireIsDot(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return false
	}
	for _, d := range horizontalDirs {
		if worldgen.GetProperty(info, state, rsDirName[d]) != "none" {
			return false
		}
	}
	return true
}
