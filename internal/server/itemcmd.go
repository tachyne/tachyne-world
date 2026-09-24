package server

import (
	"fmt"
	"strconv"
	"strings"
)

// /item (ItemCommands): put items into a block container's or a player's
// slots, either a named item or copies of another container's slots.
//
//	/item replace|fill|override block <pos>|entity <targets> <slots>
//	      with <item> [<count>]
//	      from block <pos>|entity <source> <slots>
//
// <slots> is a slot range (container.5, container.*, hotbar.0,
// inventory.*, weapon.offhand, armor.*, enderchest.3, player.cursor,
// player.crafting.0 …). replace hands the items out in order, one per slot,
// until they run out; fill cycles them over every slot; override is replace
// with every slot past the last item emptied.
//
// Not here yet: /item modify and the "modifier" tail (loot item
// functions), slot sources other than a plain range, item components in
// the item argument, and mobs as targets or sources (the command refuses
// rather than skip them). Block targets are the storage blocks: chests,
// barrels, shulker boxes, furnaces, dispensers, droppers, hoppers, brewing
// stands and crafters.

// slotRanges is SlotRanges: every range name and the slot ids it covers.
var slotRanges = func() map[string][]int {
	m := map[string][]int{}
	single := func(name string, id int) { m[name] = []int{id} }
	span := func(prefix string, off, n int) {
		all := make([]int, n)
		for i := 0; i < n; i++ {
			m[prefix+strconv.Itoa(i)] = []int{off + i}
			all[i] = off + i
		}
		m[prefix+"*"] = all
	}
	single("contents", 0)
	span("container.", 0, 54)
	span("hotbar.", 0, 9)
	span("inventory.", 9, 27)
	span("enderchest.", 200, 27)
	span("mob.inventory.", 300, 8)
	span("horse.", 500, 15)
	single("weapon", 98)
	single("weapon.mainhand", 98)
	single("weapon.offhand", 99)
	m["weapon.*"] = []int{98, 99}
	single("armor.head", 103)
	single("armor.chest", 102)
	single("armor.legs", 101)
	single("armor.feet", 100)
	single("armor.body", 105)
	m["armor.*"] = []int{103, 102, 101, 100, 105}
	single("saddle", 106)
	single("horse.chest", 499)
	single("player.cursor", 499)
	span("player.crafting.", 500, 4)
	return m
}()

// slotAccess is SlotAccess: one slot's read and (possibly refused) write.
type slotAccess struct {
	get func() invStack
	set func(invStack) bool
}

// itemProvider is ItemProvider over replace's list, fill's cycle, or
// override's list-then-empty.
type itemProvider struct {
	items    []invStack
	pos      int
	cycle    bool
	fallback bool // override: empty stacks once the list runs out
}

func (ip *itemProvider) restart() { ip.pos = 0 }
func (ip *itemProvider) hasNext() bool {
	return ip.fallback || (ip.cycle && len(ip.items) > 0) || ip.pos < len(ip.items)
}
func (ip *itemProvider) next() invStack {
	var st invStack
	switch {
	case ip.cycle:
		st = ip.items[ip.pos%len(ip.items)]
	case ip.pos < len(ip.items):
		st = ip.items[ip.pos]
	}
	ip.pos++
	return st
}

// replaceSlots is SlotCollection.replaceSlotItems with ANY_SLOT tracking:
// every slot tried counts as selected, and a slot counts as replaced when
// its write is accepted.
func (h *hub) replaceSlots(slots []slotAccess, items *itemProvider, selected *int) int {
	n := 0
	for _, s := range slots {
		if !items.hasNext() {
			break
		}
		*selected++
		if s.set(h.forkStack(items.next())) {
			n++
		}
	}
	return n
}

// itemTarget is one resolved holder of slots.
type itemTarget struct {
	name    string    // an entity's display name (players)
	pos     *blockPos // a block's position
	slotAt  func(id int) *slotAccess
	changed func(players map[int32]*tracked)
}

func (t itemTarget) slots(ids []int) []slotAccess {
	var out []slotAccess
	for _, id := range ids {
		if s := t.slotAt(id); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func stackSlot(p *invStack, clamp bool) *slotAccess {
	return &slotAccess{get: func() invStack { return *p }, set: func(s invStack) bool {
		if clamp && s.item != 0 && s.count > stackCap(s.item) { // BaseContainerBlockEntity.setItem limits the size
			s.count = stackCap(s.item)
		}
		if s.item == 0 || s.count <= 0 {
			s = invStack{}
		}
		*p = s
		return true
	}}
}

// blockItemTarget is the container at pos, or false when the block holds
// no container.
func (h *hub) blockItemTarget(dim int, pos blockPos) (itemTarget, bool) {
	sp := simPos{dim: dim, blockPos: pos}
	st := h.worldFor(dim).At(pos.x, pos.y, pos.z)
	var slots []*invStack
	switch {
	case isChestBlock(st) || isBarrel(st) || isShulkerBox(st):
		c := h.chests[sp]
		if c == nil {
			c = &chest{}
			h.fillStructureChestIn(dim, pos, c) // RandomizableContainer: the loot unpacks on first touch
			h.chests[sp] = c
		}
		for i := range c.slots {
			slots = append(slots, &c.slots[i])
		}
	case isDispenser(st) || isDropper(st) || isHopper(st) || isBrewStand(st) || isCrafter(st):
		b := h.binAt(sp, st)
		for i := range b.slots {
			slots = append(slots, &b.slots[i])
		}
	default:
		kind, ok := furnaceKindOf(st)
		if !ok {
			return itemTarget{}, false
		}
		f := h.furnaces[sp]
		if f == nil {
			f = &furnace{cookMax: 200, kind: kind}
			h.furnaces[sp] = f
		}
		for i := range f.slots {
			slots = append(slots, &f.slots[i])
		}
	}
	p := pos
	return itemTarget{pos: &p,
		slotAt: func(id int) *slotAccess {
			if id < 0 || id >= len(slots) {
				return nil
			}
			return stackSlot(slots[id], true)
		},
		changed: func(players map[int32]*tracked) { h.refreshBinViewers(players, sp) },
	}, true
}

// playerItemTarget is Player.getSlot over the engine's player model.
func (h *hub) playerItemTarget(t *tracked) itemTarget {
	armorFits := func(slot int) func(invStack) bool { // chest, legs, feet take only their own piece
		return func(s invStack) bool { return s.item == 0 || equipSlotOnUse(s.item) == slot }
	}
	return itemTarget{name: t.p.name,
		slotAt: func(id int) *slotAccess {
			switch {
			case id >= 0 && id < invSize && t.inv != nil:
				return stackSlot(&t.inv.slots[id], false)
			case id == 98 && t.inv != nil:
				return stackSlot(&t.inv.slots[t.p.heldSlot()], false)
			case id == 99:
				return stackSlot(&t.offhand, false)
			case id == 103: // head takes anything
				return stackSlot(&t.armor[0], false)
			case id >= 100 && id <= 102:
				slot := 103 - id
				s := stackSlot(&t.armor[slot], false)
				set, fits := s.set, armorFits(slot)
				s.set = func(st invStack) bool { return fits(st) && set(st) }
				return s
			case id >= 200 && id < 227:
				return stackSlot(&t.enderChest().slots[id-200], false)
			case id == 499:
				return stackSlot(&t.cursor, false)
			case id >= 500 && id < 504:
				return stackSlot(&t.craft[id-500], false)
			}
			return nil
		},
		changed: func(players map[int32]*tracked) {
			h.sendInventory(t) // the whole of window 0, cursor and crafting included
			t.p.setOffhand(t.offhand.item)
			t.refreshArmorAttrs()
			h.broadcastEquipment(players, t)
			if t.winKind == winChest && t.viewChest != nil && t.viewChest == t.ender {
				h.sendChestWindow(t, t.ender)
			}
		},
	}
}

const itemUsage = "Usage: /item replace|fill|override block <pos>|entity <targets> <slots> with <item> [count] | from block <pos>|entity <source> <slots>"

func (s *Server) cmdItem(p *player, args []string) {
	if !s.isOp(p.name) { // ItemCommands: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		t := players[p.eid]
		if t == nil {
			return
		}
		if msg := s.hub.itemCommand(players, t, args); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// itemDisplay is ItemStack.getDisplayName in plain text: the item's name in
// square brackets.
func itemDisplay(item int32) string {
	parts := strings.Split(itemNameOf[item], "_")
	for i, w := range parts {
		if w != "" {
			parts[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// itemHolder reads "block <pos>" or "entity <selector>" from args.
func (h *hub) itemHolder(players map[int32]*tracked, t *tracked, args []string, source bool) ([]itemTarget, []string, string) {
	if len(args) < 2 {
		return nil, nil, itemUsage
	}
	switch args[0] {
	case "block":
		if len(args) < 4 {
			return nil, nil, itemUsage
		}
		x, y, z, ok := parsePosition(args[1:4], t.x, t.y, t.z, t.yaw, t.pitch)
		if !ok {
			return nil, nil, itemUsage
		}
		pos := blockPos{floorInt(x), floorInt(y), floorInt(z)}
		if !h.cloneLoaded(t.dim, pos, pos) {
			return nil, nil, "That position is not loaded"
		}
		if !h.inWorldYIn(t.dim, pos.y) {
			return nil, nil, "That position is out of this world!"
		}
		tg, ok := h.blockItemTarget(t.dim, pos)
		if !ok {
			which := "Target"
			if source {
				which = "Source"
			}
			return nil, nil, fmt.Sprintf("%s position %d, %d, %d is not a container", which, pos.x, pos.y, pos.z)
		}
		return []itemTarget{tg}, args[4:], ""
	case "entity":
		if len(h.commandMobs(players, t.p.eid, args[1])) > 0 {
			return nil, nil, "Only players' slots can be set or read by /item for now"
		}
		ps := h.commandTargets(players, t.p.eid, args[1])
		if len(ps) == 0 {
			return nil, nil, "No entity was found"
		}
		out := make([]itemTarget, len(ps))
		for i, pt := range ps {
			out[i] = h.playerItemTarget(pt)
		}
		return out, args[2:], ""
	}
	return nil, nil, itemUsage
}

// itemCommand runs one /item on the hub; a non-empty string is the failure.
func (h *hub) itemCommand(players map[int32]*tracked, t *tracked, args []string) string {
	if len(args) == 0 {
		return itemUsage
	}
	mode := args[0]
	switch mode {
	case "replace", "fill", "override":
	case "modify":
		return "/item modify is not supported yet"
	default:
		return itemUsage
	}
	targets, rest, msg := h.itemHolder(players, t, args[1:], false)
	if msg != "" {
		return msg
	}
	if len(rest) < 2 {
		return itemUsage
	}
	slotName := rest[0]
	ids, ok := slotRanges[slotName]
	if !ok {
		return fmt.Sprintf("Unknown slot '%s'", slotName)
	}
	var items []invStack
	known := int32(0)
	switch rest[1] {
	case "with":
		if len(rest) < 3 || len(rest) > 4 {
			return itemUsage
		}
		name := rest[2]
		if strings.ContainsAny(name, "[{") {
			return "Item components are not supported by /item yet"
		}
		item, found := itemByName[strings.TrimPrefix(name, "minecraft:")]
		if !found {
			return fmt.Sprintf("Unknown item '%s'", name)
		}
		count := 1
		if len(rest) == 4 {
			n, err := strconv.Atoi(rest[3])
			if err != nil || n < 1 || n > 99 {
				return "Integer must be between 1 and 99, found " + rest[3]
			}
			count = n
		}
		items, known = []invStack{{item: item, count: count}}, item
	case "from":
		srcs, tail, msg := h.itemHolder(players, t, rest[2:], true)
		if msg != "" {
			return msg
		}
		if len(tail) != 1 {
			if len(tail) > 1 {
				return "Item modifiers are not supported by /item yet"
			}
			return itemUsage
		}
		srcIDs, ok := slotRanges[tail[0]]
		if !ok {
			return fmt.Sprintf("Unknown slot '%s'", tail[0])
		}
		for _, src := range srcs {
			for _, sl := range src.slots(srcIDs) {
				items = append(items, sl.get())
			}
		}
		if len(items) == 0 {
			return fmt.Sprintf("The source does not have slot %s", tail[0])
		}
	default:
		return itemUsage
	}

	prov := &itemProvider{items: items, cycle: mode == "fill", fallback: mode == "override"}
	selected := 0
	type result struct {
		tg    itemTarget
		count int
	}
	var results []result
	for _, tg := range targets {
		prov.restart()
		n := h.replaceSlots(tg.slots(ids), prov, &selected)
		results = append(results, result{tg, n})
		if n > 0 {
			tg.changed(players)
		}
	}
	if selected == 0 {
		return fmt.Sprintf("The target does not have slot %s", slotName)
	}
	// CommandResponseTracker over the targets that changed (NON_ZERO).
	total, nonZero := 0, []result{}
	for _, r := range results {
		total += r.count
		if r.count > 0 {
			nonZero = append(nonZero, r)
		}
	}
	if len(nonZero) == 0 {
		if known != 0 {
			return fmt.Sprintf("No targets accepted item %s into specified slots", itemDisplay(known))
		}
		return "No targets accepted items into specified slots"
	}
	with := ""
	if known != 0 {
		with = " with " + itemDisplay(known)
	}
	var line string
	switch r := nonZero[0]; {
	case r.tg.pos != nil:
		line = fmt.Sprintf("Replaced %d slot(s) at %d, %d, %d%s", total, r.tg.pos.x, r.tg.pos.y, r.tg.pos.z, with)
	case len(nonZero) == 1:
		line = fmt.Sprintf("Replaced %d slot(s) on %s%s", total, r.tg.name, with)
	default:
		line = fmt.Sprintf("Replaced slot(s) on %d entities%s", len(nonZero), with)
	}
	h.cmdSuccess(players, t.p, line, true)
	return ""
}
