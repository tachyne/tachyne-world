package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Pistons (tier 1c). Powered pistons push up to 12 movable blocks one cell
// along their facing and grow a piston_head; unpowering retracts (sticky
// pistons pull the block in front back in). Block movement is instant — no
// moving_piston animation entity yet — and entities aren't carried.

const (
	pistonMaxPush = 12
)

var (
	stickyPistonMin = worldgen.BlockBase("sticky_piston")
	stickyPistonMax = worldgen.BlockBase("sticky_piston") + 11
	pistonMin       = worldgen.BlockBase("piston")
	pistonMax       = worldgen.BlockBase("piston") + 11
	pistonHeadMin   = worldgen.BlockBase("piston_head")
	pistonHeadMax   = worldgen.BlockBase("piston_head") + 23

	obsidianState = worldgen.BlockBase("obsidian")

	dispenserMin = worldgen.BlockBase("dispenser") // facing(6) × triggered(2)
	dispenserMax = worldgen.BlockBase("dispenser") + 11
	dropperMin   = worldgen.BlockBase("dropper")
	dropperMax   = worldgen.BlockBase("dropper") + 11
)

func isDispenser(s uint32) bool { return s >= dispenserMin && s <= dispenserMax }
func isDropper(s uint32) bool   { return s >= dropperMin && s <= dropperMax }

// sixWayFacing: blocks whose placement uses the full up/down facing set.
func sixWayFacing(s uint32) bool {
	return isPistonBase(s) || isObserver(s) || isDispenser(s) || isDropper(s)
}

func isPistonBase(s uint32) bool {
	return (s >= pistonMin && s <= pistonMax) || (s >= stickyPistonMin && s <= stickyPistonMax)
}
func isSticky(s uint32) bool     { return s >= stickyPistonMin && s <= stickyPistonMax }
func isPistonHead(s uint32) bool { return s >= pistonHeadMin && s <= pistonHeadMax }

// pistonDelta is the base's facing as a 3D delta (6-way, like observers).
func pistonDelta(s uint32) (int, int, int) {
	switch stateFacing(s) {
	case "up":
		return 0, 1, 0
	case "down":
		return 0, -1, 0
	}
	dx, dz := facingDelta(stateFacing(s))
	return dx, 0, dz
}

// headFor builds the piston_head state matching a base: [facing(6), short(2),
// type(2)], short=false; facing order north,east,south,west,up,down.
func headFor(base uint32) uint32 {
	fIdx := map[string]uint32{"north": 0, "east": 1, "south": 2, "west": 3, "up": 4, "down": 5}[stateFacing(base)]
	head := pistonHeadMin + fIdx*4 + 2 // short=false
	if isSticky(base) {
		head++
	}
	return head
}

// pistonImmovable: blocks a piston can never move.
func pistonImmovable(s uint32) bool {
	return s == worldgen.Bedrock || s == obsidianState ||
		isPistonHead(s) ||
		(isPistonBase(s) && boolProp(s, "extended")) ||
		isChestBlock(s) ||
		(s >= furnaceStateMin && s <= furnaceStateMax) ||
		s == enchTableState || // enchanting table
		(s >= anvilStateMin && s <= anvilStateMax) ||
		(s >= grindstoneStateMin && s <= grindstoneStateMax)
}

// pistonFragile: attached/thin blocks that break instead of moving.
func pistonFragile(s uint32) bool {
	return isRedstoneish(s) || isLever(s) || worldgen.IsReplaceable(s) ||
		worldgen.IsWater(s) || worldgen.IsLava(s)
}

// updatePiston reacts to power: extend when powered, retract when not.
func (h *hub) updatePiston(players map[int32]*tracked, pos blockPos, state uint32) {
	dx, dy, dz := pistonDelta(state)
	// Power from any side except the face (vanilla ignores front power).
	powered := false
	for _, d := range rsNeighbors {
		if d[0] == dx && d[1] == dy && d[2] == dz {
			continue
		}
		if h.emitPower(pos.x+d[0], pos.y+d[1], pos.z+d[2], pos.x, pos.y, pos.z) > 0 {
			powered = true
			break
		}
	}
	extended := boolProp(state, "extended")
	if powered && !extended {
		h.extendPiston(players, pos, state, dx, dy, dz)
	} else if !powered && extended {
		h.retractPiston(players, pos, state, dx, dy, dz)
	}
}

// extendPiston gathers the movable column in front and shifts it one cell.
func (h *hub) extendPiston(players map[int32]*tracked, pos blockPos, state uint32, dx, dy, dz int) {
	var column []blockPos
	cx, cy, cz := pos.x+dx, pos.y+dy, pos.z+dz
	for {
		s := h.world.At(cx, cy, cz)
		if s == worldgen.Air || pistonFragile(s) {
			break // room (fragile blocks at the end get crushed)
		}
		if pistonImmovable(s) || len(column) >= pistonMaxPush {
			return // blocked: stay retracted
		}
		column = append(column, blockPos{cx, cy, cz})
		cx, cy, cz = cx+dx, cy+dy, cz+dz
	}
	for i := len(column) - 1; i >= 0; i-- { // far block first
		from := column[i]
		h.setBlock(players, blockPos{from.x + dx, from.y + dy, from.z + dz}, h.world.At(from.x, from.y, from.z))
	}
	h.setBlock(players, blockPos{pos.x + dx, pos.y + dy, pos.z + dz}, headFor(state))
	h.setBlock(players, pos, setBoolProp(state, "extended", true))
	h.playSound(players, "minecraft:block.piston.extend", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.7)
	h.scheduleAround(pos, 1)
}

// retractPiston removes the head; sticky pistons pull the next block in.
func (h *hub) retractPiston(players map[int32]*tracked, pos blockPos, state uint32, dx, dy, dz int) {
	head := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	fill := uint32(worldgen.Air)
	if isSticky(state) {
		bx, by, bz := head.x+dx, head.y+dy, head.z+dz
		if s := h.world.At(bx, by, bz); s != worldgen.Air && !pistonImmovable(s) && !pistonFragile(s) {
			fill = s
			h.setBlock(players, blockPos{bx, by, bz}, worldgen.Air)
		}
	}
	h.setBlock(players, head, fill)
	h.setBlock(players, pos, setBoolProp(state, "extended", false))
	h.playSound(players, "minecraft:block.piston.contract", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.7)
	h.scheduleAround(pos, 1)
}
