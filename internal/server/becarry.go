package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Carrying a block's block-entity data from one cell to another, for the
// commands that move or copy blocks wholesale (/clone). The engine keeps
// each kind of block entity in its own hub map; carriedBE gathers whatever
// a cell has across them, so it can be lifted without spilling and set
// down elsewhere.
//
// Carried: chest/barrel/shulker-box storage, furnace state, the dispenser /
// dropper / hopper / brewing-stand / crafter storage (and a brewing stand's
// progress), a jukebox's disc, a lectern's book, both kinds of shelf, a
// decorated pot's item and faces, a campfire's cooking, sign text and
// banner patterns.
type carriedBE struct {
	chest     *chest
	furnace   *furnace
	bin       *bin
	brew      *[3]int32 // progress, fuel, ingredient
	jukebox   *jukebox
	lectern   *lectern
	shelf     *[6]invStack
	shelfLast *int
	woodShelf *[3]invStack
	pot       *invStack
	sherds    *potSherds
	campfire  *campfire
	sign      *signData
	banner    []attachproto.BannerLayer
}

func (c carriedBE) empty() bool {
	return c.chest == nil && c.furnace == nil && c.bin == nil && c.brew == nil &&
		c.jukebox == nil && c.lectern == nil && c.shelf == nil && c.woodShelf == nil &&
		c.pot == nil && c.sherds == nil && c.campfire == nil && c.sign == nil && c.banner == nil
}

// peekBlockEntity copies a cell's block-entity data, leaving the cell as it
// is. With fork set every stack that points at a stored record (a shulker
// box, a bundle, a book, a carried hive) gets a record of its own, as a
// copied item does; without it the stacks keep their records, for a move.
func (h *hub) peekBlockEntity(pos simPos, fork bool) carriedBE {
	var c carriedBE
	st := func(s invStack) invStack {
		if fork {
			return h.forkStack(s)
		}
		return s
	}
	if ch := h.chests[pos]; ch != nil {
		cp := *ch
		for i := range cp.slots {
			cp.slots[i] = st(cp.slots[i])
		}
		c.chest = &cp
	}
	if f := h.furnaces[pos]; f != nil {
		cp := *f
		cp.viewers = nil // nobody has the copy's window open
		for i := range cp.slots {
			cp.slots[i] = st(cp.slots[i])
		}
		c.furnace = &cp
	}
	if b := h.bins[pos]; b != nil {
		cp := *b
		cp.slots = make([]invStack, len(b.slots))
		for i, s := range b.slots {
			cp.slots[i] = st(s)
		}
		c.bin = &cp
	}
	if prog, ok := h.brewProg[pos]; ok || h.brewFuel[pos] != 0 || h.brewIng[pos] != 0 {
		c.brew = &[3]int32{int32(prog), int32(h.brewFuel[pos]), h.brewIng[pos]}
	}
	if j := h.jukeboxes[pos]; j != nil {
		cp := *j
		cp.disc = st(cp.disc)
		c.jukebox = &cp
	}
	if l := h.lecterns[pos]; l != nil {
		cp := *l
		cp.book = st(cp.book)
		c.lectern = &cp
	}
	if sh := h.bookshelves[pos]; sh != nil {
		cp := *sh
		for i := range cp {
			cp[i] = st(cp[i])
		}
		c.shelf = &cp
		if last, ok := h.shelfLast[pos]; ok {
			c.shelfLast = &last
		}
	}
	if sh := h.woodShelves[pos]; sh != nil {
		cp := *sh
		for i := range cp {
			cp[i] = st(cp[i])
		}
		c.woodShelf = &cp
	}
	if p, ok := h.pots[pos]; ok {
		cp := st(p)
		c.pot = &cp
	}
	if h.potSherds != nil {
		if sh, ok := h.potSherds.get(pos.dim, pos.x, pos.y, pos.z); ok {
			c.sherds = &sh
		}
	}
	if cf := h.campfires[pos]; cf != nil {
		cp := *cf
		c.campfire = &cp
	}
	if h.signs != nil {
		if sd, ok := h.signs.get(pos.dim, pos.x, pos.y, pos.z); ok {
			c.sign = &sd
		}
	}
	if h.banners != nil {
		if l := h.banners.get(pos.dim, pos.x, pos.y, pos.z); len(l) > 0 {
			c.banner = append([]attachproto.BannerLayer(nil), l...)
		}
	}
	return c
}

// discardBlockEntity removes a cell's block-entity data without dropping
// anything: the block is being overwritten wholesale, as a clone does with
// drops suppressed.
func (h *hub) discardBlockEntity(pos simPos) {
	delete(h.chests, pos)
	delete(h.furnaces, pos)
	delete(h.bins, pos)
	delete(h.brewProg, pos)
	delete(h.brewFuel, pos)
	delete(h.brewIng, pos)
	delete(h.jukeboxes, pos)
	delete(h.lecterns, pos)
	delete(h.bookshelves, pos)
	delete(h.shelfLast, pos)
	if h.woodShelves[pos] != nil {
		delete(h.woodShelves, pos)
		if h.shelfView != nil {
			h.shelfView.remove(pos)
		}
	}
	delete(h.pots, pos)
	if h.potSherds != nil {
		h.potSherds.remove(pos)
	}
	delete(h.campfires, pos)
	if h.cfStore != nil {
		h.cfStore.remove(pos)
	}
	if h.signs != nil {
		h.signs.remove(pos.dim, pos.x, pos.y, pos.z)
	}
	if h.banners != nil {
		h.banners.remove(pos)
	}
}

// placeBlockEntity sets carried data down at a cell whose block has just
// been written, and shows the viewers what they can see of it.
func (h *hub) placeBlockEntity(players map[int32]*tracked, pos simPos, c carriedBE, state uint32) {
	if c.chest != nil {
		h.chests[pos] = c.chest
	}
	if c.furnace != nil {
		h.furnaces[pos] = c.furnace
	}
	if c.bin != nil {
		h.bins[pos] = c.bin
		if isHopper(state) {
			h.registerHopper(pos)
		}
	}
	if c.brew != nil {
		h.brewProg[pos], h.brewFuel[pos], h.brewIng[pos] = int(c.brew[0]), int(c.brew[1]), c.brew[2]
	}
	if c.jukebox != nil {
		h.jukeboxes[pos] = c.jukebox
	}
	if c.lectern != nil {
		h.lecterns[pos] = c.lectern
	}
	if c.shelf != nil {
		h.bookshelves[pos] = c.shelf
		if c.shelfLast != nil {
			h.shelfLast[pos] = *c.shelfLast
		}
	}
	if c.woodShelf != nil {
		h.woodShelves[pos] = c.woodShelf
		h.shelfSync(players, pos)
	}
	if c.pot != nil {
		if h.pots == nil {
			h.pots = map[simPos]invStack{}
		}
		h.pots[pos] = *c.pot
	}
	if c.sherds != nil && h.potSherds != nil {
		h.potSherds.set(pos, *c.sherds)
	}
	if c.campfire != nil {
		h.campfires[pos] = c.campfire
		h.campfireSync(players, pos, c.campfire)
	}
	if c.sign != nil && h.signs != nil {
		h.signs.set(pos.dim, pos.x, pos.y, pos.z, *c.sign)
		h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), signTextEv(pos.x, pos.y, pos.z, *c.sign))
	}
	if c.banner != nil && h.banners != nil {
		h.banners.set(pos, c.banner)
		h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), attachproto.BannerPatterns{
			X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Layers: c.banner})
	}
}

// forkStack is a stack's independent copy: a stack that keeps its contents
// in a stored record (a shulker box's slots, a bundle's items, a book's
// pages, a carried hive's bees) gets a fresh record holding the same, so
// the copy and the original no longer share one.
func (h *hub) forkStack(s invStack) invStack {
	if s.item == 0 {
		return s
	}
	if s.boxID != 0 && h.boxes != nil {
		if c, ok := h.boxes.get(s.boxID); ok {
			for i := range c.slots {
				c.slots[i] = h.forkStack(c.slots[i])
			}
			id := h.boxes.mint()
			h.boxes.set(id, c)
			s.boxID = id
		}
	}
	if s.bundleID != 0 && h.bundles != nil {
		if items := h.bundles.get(s.bundleID); len(items) > 0 {
			cp := make([]invStack, len(items))
			for i, it := range items {
				cp[i] = h.forkStack(it)
			}
			id := h.bundles.mint()
			h.bundles.set(id, cp)
			s.bundleID = id
		}
	}
	if s.bookID != 0 && h.books != nil {
		if b, ok := h.books.get(s.bookID); ok {
			b.Pages = append([]string(nil), b.Pages...)
			s.bookID = h.books.create(b)
		}
	}
	if s.hiveID != 0 && h.hiveItems != nil { // nil until a hive is first carried
		if hv, ok := h.hiveItems[s.hiveID]; ok {
			hv.Occ = append([]hiveOccupant(nil), hv.Occ...)
			h.nextHiveID++
			h.hiveItems[h.nextHiveID] = hv
			s.hiveID = h.nextHiveID
		}
	}
	return s
}
