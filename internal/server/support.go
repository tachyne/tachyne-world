package server

import (
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Whether a block has what it needs to stay where it is.
//
// worldgen.SupportFor classifies every block into the SHAPE of its
// requirement; this applies the shape at a position. One predicate, consulted
// at both ends: placement refuses a block that would not survive, and a
// neighbour change knocks down anything that no longer does. Before this,
// support was a six-block list checked only directly above an edit, so a wall
// torch survived the wall it was on and a rail could be placed in mid-air.

// plantSoils are the blocks a plant will root in — vanilla's dirt tag plus the
// handful of other grounds saplings and flowers accept.
var plantSoils = func() map[uint32]bool {
	out := map[uint32]bool{}
	for _, n := range []string{
		"dirt", "grass_block", "podzol", "coarse_dirt", "rooted_dirt", "mycelium",
		"moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots", "farmland",
		"sand", "red_sand", "suspicious_sand", "soul_sand", "soul_soil",
		"crimson_nylium", "warped_nylium", "clay", "gravel", "terracotta",
		"snow_block", "powder_snow", "end_stone", "netherrack",
	} {
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

// supported reports whether the block at pos can stay there. Taken over a
// world rather than the hub so the placement path — which runs on a session
// goroutine — can ask the same question the tick loop asks.
func supported(w *world.World, pos blockPos, state uint32) bool {
	if w == nil {
		return true
	}
	below := func() uint32 { return w.At(pos.x, pos.y-1, pos.z) }
	above := func() uint32 { return w.At(pos.x, pos.y+1, pos.z) }
	behind := func() uint32 {
		info, ok := worldgen.InfoForState(state)
		if !ok {
			return worldgen.Air
		}
		// The block a wall-mounted block hangs on is opposite the way it faces.
		dx, dz := facingDelta(oppositeOf(worldgen.GetProperty(info, state, "facing")))
		return w.At(pos.x+dx, pos.y, pos.z+dz)
	}
	prop := func(name string) string {
		info, ok := worldgen.InfoForState(state)
		if !ok {
			return ""
		}
		return worldgen.GetProperty(info, state, name)
	}

	// The upper half of a two-block block rests on its own lower half, whatever
	// that lower half needs. Without this, every tall grass, sunflower and open
	// door in the world reads as unsupported — the top half sits on a plant or
	// on a non-collidable open door, neither of which holds anything.
	if prop("half") == "upper" || prop("part") == "head" {
		return sameBlockFamily(below(), state) || holdsBlock(below())
	}

	switch worldgen.SupportFor(state) {
	case worldgen.SupportFloor:
		return holdsBlock(below())
	case worldgen.SupportSoil:
		return plantSoils[below()]
	case worldgen.SupportFarmland:
		return isFarmland(below())
	case worldgen.SupportWall:
		return holdsBlock(behind())
	case worldgen.SupportCeiling:
		return holdsBlock(above())
	case worldgen.SupportFace:
		switch prop("face") {
		case "floor":
			return holdsBlock(below())
		case "ceiling":
			return holdsBlock(above())
		default:
			return holdsBlock(behind())
		}
	case worldgen.SupportAttached:
		// Amethyst points away from the face it grew on. Lichen and sculk vein
		// carry a boolean per face instead of a facing, so any neighbour that
		// can hold them counts — this must not be read as "needs a floor", or
		// every lichen on a cave wall comes down.
		if holdsBlock(behind()) {
			return true
		}
		for _, d := range supportNeighbours {
			if holdsBlock(w.At(pos.x+d[0], pos.y+d[1], pos.z+d[2])) {
				return true
			}
		}
		return false
	case worldgen.SupportWater:
		b := below()
		return worldgen.IsWater(b) || b == worldgen.BlockBase("ice")
	case worldgen.SupportHangable:
		if prop("hanging") == "true" {
			return holdsBlock(above())
		}
		return holdsBlock(below())
	case worldgen.SupportSpeleothem:
		if prop("vertical_direction") == "up" {
			return holdsBlock(below())
		}
		return holdsBlock(above())
	}
	return true
}

// holdsBlock reports whether a block can hold another one against it.
//
// Deliberately NOT "is a full opaque cube": vanilla asks whether the FACE is
// sturdy, which a top slab, a stair, a fence post and a pane of glass all
// satisfy. Testing for a full cube instead would call every torch on a fence
// and every carpet on a slab unsupported and tear down builds that have stood
// for months. Collidable-and-not-replaceable is the conservative reading —
// it never destroys something legitimate, at the price of allowing a few
// attachments vanilla would refuse (a torch on a torch). A real per-face shape
// test is the way to tighten this.
func holdsBlock(state uint32) bool {
	return state != worldgen.Air && worldgen.Collides(state) && !worldgen.IsReplaceable(state)
}

// sameBlockFamily reports whether two states belong to the same block — used
// to let an upper half rest on its own lower half.
func sameBlockFamily(a, b uint32) bool {
	ia, oka := worldgen.InfoForState(a)
	ib, okb := worldgen.InfoForState(b)
	return oka && okb && ia.Min == ib.Min
}

// oppositeOf flips a facing name. A wall block's support is behind it.
func oppositeOf(facing string) string {
	switch facing {
	case "north":
		return "south"
	case "south":
		return "north"
	case "east":
		return "west"
	case "west":
		return "east"
	}
	return facing
}

// dropUnsupported knocks down anything around an edit that just lost what it
// was standing on, hanging from or fixed to, and cascades — a two-tall plant
// or a stack of torches goes all the way.
func (h *hub) dropUnsupported(players map[int32]*tracked, dim int, pos blockPos) {
	queue := []blockPos{pos}
	for len(queue) > 0 && len(queue) < 512 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range supportNeighbours {
			n := blockPos{p.x + d[0], p.y + d[1], p.z + d[2]}
			if !h.inWorldY(n.y) {
				continue
			}
			// world.At, not Block: naturally generated plants are not in the
			// edit overlay, and Block would miss them entirely.
			st := h.worldFor(dim).At(n.x, n.y, n.z)
			if worldgen.SupportFor(st) == worldgen.SupportNone || supported(h.worldFor(dim), n, st) {
				continue
			}
			h.setBlockAt(players, dim, n, worldgen.Air)
			h.dropLoose(players, dim, n, st)
			queue = append(queue, n)
		}
	}
}

// supportNeighbours are the six cells whose block can depend on a change here.
var supportNeighbours = [6][3]int{
	{0, 1, 0}, {0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1},
}

// dropLoose spawns what a block that fell down on its own leaves behind: the
// no-tool loot, since nobody mined it.
func (h *hub) dropLoose(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	if !h.rules.DoTileDrops {
		return
	}
	drops := h.evalBlockLoot(lootCtx{state: state, rng: h.rng.Intn, randf: h.rng.Float64})
	if drops == nil {
		drops = h.rollDrops(state)
	}
	for _, d := range drops {
		h.spawnItemIn(players, dim, d.item, d.count, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
	}
}
