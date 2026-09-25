package server

import (
	"math"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Middle click. Since 1.21.4 the server does the picking
// (ServerGamePacketListenerImpl.handlePickItemFromBlock/FromEntity →
// tryPickItem): the block's clone stack or the entity's pick result is
// looked for in the inventory — selected where it sits in the hotbar,
// swapped into a suitable hotbar slot from the main inventory — and a
// player with infinite materials is handed a fresh one when there is none.

type evPickItem struct {
	eid int32
	e   attachproto.PickItem
}

func (evPickItem) isHubEvent() {}

// Reach slack: isWithinBlockInteractionRange(pos, 1.0) and
// isWithinEntityInteractionRange(entity, 3.0).
const (
	pickBlockSlack  = 1.0
	pickEntitySlack = 3.0
)

func (h *hub) pickItem(players map[int32]*tracked, t *tracked, e attachproto.PickItem) {
	if t.inv == nil || t.dead {
		return
	}
	var st invStack
	if e.Entity {
		st = h.entityPickResult(players, t, e.EID)
	} else {
		pos := blockPos{int(e.X), int(e.Y), int(e.Z)}
		w := h.worldFor(t.dim)
		if w == nil || !w.Loaded(int32(pos.x>>4), int32(pos.z>>4)) ||
			!withinReachBox(t, t.playerAttrs().Value(attr.BlockInteractionRange)+pickBlockSlack,
				float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 1, 1) {
			return
		}
		st = invStack{item: cloneItem(w.At(pos.x, pos.y, pos.z))}
	}
	if st.item <= 0 {
		return
	}
	if st.count <= 0 {
		st.count = 1
	}
	h.tryPickItem(t, st)
}

// tryPickItem is ServerGamePacketListenerImpl.tryPickItem over the 36 main
// slots (0-8 hotbar).
func (h *hub) tryPickItem(t *tracked, st invStack) {
	inv := &t.inv.slots
	sel := t.p.heldSlot()
	found := -1
	for i := range inv { // Inventory.findSlotMatchingItem
		if inv[i].item > 0 && inv[i].count > 0 && sameItemComponents(inv[i], st) {
			found = i
			break
		}
	}
	var changed []int
	switch {
	case found >= 0 && found < 9:
		sel = found
	case found >= 0: // Inventory.pickSlot
		sel = suitableHotbarSlot(inv, sel)
		inv[sel], inv[found] = inv[found], inv[sel]
		changed = append(changed, sel, found)
	case t.gamemode == gmCreative: // Inventory.addAndPickItem
		sel = suitableHotbarSlot(inv, sel)
		if inv[sel].item > 0 && inv[sel].count > 0 {
			for i := range inv { // getFreeSlot; with none, the old stack is lost
				if inv[i].item <= 0 || inv[i].count <= 0 {
					inv[i] = inv[sel]
					changed = append(changed, i)
					break
				}
			}
		}
		inv[sel] = st
		changed = append(changed, sel)
	}
	t.p.setHeldSlot(sel)
	t.p.trySendEv(attachproto.HeldSync{Slot: int32(sel)})
	for _, s := range changed {
		h.sendSlot(t, s)
	}
}

// suitableHotbarSlot is Inventory.getSuitableHotbarSlot: the first empty
// hotbar slot from the selected one on, else the first unenchanted one,
// else the selected slot itself.
func suitableHotbarSlot(inv *[invSize]invStack, sel int) int {
	for i := 0; i < 9; i++ {
		if s := inv[(sel+i)%9]; s.item <= 0 || s.count <= 0 {
			return (sel + i) % 9
		}
	}
	for i := 0; i < 9; i++ {
		if !inv[(sel+i)%9].enchanted() {
			return (sel + i) % 9
		}
	}
	return sel
}

// entityPickResult is Entity.getPickResult for the entity eid, or the zero
// stack when it has none, is elsewhere, or is out of reach.
func (h *hub) entityPickResult(players map[int32]*tracked, t *tracked, eid int32) invStack {
	reach := t.playerAttrs().Value(attr.EntityInteractionRange) + pickEntitySlack
	in := func(dim int, x, y, z, w, ht float64) bool {
		return dim == t.dim && withinReachBox(t, reach, x, y, z, w, ht)
	}
	switch {
	case h.mobs[eid] != nil: // Mob.getPickResult: its spawn egg
		m := h.mobs[eid]
		b := m.box()
		if in(m.dim, m.x, m.y, m.z, b.w, b.h) {
			return invStack{item: itemByName[entityTypeName(m.etype)+"_spawn_egg"]}
		}
	case h.vehicles[eid] != nil: // boats and minecarts: their item
		v := h.vehicles[eid]
		w, ht := v.box()
		if in(v.dim, v.x, v.y, v.z, w, ht) {
			return invStack{item: vehicleItemFor(v.etype)}
		}
	case h.itemFrames[eid] != nil: // ItemFrame: the framed stack, else the frame
		f := h.itemFrames[eid]
		if in(f.dim, float64(f.x)+0.5, float64(f.y), float64(f.z)+0.5, 1, 1) {
			if f.held.item > 0 {
				st := f.held
				st.count = 1
				return st
			}
			if f.glow {
				return invStack{item: itemGlowItemFrame}
			}
			return invStack{item: itemItemFrame}
		}
	case h.paintings[eid] != nil:
		p := h.paintings[eid]
		if in(p.dim, float64(p.x)+0.5, float64(p.y), float64(p.z)+0.5, float64(max(p.w, 1)), float64(max(p.h, 1))) {
			return invStack{item: itemPainting}
		}
	case h.armorStands[eid] != nil:
		s := h.armorStands[eid]
		if in(s.dim, s.x, s.y, s.z, 0.5, 1.975) {
			return invStack{item: itemArmorStand}
		}
	case h.knots[eid] != nil:
		k := h.knots[eid]
		if in(k.dim, float64(k.pos.x)+0.5, float64(k.pos.y), float64(k.pos.z)+0.5, 0.375, 0.5) {
			return invStack{item: itemLead}
		}
	case h.crystals[eid] != nil:
		c := h.crystals[eid]
		if in(c.dim, c.x, c.y, c.z, 2, 2) {
			return invStack{item: itemByName["end_crystal"]}
		}
	}
	return invStack{}
}

// withinReachBox reports whether the player's eye is within reach of a box
// w wide and ht tall standing centred on (x, z) at y.
func withinReachBox(t *tracked, reach, x, y, z, w, ht float64) bool {
	ex, ey, ez := t.x, t.y+t.eyeHeight(), t.z
	dx := math.Max(0, math.Max(x-w/2-ex, ex-(x+w/2)))
	dy := math.Max(0, math.Max(y-ey, ey-(y+ht)))
	dz := math.Max(0, math.Max(z-w/2-ez, ez-(z+w/2)))
	return dx*dx+dy*dy+dz*dz <= reach*reach
}

// cloneItem is BlockState.getCloneItemStack's item without block entity
// data: the block's own item (Item.BY_BLOCK, which also answers for the
// wall variants and the custom-named block items), or one of the blocks
// that override it. 0 = nothing to pick (air, fluids, fire, portals).
func cloneItem(state uint32) int32 {
	name, ok := worldgen.StateName(state)
	if !ok {
		return 0
	}
	name = strings.TrimPrefix(name, "minecraft:")
	if strings.HasSuffix(name, "candle_cake") { // CandleCakeBlock
		return itemByName["cake"]
	}
	if n, ok := cloneItemNames[name]; ok {
		if n == "" {
			return 0
		}
		return itemByName[n]
	}
	if name == "piston_head" { // PistonHeadBlock: the piston it came from
		if info, ok := worldgen.InfoForState(state); ok && worldgen.GetProperty(info, state, "type") == "sticky" {
			return itemByName["sticky_piston"]
		}
		return itemByName["piston"]
	}
	if id, ok := itemByName[name]; ok {
		return id
	}
	if strings.Contains(name, "wall_") { // StandingAndWallBlockItem / HangingSignItem
		if id, ok := itemByName[strings.Replace(name, "wall_", "", 1)]; ok {
			return id
		}
	}
	if p, ok := strings.CutPrefix(name, "potted_"); ok { // FlowerPotBlock: the plant
		if id, ok := itemByName[p]; ok {
			return id
		}
		if id, ok := itemByName[strings.TrimSuffix(p, "_bush")]; ok { // potted_azalea_bush
			return id
		}
	}
	return 0
}

// cloneItemNames: blocks whose clone item is not the item of the same name —
// the custom-named block items (createBlockItemWithCustomItemName) and the
// getCloneItemStack overrides. "" = no item at all.
var cloneItemNames = map[string]string{
	"redstone_wire":         "redstone",
	"tripwire":              "string",
	"wheat":                 "wheat_seeds",
	"carrots":               "carrot",
	"potatoes":              "potato",
	"beetroots":             "beetroot_seeds",
	"torchflower_crop":      "torchflower_seeds",
	"pitcher_crop":          "pitcher_pod",
	"cocoa":                 "cocoa_beans",
	"pumpkin_stem":          "pumpkin_seeds",
	"melon_stem":            "melon_seeds",
	"attached_pumpkin_stem": "pumpkin_seeds",
	"attached_melon_stem":   "melon_seeds",
	"sweet_berry_bush":      "sweet_berries",
	"cave_vines":            "glow_berries",
	"cave_vines_plant":      "glow_berries",
	"kelp_plant":            "kelp", // GrowingPlantBodyBlock: the head's item
	"weeping_vines_plant":   "weeping_vines",
	"twisting_vines_plant":  "twisting_vines",
	"bamboo_sapling":        "bamboo",
	"big_dripleaf_stem":     "big_dripleaf",
	"tall_seagrass":         "seagrass",
	"powder_snow":           "powder_snow_bucket",
	"water_cauldron":        "cauldron", // registered with the cauldron item
	"lava_cauldron":         "cauldron",
	"powder_snow_cauldron":  "cauldron",
	"end_portal":            "",
	"end_gateway":           "",
	"nether_portal":         "",
	"frosted_ice":           "",
	"moving_piston":         "",
}
