package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Lectern + chiseled bookshelf, on the vanilla models. The lectern holds one
// book (menu 17: the book slot, a page dataslot, and prev/next/take/jump
// buttons) and pulses on page turns; the bookshelf stores six bookish items
// addressed by where on its face you click, mirrored into the
// slot_N_occupied block-state bools. Both persist with the containers file.

const (
	menuLectern            = 17
	worldEventPageTurn     = 1043
	lecternButtonPrev      = 1
	lecternButtonNext      = 2
	lecternButtonTake      = 3
	lecternButtonJumpStart = 100
)

var (
	lecternMin = worldgen.BlockBase("lectern") // facing(4) x has_book(2) x powered(2)
	lecternMax = worldgen.BlockBase("lectern") + 15

	bookshelfMin = worldgen.BlockBase("chiseled_bookshelf") // facing(4) x 2^6 occupied
	bookshelfMax = worldgen.BlockBase("chiseled_bookshelf") + 255
)

func isLectern(s uint32) bool   { return s >= lecternMin && s <= lecternMax }
func isBookshelf(s uint32) bool { return s >= bookshelfMin && s <= bookshelfMax }

// lecternBooks: what a lectern accepts (#minecraft:lectern_books).
func isLecternBook(item int32) bool { return item == itemWritableBook || item == itemWrittenBook }

// bookshelfBooks: what the shelf accepts (#minecraft:bookshelf_books).
var bookshelfBooks = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"book", "written_book", "writable_book", "enchanted_book", "knowledge_book"} {
		if id := int32(itemByName[n]); id != 0 {
			m[id] = true
		}
	}
	return m
}()

// lectern is one lectern's held book + open page.
type lectern struct {
	book invStack
	page int
}

type evUseLectern struct {
	eid     int32
	x, y, z int
}

func (evUseLectern) isHubEvent() {}

// onUseLectern places the held book (empty lectern) or opens the menu.
func (h *hub) onUseLectern(players map[int32]*tracked, e evUseLectern) {
	t := players[e.eid]
	if t == nil || t.inv == nil || t.dim != 0 {
		return
	}
	state := h.world.At(e.x, e.y, e.z)
	if !isLectern(state) {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	lec := h.lecterns[pos]
	if lec == nil || lec.book.item == 0 {
		held := t.inv.slots[t.p.heldSlot()]
		if !isLecternBook(held.item) || held.count <= 0 {
			return
		}
		book := held
		book.count = 1
		h.lecterns[pos] = &lectern{book: book}
		if t.gamemode != gmCreative {
			s := &t.inv.slots[t.p.heldSlot()]
			if s.count--; s.count <= 0 {
				*s = invStack{}
			}
			h.sendSlot(t, t.p.heldSlot())
		}
		info, _ := worldgen.InfoForState(state)
		h.setBlockLive(players, t.dim, e.x, e.y, e.z, worldgen.SetProperty(info, state, "has_book", "true"))
		h.playSound(players, "minecraft:item.book.put", sndBlock,
			float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
		h.incCustom(t, "interact_with_lectern", 1)
		return
	}
	h.openLectern(t, pos, lec)
	h.incCustom(t, "interact_with_lectern", 1)
}

// openLectern opens the reader menu (one read-only slot + the page property).
func (h *hub) openLectern(t *tracked, pos blockPos, lec *lectern) {
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.reclaimEnchant(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind, t.winPos = h.nextWin, winLectern, pos

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuLectern), Title: "Lectern"})
	h.sendLecternWindow(t, lec)
}

func (h *hub) sendLecternWindow(t *tracked, lec *lectern) {
	t.inv.stateId++
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: []attachproto.ItemStack{stackEv(lec.book)}, Cursor: stackEv(t.cursor)})
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(lec.page)})
}

// lecternPages is the held book's page count (for clamping).
func (h *hub) lecternPages(lec *lectern) int {
	if b, ok := h.books.get(lec.book.bookID); ok {
		return len(b.Pages)
	}
	return 1
}

// lecternButton applies a menu button: page turns, jumps, taking the book.
func (h *hub) lecternButton(players map[int32]*tracked, t *tracked, button int32) {
	lec := h.lecterns[t.winPos]
	if lec == nil || lec.book.item == 0 {
		return
	}
	page := lec.page
	switch {
	case button == lecternButtonPrev:
		page--
	case button == lecternButtonNext:
		page++
	case button >= lecternButtonJumpStart:
		page = int(button - lecternButtonJumpStart)
	case button == lecternButtonTake:
		book := lec.book
		delete(h.lecterns, t.winPos)
		changed, leftover := t.inv.addStack(book)
		for _, s := range changed {
			h.sendSlot(t, s)
		}
		if leftover > 0 {
			h.tossItem(players, t, book)
		}
		state := h.world.At(t.winPos.x, t.winPos.y, t.winPos.z)
		if info, ok := worldgen.InfoForState(state); ok && isLectern(state) {
			h.setBlockLive(players, t.dim, t.winPos.x, t.winPos.y, t.winPos.z,
				worldgen.SetProperty(info, state, "has_book", "false"))
		}
		t.winID = 0 // vanilla closes the screen with the book gone
		h.sendInventory(t)
		return
	default:
		return
	}
	if max := h.lecternPages(lec); page >= max {
		page = max - 1
	}
	if page < 0 {
		page = 0
	}
	if page == lec.page {
		return
	}
	lec.page = page
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(lec.page)})
	h.toNearbyEv(players, 0, float64(t.winPos.x), float64(t.winPos.z), attachproto.WorldFX{
		Event: worldEventPageTurn, X: t.winPos.x, Y: t.winPos.y, Z: t.winPos.z})
}

// spillLectern drops the book when the lectern goes away.
func (h *hub) spillLectern(players map[int32]*tracked, x, y, z int, newState uint32) {
	if isLectern(newState) {
		return
	}
	pos := blockPos{x, y, z}
	lec := h.lecterns[pos]
	if lec == nil {
		return
	}
	delete(h.lecterns, pos)
	if lec.book.item != 0 {
		if it := h.spawnItem(players, lec.book.item, 1, float64(x)+0.5, float64(y)+1, float64(z)+0.5); it != nil {
			it.bookID = lec.book.bookID
		}
	}
}

// --- chiseled bookshelf ---

type evUseShelf struct {
	eid        int32
	x, y, z    int
	face       int32
	cx, cy, cz float32 // click point within the face
}

func (evUseShelf) isHubEvent() {}

// shelfHitSlot maps a face click to the 2x3 grid slot (vanilla
// SelectableSlotContainer): valid only on the shelf's facing side.
func shelfHitSlot(state uint32, face int32, cx, cy, cz float32) int {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return -1
	}
	facing := worldgen.GetProperty(info, state, "facing")
	var faceName string
	switch face {
	case 2:
		faceName = "north"
	case 3:
		faceName = "south"
	case 4:
		faceName = "west"
	case 5:
		faceName = "east"
	default:
		return -1
	}
	if faceName != facing {
		return -1
	}
	var fx float32 // face-local x, left-to-right looking AT the face
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
	row := 0
	if cy < 0.5 {
		row = 1
	}
	return col + row*3
}

// onUseShelf inserts the held bookish item at the clicked slot, or takes
// the book already there.
func (h *hub) onUseShelf(players map[int32]*tracked, e evUseShelf) {
	t := players[e.eid]
	if t == nil || t.inv == nil || t.dim != 0 {
		return
	}
	state := h.world.At(e.x, e.y, e.z)
	if !isBookshelf(state) {
		return
	}
	slot := shelfHitSlot(state, e.face, e.cx, e.cy, e.cz)
	if slot < 0 {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	shelf := h.bookshelves[pos]
	if shelf == nil {
		shelf = &[6]invStack{}
		h.bookshelves[pos] = shelf
	}
	held := t.inv.slots[t.p.heldSlot()]
	if bookshelfBooks[held.item] && held.count > 0 && shelf[slot].item == 0 {
		book := held
		book.count = 1
		shelf[slot] = book
		if t.gamemode != gmCreative {
			s := &t.inv.slots[t.p.heldSlot()]
			if s.count--; s.count <= 0 {
				*s = invStack{}
			}
			h.sendSlot(t, t.p.heldSlot())
		}
		h.playSound(players, "minecraft:block.chiseled_bookshelf.insert", sndBlock,
			float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
	} else if held.item == 0 && shelf[slot].item != 0 {
		book := shelf[slot]
		shelf[slot] = invStack{}
		changed, leftover := t.inv.addStack(book)
		for _, s := range changed {
			h.sendSlot(t, s)
		}
		if leftover > 0 {
			h.tossItem(players, t, book)
		}
		h.playSound(players, "minecraft:block.chiseled_bookshelf.pickup", sndBlock,
			float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
	} else {
		return
	}
	h.shelfSyncState(players, t.dim, pos, state, shelf)
}

// shelfSyncState mirrors slot occupancy into the block-state bools.
func (h *hub) shelfSyncState(players map[int32]*tracked, dim int, pos blockPos, state uint32, shelf *[6]invStack) {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return
	}
	names := [6]string{"slot_0_occupied", "slot_1_occupied", "slot_2_occupied",
		"slot_3_occupied", "slot_4_occupied", "slot_5_occupied"}
	ns := state
	for i, n := range names {
		v := "false"
		if shelf[i].item != 0 {
			v = "true"
		}
		ns = worldgen.SetProperty(info, ns, n, v)
	}
	if ns != state {
		h.setBlockLive(players, dim, pos.x, pos.y, pos.z, ns)
	}
}

// spillShelf drops the shelf's books when the block goes away.
func (h *hub) spillShelf(players map[int32]*tracked, x, y, z int, newState uint32) {
	if isBookshelf(newState) {
		return
	}
	pos := blockPos{x, y, z}
	shelf := h.bookshelves[pos]
	if shelf == nil {
		return
	}
	delete(h.bookshelves, pos)
	for _, st := range shelf {
		if st.item != 0 {
			if it := h.spawnItem(players, st.item, 1, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5); it != nil {
				it.ench = st.ench
				it.bookID = st.bookID
			}
		}
	}
}
