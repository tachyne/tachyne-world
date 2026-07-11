package server

// placefix.go — placement behaviors for interactive one-off blocks: the bell
// (attachment from the clicked face, vanilla BellBlock.getStateForPlacement)
// and flower pots (right-click with a plant swaps in the potted block).

import (
	"github.com/tachyne/tachyne-common/protocol"

	"tachyne/internal/worldgen"
)

var (
	bellDefault    = worldgen.BlockID("bell")
	flowerPotState = worldgen.BlockID("flower_pot")

	// pottedByItem: plant item id → potted block state, from the generated
	// name-keyed table.
	pottedByItem = func() map[int32]uint32 {
		m := map[int32]uint32{}
		for name, potted := range pottedPlantState {
			if id, ok := itemByName[name]; ok {
				m[id] = potted
			}
		}
		return m
	}()

	// plantBlockByPotted maps a potted block back to the plant's own block
	// state — emptying a pot drops the PLANT (the potted block's loot table
	// yields the pot, which stays placed).
	plantBlockByPotted = func() map[uint32]uint32 {
		m := map[uint32]uint32{}
		for name, potted := range pottedPlantState {
			if id, ok := itemByName[name]; ok {
				if bs, ok := protocol.BlockForItem(id); ok {
					m[potted] = bs
				}
			}
		}
		return m
	}()
)

// placeBell places a bell with its attachment derived from the clicked face —
// vanilla BellBlock.getStateForPlacement: top → floor, bottom → ceiling
// (facing = the player's horizontal look either way), side → wall with
// double_wall when both blocks across the clicked axis are sturdy.
func (s *Server) placeBell(p *player, defState uint32, tx, ty, tz int, dir int32, seq int32) bool {
	w := s.worldFor(p)
	info, ok := worldgen.InfoForState(defState)
	if !ok {
		s.abortPlace(p, tx, ty, tz, seq)
		return false
	}
	var state uint32
	switch {
	case dir == 1: // on the floor
		if !worldgen.IsSolidFull(w.Block(tx, ty-1, tz)) {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		state = worldgen.SetProperty(info, defState, "attachment", "floor")
		state = worldgen.SetProperty(info, state, "facing", playerFacing(p.yaw))
	case dir == 0: // hanging from the ceiling
		if !worldgen.IsSolidFull(w.Block(tx, ty+1, tz)) {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		state = worldgen.SetProperty(info, defState, "attachment", "ceiling")
		state = worldgen.SetProperty(info, state, "facing", playerFacing(p.yaw))
	default: // on a wall
		var both bool
		if dir == 4 || dir == 5 { // clicked an X face: check west+east neighbours
			both = worldgen.IsSolidFull(w.Block(tx-1, ty, tz)) && worldgen.IsSolidFull(w.Block(tx+1, ty, tz))
		} else {
			both = worldgen.IsSolidFull(w.Block(tx, ty, tz-1)) && worldgen.IsSolidFull(w.Block(tx, ty, tz+1))
		}
		att := "single_wall"
		if both {
			att = "double_wall"
		}
		state = worldgen.SetProperty(info, defState, "attachment", att)
		state = worldgen.SetProperty(info, state, "facing", oppositeFacing(faceName(dir)))
	}
	s.putBlock(p, tx, ty, tz, state, true, seq)
	return true
}

// usePot handles a right click on a flower pot: a held pottable plant fills
// it; clicking a filled pot pops the plant back out as a drop.
func (s *Server) usePot(p *player, x, y, z int, state uint32, seq int32) bool {
	if state == flowerPotState {
		potted, ok := pottedByItem[p.heldItem()]
		if !ok {
			return false // empty pot, nothing pottable in hand — not our click
		}
		s.putBlock(p, x, y, z, potted, true, seq)
		if s.modes.get(p.name) == gmSurvival {
			s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		}
		return true
	}
	if plant, ok := plantBlockByPotted[state]; ok {
		s.putBlock(p, x, y, z, flowerPotState, true, seq)
		s.hub.post(evDrop{x: x, y: y, z: z, state: plant}) // the plant pops back out
		return true
	}
	return false
}
