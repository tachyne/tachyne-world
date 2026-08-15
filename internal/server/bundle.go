package server

import (
	"sync"
	"sync/atomic"

	"github.com/tachyne/tachyne-common/protocol"
)

// Bundles — a pouch that holds a stack's worth of anything.
//
// Ported from BundleItem + BundleContents. Contents live in the hub's bundle
// store keyed by the stack's bundleID, not on the stack itself: invStack must
// stay comparable, and a bundle can be dropped, stored in a chest or put inside
// another bundle, so its contents have to travel with the item. Maps, books,
// shulker boxes and Silk-Touched hives all use the same indirection.
//
// WEIGHT is vanilla's rule, in exact integer units instead of its Fraction.
// Each item costs 1/maxStackSize of the pouch, so sixty-four dirt or sixteen
// ender pearls or one bucket all fill it exactly. Working in 1/64ths makes
// every real weight a whole number (1, 4 and 64 units for the 64-, 16- and
// 1-stacking items), which avoids the float drift that would otherwise let a
// bundle end up a hair over or under full.
const (
	bundleCapacity = 64 // 1/64 units: a full pouch, vanilla's Fraction.ONE
	// BundleContents.BUNDLE_IN_BUNDLE_WEIGHT: a nested bundle costs 1/16 on
	// top of whatever it contains, so pouches cannot be nested forever.
	bundleInBundleWeight = bundleCapacity / 16
	// BundleContents.BEEHIVE_WEIGHT: a hive with bees inside fills a bundle by
	// itself, whatever it would otherwise weigh.
	bundleHiveWeight = bundleCapacity
	// The client tooltip draws at most this many entries.
	bundleMaxShown = 8
)

// bundleStore holds every bundle's contents, keyed by bundleID. It carries its
// own lock for the same reason bookStore does: the free stackComponents has to
// compose the contents component without a hub in reach.
type bundleStore struct {
	mu    sync.Mutex
	items map[int32][]invStack
	// The stack each pouch will hand back next. Vanilla keeps this out of the
	// contents codec, so it is never sent to a client — each side tracks its
	// own, and the client tells us when the player scrolls.
	selected map[int32]int
	lastID   int32
}

func newBundleStore() *bundleStore {
	return &bundleStore{items: map[int32][]invStack{}, selected: map[int32]int{}}
}

func (s *bundleStore) sel(id int32) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected == nil {
		return -1
	}
	if v, ok := s.selected[id]; ok {
		return v
	}
	return -1
}

// setSel is BundleContents.Mutable.toggleSelectedItem: selecting what is
// already selected, or an index out of range, clears it.
func (s *bundleStore) setSel(id int32, i int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected == nil {
		s.selected = map[int32]int{}
	}
	if cur, ok := s.selected[id]; (ok && cur == i) || i < 0 || i >= len(s.items[id]) {
		delete(s.selected, id)
		return
	}
	s.selected[id] = i
}

func (s *bundleStore) get(id int32) []invStack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[id]
}

func (s *bundleStore) set(id int32, items []invStack) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(items) == 0 {
		delete(s.items, id)
		return
	}
	s.items[id] = items
}

func (s *bundleStore) mint() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastID++
	return s.lastID
}

// globalBundles lets stackComponents reach the contents, exactly as
// globalBooks does for written books.
var globalBundles atomic.Pointer[bundleStore]

func (h *hub) initBundles(bs *bundleStore) {
	h.bundles = bs
	globalBundles.Store(bs)
}

// itemWeight is BundleContents.getWeight: what one of this item costs.
func (h *hub) itemWeight(st invStack) int {
	// A bundle weighs what it carries plus 1/16 — and that holds for an EMPTY
	// one too, which is why this asks whether the ITEM is a bundle rather than
	// whether it has contents. Vanilla tests for the component, and an empty
	// pouch still carries BundleContents.EMPTY; keying on the id instead let an
	// empty pouch fall through to its stack size and weigh a full pouch.
	if isBundle(st.item) {
		return h.bundleWeight(st.bundleID) + bundleInBundleWeight
	}
	if st.hiveID != 0 {
		if stow, ok := h.hiveItems[st.hiveID]; ok && len(stow.Occ) > 0 {
			return bundleHiveWeight
		}
	}
	cap := stackCap(st.item)
	if cap <= 0 {
		cap = 1
	}
	// Exact for the 1/16/64 stack sizes every vanilla item uses. An unexpected
	// size rounds UP, which refuses one item early rather than overfilling.
	return (bundleCapacity + cap - 1) / cap
}

// bundleWeight totals what a bundle is carrying.
func (h *hub) bundleWeight(id int32) int {
	total := 0
	for _, st := range h.bundles.get(id) {
		total += h.itemWeight(st) * st.count
	}
	return total
}

// canGoInBundle is BundleContents.canItemBeInBundle: anything that may live in
// a container item. A bundle cannot hold itself, and vanilla refuses items
// whose own contents make them too heavy.
func (h *hub) canGoInBundle(st invStack) bool {
	return st.count > 0 && st.item != 0 && h.itemWeight(st) <= bundleCapacity
}

// bundleRoomFor is getMaxAmountToAdd: how many of this item still fit.
func (h *hub) bundleRoomFor(id int32, st invStack) int {
	w := h.itemWeight(st)
	if w <= 0 {
		return 0
	}
	free := bundleCapacity - h.bundleWeight(id)
	if free <= 0 {
		return 0
	}
	return free / w
}

// newBundleID mints an id for a bundle that is about to hold something. A
// bundle with nothing in it never needs one.
func (h *hub) newBundleID() int32 { return h.bundles.mint() }

// bundleInsert is BundleContents.Mutable.tryInsert: put as much of `add` into
// the bundle as will fit, merging with a matching stack already inside.
// Returns how many went in, and the (possibly new) bundle id.
func (h *hub) bundleInsert(id int32, add invStack) (int, int32) {
	if !h.canGoInBundle(add) {
		return 0, id
	}
	if id == 0 {
		id = h.newBundleID()
	}
	room := h.bundleRoomFor(id, add)
	n := add.count
	if room < n {
		n = room
	}
	if n <= 0 {
		return 0, id
	}
	items := h.bundles.get(id)
	// Vanilla merges into the matching stack and moves it to the FRONT, so the
	// most recently touched item is what comes back out first.
	key := add
	key.count = 0
	for i, st := range items {
		probe := st
		probe.count = 0
		if probe == key {
			merged := st
			merged.count = st.count + n
			items = append(items[:i], items[i+1:]...)
			h.bundles.set(id, append([]invStack{merged}, items...))
			return n, id
		}
	}
	one := add
	one.count = n
	h.bundles.set(id, append([]invStack{one}, items...))
	return n, id
}

// bundleRemoveOne is BundleContents.Mutable.removeOne: take out the selected
// stack, or the front one when nothing is selected.
func (h *hub) bundleRemoveOne(id int32, selected int) (invStack, bool) {
	items := h.bundles.get(id)
	if len(items) == 0 {
		return invStack{}, false
	}
	i := selected
	if i < 0 || i >= len(items) {
		i = 0
	}
	out := items[i]
	h.bundles.set(id, append(items[:i:i], items[i+1:]...))
	h.bundles.setSel(id, -1) // removeOne clears the selection
	return out, true
}

// bundleComponentBytes composes the bundle_contents component: a varint count
// followed by that many full Slots, which is exactly what appendStack writes.
func bundleComponentBytes(id int32) []byte {
	bs := globalBundles.Load()
	if bs == nil {
		return protocol.AppendVarInt(nil, 0)
	}
	items := bs.get(id)
	b := protocol.AppendVarInt(nil, int32(len(items)))
	for _, st := range items {
		b = appendStack(b, st)
	}
	return b
}

// isBundle reports whether an item is one of the seventeen bundles.
func isBundle(item int32) bool { return bundleItems[item] }

var bundleItems = func() map[int32]bool {
	names := []string{
		"bundle", "black_bundle", "blue_bundle", "brown_bundle", "cyan_bundle",
		"gray_bundle", "green_bundle", "light_blue_bundle", "light_gray_bundle",
		"lime_bundle", "magenta_bundle", "orange_bundle", "pink_bundle",
		"purple_bundle", "red_bundle", "white_bundle", "yellow_bundle",
	}
	out := make(map[int32]bool, len(names))
	for _, n := range names {
		if id, ok := itemByName[n]; ok {
			out[id] = true
		}
	}
	return out
}()

// tryBundleClick is BundleItem.overrideStackedOnOther / overrideOtherStackedOnMe:
// the two clicks that move items in and out of a pouch.
//
// It runs BEFORE the ordinary click reconciliation and, when it acts, resyncs
// the window instead of trusting the client's declared slot states. It has to:
// a bundle's contents are server state the client cannot describe, so the
// declared outcome of "left-click a stack with a bundle on the cursor" is a
// slot that simply emptied, which the conservation tally would otherwise read
// as items being thrown away.
//
// Vanilla's two directions:
//   - bundle on the CURSOR, clicked onto a slot: left inserts that slot's
//     stack, right (on an empty slot) drops one stack out of the pouch.
//   - bundle IN the slot, item on the cursor: left puts the cursor's stack in.
func (h *hub) tryBundleClick(players map[int32]*tracked, t *tracked, e evClick) bool {
	if e.mode != 0 || e.slot < 0 || int(e.slot) >= len(t.inv.slots) {
		return false // only plain clicks; drag/shift/hotbar modes are not bundle actions
	}
	slot := &t.inv.slots[e.slot]
	cursor := t.cursor

	switch {
	case isBundle(cursor.item) && cursor.count == 1 && e.button == 0 && slot.count > 0:
		// Left-click a stack with the pouch in hand: as much as fits goes in.
		n, id := h.bundleInsert(cursor.bundleID, *slot)
		if n == 0 {
			h.playSound(players, "minecraft:item.bundle.insert_fail", sndPlayer, t.x, t.y, t.z, 0.8, 1)
			h.resyncWindow(t)
			return true
		}
		cursor.bundleID = id
		t.cursor = cursor
		if slot.count -= n; slot.count <= 0 {
			*slot = invStack{}
		}
		h.playSound(players, "minecraft:item.bundle.insert", sndPlayer, t.x, t.y, t.z, 0.8, 1)

	case isBundle(cursor.item) && cursor.count == 1 && e.button == 1 && slot.count == 0:
		// Right-click an empty slot with the pouch in hand: one stack comes out.
		out, ok := h.bundleRemoveOne(cursor.bundleID, h.bundles.sel(cursor.bundleID))
		if !ok {
			return false
		}
		*slot = out
		h.playSound(players, "minecraft:item.bundle.remove_one", sndPlayer, t.x, t.y, t.z, 0.8, 1)

	case isBundle(slot.item) && slot.count == 1 && e.button == 0 && cursor.count > 0:
		// Pouch sitting in a slot, item on the cursor: the cursor's stack goes in.
		n, id := h.bundleInsert(slot.bundleID, cursor)
		if n == 0 {
			h.playSound(players, "minecraft:item.bundle.insert_fail", sndPlayer, t.x, t.y, t.z, 0.8, 1)
			h.resyncWindow(t)
			return true
		}
		slot.bundleID = id
		if cursor.count -= n; cursor.count <= 0 {
			cursor = invStack{}
		}
		t.cursor = cursor
		h.playSound(players, "minecraft:item.bundle.insert", sndPlayer, t.x, t.y, t.z, 0.8, 1)

	default:
		return false
	}
	h.resyncWindow(t)
	return true
}

// evBundleSelect is the player scrolling through a pouch's contents.
type evBundleSelect struct {
	eid      int32
	slot     int32
	selected int32
}

func (evBundleSelect) isHubEvent() {}

// selectBundleItem records which stack a pouch hands back next.
func (h *hub) selectBundleItem(t *tracked, slot, selected int32) {
	if t.inv == nil || slot < 0 || int(slot) >= len(t.inv.slots) {
		return
	}
	st := t.inv.slots[slot]
	if !isBundle(st.item) || st.bundleID == 0 {
		return
	}
	h.bundles.setSel(st.bundleID, int(selected))
}
