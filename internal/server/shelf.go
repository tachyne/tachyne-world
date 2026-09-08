package server

import (
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Wooden shelves (vanilla ShelfBlock + ShelfBlockEntity + SideChainPartBlock,
// 1.21.9). A shelf shows three stacks on its face; clicking a slot swaps
// the whole held stack with what sits there. Power it with redstone and it
// links to powered shelves beside it that face the same way, up to three
// in a row; clicking any of them then swaps the row's slots with the
// player's hotbar in one go (the rightmost shelf takes hotbar slots 7-9).
// The stacks are persisted with the containers and shown to clients
// through the shelf's block-entity tag (chunk section and live frame).

const shelfMaxChain = 3

var woodShelfRanges = rangesOf([]string{"oak_shelf", "spruce_shelf", "birch_shelf", "jungle_shelf", "acacia_shelf",
	"dark_oak_shelf", "mangrove_shelf", "cherry_shelf", "pale_oak_shelf", "bamboo_shelf", "crimson_shelf", "warped_shelf"})

func isWoodShelf(s uint32) bool { return inRanges(woodShelfRanges, s) }

// shelfSlot / shelfView are the chunk builders' read view: names and counts.
type shelfSlot struct {
	Name  string
	Count int32
}
type shelfView struct{ Items [3]shelfSlot }

func (v shelfView) nbt() [3]protocol.ShelfItemNBT {
	var out [3]protocol.ShelfItemNBT
	for i, s := range v.Items {
		out[i] = protocol.ShelfItemNBT{ID: s.Name, Count: s.Count}
	}
	return out
}

func shelfViewOf(sh *[3]invStack) shelfView {
	var v shelfView
	if sh == nil {
		return v
	}
	for i, st := range sh {
		if st.item != 0 && st.count > 0 {
			v.Items[i] = shelfSlot{Name: "minecraft:" + itemNameOf[st.item], Count: int32(st.count)}
		}
	}
	return v
}

func (v shelfView) frame(pos simPos) attachproto.ShelfItems {
	e := attachproto.ShelfItems{X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z)}
	for i, s := range v.Items {
		e.Items[i] = attachproto.ShelfItem{Name: s.Name, Count: s.Count}
	}
	return e
}

// shelfStore is the mutex'd view chunk builders read on their own goroutines.
type shelfStore struct {
	mu sync.Mutex
	m  map[string]shelfView
}

func newShelfStore() *shelfStore { return &shelfStore{m: map[string]shelfView{}} }

func (s *shelfStore) get(dim, x, y, z int) (shelfView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[simKey(simPos{dim: dim, blockPos: blockPos{x, y, z}})]
	return v, ok
}

func (s *shelfStore) set(pos simPos, v shelfView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[simKey(pos)] = v
}

func (s *shelfStore) remove(pos simPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, simKey(pos))
}

// shelfSync publishes a shelf's contents: the read view and a live frame.
func (h *hub) shelfSync(players map[int32]*tracked, pos simPos) {
	v := shelfViewOf(h.woodShelves[pos])
	h.shelfView.set(pos, v)
	h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), v.frame(pos))
}

// woodShelfHitSlot maps a click on the shelf's front face to a column
// (SelectableSlotContainer with 3 columns, 1 row); -1 off the face.
func woodShelfHitSlot(state uint32, face int32, cx, cz float32) int {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return -1
	}
	facing := worldgen.GetProperty(info, state, "facing")
	if faceName(face) != facing || face < 2 {
		return -1
	}
	var fx float32
	switch facing {
	case "north":
		fx = 1 - cx
	case "south":
		fx = cx
	case "west":
		fx = cz
	case "east":
		fx = 1 - cz
	}
	col := int(fx * 3)
	if col > 2 {
		col = 2
	}
	return col
}

type evUseWoodShelf struct {
	eid        int32
	x, y, z    int
	face       int32
	cx, cy, cz float32
}

func (evUseWoodShelf) isHubEvent() {}

// useWoodShelf is ShelfBlock.useItemOn: a single swap, or the hotbar swap
// across a powered chain.
func (h *hub) useWoodShelf(players map[int32]*tracked, e evUseWoodShelf) {
	t := players[e.eid]
	if t == nil || t.inv == nil {
		return
	}
	w := h.worldFor(t.dim)
	state := w.At(e.x, e.y, e.z)
	if !isWoodShelf(state) || woodShelfHitSlot(state, e.face, e.cx, e.cz) < 0 {
		return
	}
	pos := simPos{dim: t.dim, blockPos: blockPos{e.x, e.y, e.z}}
	cx, cy, cz := float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5
	held := t.p.heldSlot()
	if !boolProp(state, "powered") {
		slot := woodShelfHitSlot(state, e.face, e.cx, e.cz)
		sh := h.woodShelves[pos]
		if sh == nil {
			sh = &[3]invStack{}
			h.woodShelves[pos] = sh
		}
		in := t.inv.slots[held]
		out := sh[slot]
		if in.item == 0 && out.item == 0 {
			return
		}
		sh[slot] = in
		if t.gamemode == gmCreative && out.item == 0 {
			out = in // creative keeps a copy
		}
		t.inv.slots[held] = out
		h.sendSlot(t, held)
		switch {
		case out.item != 0 && in.item == 0:
			h.playSoundDim(players, t.dim, "minecraft:block.shelf.take_item", sndBlock, cx, cy, cz, 1, 1)
		case out.item != 0:
			h.playSoundDim(players, t.dim, "minecraft:block.shelf.single_swap", sndBlock, cx, cy, cz, 1, 1)
		default:
			h.playSoundDim(players, t.dim, "minecraft:block.shelf.place_item", sndBlock, cx, cy, cz, 1, 1)
		}
		h.shelfSync(players, pos)
		h.scheduleSignalAround(pos.blockPos)
		return
	}
	// Powered: the chain's slots against hotbar slots 9 - (n-i)*3 + slot.
	chain := h.shelfChain(w, pos.blockPos, state)
	swapped := false
	for i, cp := range chain {
		sp := simPos{dim: t.dim, blockPos: cp}
		sh := h.woodShelves[sp]
		if sh == nil {
			sh = &[3]invStack{}
			h.woodShelves[sp] = sh
		}
		for slot := 0; slot < 3; slot++ {
			inv := 9 - (len(chain)-i)*3 + slot
			if inv < 0 || inv >= 9 {
				continue
			}
			a, b := t.inv.slots[inv], sh[slot]
			if a.item == 0 && b.item == 0 {
				continue
			}
			t.inv.slots[inv], sh[slot] = b, a
			h.sendSlot(t, inv)
			swapped = true
		}
		h.shelfSync(players, sp)
		h.scheduleSignalAround(cp)
	}
	if swapped {
		h.playSoundDim(players, t.dim, "minecraft:block.shelf.multi_swap", sndBlock, cx, cy, cz, 1, 1)
	}
}

// shelfLeft / shelfRight are vanilla's chain directions: left is the
// facing's clockwise side, right its counter-clockwise side.
func shelfLeft(facing string) (int, int)  { return facingDelta(rightOf(facing)) }
func shelfRight(facing string) (int, int) { return facingDelta(leftOf(facing)) }

func shelfPart(state uint32) string {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return "unconnected"
	}
	return worldgen.GetProperty(info, state, "side_chain")
}

func withShelfPart(state uint32, part string) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	return worldgen.SetProperty(info, state, "side_chain", part)
}

// shelfConnectable is isConnectable: a powered wooden shelf.
func shelfConnectable(state uint32) bool { return isWoodShelf(state) && boolProp(state, "powered") }

// shelfChain is getAllBlocksConnectedTo: the connected shelves left to
// right (a shelf that is not connectable is a chain of nothing).
func (h *hub) shelfChain(w *world.World, pos blockPos, state uint32) []blockPos {
	if !shelfConnectable(state) {
		return nil
	}
	facing := stateFacing(state)
	chain := []blockPos{pos}
	walk := func(dx, dz int, end string, front bool) {
		for steps := 1; steps < shelfMaxChain; steps++ {
			p := blockPos{pos.x + dx*steps, pos.y, pos.z + dz*steps}
			s := w.At(p.x, p.y, p.z)
			if !shelfConnectable(s) || stateFacing(s) != facing {
				return
			}
			part := shelfPart(s)
			if part == "center" || part == end {
				if front {
					chain = append([]blockPos{p}, chain...)
				} else {
					chain = append(chain, p)
				}
			}
			if part != "center" {
				return
			}
		}
	}
	lx, lz := shelfLeft(facing)
	walk(lx, lz, "left", true)
	rx, rz := shelfRight(facing)
	walk(rx, rz, "right", false)
	return chain
}

// updateShelfPower is ShelfBlock.neighborChanged: follow the neighbour signal.
func (h *hub) updateShelfPower(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if boolProp(state, "powered") == powered {
		return
	}
	next := setBoolProp(state, "powered", powered)
	if !powered {
		next = withShelfPart(next, "unconnected")
	}
	old := state
	h.setBlockLive(players, dim, pos.x, pos.y, pos.z, next)
	snd := "minecraft:block.shelf.deactivate"
	if powered {
		snd = "minecraft:block.shelf.activate"
	}
	h.playSoundDim(players, dim, snd, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	if powered {
		h.shelfPowerUp(players, dim, pos, next, old)
	} else {
		h.shelfPowerDown(players, dim, pos, next)
	}
}

// shelfPlaced is ShelfBlock.getStateForPlacement's powered read + onPlace.
func (h *hub) shelfPlaced(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if powered != boolProp(state, "powered") {
		state = setBoolProp(state, "powered", powered)
		h.setBlockLive(players, dim, pos.x, pos.y, pos.z, state)
	}
	if powered {
		h.shelfPowerUp(players, dim, pos, state, worldgen.Air)
	} else {
		h.shelfPowerDown(players, dim, pos, state)
	}
}

type shelfNeighbour struct {
	pos   blockPos
	state uint32
	ok    bool // a connectable shelf facing the same way
}

func (h *hub) shelfNeighbour(dim int, pos blockPos, facing string, dx, dz int) shelfNeighbour {
	p := blockPos{pos.x + dx, pos.y, pos.z + dz}
	s := h.worldFor(dim).At(p.x, p.y, p.z)
	return shelfNeighbour{pos: p, state: s, ok: shelfConnectable(s) && stateFacing(s) == facing}
}

func (h *hub) setShelfPart(players map[int32]*tracked, dim int, n shelfNeighbour, part string) {
	if !n.ok || shelfPart(n.state) == part {
		return
	}
	h.setBlockLive(players, dim, n.pos.x, n.pos.y, n.pos.z, withShelfPart(n.state, part))
}

// shelfPowerUp is updateSelfAndNeighborsOnPoweringUp.
func (h *hub) shelfPowerUp(players map[int32]*tracked, dim int, pos blockPos, state, old uint32) {
	if !shelfConnectable(state) {
		return
	}
	gettingConnected := shelfPart(state) != "unconnected"
	wasConnected := shelfConnectable(old) && shelfPart(old) != "unconnected"
	if gettingConnected || wasConnected {
		return // a neighbour is doing the updating
	}
	facing := stateFacing(state)
	w := h.worldFor(dim)
	lx, lz := shelfLeft(facing)
	rx, rz := shelfRight(facing)
	left := h.shelfNeighbour(dim, pos, facing, lx, lz)
	right := h.shelfNeighbour(dim, pos, facing, rx, rz)
	part := "unconnected"
	chainLen := 1
	leftN, rightN := 0, 0
	if left.ok {
		leftN = len(h.shelfChain(w, left.pos, left.state))
	}
	if right.ok {
		rightN = len(h.shelfChain(w, right.pos, right.state))
	}
	canConnect := func(n int) bool { return n > 0 && chainLen+n <= shelfMaxChain }
	if canConnect(leftN) {
		part = whenConnectedToTheLeft(part)
		h.setShelfPart(players, dim, left, whenConnectedToTheRight(shelfPart(left.state)))
		chainLen += leftN
	}
	if canConnect(rightN) {
		part = whenConnectedToTheRight(part)
		h.setShelfPart(players, dim, right, whenConnectedToTheLeft(shelfPart(right.state)))
	}
	if shelfPart(state) != part {
		h.setBlockLive(players, dim, pos.x, pos.y, pos.z, withShelfPart(state, part))
	}
}

// shelfPowerDown is updateNeighborsAfterPoweringDown: the neighbours let go.
func (h *hub) shelfPowerDown(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	facing := stateFacing(state)
	lx, lz := shelfLeft(facing)
	rx, rz := shelfRight(facing)
	left := h.shelfNeighbour(dim, pos, facing, lx, lz)
	right := h.shelfNeighbour(dim, pos, facing, rx, rz)
	h.setShelfPart(players, dim, left, whenDisconnectedFromTheRight(shelfPart(left.state)))
	h.setShelfPart(players, dim, right, whenDisconnectedFromTheLeft(shelfPart(right.state)))
}

// The SideChainPart transitions.
func whenConnectedToTheRight(p string) string {
	if p == "unconnected" || p == "left" {
		return "left"
	}
	return "center"
}
func whenConnectedToTheLeft(p string) string {
	if p == "unconnected" || p == "right" {
		return "right"
	}
	return "center"
}
func whenDisconnectedFromTheRight(p string) string {
	if p == "unconnected" || p == "left" {
		return "unconnected"
	}
	return "right"
}
func whenDisconnectedFromTheLeft(p string) string {
	if p == "unconnected" || p == "right" {
		return "unconnected"
	}
	return "left"
}

// spillWoodShelf drops a removed shelf's stacks.
func (h *hub) spillWoodShelf(players map[int32]*tracked, pos simPos) {
	sh := h.woodShelves[pos]
	if sh == nil {
		return
	}
	delete(h.woodShelves, pos)
	h.shelfView.remove(pos)
	for _, st := range sh {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		if it := h.spawnItemIn(players, pos.dim, st.item, st.count, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5); it != nil {
			it.dmg, it.ench, it.name, it.color, it.potion, it.repairCost = st.dmg, st.ench, st.name, st.color, st.potion, st.repairCost
			h.refreshItemMeta(players, it)
		}
	}
}
