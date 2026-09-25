package server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /loot (LootCommand): roll a loot table and send what it gives somewhere.
//
//	/loot give <players> | insert <pos> | spawn <pos>
//	      | replace block <pos> <slot> [<count>] | replace entity <targets> <slot> [<count>]
//	      loot <table> | kill <target> | mine <pos> [<tool>|mainhand|offhand]
//
// The tables are the engine's baked ones: the chest, barrel, dispenser,
// archaeology, equipment and gameplay tables for "loot"; each mob's death
// table for "kill" (rolled as a magic kill by the caller, with the caller's
// Looting); each block's table for "mine" (with the tool's Silk Touch and
// Fortune); the fishing tables for "fish" (gameplay/fishing and its fish,
// junk and treasure pools, rolled with no hook and no luck). Not here yet:
// tables the engine has not baked, and mobs as "replace entity" targets.
//
// What was used is counted as vanilla's CommandResponseTracker counts it —
// one per stack that went somewhere — and answered only to the caller.

const lootUsage = "Usage: /loot give <players>|insert <pos>|spawn <pos>|replace block <pos> <slot> [count]|replace entity <targets> <slot> [count] loot <table>|kill <target>|mine <pos> [tool|mainhand|offhand]"

var lootSources = map[string]bool{"loot": true, "kill": true, "mine": true, "fish": true}

func (s *Server) cmdLoot(p *player, args []string) {
	if !s.isOp(p.name) { // LootCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		t := players[p.eid]
		if t == nil {
			return
		}
		if msg := s.hub.lootCommand(players, t, args); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// lootRoll is what a source produced, and the table it names in feedback
// ("" for "loot <table>", which vanilla answers without the table).
type lootRoll struct {
	drops []invStack
	table string
}

// lootCommand runs one /loot on the hub; a non-empty string is the failure.
func (h *hub) lootCommand(players map[int32]*tracked, t *tracked, args []string) string {
	if len(args) < 2 {
		return lootUsage
	}
	// Split the target part from the source part: the source starts at the
	// first source keyword after the target's own arguments.
	var target, source []string
	start := 0
	switch args[0] {
	case "give":
		start = 2
	case "insert", "spawn":
		start = 4
	case "replace":
		if len(args) < 2 {
			return lootUsage
		}
		switch args[1] {
		case "block":
			start = 6
		case "entity":
			start = 4
		default:
			return lootUsage
		}
		if len(args) > start && !lootSources[args[start]] {
			start++ // the optional count
		}
	default:
		return lootUsage
	}
	if len(args) <= start || !lootSources[args[start]] {
		return lootUsage
	}
	target, source = args[:start], args[start:]

	roll, msg := h.lootSource(players, t, source)
	if msg != "" {
		return msg
	}
	var used []invStack // one per stack that went somewhere (the tracker's elements)
	track := func(st invStack) { used = append(used, st) }
	if msg := h.lootTarget(players, t, target, roll.drops, track); msg != "" {
		return msg
	}
	var line string
	switch {
	case len(used) == 1 && roll.table != "":
		line = fmt.Sprintf("Dropped %d %s from loot table %s", used[0].count, itemDisplay(used[0].item), roll.table)
	case len(used) == 1:
		line = fmt.Sprintf("Dropped %d %s", used[0].count, itemDisplay(used[0].item))
	case roll.table != "":
		line = fmt.Sprintf("Dropped %d items from loot table %s", len(used), roll.table)
	default:
		line = fmt.Sprintf("Dropped %d items", len(used))
	}
	h.cmdSuccess(players, t.p, line, false)
	return ""
}

// splitStacks is the table's stack splitter: nothing over its stack size,
// nothing empty.
func splitStacks(in []invStack) []invStack {
	var out []invStack
	for _, s := range in {
		if s.item == 0 || s.count <= 0 {
			continue
		}
		cap := max(stackCap(s.item), 1)
		for s.count > cap {
			part := s
			part.count = cap
			out = append(out, part)
			s.count -= cap
		}
		out = append(out, s)
	}
	return out
}

func dropsToStacks(ds []drop) []invStack {
	out := make([]invStack, 0, len(ds))
	for _, d := range ds {
		out = append(out, invStack{item: d.item, count: d.count, potion: d.potion})
	}
	return splitStacks(out)
}

// lootSource rolls the source part.
func (h *hub) lootSource(players map[int32]*tracked, t *tracked, src []string) (lootRoll, string) {
	switch src[0] {
	case "fish": // fish <loot_table> <pos> [<tool>|mainhand|offhand]: the fishing tables, no hook
		if len(src) < 5 || len(src) > 6 {
			return lootRoll{}, lootUsage
		}
		x, y, z, ok := parsePosition(src[2:5], t.x, t.y, t.z, t.yaw, t.pitch)
		if !ok {
			return lootRoll{}, lootUsage
		}
		name := strings.TrimPrefix(src[1], "minecraft:")
		b := &bobberEntity{dim: t.dim, x: float64(floorInt(x)) + 0.5, y: float64(floorInt(y)) + 0.5, z: float64(floorInt(z)) + 0.5}
		var st invStack
		switch name {
		case "gameplay/fishing":
			// No hook, so no open-water treasure, and no luck in the context.
			if h.rng.Intn(95) < 85 {
				st = h.rollFish()
			} else {
				st = h.rollFishJunk(b)
			}
		case "gameplay/fishing/fish":
			st = h.rollFish()
		case "gameplay/fishing/junk":
			st = h.rollFishJunk(b)
		case "gameplay/fishing/treasure":
			st = h.rollFishTreasure()
		default:
			return lootRoll{}, fmt.Sprintf("The loot table minecraft:%s is not available on this server", name)
		}
		return lootRoll{drops: splitStacks([]invStack{st}), table: "minecraft:" + name}, ""
	case "loot":
		if len(src) != 2 {
			return lootRoll{}, lootUsage
		}
		name := strings.TrimPrefix(src[1], "minecraft:")
		tbl, ok := lootForChest(name)
		if !ok {
			return lootRoll{}, fmt.Sprintf("The loot table minecraft:%s is not available on this server", name)
		}
		fx, fy, fz := floorInt(t.x), floorInt(t.y), floorInt(t.z)
		ctx := &lootCtx{rng: h.rng.Intn, randf: h.rng.Float64, pos: blockPos{fx, fy, fz}, located: true,
			biomeAt: h.worldFor(t.dim).BiomeAt3D}
		return lootRoll{drops: splitStacks(h.evalChestStacks(tbl, ctx, 0))}, ""
	case "kill":
		if len(src) != 2 {
			return lootRoll{}, lootUsage
		}
		ms := h.commandMobs(players, t.p.eid, src[1])
		ps := h.commandTargets(players, t.p.eid, src[1])
		switch {
		case len(ms)+len(ps) == 0:
			return lootRoll{}, "No entity was found"
		case len(ms)+len(ps) > 1:
			return lootRoll{}, "Only one entity is allowed, but the provided selector allows more than one"
		case len(ps) == 1: // entities/player is an empty table
			return lootRoll{table: "minecraft:entities/player"}, ""
		}
		m := ms[0]
		name := strings.TrimPrefix(advEntityName[m.etype], "minecraft:")
		looting := heldStack(t).enchLvl(enchLooting)
		ctx := lootCtx{looting: looting, killedByPlayer: true, onFire: m.burning, source: "player",
			rng: h.rng.Intn, randf: h.rng.Float64}
		var ds []drop
		if got, ok := h.evalEntityLoot(int32(m.etype), ctx); ok {
			ds = got
			if m.etype == entitySheep && m.sheared {
				ds = nil
			}
		} else {
			for _, d := range h.mobLoot(m) {
				if looting > 0 && !d.fixed {
					d.count += h.rng.Intn(looting + 1)
				}
				ds = append(ds, d)
			}
		}
		return lootRoll{drops: dropsToStacks(ds), table: "minecraft:entities/" + name}, ""
	case "mine":
		if len(src) < 4 || len(src) > 5 {
			return lootRoll{}, lootUsage
		}
		x, y, z, ok := parsePosition(src[1:4], t.x, t.y, t.z, t.yaw, t.pitch)
		if !ok {
			return lootRoll{}, lootUsage
		}
		pos := blockPos{floorInt(x), floorInt(y), floorInt(z)}
		if !h.cloneLoaded(t.dim, pos, pos) {
			return lootRoll{}, "That position is not loaded"
		}
		if !h.inWorldYIn(t.dim, pos.y) {
			return lootRoll{}, "That position is out of this world!"
		}
		var tool invStack
		if len(src) == 5 {
			switch src[4] {
			case "mainhand":
				tool = heldStack(t)
			case "offhand":
				tool = t.offhand
			default:
				item, ok := itemByName[strings.TrimPrefix(src[4], "minecraft:")]
				if !ok {
					return lootRoll{}, fmt.Sprintf("Unknown item '%s'", src[4])
				}
				tool = invStack{item: item, count: 1}
			}
		}
		st := h.worldFor(t.dim).At(pos.x, pos.y, pos.z)
		name, _ := worldgen.StateName(st)
		ds := h.evalBlockLoot(lootCtx{state: st, tool: tool.item, silk: tool.enchLvl(enchSilkTouch) > 0,
			fortune: tool.enchLvl(enchFortune), rng: h.rng.Intn, randf: h.rng.Float64})
		if ds == nil { // no baked table: the engine's own drops for the block, as breaking it gives
			if sd, ok := h.specialBlockDrops(st, tool.item, tool.enchLvl(enchSilkTouch) > 0); ok {
				ds = sd
			} else if psAir(st) {
				return lootRoll{}, fmt.Sprintf("Block %s has no loot table", blockDisplayName(name))
			} else {
				ds = h.rollDrops(st)
			}
		}
		return lootRoll{drops: dropsToStacks(ds), table: "minecraft:blocks/" + name}, ""
	}
	return lootRoll{}, lootUsage
}

// blockDisplayName is a block's name as a message shows it.
func blockDisplayName(name string) string {
	parts := strings.Split(name, "_")
	for i, w := range parts {
		if w != "" {
			parts[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(parts, " ")
}

// singleSlot is SlotArgument: a slot name that covers exactly one slot.
func singleSlot(name string) (int, string) {
	ids, ok := slotRanges[name]
	if !ok {
		return 0, fmt.Sprintf("Unknown slot '%s'", name)
	}
	if len(ids) != 1 {
		return 0, fmt.Sprintf("Only single slots allowed: got '%s'", name)
	}
	return ids[0], ""
}

// lootTarget hands the drops to the target part.
func (h *hub) lootTarget(players map[int32]*tracked, t *tracked, tg []string, drops []invStack, track func(invStack)) string {
	blockAt := func(a []string) (itemTarget, string) {
		x, y, z, ok := parsePosition(a, t.x, t.y, t.z, t.yaw, t.pitch)
		if !ok {
			return itemTarget{}, lootUsage
		}
		pos := blockPos{floorInt(x), floorInt(y), floorInt(z)}
		if !h.cloneLoaded(t.dim, pos, pos) {
			return itemTarget{}, "That position is not loaded"
		}
		if !h.inWorldYIn(t.dim, pos.y) {
			return itemTarget{}, "That position is out of this world!"
		}
		it, ok := h.blockItemTarget(t.dim, pos)
		if !ok {
			return itemTarget{}, fmt.Sprintf("Target position %d, %d, %d is not a container", pos.x, pos.y, pos.z)
		}
		return it, ""
	}
	switch tg[0] {
	case "give":
		if len(h.commandMobs(players, t.p.eid, tg[1])) > 0 {
			return "Only players may be affected by this command, but the provided selector includes entities"
		}
		ps := h.commandTargets(players, t.p.eid, tg[1])
		if len(ps) == 0 {
			return "No player was found"
		}
		for _, d := range drops {
			for _, p := range ps {
				if p.inv == nil {
					continue
				}
				changed, left := p.inv.addStack(h.forkStack(d))
				for _, sl := range changed {
					h.sendSlot(p, sl)
				}
				// Inventory.add: true when any of it went in, or for a
				// player with infinite materials however much did.
				if left < d.count || p.gamemode == gmCreative {
					track(d)
				}
			}
		}
	case "spawn":
		x, y, z, ok := parsePosition(tg[1:4], t.x, t.y, t.z, t.yaw, t.pitch)
		if !ok {
			return lootUsage
		}
		for _, d := range drops {
			if it := h.spawnItemIn(players, t.dim, d.item, d.count, x, y, z); it != nil {
				it.setFrom(h.forkStack(d))
				h.refreshItemMeta(players, it)
			}
			track(d)
		}
	case "insert":
		it, msg := blockAt(tg[1:4])
		if msg != "" {
			return msg
		}
		for _, d := range drops {
			if h.distributeInto(it, d) {
				track(d)
			}
		}
		it.changed(players)
	case "replace":
		var its []itemTarget
		var rest []string
		if tg[1] == "block" {
			it, msg := blockAt(tg[2:5])
			if msg != "" {
				return msg
			}
			its, rest = []itemTarget{it}, tg[5:]
		} else {
			if len(h.commandMobs(players, t.p.eid, tg[2])) > 0 {
				return "Only players' slots can be set by /loot for now"
			}
			ps := h.commandTargets(players, t.p.eid, tg[2])
			if len(ps) == 0 {
				return "No entity was found"
			}
			for _, p := range ps {
				its = append(its, h.playerItemTarget(p))
			}
			rest = tg[3:]
		}
		start, msg := singleSlot(rest[0])
		if msg != "" {
			return msg
		}
		count := len(drops)
		if len(rest) == 2 {
			n, err := strconv.Atoi(rest[1])
			if err != nil || n < 0 {
				return lootUsage
			}
			count = n
		}
		for _, it := range its {
			if it.pos != nil && it.slotAt(start) == nil { // blockReplace checks the start slot
				return fmt.Sprintf("The target does not have slot %d", start)
			}
			changed := false
			for i := 0; i < count; i++ {
				var add invStack
				if i < len(drops) {
					add = drops[i]
				}
				sl := it.slotAt(start + i)
				if sl == nil || (it.canPlace != nil && !it.canPlace(start+i, add)) {
					continue
				}
				if sl.set(h.forkStack(add)) {
					track(add)
					changed = true
				}
			}
			if changed {
				it.changed(players)
			}
		}
	default:
		return lootUsage
	}
	return ""
}

// distributeInto is LootCommand.distributeToContainer: the first empty slot
// that may take the stack takes all of it; before that, matching stacks
// with room top up.
func (h *hub) distributeInto(it itemTarget, d invStack) bool {
	left := d
	changed := false
	for id := 0; left.count > 0; id++ {
		sl := it.slotAt(id)
		if sl == nil {
			break
		}
		if it.canPlace != nil && !it.canPlace(id, left) {
			continue
		}
		cur := sl.get()
		if cur.item == 0 || cur.count <= 0 {
			sl.set(h.forkStack(left))
			return true
		}
		probe := cur
		probe.count = left.count
		if probe == left && cur.count <= stackCap(cur.item) { // canMergeItems: isSameItemSameComponents
			// A merge counts as a change even when the stack had no room,
			// as vanilla's does.
			changed = true
			if n := min(left.count, stackCap(cur.item)-cur.count); n > 0 {
				cur.count += n
				left.count -= n
				sl.set(cur)
			}
		}
	}
	return changed
}
