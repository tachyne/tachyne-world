package server

import (
	"sort"
	"strings"
)

// The entities a selector reaches beyond players and mobs. Vanilla's @e is
// every entity in the level — dropped items, experience orbs, projectiles,
// boats and minecarts, fireworks, primed TNT, falling blocks, end crystals,
// effect clouds, armor stands, paintings and item frames — and the engine
// keeps each of those in its own table. otherEnt is a snapshot of one of
// them for the selector's predicates, with the two things a command does to
// an entity it does not model as living: move it (/tp) and kill it (/kill).
//
// These are reached only through the selections that ask for them
// (commandEntitiesAll): the commands written for players and mobs never
// see an otherEnt.

// otherEnt is one non-living entity as a selector sees it.
type otherEnt struct {
	eid        int32
	etype      string // registry name without the namespace ("item", "oak_boat")
	uuid       [16]byte
	dim        int
	x, y, z    float64
	yaw, pitch float32
	w, h       float64 // EntityType's registered size
	custom     string  // a custom name, "" for none
	item       invStack
	// setPos moves the live entity (nil: it cannot be teleported — a
	// painting or an item frame hangs on its block).
	setPos func(x, y, z float64, yaw float32)
	// kill is Entity.kill for it: out of its table and out of every view.
	kill func(players map[int32]*tracked)
}

// otherSizes are EntityType's registered sizes for the entities here.
var otherSizes = map[string][2]float64{
	"item": {0.25, 0.25}, "experience_orb": {0.5, 0.5},
	"arrow": {0.5, 0.5}, "spectral_arrow": {0.5, 0.5}, "trident": {0.5, 0.5},
	"snowball": {0.25, 0.25}, "egg": {0.25, 0.25}, "ender_pearl": {0.25, 0.25},
	"splash_potion": {0.25, 0.25}, "lingering_potion": {0.25, 0.25}, "experience_bottle": {0.25, 0.25},
	"fireball": {1, 1}, "small_fireball": {0.3125, 0.3125}, "dragon_fireball": {1, 1},
	"wither_skull": {0.3125, 0.3125}, "wind_charge": {0.3125, 0.3125}, "breeze_wind_charge": {0.3125, 0.3125},
	"llama_spit": {0.25, 0.25}, "shulker_bullet": {0.3125, 0.3125},
	"firework_rocket": {0.25, 0.25}, "eye_of_ender": {0.25, 0.25},
	"tnt": {0.98, 0.98}, "falling_block": {0.98, 0.98}, "end_crystal": {2, 2},
	"area_effect_cloud": {6, 0.5}, "armor_stand": {0.5, 1.975},
	"painting": {0.5, 0.5}, "item_frame": {0.5, 0.5}, "glow_item_frame": {0.5, 0.5},
}

// otherSize is the entity's box: the table's, a boat's or a minecart's by
// its family, or a quarter block for anything else.
func otherSize(etype string) (float64, float64) {
	if s, ok := otherSizes[etype]; ok {
		return s[0], s[1]
	}
	switch {
	case strings.HasSuffix(etype, "boat") || strings.HasSuffix(etype, "raft"):
		return 1.375, 0.5625
	case strings.HasSuffix(etype, "minecart"):
		return 0.98, 0.7
	}
	return 0.25, 0.25
}

func newOtherEnt(eid int32, etype string, uuid [16]byte, dim int, x, y, z float64) *otherEnt {
	w, h := otherSize(etype)
	return &otherEnt{eid: eid, etype: etype, uuid: uuid, dim: dim, x: x, y: y, z: z, w: w, h: h}
}

// name is Entity.getName: the custom name, else the type's (an item
// entity goes by its item's).
func (o *otherEnt) name() string {
	if o.custom != "" {
		return o.custom
	}
	n := o.etype
	if o.etype == "item" && o.item.item != 0 {
		n = itemNameOf[o.item.item]
	}
	return titleWords(n)
}

// titleWords turns a registry name into its English display form
// ("oak_boat" → "Oak Boat").
func titleWords(name string) string {
	parts := strings.Split(strings.TrimPrefix(name, "minecraft:"), "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// otherEntities snapshots every non-living entity the engine tracks, in
// eid order.
func (h *hub) otherEntities() []*otherEnt {
	var out []*otherEnt
	for _, it := range h.items {
		it := it
		o := newOtherEnt(it.eid, "item", it.uuid, it.dim, it.x, it.y, it.z)
		o.item = invStack{item: it.item, count: it.count}
		o.custom = it.name
		o.setPos = func(x, y, z float64, _ float32) { it.x, it.y, it.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			if h.items[it.eid] == it {
				delete(h.items, it.eid)
				h.entityGone(players, it.dim, it.eid)
			}
		}
		out = append(out, o)
	}
	for _, orb := range h.orbs {
		orb := orb
		o := newOtherEnt(orb.eid, "experience_orb", orb.uuid, orb.dim, orb.x, orb.y, orb.z)
		o.setPos = func(x, y, z float64, _ float32) { orb.x, orb.y, orb.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			if h.orbs[orb.eid] == orb {
				delete(h.orbs, orb.eid)
				h.entityGone(players, orb.dim, orb.eid)
			}
		}
		out = append(out, o)
	}
	for _, a := range h.arrows {
		a := a
		name := entityTypeName(a.etype)
		if name == "" {
			name = "arrow"
		}
		o := newOtherEnt(a.eid, name, a.uuid, a.dim, a.x, a.y, a.z)
		o.setPos = func(x, y, z float64, _ float32) { a.x, a.y, a.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			if h.arrows[a.eid] == a {
				delete(h.arrows, a.eid)
				h.entityGone(players, a.dim, a.eid)
			}
		}
		out = append(out, o)
	}
	for _, v := range h.vehicles {
		v := v
		o := newOtherEnt(v.eid, entityTypeName(v.etype), v.uuid, v.dim, v.x, v.y, v.z)
		o.yaw = v.yaw
		o.setPos = func(x, y, z float64, yaw float32) { v.x, v.y, v.z, v.yaw = x, y, z, yaw }
		o.kill = func(players map[int32]*tracked) {
			// VehicleEntity.kill is remove(KILLED): no item back, but a
			// container's cargo spills (discardVehicle).
			if h.vehicles[v.eid] == v {
				h.discardVehicle(players, v)
			}
		}
		out = append(out, o)
	}
	for _, r := range h.rockets {
		r := r
		o := newOtherEnt(r.eid, "firework_rocket", r.uuid, r.dim, r.x, r.y, r.z)
		o.setPos = func(x, y, z float64, _ float32) { r.x, r.y, r.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			if h.rockets[r.eid] == r {
				delete(h.rockets, r.eid)
				h.entityGone(players, r.dim, r.eid)
			}
		}
		out = append(out, o)
	}
	for _, e := range h.eyes {
		e := e
		o := newOtherEnt(e.eid, "eye_of_ender", e.uuid, e.dim, e.x, e.y, e.z)
		o.setPos = func(x, y, z float64, _ float32) { e.x, e.y, e.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			if h.eyes[e.eid] == e {
				delete(h.eyes, e.eid)
				h.entityGone(players, e.dim, e.eid)
			}
		}
		out = append(out, o)
	}
	for _, c := range h.crystals {
		c := c
		o := newOtherEnt(c.eid, "end_crystal", c.uuid, c.dim, c.x, c.y, c.z)
		o.setPos = func(x, y, z float64, _ float32) { c.x, c.y, c.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			// EndCrystal.kill: onDestroyedBy (the dragon loses its healer),
			// then gone — no blast.
			if h.crystals[c.eid] == c {
				delete(h.crystals, c.eid)
				h.entityGone(players, c.dim, c.eid)
				h.dragonCrystalDestroyed(players, c, nil)
			}
		}
		out = append(out, o)
	}
	for _, c := range h.clouds {
		c := c
		o := newOtherEnt(c.eid, "area_effect_cloud", c.uuid, c.dim, c.x, c.y, c.z)
		o.w = float64(c.radius) * 2
		o.setPos = func(x, y, z float64, _ float32) { c.x, c.y, c.z = x, y, z }
		o.kill = func(players map[int32]*tracked) { h.removeCloud(players, c) }
		out = append(out, o)
	}
	for _, st := range h.armorStands {
		st := st
		o := newOtherEnt(st.eid, "armor_stand", [16]byte{}, st.dim, st.x, st.y, st.z)
		o.yaw, o.custom = st.yaw, st.name
		o.setPos = func(x, y, z float64, yaw float32) { st.x, st.y, st.z, st.yaw = x, y, z, yaw }
		o.kill = func(players map[int32]*tracked) {
			// ArmorStand.kill: remove(KILLED), nothing dropped.
			if h.armorStands[st.eid] == st {
				h.breakStand(players, st, false, false)
			}
		}
		out = append(out, o)
	}
	for _, pt := range h.paintings {
		pt := pt
		o := newOtherEnt(pt.eid, "painting", [16]byte{}, pt.dim, float64(pt.x)+0.5, float64(pt.y)+0.5, float64(pt.z)+0.5)
		o.kill = func(players map[int32]*tracked) {
			if h.paintings[pt.eid] == pt { // Entity.kill: gone, no item
				delete(h.paintings, pt.eid)
				h.entityGone(players, pt.dim, pt.eid)
			}
		}
		out = append(out, o)
	}
	for _, f := range h.itemFrames {
		f := f
		name := "item_frame"
		if f.glow {
			name = "glow_item_frame"
		}
		o := newOtherEnt(f.eid, name, [16]byte{}, f.dim, float64(f.x)+0.5, float64(f.y)+0.5, float64(f.z)+0.5)
		o.kill = func(players map[int32]*tracked) {
			if h.itemFrames[f.eid] == f { // ItemFrame.kill: the framed map let go, nothing dropped
				delete(h.itemFrames, f.eid)
				h.entityGone(players, f.dim, f.eid)
				h.markFrameMapsDirty(f)
				h.frameOutputChanged(players, f)
			}
		}
		out = append(out, o)
	}
	for _, pt := range h.tnt {
		pt := pt
		o := newOtherEnt(pt.eid, "tnt", [16]byte{}, pt.dim, pt.x, pt.y, pt.z)
		o.setPos = func(x, y, z float64, _ float32) { pt.x, pt.y, pt.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			for i, q := range h.tnt {
				if q == pt {
					h.tnt = append(h.tnt[:i:i], h.tnt[i+1:]...)
					h.entityGone(players, pt.dim, pt.eid)
					return
				}
			}
		}
		out = append(out, o)
	}
	for _, fb := range h.fallingBlocks {
		fb := fb
		o := newOtherEnt(fb.eid, "falling_block", [16]byte{}, fb.dim, fb.x, fb.y, fb.z)
		o.setPos = func(x, y, z float64, _ float32) { fb.x, fb.y, fb.z = x, y, z }
		o.kill = func(players map[int32]*tracked) {
			for i, q := range h.fallingBlocks {
				if q == fb {
					h.fallingBlocks = append(h.fallingBlocks[:i:i], h.fallingBlocks[i+1:]...)
					h.discardFalling(players, fb)
					return
				}
			}
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].eid < out[j].eid })
	return out
}

// commandEntitiesAll resolves an EntityArgument over every entity the
// engine keeps: players, mobs, and (for @e) the non-living ones too. Only
// commands that handle an otherEnt ask for it (/kill, /tp).
func (h *hub) commandEntitiesAll(players map[int32]*tracked, by int32, arg string) []cmdEntity {
	spec, ok := parseTargetSpec(arg)
	if !ok {
		return nil
	}
	from := players[by]
	wide := reachesEntities(spec, from)
	return h.selectEntitiesAll(players, from, spec, true, wide, wide)
}

// ---- nbt= -----------------------------------------------------------------

// entityNBTView is the part of Entity.saveWithoutId the nbt= predicate can
// compare against: the tags every entity saves, a living entity's health,
// a player's level, game mode and held item, an item entity's stack.
func (h *hub) entityNBTView(en cmdEntity) map[string]any {
	x, y, z := en.pos()
	out := map[string]any{"Pos": []any{x, y, z}}
	var tags map[string]bool
	switch {
	case en.t != nil:
		t := en.t
		tags = t.tags
		out["Rotation"] = []any{float64(t.yaw), float64(t.pitch)}
		out["Health"] = float64(t.health)
		out["XpLevel"] = int64(t.xpLevel)
		out["playerGameType"] = int64(t.gamemode)
		out["Dimension"] = dimensionName(t.dim)
		slot := t.p.heldSlot()
		out["SelectedItemSlot"] = int64(slot)
		if slot >= 0 && slot < len(t.inv.slots) {
			if st := t.inv.slots[slot]; st.item != 0 && st.count > 0 {
				out["SelectedItem"] = map[string]any{"id": "minecraft:" + itemNameOf[st.item], "count": int64(st.count)}
			}
		}
	case en.m != nil:
		m := en.m
		tags = m.tags
		out["Rotation"] = []any{float64(m.yaw), 0.0}
		out["Health"] = float64(m.health)
		if m.customName != "" {
			out["CustomName"] = m.customName
		}
		if m.baby {
			out["Age"] = int64(-24000)
		}
	case en.o != nil:
		o := en.o
		out["Rotation"] = []any{float64(o.yaw), float64(o.pitch)}
		if o.custom != "" {
			out["CustomName"] = o.custom
		}
		if o.etype == "item" && o.item.item != 0 {
			out["Item"] = map[string]any{"id": "minecraft:" + itemNameOf[o.item.item], "count": int64(o.item.count)}
		}
	}
	if len(tags) > 0 {
		names := make([]string, 0, len(tags))
		for tg := range tags {
			names = append(names, tg)
		}
		sort.Strings(names)
		list := make([]any, len(names))
		for i, n := range names {
			list[i] = n
		}
		out["Tags"] = list
	}
	return out
}

// dimensionName is a dimension's registry key.
func dimensionName(dim int) string {
	switch dim {
	case dimNether:
		return "minecraft:the_nether"
	case dimEnd:
		return "minecraft:the_end"
	}
	return "minecraft:overworld"
}

// ---- advancements= ----------------------------------------------------------

// advSelPred is one advancements= entry: the advancement done or not, or
// each named criterion done or not.
type advSelPred struct {
	id    string
	done  bool
	crits map[string]bool // nil: test the whole advancement
}

// parseAdvancementsOption reads advancements={id=bool, id={crit=bool,…},…}.
func parseAdvancementsOption(v string) ([]advSelPred, bool) {
	if !strings.HasPrefix(v, "{") || !strings.HasSuffix(v, "}") {
		return nil, false
	}
	var out []advSelPred
	for _, ent := range splitPredicates(v[1 : len(v)-1]) {
		k, val, ok := strings.Cut(ent, "=")
		if !ok {
			return nil, false
		}
		k, val = strings.TrimSpace(k), strings.TrimSpace(val)
		if k == "" {
			return nil, false
		}
		p := advSelPred{id: nsID(k)}
		if strings.HasPrefix(val, "{") {
			if !strings.HasSuffix(val, "}") {
				return nil, false
			}
			p.crits = map[string]bool{}
			for _, c := range splitPredicates(val[1 : len(val)-1]) {
				ck, cv, ok := strings.Cut(c, "=")
				if !ok {
					return nil, false
				}
				b, ok := parseSelectorBool(strings.TrimSpace(cv))
				if !ok {
					return nil, false
				}
				p.crits[strings.TrimSpace(ck)] = b
			}
		} else {
			b, ok := parseSelectorBool(val)
			if !ok {
				return nil, false
			}
			p.done = b
		}
		out = append(out, p)
	}
	return out, true
}

func parseSelectorBool(s string) (bool, bool) {
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// advancementsMatch tests a player's progress: an advancement unknown to
// the server fails, as does a criterion it does not have.
func advancementsMatch(st advState, preds []advSelPred) bool {
	for _, p := range preds {
		n := curAdv().byID[p.id]
		if n == nil {
			return false
		}
		if p.crits == nil {
			if st.done(n) != p.done {
				return false
			}
			continue
		}
		for crit, want := range p.crits {
			if !advHasCriterion(n, crit) {
				return false
			}
			_, got := st[n.id][crit]
			if got != want {
				return false
			}
		}
	}
	return true
}

func advHasCriterion(n *advNode, crit string) bool {
	for _, group := range n.reqs {
		for _, c := range group {
			if c == crit {
				return true
			}
		}
	}
	return false
}
