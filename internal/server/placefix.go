package server

// placefix.go — placement behaviors for interactive one-off blocks: the bell
// (attachment from the clicked face, vanilla BellBlock.getStateForPlacement)
// and flower pots (right-click with a plant swaps in the potted block).

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
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

	// plantItemByPotted maps a potted block to its plant's item: what
	// FlowerPotBlock.useWithoutItem hands back (new ItemStack(potted)).
	plantItemByPotted = func() map[uint32]int32 {
		m := map[uint32]int32{}
		for name, potted := range pottedPlantState {
			if id, ok := itemByName[name]; ok {
				m[potted] = id
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

// usePot handles a right click on a flower pot (FlowerPotBlock.useItemOn /
// useWithoutItem): a held pottable plant fills an empty pot; a filled pot
// clicked with a pottable plant does nothing (CONSUME); clicked with anything
// else it gives its plant back — into the inventory, dropped only if there is
// no room. Both changes are a BLOCK_CHANGE for sculk.
func (s *Server) usePot(p *player, off bool, x, y, z int, state uint32, seq int32) bool {
	potted, pottable := pottedByItem[p.handItem(off)]
	if state == flowerPotState {
		if !pottable {
			return false // empty pot, nothing pottable in hand — not our click
		}
		s.putBlock(p, x, y, z, potted, true, seq)
		s.hub.post(evStat{eid: p.eid, name: "pot_flower"})
		s.hub.post(evPotChange{eid: p.eid, dim: p.dim, x: x, y: y, z: z})
		if isSurvival(s.modes.get(p.key())) {
			s.hub.post(evConsume{eid: p.eid, slot: p.handSlot(off)})
		}
		return true
	}
	plant, ok := plantItemByPotted[state]
	if !ok {
		return false
	}
	if pottable {
		s.sendBlockChange(p, x, y, z, state, seq) // CONSUME: the pot keeps its plant
		return true
	}
	s.putBlock(p, x, y, z, flowerPotState, true, seq)
	s.hub.post(evPotChange{eid: p.eid, dim: p.dim, x: x, y: y, z: z, give: plant})
	return true
}

// evPotChange is a flower pot filled or emptied by a player: the vibration,
// and for an emptied pot the plant handed back.
type evPotChange struct {
	eid     int32
	dim     int
	x, y, z int
	give    int32 // the plant's item, when the pot was emptied
}

func (evPotChange) isHubEvent() {}

func (h *hub) onPotChange(players map[int32]*tracked, e evPotChange) {
	t := players[e.eid]
	if t == nil {
		return
	}
	if e.give != 0 && t.inv != nil {
		plant := invStack{item: e.give, count: 1}
		changed, leftover := t.inv.addStack(plant) // player.addItem, else drop
		for _, sl := range changed {
			h.sendSlot(t, sl)
		}
		if leftover > 0 {
			plant.count = leftover
			h.tossItem(players, t, plant)
		}
	}
	h.vib(e.dim, freqBlockChange, e.x, e.y, e.z, t.p.eid)
}
