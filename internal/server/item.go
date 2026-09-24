package server

import (
	"bytes"

	"encoding/binary"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Dropped-item entities: the visible item stacks that pop out when a block is
// broken or a plant loses its support. They are hub-owned like mobs, shown to
// nearby players via Spawn Entity + the item's metadata, and despawn after the
// vanilla five minutes. Pickup/inventory is a survival-mechanics follow-up; for
// now they render, rest on the ground, and time out.

const (
	itemDespawnTicks   = 6000 // 5 minutes — vanilla item lifetime
	itemMetaIndexStack = 8    // entity-metadata index of an item entity's stack
	itemMetaTypeSlot   = 7    // metadata value type id for Slot/ItemStack (1.21.5)
	itemMetaEnd        = 0xff // metadata list terminator
)

var (
	entityItem = entityID("item") // minecraft:entity_type "item" network ID (1.21.5)
)

type itemEntity struct {
	dim        int // dimension the drop lives in
	eid        int32
	uuid       [16]byte
	x, y, z    float64
	item       int32
	count      int
	dmg        int            // durability damage carried by the dropped stack
	ench       enchList       // enchantments carried by the dropped stack
	mapID      int32          // filled_map identity carried by the dropped stack
	pats       [6]bannerLayer // banner pattern layers carried by the dropped stack
	trimMat    int8           // armor trim carried by the dropped stack (+1 enc)
	trimPat    int8
	bookID     int32 // book identity carried by the dropped stack
	boxID      int32 // shulker-box identity carried by the dropped stack
	color      int32 // dyed_color of leather armour
	thrower    int32 // the player who tossed it (0 = the world did), for the thrower's advancement
	portalCool int   // ticks before this drop may take a portal again (Entity.portalCooldown)
	hiveID     int32 // carried-hive identity (Silk-Touched hive's bees + honey)
	// The five below were missing until 2026-09-05: a dropped bundle lost its
	// contents, a dropped potion became water, a dropped renamed item lost its
	// name and prior-work cost, a dropped goat horn forgot its instrument.
	bundleID      int32
	potion        int8
	repairCost    int
	instrument    int8
	name          string
	lode          lodeTracker // lodestone compass target
	stew          int8        // suspicious stew's hidden flower (0 = none) — was lost on the floor until 2026-09-11
	sherds        potSherds   // a decorated pot's four faces, carried by the dropped stack
	flight        int8        // a firework rocket's flight duration
	starID        int32       // a firework's bursts
	shieldBase    int8        // a decorated shield's banner base (dye + 1)
	vx, vy, vz    float64     // motion per tick (tickItem); all 0 at rest
	born          uint64      // world tick spawned (for despawn)
	noPickupUntil uint64      // absolute tick pickup unlocks (tosses get a longer hold;
	//                      NEVER fake this by moving born forward — a future born
	//                      underflows the unsigned despawn age and vanishes the item)
}

// stack is the dropped item as the stack it would be in a slot — the ONE
// conversion, so a field added to invStack is carried by the pickup path, the
// hopper path and the ground render alike (three copies used to disagree).
func (it *itemEntity) stack() invStack {
	return invStack{item: it.item, count: it.count, dmg: it.dmg, ench: it.ench, mapID: it.mapID,
		pats: it.pats, trimMat: it.trimMat, trimPat: it.trimPat, bookID: it.bookID, boxID: it.boxID,
		hiveID: it.hiveID, bundleID: it.bundleID, potion: it.potion, repairCost: it.repairCost,
		instrument: it.instrument, name: it.name, lode: it.lode, color: it.color, stew: it.stew,
		shieldBase: it.shieldBase, sherds: it.sherds, flight: it.flight, starID: it.starID}
}

// setFrom is stack()'s inverse: everything a slot carries, onto the dropped
// item. One conversion, for the same reason stack() is one — five drop sites
// had the field list written out by hand, and a field added to invStack was
// carried by whichever of them somebody remembered. It deliberately does NOT
// touch count or the item id: a drop site decides how many it is dropping.
func (it *itemEntity) setFrom(st invStack) {
	it.dmg, it.ench, it.mapID = st.dmg, st.ench, st.mapID
	it.pats = st.pats
	it.trimMat, it.trimPat, it.color = st.trimMat, st.trimPat, st.color
	it.bookID, it.boxID, it.hiveID, it.bundleID = st.bookID, st.boxID, st.hiveID, st.bundleID
	it.potion, it.repairCost, it.instrument = st.potion, st.repairCost, st.instrument
	it.name, it.lode, it.stew, it.shieldBase = st.name, st.lode, st.stew, st.shieldBase
	it.sherds, it.flight, it.starID = st.sherds, st.flight, st.starID
}

// refreshItemMeta re-sends a ground item's stack after a drop site has
// finished filling in its fields — the spawn broadcast happens first with the
// bare (item, count), so without this a renamed potion lay on the floor
// looking like water until someone picked it up.
func (h *hub) refreshItemMeta(players map[int32]*tracked, it *itemEntity) {
	h.toTracking(players, it.eid, it.dim, it.x, it.z, metaEv(itemMetadata(it.eid, it.stack())))
}

// spawnItemIn drops into an explicit dimension.
func (h *hub) spawnItemIn(players map[int32]*tracked, dim int, item int32, count int, x, y, z float64) *itemEntity {
	if item == 0 || count <= 0 {
		return nil
	}
	// Fall from the spawn point to the local floor, so a drop mined underground
	// rests in the tunnel (not teleported to the world surface) and a popped
	// plant's drop doesn't hang in the air.
	y = float64(h.worldFor(dim).DropY(int(math.Floor(x)), int(math.Ceil(y)), int(math.Floor(z))))
	return h.spawnItemAt(players, dim, item, count, x, y, z, 0, 0, 0)
}

// spawnItemAt drops an item exactly where asked, with an initial velocity,
// and leaves the physics tick to land it (a block's popped drop, a toss).
func (h *hub) spawnItemAt(players map[int32]*tracked, dim int, item int32, count int, x, y, z, vx, vy, vz float64) *itemEntity {
	if item == 0 || count <= 0 {
		return nil
	}
	eid := h.allocEID()
	now := h.tick.Load()
	it := &itemEntity{eid: eid, dim: dim, x: x, y: y, z: z, vx: vx, vy: vy, vz: vz, item: item, count: count, born: now, noPickupUntil: now + pickupDelay}
	binary.BigEndian.PutUint32(it.uuid[12:], uint32(eid))
	h.items[eid] = it

	// syncTracking spawns it for whoever can see it, with its stack.
	h.bus.publish("item_drop", map[string]any{"eid": eid, "item": item, "count": count, "x": x, "y": y, "z": z})
	return it
}

// spawnBlockDrop places a broken block's loot as vanilla Block.popResource
// does: at the block's centre nudged by up to a quarter block each way, with
// a small random sideways push and a hop, so neighbouring blocks' drops
// scatter and merge as they land.
func (h *hub) spawnBlockDrop(players map[int32]*tracked, dim int, item int32, count int, x, y, z int) {
	if h.worldFor(dim) == nil {
		return
	}
	off := func() float64 { return h.rng.Float64()*0.5 - 0.25 }
	px := float64(x) + 0.5 + off()
	py := float64(y) + 0.5 + off() - 0.125 // minus half the item's height
	pz := float64(z) + 0.5 + off()
	h.spawnItemAt(players, dim, item, count, px, py, pz, h.rng.Float64()*0.2-0.1, 0.2, h.rng.Float64()*0.2-0.1)
}

// updateItems despawns dropped items past their lifetime and merges nearby
// identical stacks (vanilla: ground items within ~0.5 blocks combine).
func (h *hub) updateItems(players map[int32]*tracked) {
	now := h.tick.Load()
	type cell struct {
		dim  int
		x, z int
	}
	// Items bucketed by the block column they are in: a merge partner lies
	// within 0.75 horizontally, so only the 3×3 columns around an item can
	// hold one. Comparing every item with every other (as this did until
	// 2026-09-24) took most of a second once a few thousand lay about, and
	// the stalled tick held back everyone's movement.
	buckets := map[cell][]int32{}
	for eid, it := range h.items {
		if now-it.born >= itemDespawnTicks {
			delete(h.items, eid)
			h.entityGone(players, it.dim, eid)
			continue
		}
		c := cell{it.dim, floorInt(it.x), floorInt(it.z)}
		buckets[c] = append(buckets[c], eid)
	}
	for eid, it := range h.items {
		if h.items[eid] == nil {
			continue // absorbed into another this pass
		}
		cx, cz := floorInt(it.x), floorInt(it.z)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				for _, oid := range buckets[cell{it.dim, cx + dx, cz + dz}] {
					other := h.items[oid]
					if oid == eid || other == nil || !sameItemComponents(it.stack(), other.stack()) ||
						it.count+other.count > stackCap(it.item) {
						continue
					}
					// Vanilla merges within the item's bbox inflated 0.5 horizontally,
					// 0 vertically — a flat horizontal AABB (~0.75 wide, ~0.25 tall), not
					// a 1.0 sphere: items on different shelves/levels don't merge.
					ox, oy, oz := other.x-it.x, other.y-it.y, other.z-it.z
					if math.Abs(ox) > 0.75 || math.Abs(oz) > 0.75 || math.Abs(oy) > 0.25 {
						continue
					}
					it.count += other.count // absorb the other into this one
					delete(h.items, oid)
					h.entityGone(players, other.dim, oid)
					h.toNearbyEv(players, it.dim, it.x, it.z, metaEv(itemMetadata(eid, it.stack())))
				}
			}
		}
	}
}

// appendSlot encodes a Slot/ItemStack (1.21.5 component format). count 0 = empty.
func appendSlot(b []byte, item int32, count int) []byte {
	return appendStack(b, invStack{item: item, count: count})
}

// Structured-component ids we attach to Slots, in CANONICAL (770) numbering.
// damage renders the durability bar (id identical 1.21.5→26.2); enchantments
// renders the glint + tooltip (the chain renumbers it to 13 for 774+ clients
// — see protocol.copyFullSlot).
const (
	componentDamage         = 3
	componentEnchantments   = 10
	componentStoredEnch     = 34 // books; remapped per version by the chain
	componentCustomName     = 5  // anvil renames (NBT text); remapped per version
	componentLore           = 8  // plugin-UI item lore (list of NBT texts); remapped per version
	componentMapID          = 37 // filled_map's map number; remapped per version
	componentDyedColor      = 35 // dyed_color rgb (leather armour); remapped per version
	componentTrim           = 47 // armor trim (material + pattern holders); remapped per version
	componentBannerPats     = 63 // banner pattern layers; remapped per version
	componentBundleContents = 41 // bundle contents (list of Slots); remapped per version
	componentLodestone      = 58 // lodestone_tracker (lodestone compass target); remapped per version
	componentBaseColor      = 64 // base_color (a decorated shield's banner base, one dye varint); remapped per version
	componentPotionContents = 42 // potion_contents (what a brew does, and so what colour it is); remapped per version
	componentStewEffects    = 44 // suspicious_stew_effects (creative tooltip only, but vanilla syncs it); remapped per version
	componentRepairCost     = 16 // repair_cost (the anvil's prior-work penalty); remapped per version
	componentContainer      = 66 // container (a shulker box's contents, in its tooltip); remapped per version
	componentOminousBottle  = 54 // ominous_bottle_amplifier (the Bad Omen level); remapped per version
	componentFireworks      = 60 // fireworks (a rocket's flight duration + bursts); remapped per version
	componentFireworkStar   = 59 // firework_explosion (one star's burst); remapped per version
	componentPotDecorations = 65 // pot_decorations (a pot's four faces, as ITEM ids); remapped per version
)

// appendStack encodes a Slot, attaching the damage component when the stack
// has taken durability wear and the enchantments component when enchanted.
func appendStack(b []byte, st invStack) []byte {
	b = protocol.AppendVarInt(b, int32(st.count))
	if st.count == 0 {
		return b
	}
	b = protocol.AppendVarInt(b, st.item)
	return append(b, stackComponents(st)...)
}

// stackEv is the domain form of a stack: id + count + the same component
// bytes appendStack writes (canonical wire form, opaque scaffolding).
func stackEv(st invStack) attachproto.ItemStack {
	if st.count == 0 {
		return attachproto.ItemStack{}
	}
	return attachproto.ItemStack{ID: st.item, Count: int32(st.count), Components: stackComponents(st)}
}

// stackNumEv is stackEv for a bare (item, count) pair.
func stackNumEv(item int32, count int) attachproto.ItemStack {
	return stackEv(invStack{item: item, count: count})
}

// metaEv wraps a canonical entity-metadata body (eid-prefixed, as every
// metadata builder here produces) into the typed event: eid split out, the
// list bytes opaque (typed later — same story as ItemStack.Components).
func metaEv(body []byte) attachproto.EntityMeta {
	r := bytes.NewReader(body)
	eid, _ := protocol.ReadVarInt(r)
	return attachproto.EntityMeta{EID: eid, Meta: body[len(body)-r.Len():]}
}

// stackComponents encodes what follows the item id in a Slot: the add/remove
// component counts and the component entries.
func stackComponents(st invStack) []byte {
	var b []byte
	var enchN int32
	for _, e := range st.ench {
		if e.lvl > 0 {
			enchN++
		}
	}
	comps := int32(0)
	if st.dmg > 0 {
		comps++
	}
	if enchN > 0 {
		comps++
	}
	if st.name != "" {
		comps++
	}
	if st.mapID != 0 {
		comps++
	}
	if st.color != 0 {
		comps++
	}
	patN := int32(st.patCount())
	if patN > 0 {
		comps++
	}
	if st.trimMat != 0 || st.trimPat != 0 {
		comps++
	}
	if st.lode.has {
		comps++
	}
	if st.shieldBase != 0 {
		comps++
	}
	if st.potion != potNone {
		comps++ // potion_contents, or the ominous bottle's level — see below
	}
	if _, ok := stewEffectOf(st.stew); ok {
		comps++
	}
	if st.repairCost > 0 {
		comps++
	}
	if st.item == itemFireworkRocket {
		comps++
	}
	if st.item == itemFireworkStar && len(burstsOf(st)) == 1 {
		comps++
	}
	if !st.sherds.empty() {
		comps++
	}
	var bookBytes []byte
	if st.bookID != 0 {
		if bs := globalBooks.Load(); bs != nil {
			if bk, ok := bs.get(st.bookID); ok {
				bookBytes = bookComponentBytes(st.item, bk)
				comps++
			}
		}
	}
	var boxBytes []byte
	if st.boxID != 0 {
		if bs := boxComponentBytes(st.boxID); len(bs) > 0 {
			boxBytes = bs
			comps++
		}
	}
	var bundleBytes []byte
	if st.bundleID != 0 {
		if bs := bundleComponentBytes(st.bundleID); len(bs) > 0 {
			bundleBytes = bs
			comps++
		}
	}
	b = protocol.AppendVarInt(b, comps) // components to add
	b = protocol.AppendVarInt(b, 0)     // components to remove
	if st.color != 0 {
		// First on purpose: the Bedrock gateway reads a stack's leading
		// component for its NBT (maps, books, and now the dye), and a dyed
		// piece is never a map or a book.
		b = protocol.AppendVarInt(b, componentDyedColor)
		b = protocol.AppendVarInt(b, st.color)
	}
	if st.dmg > 0 {
		b = protocol.AppendVarInt(b, componentDamage)
		b = protocol.AppendVarInt(b, int32(st.dmg))
	}
	if bundleBytes != nil {
		b = protocol.AppendVarInt(b, componentBundleContents)
		b = append(b, bundleBytes...)
	}
	if st.mapID != 0 {
		b = protocol.AppendVarInt(b, componentMapID)
		b = protocol.AppendVarInt(b, st.mapID)
	}
	if enchN > 0 {
		// Books carry STORED enchantments (what the anvil applies); everything
		// else carries live ones. Same wire shape, different component.
		comp := int32(componentEnchantments)
		if st.item == itemEnchantedBook {
			comp = componentStoredEnch
		}
		b = protocol.AppendVarInt(b, comp)
		b = protocol.AppendVarInt(b, enchN)
		for _, e := range st.ench {
			if e.lvl > 0 {
				b = protocol.AppendVarInt(b, int32(e.id))
				b = protocol.AppendVarInt(b, int32(e.lvl))
			}
		}
	}
	if st.name != "" {
		b = protocol.AppendVarInt(b, componentCustomName)
		b = append(b, chatNBT(st.name)...)
	}
	if patN > 0 {
		// banner_patterns: layer count + (pattern holder = id+1, dye) pairs —
		// exactly the stored form.
		b = protocol.AppendVarInt(b, componentBannerPats)
		b = protocol.AppendVarInt(b, patN)
		for _, l := range st.pats[:patN] {
			b = protocol.AppendVarInt(b, int32(l.patPlus1))
			b = protocol.AppendVarInt(b, int32(l.color))
		}
	}
	if st.trimMat != 0 || st.trimPat != 0 {
		// trim: material + pattern holders (stored +1-encoded, = holder refs).
		b = protocol.AppendVarInt(b, componentTrim)
		b = protocol.AppendVarInt(b, int32(st.trimMat))
		b = protocol.AppendVarInt(b, int32(st.trimPat))
	}
	if st.potion != potNone {
		// The potion field does double duty: on a bottle from a raid captain
		// it is the Bad Omen level, not a brew, and sending that as
		// potion_contents would give the bottle some unrelated potion's
		// colour and effect list.
		if st.item == itemOminousBottle {
			b = protocol.AppendVarInt(b, componentOminousBottle)
			b = protocol.AppendVarInt(b, int32(ominousBottleLevel(st)-1)) // stored level, wire amplifier
		} else {
			// potion_contents: the brew's effects. The client colours the
			// liquid from them and lists them in the tooltip — without it
			// every potion is the same purple and says nothing about what it
			// does.
			b = protocol.AppendVarInt(b, componentPotionContents)
			b = append(b, potionComponentBytes(st.potion)...)
		}
	}
	if e, ok := stewEffectOf(st.stew); ok {
		// suspicious_stew_effects: (effect holder, duration) pairs. The stew
		// keeps its secret — vanilla shows these only on a creative tooltip —
		// but it is a synced component, so it belongs on the wire.
		b = protocol.AppendVarInt(b, componentStewEffects)
		b = protocol.AppendVarInt(b, 1)
		b = protocol.AppendVarInt(b, e.effect+1) // holder ref = id + 1
		b = protocol.AppendVarInt(b, int32(e.secs*20))
	}
	if st.repairCost > 0 {
		b = protocol.AppendVarInt(b, componentRepairCost)
		b = protocol.AppendVarInt(b, int32(st.repairCost))
	}
	if st.item == itemFireworkRocket {
		// fireworks: how long the rocket flies, then the bursts it shows.
		bursts := burstsOf(st)
		if len(bursts) > maxRocketBursts {
			bursts = bursts[:maxRocketBursts]
		}
		b = protocol.AppendVarInt(b, componentFireworks)
		b = protocol.AppendVarInt(b, int32(rocketFlight(st)))
		b = protocol.AppendVarInt(b, int32(len(bursts)))
		for _, e := range bursts {
			b = appendBurst(b, e)
		}
	}
	if !st.sherds.empty() {
		// pot_decorations: the pot's four faces as item ids, back first. The
		// ids inside are remapped for the client like the stack's own.
		b = protocol.AppendVarInt(b, componentPotDecorations)
		b = protocol.AppendVarInt(b, int32(len(st.sherds)))
		for _, f := range st.sherds {
			if f == 0 {
				f = itemBrick
			}
			b = protocol.AppendVarInt(b, f)
		}
	}
	if bursts := burstsOf(st); st.item == itemFireworkStar && len(bursts) == 1 {
		// firework_explosion: the single burst a star carries, which is what
		// draws its colours in the tooltip and on the item itself.
		b = protocol.AppendVarInt(b, componentFireworkStar)
		b = appendBurst(b, bursts[0])
	}
	if boxBytes != nil {
		// container: what a broken shulker box is carrying. The client draws
		// the first few of them under the item's name, which is the whole
		// point of being able to pick a full box up.
		b = protocol.AppendVarInt(b, componentContainer)
		b = append(b, boxBytes...)
	}
	if st.lode.has {
		b = lodestoneComponent(b, st.lode)
	}
	if st.shieldBase != 0 {
		// base_color: the banner base under a decorated shield's patterns,
		// one DyeColor varint (the layers above ride in banner_patterns).
		b = protocol.AppendVarInt(b, componentBaseColor)
		b = protocol.AppendVarInt(b, int32(st.shieldBase-1))
	}
	b = append(b, bookBytes...) // writable/written book content (see book.go)
	return b
}

// itemMetadata builds set_entity_metadata (0x5c) setting an item entity's stack
// (index 8, Slot type) and terminating the list.
func itemMetadata(eid int32, st invStack) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, itemMetaIndexStack)
	b = protocol.AppendVarInt(b, itemMetaTypeSlot)
	b = appendStack(b, st)
	b = protocol.AppendU8(b, itemMetaEnd)
	return b
}
