package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// pickAndPlace ctrl-picks the block at src and places the picked stack at
// dst as state, through the pick action and the placement's hub event.
func pickAndPlace(t *testing.T, h *hub, players map[int32]*tracked, pl *tracked, src, dst simPos, state uint32) invStack {
	t.Helper()
	pl.inv.slots = [invSize]invStack{}
	pl.p.setHeldSlot(0)
	pickVia(h, players, pl, attachproto.PickItem{X: int32(src.x), Y: int32(src.y), Z: int32(src.z), IncludeData: true})
	st := heldStack(pl)
	if st.item == 0 {
		t.Fatalf("nothing picked at %+v", src)
	}
	h.world.SetBlock(dst.x, dst.y, dst.z, state)
	h.onBlock(players, evBlock{x: dst.x, y: dst.y, z: dst.z, state: state, by: pl.p.eid, placed: true})
	return st
}

// Stacks inside a copied container keep every component the engine models,
// not only damage, name, potion and enchantments: a dyed helmet's colour, a
// lore line, a head's note sound.
func TestCtrlPickNestedStacksKeepComponents(t *testing.T) {
	h, players, pl := pickDataHub(t)
	chestState := worldgen.BlockBase("chest")
	src := simPos{blockPos: blockPos{2, 180, 0}}
	dst := simPos{blockPos: blockPos{0, 180, 2}}
	h.world.SetBlock(src.x, src.y, src.z, chestState)
	helmet := invStack{item: itemByName["leather_helmet"], count: 1, dmg: 4, color: 0x123456}
	lored := invStack{item: itemByName["stick"], count: 3}
	lored.tags.lore = "A stick of note"
	head := invStack{item: itemPlayerHead, count: 1, noteSound: testNoteSound}
	c := &chest{}
	c.slots[0], c.slots[1], c.slots[2] = helmet, lored, head
	h.chests[src] = c

	st := pickAndPlace(t, h, players, pl, src, dst, chestState)
	if !strings.Contains(st.beData, pickComponentsKey) {
		t.Fatalf("the copied stacks should carry their component patch: %s", st.beData)
	}
	got := h.chests[dst]
	if got == nil {
		t.Fatal("the placed chest is empty")
	}
	if s := got.slots[0]; s.item != helmet.item || s.dmg != 4 || s.color != 0x123456 {
		t.Errorf("the helmet lost its colour or wear: %+v", s)
	}
	if s := got.slots[1]; s.item != lored.item || s.count != 3 || s.tags.lore != "A stick of note" {
		t.Errorf("the stick lost its lore: %+v", s)
	}
	if s := got.slots[2]; s.item != itemPlayerHead || s.noteSound != testNoteSound {
		t.Errorf("the head lost its note sound: %+v", s)
	}
}

// A copied sign keeps its text (sign_text_front / back are item components
// in 26.3, not op-gated block entity data); the editor opens on that text,
// and not at all on a waxed sign (SignBlock.setPlacedBy).
func TestCtrlPickSignKeepsText(t *testing.T) {
	h, players, pl := pickDataHub(t)
	h.isOp = func(string) bool { return false }
	sign := worldgen.BlockBase("oak_sign")
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, sign)
	text := signData{Front: signSide{Lines: [4]string{"Hello", "", "world", ""}, Color: "red"}}
	h.signs.set(0, src.x, src.y, src.z, text)

	for i, waxed := range []bool{false, true} {
		sd := text
		sd.Waxed = waxed
		h.signs.set(0, src.x, src.y, src.z, sd)
		dst := simPos{blockPos: blockPos{0, 180, 2 + 2*i}}
		st := pickAndPlace(t, h, players, pl, src, dst, sign)
		if !strings.Contains(st.beData, "front_text") {
			t.Fatalf("the picked sign should carry its text: %q", st.beData)
		}
		drainEvs(pl.p)
		h.onSignPlaced(players, evSignPlaced{eid: pl.p.eid, x: dst.x, y: dst.y, z: dst.z, dim: 0})
		got, ok := h.signs.get(0, dst.x, dst.y, dst.z)
		if !ok || got.Front.Lines != text.Front.Lines || got.Front.Color != "red" || got.Waxed != waxed {
			t.Fatalf("waxed=%v: the placed sign should keep the copied text, got %+v", waxed, got)
		}
		editor := false
		for _, ev := range drainEvs(pl.p) {
			if _, ok := ev.(attachproto.SignEditor); ok {
				editor = true
			}
		}
		if editor == waxed {
			t.Errorf("waxed=%v: editor opened = %v", waxed, editor)
		}
	}
}

// A lectern's book is op-only block entity data: an operator's placement
// takes it and the lectern shows it (HAS_BOOK), anyone else's does not.
func TestCtrlPickLecternBook(t *testing.T) {
	h, players, pl := pickDataHub(t)
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, withProps(t, worldgen.BlockBase("lectern"), map[string]string{"has_book": "true"}))
	h.lecterns[src] = &lectern{book: invStack{item: itemWrittenBook, count: 1, name: "Rules"}}
	empty := withProps(t, worldgen.BlockBase("lectern"), map[string]string{"has_book": "false", "powered": "false"})

	h.isOp = func(string) bool { return false }
	dst := simPos{blockPos: blockPos{0, 180, 2}}
	st := pickAndPlace(t, h, players, pl, src, dst, empty)
	if !strings.Contains(st.beData, "Book") {
		t.Fatalf("the picked lectern should carry its Book: %q", st.beData)
	}
	if l := h.lecterns[dst]; l != nil && l.book.item != 0 {
		t.Fatal("a non-operator's placement loaded the lectern's book")
	}

	h.isOp = func(string) bool { return true }
	dst = simPos{blockPos: blockPos{0, 180, 4}}
	pickAndPlace(t, h, players, pl, src, dst, empty)
	l := h.lecterns[dst]
	if l == nil || l.book.item != itemWrittenBook || l.book.name != "Rules" {
		t.Fatalf("an operator's placement should load the book: %+v", l)
	}
	info, _ := worldgen.InfoForState(h.world.At(dst.x, dst.y, dst.z))
	if worldgen.GetProperty(info, h.world.At(dst.x, dst.y, dst.z), "has_book") != "true" {
		t.Error("the lectern should show its book (HAS_BOOK)")
	}
}

// A jukebox carries its disc and how far its song has played; the placed
// one holds the disc (HAS_RECORD) and carries on from there.
func TestCtrlPickJukebox(t *testing.T) {
	h, players, pl := pickDataHub(t)
	h.tick.Store(1000)
	cat := itemByName["music_disc_cat"]
	_, length, _ := jukeboxSongFor(cat)
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, jukeboxState(true))
	h.jukeboxes[src] = &jukebox{disc: invStack{item: cat, count: 1}, started: 900, length: length}

	dst := simPos{blockPos: blockPos{0, 180, 2}}
	st := pickAndPlace(t, h, players, pl, src, dst, jukeboxState(false))
	if !strings.Contains(st.beData, "RecordItem") || !strings.Contains(st.beData, "ticks_since_song_started") {
		t.Fatalf("the picked jukebox should carry its disc and song time: %q", st.beData)
	}
	j := h.jukeboxes[dst]
	if j == nil || j.disc.item != cat || j.started != 900 || j.length != length {
		t.Fatalf("the placed jukebox should hold the disc, 100 ticks in: %+v", j)
	}
	if h.world.At(dst.x, dst.y, dst.z) != jukeboxState(true) {
		t.Error("the placed jukebox should be has_record")
	}
}

// A campfire carries its four items and cooking times.
func TestCtrlPickCampfire(t *testing.T) {
	h, players, pl := pickDataHub(t)
	fire := worldgen.BlockBase("campfire")
	beef := itemByName["beef"]
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, fire)
	h.campfires[src] = &campfire{items: [4]int32{0, beef, 0, 0}, prog: [4]int{0, 55, 0, 0}, total: [4]int{0, 600, 0, 0}}

	dst := simPos{blockPos: blockPos{0, 180, 2}}
	pickAndPlace(t, h, players, pl, src, dst, fire)
	cf := h.campfires[dst]
	if cf == nil || cf.items[1] != beef || cf.prog[1] != 55 || cf.total[1] != 600 || cf.items[0] != 0 {
		t.Fatalf("the placed campfire should carry the beef and its cooking: %+v", cf)
	}
}

// A decorated pot carries its faces (pot_decorations) and its stack.
func TestCtrlPickDecoratedPot(t *testing.T) {
	h, players, pl := pickDataHub(t)
	pot := worldgen.BlockBase("decorated_pot")
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, pot)
	faces := potSherds{0, itemByName["angler_pottery_sherd"], 0, itemByName["skull_pottery_sherd"]}
	h.potSherds.set(src, faces)
	if h.pots == nil {
		h.pots = map[simPos]invStack{}
	}
	h.pots[src] = invStack{item: itemByName["diamond"], count: 3}

	dst := simPos{blockPos: blockPos{0, 180, 2}}
	st := pickAndPlace(t, h, players, pl, src, dst, pot)
	if st.sherds != faces {
		t.Errorf("the picked pot should wear the faces: %v", st.sherds)
	}
	if got := h.pots[dst]; got.item != itemByName["diamond"] || got.count != 3 {
		t.Fatalf("the placed pot should hold the diamonds: %+v", got)
	}
}

// A hive carries its bees and honey (bees + block_state) on a hive record;
// the source keeps its own, and a creative placement leaves the stack's
// record for the next.
func TestCtrlPickBeehive(t *testing.T) {
	h, players, pl := pickDataHub(t)
	if h.hives == nil {
		h.hives = map[simPos][]hiveOccupant{}
	}
	hive := withProps(t, worldgen.BlockBase("beehive"), map[string]string{"honey_level": "3"})
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, hive)
	h.hives[src] = []hiveOccupant{{SecsLeft: 100}, {SecsLeft: 20, Nectar: true}}

	pickVia(h, players, pl, attachproto.PickItem{X: int32(src.x), Y: int32(src.y), Z: int32(src.z), IncludeData: true})
	st := heldStack(pl)
	stow, ok := h.hiveItems[st.hiveID]
	if st.hiveID == 0 || !ok || len(stow.Occ) != 2 || stow.Honey != 3 {
		t.Fatalf("the picked hive should carry two bees and honey 3: id %d %+v", st.hiveID, stow)
	}
	if len(h.hives[src]) != 2 {
		t.Fatal("picking must not empty the source hive")
	}
	dst := blockPos{0, 180, 2}
	h.world.SetBlock(dst.x, dst.y, dst.z, worldgen.BlockBase("beehive"))
	h.restoreBeeHome(players, 0, dst, st.hiveID, true)
	if len(h.hives[simPos{blockPos: dst}]) != 2 {
		t.Fatal("the placed hive should take the bees")
	}
	if _, kept := h.hiveItems[st.hiveID]; !kept {
		t.Fatal("a creative placement should leave the stack's bees for the next")
	}
}

// A chiseled bookshelf carries its books, each whole, and its last slot.
func TestCtrlPickChiseledBookshelf(t *testing.T) {
	h, players, pl := pickDataHub(t)
	shelf := worldgen.BlockBase("chiseled_bookshelf")
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, shelf)
	enchanted := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	h.bookshelves[src] = &[6]invStack{0: {item: itemByName["book"], count: 1}, 3: enchanted}
	h.shelfLast[src] = 3

	dst := simPos{blockPos: blockPos{0, 180, 2}}
	pickAndPlace(t, h, players, pl, src, dst, shelf)
	got := h.bookshelves[dst]
	if got == nil || got[0].item != itemByName["book"] || got[3] != enchanted {
		t.Fatalf("the placed shelf should hold the books: %+v", got)
	}
	if h.shelfLast[dst] != 3 {
		t.Errorf("last_interacted_slot = %d, want 3", h.shelfLast[dst])
	}
}

// A player head's ctrl-pick carries its owner and note sound.
func TestCtrlPickPlayerHead(t *testing.T) {
	h, players, pl := pickDataHub(t)
	head := worldgen.BlockID("player_head")
	src := simPos{blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, head)
	prof := profileString(testProfile())
	h.skulls.set(src, prof)
	h.skulls.setNote(src, testNoteSound)
	pickVia(h, players, pl, attachproto.PickItem{X: int32(src.x), Y: int32(src.y), Z: int32(src.z), IncludeData: true})
	if st := heldStack(pl); st.item != itemPlayerHead || st.profile != prof || st.noteSound != testNoteSound {
		t.Fatalf("the picked head should carry profile and note sound: %+v", st)
	}
}
