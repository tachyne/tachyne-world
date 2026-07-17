package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Double chests: two adjacent single chests of the same block, same facing, on
// each other's connect side, form one 54-slot Large Chest (vanilla ChestBlock
// pairing). Each half keeps its own 27-slot storage + persistence; the menu
// just presents them as one — LEFT half on top (slots 0-26), RIGHT below
// (27-53). Pairing is decided at placement; breaking either half reverts the
// survivor to a single chest.

// Horizontal facing deltas and the clockwise/counter-clockwise neighbours
// (viewed from above), matching Direction.getClockWise / getCounterClockWise.
var (
	horizDelta = map[string][3]int{
		"north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
	}
	horizClockwise        = map[string]string{"north": "east", "east": "south", "south": "west", "west": "north"}
	horizCounterClockwise = map[string]string{"north": "west", "west": "south", "south": "east", "east": "north"}
)

// chestBlockBase identifies which chest BLOCK a state belongs to (chests only
// pair with the same block — a copper chest never pairs with wood or a
// different oxidation stage). Returns the block's min state, or 0 if not a chest.
func chestBlockBase(state uint32) uint32 {
	if !isChestBlock(state) {
		return 0
	}
	if info, ok := worldgen.InfoForState(state); ok {
		return info.Min
	}
	return 0
}

// chestFacingType reads a chest's facing + type ("single"/"left"/"right").
func chestFacingType(state uint32) (facing, ctype string) {
	if info, ok := worldgen.InfoForState(state); ok {
		return worldgen.GetProperty(info, state, "facing"), worldgen.GetProperty(info, state, "type")
	}
	return "", ""
}

// connectedDir is ChestBlock.getConnectedDirection: a LEFT chest's partner sits
// clockwise of its facing, a RIGHT chest's counter-clockwise.
func connectedDir(facing, ctype string) (string, bool) {
	switch ctype {
	case "left":
		return horizClockwise[facing], true
	case "right":
		return horizCounterClockwise[facing], true
	}
	return "", false
}

// chestPairPositions returns the ordered (left, right) block positions of the
// double chest that (x,y,z) belongs to, or paired=false for a single chest. It
// verifies the partner is a matching chest with the complementary type.
func (h *hub) chestPairPositions(x, y, z int, state uint32) (left, right blockPos, paired bool) {
	facing, ctype := chestFacingType(state)
	dir, ok := connectedDir(facing, ctype)
	if !ok {
		return blockPos{}, blockPos{}, false
	}
	d := horizDelta[dir]
	pp := blockPos{x + d[0], y + d[1], z + d[2]}
	ps := h.world.At(pp.x, pp.y, pp.z)
	if chestBlockBase(ps) != chestBlockBase(state) {
		return blockPos{}, blockPos{}, false // partner gone or mismatched
	}
	pf, pt := chestFacingType(ps)
	if pf != facing || pt == "single" || pt == ctype {
		return blockPos{}, blockPos{}, false
	}
	self := blockPos{x, y, z}
	if ctype == "left" {
		return self, pp, true
	}
	return pp, self, true
}

// formChestPair upgrades a lone single chest and a matching single-chest
// neighbour into a left/right pair, returning the ordered halves. This
// self-heals worlds whose chests were placed before pairing existed (two
// adjacent singles on a connect side — a state vanilla never produces). No-op
// (ok=false) for a chest that is already typed or has no eligible neighbour.
func (h *hub) formChestPair(x, y, z int, state uint32) (left, right blockPos, ok bool) {
	info, iok := worldgen.InfoForState(state)
	if !iok || !isChestBlock(state) || !info.HasProperty("type") {
		return blockPos{}, blockPos{}, false
	}
	facing, ctype := chestFacingType(state)
	if ctype != "single" {
		return blockPos{}, blockPos{}, false
	}
	base := chestBlockBase(state)
	self := blockPos{x, y, z}
	try := func(side, selfType, partnerType string) (blockPos, blockPos, bool) {
		d := horizDelta[horizSide(side, facing)]
		pp := blockPos{x + d[0], y + d[1], z + d[2]}
		ps := h.world.At(pp.x, pp.y, pp.z)
		if chestBlockBase(ps) != base {
			return blockPos{}, blockPos{}, false
		}
		if pf, pt := chestFacingType(ps); pf != facing || pt != "single" {
			return blockPos{}, blockPos{}, false
		}
		pinfo, _ := worldgen.InfoForState(ps)
		h.setBlock(h.playersRef, self, worldgen.SetProperty(info, state, "type", selfType))
		h.setBlock(h.playersRef, pp, worldgen.SetProperty(pinfo, ps, "type", partnerType))
		if selfType == "left" {
			return self, pp, true
		}
		return pp, self, true
	}
	if l, r, ok := try("cw", "left", "right"); ok {
		return l, r, true
	}
	if l, r, ok := try("ccw", "right", "left"); ok {
		return l, r, true
	}
	return blockPos{}, blockPos{}, false
}

// openDoubleChest opens a Large Chest window over both halves' storage.
func (h *hub) openDoubleChest(t *tracked, left, right blockPos) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	for _, pos := range [2]blockPos{left, right} {
		if h.chests[pos] == nil {
			c := &chest{}
			h.fillStructureChest(pos, c)
			h.chests[pos] = c
		}
	}
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winPos2, t.winKind = h.nextWin, left, right, winDoubleChest
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuGeneric9x6), Title: "Large Chest"})
	h.sendDoubleChestWindow(t)
	t.p.trySendEv(soundEv("minecraft:block.chest.open", sndBlock,
		float64(left.x)+0.5, float64(left.y), float64(left.z)+0.5, 0.5, 1))
}

// sendDoubleChestWindow refreshes all 54 chest slots (LEFT then RIGHT) plus the
// player's inventory.
func (h *hub) sendDoubleChestWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 90)
	for _, pos := range [2]blockPos{t.winPos, t.winPos2} {
		c := h.chests[pos]
		for i := 0; i < 27; i++ {
			if c != nil {
				slots = append(slots, stackEv(c.slots[i]))
			} else {
				slots = append(slots, stackEv(invStack{}))
			}
		}
	}
	for i := 9; i <= 35; i++ { // main inventory
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ { // hotbar
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// pairChestOnPlace runs ChestBlock.getStateForPlacement's non-sneak pairing: a
// freshly placed chest joins an adjacent matching single chest, choosing LEFT/
// RIGHT and flipping the partner to the complement. Returns the placed chest's
// (possibly re-typed) state.
func (s *Server) pairChestOnPlace(p *player, x, y, z int, state uint32) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok || !isChestBlock(state) || !info.HasProperty("type") {
		return state
	}
	facing, ctype := chestFacingType(state)
	if ctype != "single" {
		return state
	}
	base := chestBlockBase(state)
	// Clockwise partner → this chest is LEFT (partner becomes RIGHT); else the
	// counter-clockwise partner → this chest is RIGHT (partner becomes LEFT).
	try := func(side, selfType, partnerType string) (uint32, bool) {
		d := horizDelta[horizSide(side, facing)]
		pp := blockPos{x + d[0], y + d[1], z + d[2]}
		ps := s.worldFor(p).Block(pp.x, pp.y, pp.z)
		if chestBlockBase(ps) != base {
			return 0, false
		}
		if pf, pt := chestFacingType(ps); pf != facing || pt != "single" {
			return 0, false
		}
		pinfo, _ := worldgen.InfoForState(ps)
		s.setPartnerChest(p, pp, worldgen.SetProperty(pinfo, ps, "type", partnerType))
		return worldgen.SetProperty(info, state, "type", selfType), true
	}
	if ns, ok := try("cw", "left", "right"); ok {
		return ns
	}
	if ns, ok := try("ccw", "right", "left"); ok {
		return ns
	}
	return state
}

func horizSide(side, facing string) string {
	if side == "cw" {
		return horizClockwise[facing]
	}
	return horizCounterClockwise[facing]
}

// setPartnerChest writes the partner half's new state and broadcasts it (by:0 —
// no editor to exclude), mirroring the connect-neighbour update path.
func (s *Server) setPartnerChest(p *player, pos blockPos, ns uint32) {
	s.worldFor(p).SetBlock(pos.x, pos.y, pos.z, ns)
	s.hub.post(evBlock{x: pos.x, y: pos.y, z: pos.z, dim: p.dim, state: ns, by: 0})
}

// unpairChestNeighbors reverts any chest that was paired with (x,y,z) back to a
// single chest — called from the hub when the block there stops being a chest.
func (h *hub) unpairChestNeighbors(players map[int32]*tracked, x, y, z int) {
	for dir, d := range horizDelta {
		_ = dir
		np := blockPos{x + d[0], y + d[1], z + d[2]}
		ns := h.world.At(np.x, np.y, np.z)
		if !isChestBlock(ns) {
			continue
		}
		facing, ctype := chestFacingType(ns)
		cd, ok := connectedDir(facing, ctype)
		if !ok {
			continue
		}
		pd := horizDelta[cd]
		if np.x+pd[0] == x && np.y+pd[1] == y && np.z+pd[2] == z {
			info, _ := worldgen.InfoForState(ns)
			h.setBlock(players, np, worldgen.SetProperty(info, ns, "type", "single"))
		}
	}
}
