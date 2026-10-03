package server

import (
	"log"
	"slices"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// spawnEggEntity maps each *_spawn_egg item to the entity it spawns
// (SpawnEggItem.getType), so a dispenser can spawn the mob it faces.
var spawnEggEntity = func() map[int32]int {
	m := map[int32]int{}
	for name, id := range itemByName {
		base, ok := strings.CutSuffix(name, "_spawn_egg")
		if !ok {
			continue
		}
		if et, ok := entityByName[base]; ok {
			m[int32(id)] = et
		}
	}
	return m
}()

// Bins: the shared container behind dispensers, droppers (9 slots,
// generic_3x3) and hoppers (5 slots). Dispensers/droppers eject on a rising
// redstone edge (the `triggered` property tracks the edge); hoppers move one
// item every 8 ticks — pull from the container above (or suck item entities
// above/inside), push into the container they face — and pause while powered.

const (
	menuGeneric3x3 = 6 // static menu registry, vanilla order
	menuHopper     = 16

	hopperCadence = 8 // game ticks per moved item (vanilla)
)

var (
	hopperMin = worldgen.BlockBase("hopper") // enabled(2) × facing(down,north,south,west,east)
	hopperMax = worldgen.BlockBase("hopper") + 9
)

var (
	itemBucket       = itemByName["bucket"]
	itemBucketH2O    = itemByName["water_bucket"]
	itemBucketLav    = itemByName["lava_bucket"]
	itemFireCharge   = int32(itemByName["fire_charge"])
	itemWindCharge   = itemByName["wind_charge"]
	itemWitherSkull  = itemByName["wither_skeleton_skull"]
	itemBlueEgg      = itemByName["blue_egg"]
	itemBrownEgg     = itemByName["brown_egg"]
	itemSpectralArr  = itemByName["spectral_arrow"]
	itemTippedArrow  = itemByName["tipped_arrow"]
	itemPowderBucket = itemByName["powder_snow_bucket"]
	witherSkullBlock = worldgen.BlockBase("wither_skeleton_skull")
	powderSnowBlock  = worldgen.BlockBase("powder_snow")
	rootedDirtBlock  = worldgen.BlockBase("rooted_dirt")
)

func isHopper(s uint32) bool { return s >= hopperMin && s <= hopperMax }

func hopperEnabled(s uint32) bool { return (s-hopperMin)/5 == 0 } // enabled=true first
func hopperWith(s uint32, enabled bool) uint32 {
	f := (s - hopperMin) % 5
	if enabled {
		return hopperMin + f
	}
	return hopperMin + 5 + f
}

// hopperDelta is the push direction (down or a cardinal).
func hopperDelta(s uint32) (int, int, int) {
	switch (s - hopperMin) % 5 {
	case 0:
		return 0, -1, 0
	case 1:
		return 0, 0, -1 // north
	case 2:
		return 0, 0, 1 // south
	case 3:
		return -1, 0, 0 // west
	}
	return 1, 0, 0 // east
}

// bin is the storage for one dispenser/dropper/hopper block.
type bin struct {
	slots    []invStack
	disabled [9]bool // crafter only: grid slots toggled off (hoppers skip; recipe holes)
	// Hopper only (HopperBlockEntity): the transfer cooldown, counted down
	// every tick, and the game tick it last ticked on (tickedGameTime), which
	// decides whether a hopper fed by another waits eight ticks or seven.
	cooldown int
	ticked   uint64
}

// binAt is the storage behind a dispenser/dropper/hopper block, created on
// first touch — and, for a structure's dispenser, filled from its loot
// table then (the jungle temple's arrow traps).
func (h *hub) binAt(pos simPos, state uint32) *bin {
	c := h.bins[pos]
	if c == nil {
		c = &bin{slots: make([]invStack, binSizeFor(state)), cooldown: -1}
		if name, ok := h.structureBinTable(pos.dim, pos.blockPos); ok {
			h.fillSlots(c.slots, name, pos.blockPos)
		}
		h.bins[pos] = c
	}
	return c
}

// mobInBlock reports whether a mob's bounding box overlaps the single-block
// cube at p (the dispenser's front cell) — the AABB(blockPos) test vanilla's
// shears dispense uses.
func mobInBlock(m *mob, p blockPos) bool {
	const hw = 0.45 // most animals are ~0.9 wide
	return m.x+hw > float64(p.x) && m.x-hw < float64(p.x)+1 &&
		m.z+hw > float64(p.z) && m.z-hw < float64(p.z)+1 &&
		m.y < float64(p.y)+1 && m.y+1.3 > float64(p.y)
}

func binSizeFor(state uint32) int {
	if isHopper(state) || isBrewStand(state) {
		return 5
	}
	return 9
}

type evOpenBin struct {
	eid     int32
	x, y, z int
}

func (evOpenBin) isHubEvent() {}

// openBin opens the dispenser/dropper/hopper window at a block position.
func (h *hub) openBin(t *tracked, x, y, z int) {
	if t.inv == nil {
		return
	}
	state := h.worldFor(t.dim).At(x, y, z)
	if isCrafter(state) {
		h.openCrafter(t, x, y, z) // its own menu: result preview + disabled slots
		return
	}
	menu, title := int32(menuGeneric3x3), "Dispenser"
	switch {
	case isDropper(state):
		title = "Dropper"
	case isHopper(state):
		menu, title = menuHopper, "Item Hopper"
	case isBrewStand(state):
		menu, title = menuBrewing, "Brewing Stand"
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	pos := simPos{dim: t.dim, blockPos: blockPos{x, y, z}}
	c := h.binAt(pos, state)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind = h.nextWin, pos, winBin // no opener counter: no CONTAINER_OPEN

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menu), Title: h.containerTitle(pos, title)})
	h.sendBinWindow(t, c)
}

// sendBinWindow refreshes the whole bin window: container + main + hotbar.
func (h *hub) sendBinWindow(t *tracked, c *bin) {
	t.inv.stateId++
	n := len(c.slots)
	slots := make([]attachproto.ItemStack, 0, n+36)
	for i := 0; i < n; i++ {
		slots = append(slots, stackEv(c.slots[i]))
	}
	for i := 9; i <= 35; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// binInsert moves count items of a stack into a slot list (merge, then empty
// slots). Returns how many did NOT fit.
func binInsert(slots []invStack, st invStack) int {
	left := st.count
	for i := range slots {
		s := &slots[i]
		if left == 0 {
			break
		}
		if s.count > 0 && sameItemComponents(*s, st) {
			room := stackCap(s.item) - s.count // per-item cap: 16 for eggs, 1 for tools, not a flat 64
			if room <= 0 {
				continue
			}
			take := left
			if take > room {
				take = room
			}
			s.count += take
			left -= take
		}
	}
	for i := range slots {
		if left == 0 {
			break
		}
		if slots[i].item == 0 || slots[i].count == 0 {
			take := left
			if cap := stackCap(st.item); take > cap {
				take = cap // an empty slot takes at most one stack of the item
			}
			slots[i] = st
			slots[i].count = take
			left -= take
		}
	}
	return left
}

// binFireDelay is vanilla DispenserBlock's TICK_DELAY: a dispenser/dropper
// ejects 4 ticks after its rising redstone edge, not immediately.
const binFireDelay = 4

// updateBinTrigger is the dispenser/dropper redstone step (vanilla
// DispenserBlock.neighborChanged): on the rising edge it latches `triggered`
// and schedules the ejection 4 ticks out; the falling edge just clears the
// latch. The power test includes quasi-connectivity — a signal at the block
// directly ABOVE the dispenser counts, the classic QC quirk (BUD-prone, since
// the dispenser only re-checks when it or a direct neighbour updates).
func (h *hub) updateBinTrigger(players map[int32]*tracked, pos simPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0 ||
		h.inputPower(pos.x, pos.y+1, pos.z, false) > 0
	triggered := boolProp(state, "triggered")
	if powered == triggered {
		return
	}
	h.setBlockAt(players, pos.dim, pos.blockPos, setBoolProp(state, "triggered", powered))
	if powered {
		// Vanilla scheduleTick(pos, 4): the dispense fires later, unconditionally
		// (a pulse shorter than the delay still ejects). A tick already
		// pending stays as it is (LevelTicks keeps one per position).
		if _, pending := h.binFire[pos]; !pending {
			h.binFire[pos] = h.tick.Load() + binFireDelay
		}
	}
}

// runBinFires ejects from every dispenser/dropper whose scheduled fire tick has
// come due (vanilla DispenserBlock.tick → dispenseFrom). The fire is
// unconditional: it does not re-check power, so a sub-4-tick pulse still fires.
func (h *hub) runBinFires(players map[int32]*tracked, age uint64) {
	for pos, due := range h.binFire {
		if due > age {
			continue
		}
		delete(h.binFire, pos)
		if !h.inWorldYIn(pos.dim, pos.y) {
			continue
		}
		w := h.worldFor(pos.dim)
		if w == nil {
			continue
		}
		state := w.Block(pos.x, pos.y, pos.z)
		if isDispenser(state) || isDropper(state) { // still a bin (not broken meanwhile)
			// The cell's own dimension for the whole ejection, and back after.
			h.inDim(pos.dim, func() { h.ejectFromBin(players, pos, state) })
		}
	}
}

// convertableToMud is the vanilla #convertable_to_mud block tag — the blocks a
// dispensed water bottle turns to mud.
func convertableToMud(s uint32) bool {
	return s == worldgen.Dirt || s == worldgen.CoarseDirt || s == rootedDirtBlock
}

// ProjectileItem.DispenseConfig's defaults, and the charges' own uncertainty.
const (
	dispenseShotPower         = 1.1
	dispenseShotUncertainty   = 6.0
	chargeDispenseUncertainty = 6.6666665
)

// ejectFromBin fires/drops the first non-empty slot's item out of the face.
func (h *hub) ejectFromBin(players map[int32]*tracked, pos simPos, state uint32) {
	// Every block this behaviour table touches is in the dispenser's OWN world.
	// Redstone only drives dispensers in the overworld today, so this is
	// belt-and-braces — but it is the difference between a Nether dispenser
	// being inert and one that quietly edits the overworld.
	w := h.worldFor(pos.dim)
	if w == nil {
		return
	}
	c := h.binAt(pos, state)
	arrowsBefore := len(h.arrows) // a projectile behaviour plays the launch sound, not the dispense click
	// Vanilla getRandomSlot: a uniformly random non-empty slot (reservoir
	// sampling), not the first one — so a dispenser empties unpredictably.
	var st *invStack
	seen := 0
	for i := range c.slots {
		if c.slots[i].item != 0 && c.slots[i].count > 0 {
			seen++
			if h.rng.Intn(seen) == 0 {
				st = &c.slots[i]
			}
		}
	}
	if st == nil { // DispenserBlock.dispenseFrom: levelEvent 1001 and BLOCK_ACTIVATE
		h.levelEvent(players, pos.dim, worldEventDispenseFail, pos.x, pos.y, pos.z, 0)
		h.vib(pos.dim, freqBlockActivate, pos.x, pos.y, pos.z, 0)
		return
	}
	dx, dy, dz := pistonDelta(state) // same 6-way facing math
	fx, fy, fz := float64(pos.x)+0.5+float64(dx)*0.7, float64(pos.y)+0.5+float64(dy)*0.7, float64(pos.z)+0.5+float64(dz)*0.7
	// ProjectileDispenseBehavior: a projectile leaves from its DispenseConfig
	// position — by default 0.7 out of the face and 0.1 up — and is shot
	// along the facing at its power with its uncertainty (default 1.1 and 6).
	shotFrom := func(scale, lift float64) (float64, float64, float64) {
		return float64(pos.x) + 0.5 + float64(dx)*scale, float64(pos.y) + 0.5 + float64(dy)*scale + lift, float64(pos.z) + 0.5 + float64(dz)*scale
	}
	shot := func(pow, uncertainty float64) (float64, float64, float64) {
		return h.shootVector(float64(dx), float64(dy), float64(dz), pow, uncertainty)
	}
	shoot := func(etype, dim int, pow, uncertainty float64) *arrowEntity {
		px, py, pz := shotFrom(0.7, 0.1)
		vx, vy, vz := shot(pow, uncertainty)
		return h.launchProjectileIn(players, etype, dim, px, py, pz, vx, vy, vz)
	}
	front := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	item := st.item
	dispense := isDispenser(state)

	// Dropper facing a container inserts one item instead of tossing it
	// (vanilla DropperBlock): the redstone-driven item pipe.
	if !dispense {
		if dst := h.containerSlots(pos.at(front)); dst != nil {
			// HopperBlockEntity.addItem with stack.copyWithCount(1): the item
			// goes in whole, data and all, and quietly — no dispense sound or
			// smoke for a container insert.
			one := *st
			one.count = 1
			if h.insertByFace(pos.at(front), dy, one) {
				if st.count--; st.count <= 0 {
					*st = invStack{}
				}
				h.containerChanged(players, pos.at(front)) // update anyone viewing the target
			}
			h.containerChanged(players, pos)
			return
		}
	}
	// toss is DefaultDispenseItemBehavior: one of the stack flies out whole —
	// a potion keeps its brew, dyed or named armour its colour and name, a
	// filled bundle its contents. Every behaviour that falls back to a toss
	// uses it.
	//
	// DefaultDispenseItemBehavior.spawnItem: the item starts 0.7 out of the
	// face, dropped an eighth (up or down) or 5/32 (sideways) so it clears
	// the rim, and is thrown — outward at 0.2-0.3 along the facing, 0.2
	// upward whatever the facing, each spread by triangle(·, 0.0172275 × 6).
	// It flies and lands under the item physics like any other drop.
	toss := func() {
		sy := fy - 0.15625
		if dy != 0 {
			sy = fy - 0.125
		}
		pow := h.rng.Float64()*0.1 + 0.2
		const spread = 0.0172275 * 6
		vx, vy, vz := h.triangle(float64(dx)*pow, spread), h.triangle(0.2, spread), h.triangle(float64(dz)*pow, spread)
		if it := h.spawnItemAt(players, h.rsDim, item, 1, fx, sy, fz, vx, vy, vz); it != nil {
			one := *st
			one.count = 1
			it.setFrom(one)
			h.refreshItemMeta(players, it)
		}
	}
	eggEnt, isEgg := spawnEggEntity[item]
	vehEt, isVeh := vehicleItems[item]
	took := true
	failed := false // an OptionalDispenseItemBehavior that did nothing: the fail click (1001)
	switch {
	case dispense && (item == itemArrowAmmo || item == itemSpectralArr || item == itemTippedArrow):
		// Vanilla registerProjectileBehavior: plain, spectral and tipped arrows
		// each fire as themselves — a tipped arrow carries its potion onto a
		// hit, a spectral one makes what it hits glow (SpectralArrow).
		one := *st
		one.count = 1
		a := shoot(arrowEntityFor(one), h.rsDim, dispenseShotPower, dispenseShotUncertainty)
		a.dmg, a.playerShot = arrowDamage, true // hits mobs; retrievable when stuck
		loadArrow(a, one)
	case dispense && item == itemSnowball:
		shoot(entitySnowball, h.rsDim, dispenseShotPower, dispenseShotUncertainty).breaks = true
	case dispense && (item == itemEgg || item == itemBlueEgg || item == itemBrownEgg):
		// All three egg variants throw as an egg projectile (vanilla registers
		// BLUE_EGG/BROWN_EGG alongside EGG).
		a := shoot(entityEggProj, h.rsDim, dispenseShotPower, dispenseShotUncertainty)
		a.breaks, a.egg, a.eggItem = true, true, item // a dispensed egg hatches too
	case dispense && item == itemFireCharge:
		// An ownerless small fireball: the same burn and 5 damage as a blaze's,
		// from a full block out, at power 1 (FireChargeItem's config).
		px, py, pz := shotFrom(1, 0)
		vx, vy, vz := shot(1, chargeDispenseUncertainty)
		a := h.launchProjectileIn(players, entitySmallFireball, h.rsDim, px, py, pz, vx, vy, vz)
		a.dmg, a.fire = blazeFireballDmg, true
	case dispense && item == itemXPBottle:
		// Vanilla registerProjectileBehavior: the bottle flies and shatters.
		// ExperienceBottleItem's config: half the scatter, a quarter more power.
		a := shoot(entityXPBottle, pos.dim, dispenseShotPower*1.25, dispenseShotUncertainty*0.5)
		a.breaks, a.xpBottle = true, true
	case dispense && item == itemFireworkRocket:
		// A rocket leaves along the facing (FireworkRocketItem's projectile
		// config: power 0.5) and climbs from there.
		r := h.spawnRocket(players, pos.dim, fx, fy, fz, 0, *st)
		r.vx, r.vy, r.vz = float64(dx)*0.5, float64(dy)*0.5, float64(dz)*0.5
	case dispense && item == itemSulfurCubeBucket:
		// The sulfur cube bucket holds no fluid: only the cube comes out.
		took = false
		if ts := w.At(front.x, front.y, front.z); ts == worldgen.Air || worldgen.IsReplaceable(ts) || worldgen.IsWater(ts) {
			h.releaseSulfurBucket(players, pos.dim, *st, front.x, front.y, front.z)
			*st = invStack{item: itemBucket, count: 1}
		}
	case dispense && isMobBucket(item):
		// A mob bucket pours its water and its passenger into the cell ahead
		// and leaves an empty bucket (DispensibleContainerItem.emptyContents).
		took = false
		if ts := w.At(front.x, front.y, front.z); ts == worldgen.Air || worldgen.IsReplaceable(ts) || worldgen.IsWater(ts) {
			if !worldgen.IsWater(ts) {
				h.setBlockAt(players, pos.dim, front, worldgen.WaterBase)
			}
			h.releaseBucketMob(players, pos.dim, *st, front.x, front.y, front.z)
			st.item, st.count = itemBucket, 1
		}
	case dispense && item == int32(itemByName["chest"]) && h.dispenseChest(players, pos.dim, front):
		// Strapped onto a tamed, unchested donkey, mule or llama in front.
	case dispense && item == itemCarvedPumpkin:
		// The CARVED_PUMPKIN behaviour: placed only into an empty cell where
		// it completes a golem (canSpawnGolem); otherwise it is a head for
		// whoever stands there, and with nobody to wear it the dispenser
		// fails and keeps it.
		if w.At(front.x, front.y, front.z) == worldgen.Air && h.canSpawnGolem(pos.dim, front) {
			h.dispenseCarvedPumpkin(players, pos.dim, front)
		} else {
			took = false
			failed = !h.dispenseEquipment(players, pos.dim, front, st)
		}
	case dispense && isShulkerBoxItem(item):
		// ShulkerBoxDispenseBehavior: the box is placed in the cell ahead, its
		// contents intact, opening along the dispense direction (upward when
		// there is nothing under it).
		if ts := w.At(front.x, front.y, front.z); ts == worldgen.Air || worldgen.IsReplaceable(ts) {
			h.dispenseShulkerBox(players, pos.dim, state, front, st)
		} else {
			took = false
		}
	case dispense && item == itemGlowstoneBlock && h.dispenseGlowstone(players, pos.dim, front):
		// Charged a respawn anchor ahead (else the glowstone is tossed below).
	case dispense && item == itemBrush:
		// Brushes the armadillo in front for a scute, wearing the brush 16.
		took = false
		if h.dispenseBrush(players, pos.dim, front) {
			h.wearDispensed(st, 16)
		}
	case dispense && item == itemWindCharge:
		// Vanilla WindChargeItem projectile behaviour — the same burst the Breeze
		// throws, launched out of the face.
		// WindChargeItem's config, like the fire charge's: a block out, power 1.
		px, py, pz := shotFrom(1, 0)
		vx, vy, vz := shot(1, chargeDispenseUncertainty)
		h.launchProjectileIn(players, entityWindCharge, h.rsDim, px, py, pz, vx, vy, vz)
	case dispense && (item == itemSplashPotion || item == itemLingerPotion):
		// Thrown-potion projectile: shatters into a splash / lingering cloud
		// carrying this stack's potion kind.
		lingering := item == itemLingerPotion
		// ThrowablePotionItem's config: half the scatter, a quarter more power.
		a := shoot(thrownPotionType(lingering), h.rsDim, dispenseShotPower*1.25, dispenseShotUncertainty*0.5)
		a.splash, a.breaks, a.potion, a.lingering = true, true, st.potion, lingering
	case dispense && item == itemPotion && st.potion == potWater:
		// Vanilla POTION behaviour: a WATER bottle onto a CONVERTABLE_TO_MUD block
		// (dirt / coarse dirt / rooted dirt) turns it to mud and empties the
		// bottle to glass; anything else falls back to the default toss.
		if convertableToMud(w.At(front.x, front.y, front.z)) {
			h.rsSet(players, front, worldgen.Mud)
			h.spawnParticles(players, pos.dim, particleSplash, float64(front.x)+0.5, float64(front.y)+1, float64(front.z)+0.5, 0.3, 0.1, 5)
			h.rsSound(players, "minecraft:item.bottle.empty", sndBlock,
				float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
			*st = invStack{item: itemGlassBottle, count: 1}
			took = false
		} else {
			toss()
		}
	case dispense && item == itemGlassBottle:
		// Vanilla GLASS_BOTTLE behaviour: a FULL hive ahead fills the bottle
		// with honey and releases the bees calm (the hive branch is tried
		// before water); else fill from a water source → water bottle (the
		// source is not drained). Otherwise toss like the default.
		if fs := w.At(front.x, front.y, front.z); isBeeHome(fs) && honeyLevel(fs) >= beeMaxHoney {
			took = false
			h.releaseHiveBees(players, h.rsDim, front, nil)
			h.rsSet(players, front, withHoney(fs, 0))
			hb := invStack{item: itemHoneyBottle, count: 1}
			if st.count <= 1 {
				*st = hb
			} else {
				st.count--
				if binInsert(c.slots, hb) > 0 {
					h.spawnItemIn(players, h.rsDim, itemHoneyBottle, 1, fx, fy, fz) // no room: pop the bottle out
				}
			}
		} else if worldgen.IsWater(w.At(front.x, front.y, front.z)) {
			took = false
			wb := potionStack(potWater)
			if st.count <= 1 {
				*st = wb
			} else {
				st.count--
				if binInsert(c.slots, wb) > 0 {
					h.spawnItemIn(players, h.rsDim, itemGlassBottle, 1, fx, fy, fz) // no room: refund a bottle
				}
			}
		} else {
			toss()
		}
	case dispense && item == itemWitherSkull:
		// Vanilla WITHER_SKELETON_SKULL behaviour (a dedicated behaviour that
		// overrides the default equip-a-wearable): place the skull in the empty
		// cell ahead only where the wither's base stands under it
		// (canSpawnMob), and try to raise a wither; otherwise it is a head for
		// whoever stands there, and with nobody the dispenser fails and keeps it.
		took = false
		if w.At(front.x, front.y, front.z) == worldgen.Air && h.canSpawnWither(pos.dim, front) {
			h.rsSet(players, front, witherSkullBlock)
			h.checkWitherBuild(players, 0, pos.dim, front.x, front.y, front.z, witherSkullBlock) // no builder: a machine placed the skull
			took = true
		} else {
			failed = !h.dispenseEquipment(players, pos.dim, front, st)
		}
	case dispense && item == itemArmorStand:
		// Vanilla ARMOR_STAND behaviour: spawn a stand on the cell ahead facing
		// away from the dispenser; if the cell is occupied, toss the item.
		if w.At(front.x, front.y, front.z) == worldgen.Air {
			yaw := float32(0) // toYRot: south=0, west=90, north=180, east=270
			switch {
			case dx < 0:
				yaw = 90
			case dz < 0:
				yaw = 180
			case dx > 0:
				yaw = 270
			}
			sd := &armorStand{eid: h.allocEID(), dim: pos.dim,
				x: float64(front.x) + 0.5, y: float64(front.y), z: float64(front.z) + 0.5, yaw: yaw, name: st.name}
			sd.applyItemTags(st.standTags) // createDefaultStackConfig: the item's entity_data
			h.armorStands[sd.eid] = sd
			h.toNearbyEv(players, sd.dim, sd.x, sd.z, h.standAddEv(sd))
			if sd.name != "" {
				h.toNearbyEv(players, sd.dim, sd.x, sd.z, metaEv(nameMeta(sd.eid, sd.name)))
			}
			if sd.flagged() {
				h.toNearbyEv(players, sd.dim, sd.x, sd.z, metaEv(standMeta(sd)))
			}
			h.rsSound(players, "minecraft:entity.armor_stand.place", sndBlock, sd.x, sd.y, sd.z, 0.75, 0.8)
		} else {
			toss()
		}
	case dispense && dispenseEquippable(item):
		// Vanilla EquipmentDispenseItemBehavior: put the piece on the first
		// living thing in the cell ahead that can wear it; else toss it.
		if h.dispenseEquipment(players, pos.dim, front, st) {
			took = false // dispenseEquipment took its one
		} else {
			toss()
		}
	case dispense && item == itemTNTBlock:
		// The TNT behaviour spawns a lit charge in the cell ahead and leaves
		// whatever block stands there alone; with tnt_explodes off it
		// dispenses nothing.
		if !h.rules.TNTExplodes {
			took = false
			break
		}
		h.spawnPrimedTNT(players, h.rsDim, front.x, front.y, front.z, tntFuseTicks)
		h.vib(h.rsDim, freqEntityPlace, front.x, front.y, front.z, 0)
	case dispense && item == itemFlintSteel:
		// FlintAndSteelDispenseItemBehavior: an explosive sulfur cube in the
		// cell is lit first; else a fire, a lightable block or TNT. Only TNT
		// that will not prime (tnt_explodes off) spares the tool; anything
		// else, even lighting nothing, wears it a point.
		took = false // vanilla damages the tool instead of consuming it
		lit := true
		if cube := h.cubeInCell(pos.dim, front); cube != nil {
			h.primeSulfurCube(players, cube, false)
		} else if fs := w.At(front.x, front.y, front.z); fs == worldgen.Air {
			h.igniteFire(players, front, 0) // light a fire in the cell ahead
		} else if canLightBlock(fs) {
			h.lightBlock(players, pos.dim, front, fs, sndFlintSteelUse)
		} else if isTNT(fs) {
			lit = h.primeTNTBy(players, pos.dim, front.x, front.y, front.z, tntFuseTicks, 0) != nil
		}
		if lit {
			h.wearDispensed(st, 1)
		}
	case dispense && item == itemBoneMeal:
		if !h.applyBoneMeal(players, pos.dim, front.x, front.y, front.z, w.At(front.x, front.y, front.z)) {
			// nothing growable ahead → fall back to tossing the meal out
			toss()
		}
	case dispense && isEgg:
		// Spawn the egg's mob in the block ahead (facing offset so it clears
		// the dispenser); vanilla consumes the egg whether or not it takes.
		h.spawnConfigured(players, eggEnt, h.rsDim, float64(front.x)+0.5, float64(front.y), float64(front.z)+0.5)
	case dispense && item == int32(itemShears):
		// A full hive ahead is sheared first (tryShearBeehive): three honeycomb
		// pop out and the bees are released CALM — a dispenser has nobody to
		// blame. Otherwise shear the first shearable mob overlapping the cell
		// (ShearsDispenseItemBehavior); the tool wears rather than ejects.
		took = false
		sheared := false
		if fs := w.At(front.x, front.y, front.z); isBeeHome(fs) && honeyLevel(fs) >= beeMaxHoney {
			h.rsSound(players, "minecraft:block.beehive.shear", sndBlock,
				float64(front.x)+0.5, float64(front.y)+0.5, float64(front.z)+0.5, 1, 1)
			h.spawnItemIn(players, h.rsDim, int32(itemHoneycomb), beeHoneycombYield,
				float64(front.x)+0.5, float64(front.y)+0.5, float64(front.z)+0.5)
			h.releaseHiveBees(players, h.rsDim, front, nil)
			h.rsSet(players, front, withHoney(fs, 0))
			sheared = true
		} else {
			for _, m := range h.mobs {
				if m.dim == pos.dim && mobInBlock(m, front) && h.shearMob(players, m) {
					sheared = true
					break
				}
			}
		}
		if sheared {
			h.wearDispensed(st, 1)
		}
	case dispense && isVeh:
		// Place a boat/minecart in the cell ahead (rail for carts, water for
		// boats); if it can't be placed, toss the item like the default.
		yaw := float32(0) // Direction.toYRot: south=0, west=90, north=180, east=270
		switch {
		case dx < 0:
			yaw = 90
		case dz < 0:
			yaw = 180
		case dx > 0:
			yaw = 270
		}
		if !h.spawnVehicleFacing(players, pos.dim, vehEt, front.x, front.y, front.z, yaw) {
			toss()
		}
	case dispense && item == int32(itemHoneycomb):
		// Wax the copper block ahead (HoneycombItem.getWaxed); otherwise toss.
		if ws, ok := waxedCopper(w.At(front.x, front.y, front.z)); ok {
			h.rsSet(players, front, ws)
		} else {
			toss()
		}
	case dispense && (item == itemBucketH2O || item == itemBucketLav):
		// Pour the bucket's fluid into the cell ahead (buckets are no longer
		// placeable items — the engine owns fluid placement since #56).
		fluid := uint32(worldgen.WaterBase)
		if item == itemBucketLav {
			fluid = worldgen.LavaBase
		}
		took = false
		if ts := w.At(front.x, front.y, front.z); ts == worldgen.Air || worldgen.IsReplaceable(ts) {
			h.rsSet(players, front, fluid)
			st.item = itemBucket // filled bucket (stack size 1) empties in place
		}
	case dispense && item == itemPowderBucket:
		// Powder snow is a solid block, but the bucket empties like a fluid one:
		// pour it into the cell ahead and leave an empty bucket in the slot.
		took = false
		if ts := w.At(front.x, front.y, front.z); ts == worldgen.Air || worldgen.IsReplaceable(ts) {
			h.rsSet(players, front, powderSnowBlock)
			st.item = itemBucket
		}
	case dispense && item == itemBucket:
		// Scoop a fluid source in the cell ahead into an empty bucket.
		took = false
		var filled int32
		switch w.At(front.x, front.y, front.z) {
		case worldgen.WaterBase:
			filled = itemBucketH2O
		case worldgen.LavaBase:
			filled = itemBucketLav
		}
		if filled != 0 {
			h.rsSet(players, front, worldgen.Air)
			if st.count <= 1 {
				*st = invStack{item: filled, count: 1}
			} else { // empty buckets stack — the filled one finds its own slot
				st.count--
				if binInsert(c.slots, invStack{item: filled, count: 1}) > 0 {
					h.spawnItemIn(players, h.rsDim, filled, 1, fx, fy, fz)
				}
			}
		}
	default: // dropper (or a dispenser with a plain item): toss it out
		toss()
	}
	if took {
		st.count--
		if st.count <= 0 {
			*st = invStack{}
		}
	}
	// The behaviour's playSound is a level event: the dispense click, the
	// fail click of an OptionalDispenseItemBehavior that did nothing, or a
	// projectile's launch — which a rocket and a wind charge override with
	// their own (DispenseConfig.overrideDispenseEvent).
	ev := int32(worldEventDispense)
	switch {
	case failed:
		ev = worldEventDispenseFail
	case dispense && item == itemFireworkRocket:
		ev = worldEventFireworkShoot
	case dispense && item == itemWindCharge:
		ev = worldEventWindChargeShoot
	case len(h.arrows) > arrowsBefore:
		ev = worldEventDispenseLaunch
	}
	h.levelEvent(players, pos.dim, ev, pos.x, pos.y, pos.z, 0)
	h.levelEvent(players, pos.dim, worldEventDispenserSmoke, pos.x, pos.y, pos.z, dir3D(dx, dy, dz)) // the puff out of the face
	h.containerChanged(players, pos)
}

// hopperPowerCheck is HopperBlock.checkPoweredState: ENABLED is the inverse
// of power, flipped the moment a neighbour changes. Item transfers stay on
// the hopper's own cadence (updateHopper).
func (h *hub) hopperPowerCheck(players map[int32]*tracked, pos simPos, state uint32) {
	var powered bool
	h.inDim(pos.dim, func() { powered = h.inputPower(pos.x, pos.y, pos.z, false) > 0 })
	if hopperEnabled(state) == powered {
		h.setBlockAt(players, pos.dim, pos.blockPos, hopperWith(state, !powered))
	}
}

// updateHopper is the hopper's scheduled/neighbour update: ENABLED follows
// the inverse of power, and the hopper joins the block-entity tickers. Item
// movement is tickHoppers' business alone — driving it from here gave every
// block update beside a hopper a transfer chain of its own, and they stacked.
func (h *hub) updateHopper(players map[int32]*tracked, pos simPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if hopperEnabled(state) == powered { // enabled must be the inverse of powered
		state = hopperWith(state, !powered)
		h.setBlockAt(players, pos.dim, pos.blockPos, state)
	}
	h.binAt(pos, state)
	h.registerHopper(pos)
}

// registerHopper adds a hopper to the block-entity tickers, once, in the
// order hoppers appear (vanilla ticks block entities in the order they were
// added).
func (h *hub) registerHopper(pos simPos) {
	if h.hopperTicking == nil {
		h.hopperTicking = map[simPos]bool{}
	}
	if !h.hopperTicking[pos] {
		h.hopperTicking[pos] = true
		h.hopperOrder = append(h.hopperOrder, pos)
	}
}

// tickHoppers is every hopper's HopperBlockEntity.pushItemsTick, once per
// game tick: the cooldown counts down, and a hopper off cooldown and enabled
// pushes one item out (ejectItems) and, unless full, takes one in
// (suckInItems); if either moved anything it waits eight ticks. An idle
// hopper so answers on the very next tick, not on an eight-tick beat.
func (h *hub) tickHoppers(players map[int32]*tracked) {
	now := h.tick.Load()
	keep := h.hopperOrder[:0]
	for _, pos := range h.hopperOrder {
		w := h.worldFor(pos.dim)
		if w == nil {
			delete(h.hopperTicking, pos)
			continue
		}
		if !w.Loaded(int32(pos.x>>4), int32(pos.z>>4)) || !h.cellWithinBorder(pos.dim, pos.x, pos.z) {
			keep = append(keep, pos) // an unloaded hopper, or one past the world border, waits, as vanilla's does
			continue
		}
		state := w.At(pos.x, pos.y, pos.z)
		if !isHopper(state) {
			delete(h.hopperTicking, pos) // the block went: its ticker goes with it
			continue
		}
		keep = append(keep, pos)
		c := h.binAt(pos, state)
		c.cooldown--
		c.ticked = now
		if c.cooldown > 0 {
			continue
		}
		c.cooldown = 0
		if !hopperEnabled(state) {
			continue
		}
		moved := false
		if !binEmpty(c.slots) {
			moved = h.hopperPush(players, pos, state, c)
		}
		if !binFull(c.slots) && h.hopperPull(players, pos, c) {
			moved = true
		}
		if moved {
			c.cooldown = hopperCadence
			h.containerChanged(players, pos)
		}
	}
	h.hopperOrder = keep
}

// hopperFed is tryMoveInItem's wasEmpty rule: a hopper that was empty and
// receives an item waits eight ticks before passing it on — seven when the
// hopper feeding it has not ticked yet this tick — unless it is on a longer
// cooldown already.
func (h *hub) hopperFed(dst *bin, src *bin) {
	if dst.cooldown > hopperCadence {
		return
	}
	skip := 0
	if src != nil && dst.ticked >= src.ticked {
		skip = 1
	}
	dst.cooldown = hopperCadence - skip
}

// binEmpty and binFull are Container.isEmpty and HopperBlockEntity.inventoryFull.
func binEmpty(slots []invStack) bool {
	for _, st := range slots {
		if st.item != 0 && st.count > 0 {
			return false
		}
	}
	return true
}

func binFull(slots []invStack) bool {
	for _, st := range slots {
		if st.item == 0 || st.count <= 0 || st.count != stackCap(st.item) {
			return false
		}
	}
	return true
}

// hopperPull takes one item from the container above, or sucks up item
// entities sitting above or inside the hopper cell.
func (h *hub) hopperPull(players map[int32]*tracked, pos simPos, c *bin) bool {
	// HopperBlock.entityInside: an item that has fallen into the hopper's
	// own cell is taken whatever sits above it.
	if h.hopperTakeItems(players, pos, c, false) {
		return true
	}
	above := blockPos{pos.x, pos.y + 1, pos.z}
	// ComposterBlock is a WorldlyContainerHolder: a READY composter is a
	// one-slot container holding its bone meal, and taking it empties the bin.
	// That is what makes the hopper-under-composter farm work.
	if w := h.worldFor(pos.dim); w != nil {
		if lvl, ok := composterLevel(w.At(above.x, above.y, above.z)); ok && lvl == composterReady {
			one := invStack{item: itemBoneMeal, count: 1}
			if binInsert(c.slots, one) == 0 {
				h.setBlockAt(h.playersRef, pos.dim, above, composterBase)
				h.vib(pos.dim, freqBlockChange, above.x, above.y, above.z, 0)
				h.playSoundDim(h.playersRef, pos.dim, "minecraft:block.composter.empty", sndBlock,
					float64(above.x)+0.5, float64(above.y)+0.5, float64(above.z)+0.5, 1, 1)
				return true
			}
			return false
		}
	}
	if w := h.worldFor(pos.dim); w != nil && isDecoratedPot(w.At(above.x, above.y, above.z)) {
		if one, ok := h.potExtract(pos.at(above)); ok {
			if binInsert(c.slots, one) == 0 {
				return true
			}
			h.potInsert(pos.at(above), one) // no room below — put it back
		}
		return false
	}
	src := h.containerSlots(pos.at(above))
	if src != nil {
		for i := range src {
			s := &src[i]
			if s.item == 0 || s.count == 0 || !h.canTakeFromBelow(pos.at(above), i, s.item) {
				continue
			}
			one := *s
			one.count = 1
			if binInsert(c.slots, one) == 0 {
				s.count--
				if s.count <= 0 {
					*s = invStack{}
				}
				h.containerChanged(players, pos.at(above))
				return true
			}
		}
		return false
	}
	// No container above: vacuum item entities in this cell and the one above.
	return h.hopperTakeItems(players, pos, c, true)
}

// hopperTakeItems is HopperBlockEntity.addItem over the items whose box
// meets the hopper's SUCK_AABB (the full cell from 11/16 up to the top of
// the block above): the first item taken WHOLE ends the pull; one only
// partly taken leaves the rest lying and the hopper tries the next. Alone
// (HopperBlock.entityInside) only items in the hopper's own cell count; with
// aboveToo (suckInItems) a full block above that is not #does_not_block_hoppers
// shuts the hopper off. Nothing is heard: a hopper takes items silently.
func (h *hub) hopperTakeItems(players map[int32]*tracked, pos simPos, c *bin, aboveToo bool) bool {
	if aboveToo {
		if w := h.worldFor(pos.dim); w != nil {
			if above := w.At(pos.x, pos.y+1, pos.z); worldgen.IsFullCube(above) && !isBeeHome(above) {
				return false // isBlocked
			}
		}
	}
	const half = 0.125 // an item entity is 0.25 wide and tall
	x0, x1 := float64(pos.x), float64(pos.x)+1
	y0, y1 := float64(pos.y)+11.0/16, float64(pos.y)+2
	z0, z1 := float64(pos.z), float64(pos.z)+1
	for _, eid := range h.itemsInOrder(func(it *itemEntity) bool {
		return it.dim == pos.dim && it.x+half > x0 && it.x-half < x1 && it.z+half > z0 && it.z-half < z1 &&
			it.y+2*half > y0 && it.y < y1 && (aboveToo || floorInt(it.y) == pos.y) // entityInside: its own cell
	}) {
		it := h.items[eid]
		st := it.stack()
		left := binInsert(c.slots, st)
		if left == 0 {
			delete(h.items, eid)
			h.entityGone(players, it.dim, eid)
			return true
		}
		it.count = left
	}
	return false
}

// hopperPush moves one item into the container the hopper faces. Furnaces
// take smelt input from above and fuel from the side (vanilla).
func (h *hub) hopperPush(players map[int32]*tracked, pos simPos, state uint32, c *bin) bool {
	dx, dy, dz := hopperDelta(state)
	target := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	if h.containerSlots(pos.at(target)) == nil {
		return false
	}
	cb := h.crafterBinAt(pos.at(target)) // non-nil → fill via the disabled-aware rule
	var fed *bin                         // a hopper downstream, and whether it was empty before this item
	if w := h.worldFor(pos.dim); w != nil && isHopper(w.At(target.x, target.y, target.z)) {
		if b := h.bins[pos.at(target)]; b != nil && binEmpty(b.slots) {
			fed = b
		}
	}
	for i := range c.slots {
		s := &c.slots[i]
		if s.item == 0 || s.count == 0 {
			continue
		}
		one := *s
		one.count = 1
		placed := false
		if cb != nil {
			placed = crafterInsert(cb, one) == 0
		} else {
			placed = h.insertByFace(pos.at(target), dy, one)
		}
		if placed {
			s.count--
			if s.count <= 0 {
				*s = invStack{}
			}
			if fed != nil {
				h.hopperFed(fed, c)
			}
			h.containerChanged(players, pos.at(target))
			return true
		}
	}
	return false
}

// containerSlots exposes any container's raw slots at a position (nil if the
// position holds no known container): a block's, or else a container cart
// parked in the cell, as a hopper or dropper finds one.
func (h *hub) containerSlots(pos simPos) []invStack {
	if s := h.blockContainerSlots(pos); s != nil {
		return s
	}
	return h.vehicleContainerAt(pos) // a chest or hopper cart parked in the cell
}

// blockContainerSlots is containerSlots without the carts: the block
// entity's own storage. It is the BLOCK that decides (HopperBlockEntity
// .getContainerAt asks the block entity the block there owns): storage left
// in a hub map at a cell whose block has gone is no container, and a
// container block nobody has opened yet is one all the same — vanilla's
// block entity exists from placement, so a new dropper takes an item from
// the one below it before anyone looks inside.
func (h *hub) blockContainerSlots(pos simPos) []invStack {
	w := h.worldFor(pos.dim)
	if w == nil {
		return nil
	}
	state := w.At(pos.x, pos.y, pos.z)
	switch {
	case containerOpenFor(state) == openChestWindow:
		c := h.chests[pos]
		if c == nil {
			c = &chest{}
			h.fillStructureChestIn(pos.dim, pos.blockPos, c)
			h.chests[pos] = c
		}
		return c.slots[:]
	case isCookerBlock(state):
		return h.furnaceAt(pos, state).slots[:]
	case isBinBlock(state):
		return h.binAt(pos, state).slots
	}
	return nil
}

// dropOrphanStorage is the boot sweep for dispenser, dropper, hopper,
// brewing-stand, crafter and furnace storage whose block has gone. A block
// entity lives and dies with its block, but a container snapshot and a world
// save taken on either side of a block change can leave storage at a cell
// that is now air or stone — where the next container placed in that cell
// would find it. Its contents fall out where the block stood, as they would
// have when it was broken.
func (h *hub) dropOrphanStorage() {
	n := 0
	drop := func(pos simPos, slots []invStack) {
		n++
		for _, st := range slots {
			if st.item != 0 && st.count > 0 {
				if it := h.spawnItemIn(nil, pos.dim, st.item, st.count, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5); it != nil {
					it.setFrom(st)
				}
			}
		}
	}
	for pos, b := range h.bins {
		if w := h.worldFor(pos.dim); w != nil && !isBinBlock(w.At(pos.x, pos.y, pos.z)) {
			drop(pos, b.slots)
			delete(h.bins, pos)
		}
	}
	for pos, f := range h.furnaces {
		if w := h.worldFor(pos.dim); w != nil && !isCookerBlock(w.At(pos.x, pos.y, pos.z)) {
			drop(pos, f.slots[:])
			delete(h.furnaces, pos)
		}
	}
	if n > 0 {
		log.Printf("container boot sweep: %d store(s) with no block left, contents dropped", n)
	}
}

// isBinBlock reports whether a block keeps its storage in h.bins.
func isBinBlock(state uint32) bool {
	return isDispenser(state) || isDropper(state) || isHopper(state) || isBrewStand(state) || isCrafter(state)
}

// containerSignal is the comparator's read of a container: 0 when empty, else
// 1 + floor(14 × average slot fullness). Returns -1 for non-containers.
func (h *hub) containerSignal(pos simPos) int {
	if cb := h.crafterBinAt(pos); cb != nil {
		return crafterComparator(cb) // filled OR disabled slot count (vanilla), 0-9
	}
	// A cart is no block: only a detector rail under it reads it
	// (analogSignalFrom), so a comparator never sees one on a plain rail.
	slots := h.blockContainerSlots(pos)
	// ChestBlock.getAnalogOutputSignal reads getContainer(…, ignoreBlocked
	// false): a blocked chest (a solid block or a sitting cat on its lid)
	// has no container and reads 0, and a pair reads as one 54-slot chest —
	// blocked if either half is.
	if w := h.worldFor(pos.dim); w != nil && slots != nil {
		if st := w.At(pos.x, pos.y, pos.z); isChestBlock(st) || isTrappedChest(st) {
			left, right, paired := h.chestPairPositions(pos.dim, pos.x, pos.y, pos.z, st)
			if !paired {
				if h.chestBlockedAt(pos.dim, pos.blockPos) {
					return 0
				}
			} else {
				if h.chestBlockedAt(pos.dim, left) || h.chestBlockedAt(pos.dim, right) {
					return 0
				}
				a, b := h.blockContainerSlots(pos.at(left)), h.blockContainerSlots(pos.at(right))
				slots = append(append(make([]invStack, 0, len(a)+len(b)), a...), b...)
			}
		}
	}
	// A decorated pot reads as a one-slot container (DecoratedPotBlock's
	// getRedstoneSignalFromBlockEntity); its storage is read-only here so a
	// hopper never writes into a copy.
	if w := h.worldFor(pos.dim); slots == nil && w != nil && isDecoratedPot(w.At(pos.x, pos.y, pos.z)) {
		if st, ok := h.pots[pos]; ok && st.item != 0 {
			slots = []invStack{st}
		} else {
			return 0
		}
	}
	if slots == nil {
		return -1
	}
	return fullnessSignal(slots)
}

// fullnessSignal is AbstractContainerMenu.getRedstoneSignalFromContainer:
// 0 when empty, else 1 + floor(14 × average slot fullness).
func fullnessSignal(slots []invStack) int {
	full, any := 0.0, false
	for _, s := range slots {
		if s.item != 0 && s.count > 0 {
			any = true
			full += float64(s.count) / float64(stackCap(s.item)) // ContainerHelper: each slot's fraction of ITS cap
		}
	}
	if !any {
		return 0
	}
	return 1 + int(full/float64(len(slots))*14)
}

// containerChanged is BlockEntity.setChanged for a container whose contents
// moved: anyone looking in sees it, and a comparator reading it hears of it
// (Level.updateNeighbourForOutputSignal) and takes its 2 ticks to answer.
// Without this a comparator behind a dropper or a hopper only noticed the
// items when something else happened to update it.
func (h *hub) containerChanged(players map[int32]*tracked, pos simPos) {
	h.refreshBinViewers(players, pos)
	h.inDim(pos.dim, func() { h.updateNeighbourForOutputSignal(players, pos.blockPos) })
}

// windowContentsChanged is Slot.setChanged for a click in a block
// container's window: the container's comparators hear of it. (The window
// itself is the click's business, so no viewer refresh here.)
func (h *hub) windowContentsChanged(players map[int32]*tracked, t *tracked) {
	var at []simPos
	switch t.winKind {
	case winDoubleChest:
		at = []simPos{t.winPos, t.winPos2}
	case winChest, winFurnace, winBin, winCrafter:
		at = []simPos{t.winPos}
	}
	for _, pos := range at {
		w := h.worldFor(pos.dim)
		if w == nil {
			continue
		}
		if st := w.At(pos.x, pos.y, pos.z); containerOpenFor(st) == openChestWindow || isCookerBlock(st) || isBinBlock(st) {
			h.inDim(pos.dim, func() { h.updateNeighbourForOutputSignal(players, pos.blockPos) })
		}
	}
}

// refreshBinViewers resyncs any player looking at a container we just mutated.
func (h *hub) refreshBinViewers(players map[int32]*tracked, pos simPos) {
	for _, t := range players {
		if t.winID == 0 {
			continue
		}
		if t.winKind == winDoubleChest {
			if t.winPos == pos || t.winPos2 == pos {
				h.sendDoubleChestWindow(t)
			}
			continue
		}
		if t.winPos != pos {
			continue
		}
		switch t.winKind {
		case winBin:
			if c := h.bins[pos]; c != nil {
				h.sendBinWindow(t, c)
			}
		case winCrafter:
			if c := h.bins[pos]; c != nil {
				h.sendCrafterWindow(t, c)
			}
		case winChest:
			if c := t.viewChest; c != nil {
				h.sendChestWindow(t, c) // whatever this window looks at, block or not
			}
		case winFurnace:
			if f := h.furnaces[pos]; f != nil {
				h.sendFurnaceWindow(t, f)
			}
		}
	}
}

// insertByFace puts one item into a container the way a hopper or dropper
// arriving along dy would (vanilla WorldlyContainer.getSlotsForFace +
// canPlaceItemThroughFace): a furnace takes smelt input from above and
// fuel (or an empty bucket) from the sides or below, never output; a
// brewing stand takes the ingredient from above, bottles into empty bottle
// slots and blaze powder as fuel from the sides, and bottles or ingredient
// from below. Any other container takes the item wherever it fits. dy is the
// direction the item travels: negative from above, positive from below,
// zero from a side.
func (h *hub) insertByFace(target simPos, dy int, one invStack) bool {
	w := h.worldFor(target.dim)
	var ts uint32
	if w != nil {
		ts = w.At(target.x, target.y, target.z)
	}
	if isCookerBlock(ts) {
		f := h.furnaceAt(target, ts)
		if dy < 0 {
			return binInsert(f.slots[0:1], one) == 0
		}
		// AbstractFurnaceBlockEntity.canPlaceItem(1): burnable, or an empty
		// bucket when none is there already.
		if cookerFuelTicks(f.kind, one.item) > 0 || (one.item == itemBucket && f.slots[1].item != itemBucket) {
			return binInsert(f.slots[1:2], one) == 0
		}
		return false
	}
	if isBrewStand(ts) {
		b := h.binAt(target, ts)
		var slots []int
		switch {
		case dy < 0:
			slots = []int{3}
		case dy > 0:
			slots = []int{0, 1, 2, 3}
		default:
			slots = []int{0, 1, 2, 4}
		}
		for _, i := range slots {
			if !brewCanPlace(b, i, one.item) {
				continue
			}
			if binInsert(b.slots[i:i+1], one) == 0 {
				return true
			}
		}
		return false
	}
	// A decorated pot is a one-slot container (ContainerSingleItem): a hopper
	// can fill it one item at a time, and one underneath empties it.
	if w != nil && isDecoratedPot(w.At(target.x, target.y, target.z)) {
		return h.potInsert(target, one)
	}
	// ComposterBlock's InputContainer: a hopper aimed at a composter that is
	// not yet full feeds it, and the item takes the same chance a hand does.
	if w != nil {
		if lvl, ok := composterLevel(w.At(target.x, target.y, target.z)); ok {
			return h.composterInsert(target, lvl, one)
		}
	}
	// ShulkerBoxBlockEntity.canPlaceItemThroughFace: a hopper cannot post a
	// shulker box into a shulker box either.
	if w != nil && isShulkerBox(w.At(target.x, target.y, target.z)) && isShulkerBoxItem(one.item) {
		return false
	}
	dst := h.containerSlots(target)
	return dst != nil && binInsert(dst, one) == 0
}

// brewCanPlace is BrewingStandBlockEntity.canPlaceItem.
func brewCanPlace(b *bin, slot int, item int32) bool {
	switch slot {
	case 3:
		return brewIsIngredient(item)
	case 4:
		return item == itemBlazePowder // #minecraft:brewing_fuel
	}
	isBottle := item == itemPotion || item == itemSplashPotion || item == itemLingerPotion || item == itemGlassBottle || packBrewInput(item)
	return isBottle && (b.slots[slot].item == 0 || b.slots[slot].count == 0)
}

// brewIsIngredient: anything some mix starts from (PotionBrewing.isIngredient).
func brewIsIngredient(item int32) bool {
	if packBrewReagent(item) {
		return true
	}
	if _, ok := brewContainerMixes[item]; ok {
		return true
	}
	for _, m := range brewMixes {
		if m.ingredient == item {
			return true
		}
	}
	return false
}

// canTakeFromBelow is canTakeItemThroughFace for a hopper under a
// container: a furnace yields its output, and its fuel slot only once the
// fuel has become an empty bucket; a brewing stand yields its bottles, its
// ingredient only as a glass bottle, and never its fuel.
func (h *hub) canTakeFromBelow(src simPos, slot int, item int32) bool {
	w := h.worldFor(src.dim)
	if w != nil && isCookerBlock(w.At(src.x, src.y, src.z)) {
		switch slot {
		case 2:
			return true
		case 1:
			return item == itemBucket || item == itemBucketH2O
		}
		return false
	}
	if b := h.bins[src]; b != nil && len(b.slots) == 5 && w != nil && isBrewStand(w.At(src.x, src.y, src.z)) {
		switch slot {
		case 3:
			return item == itemGlassBottle
		case 4:
			return false
		}
	}
	return true
}

// itemsInOrder is the item entities a pick accepts, in the order they came
// into the world — the order a hopper's getEntitiesOfClass meets them in,
// where the item map alone would hand them over at random.
func (h *hub) itemsInOrder(pick func(*itemEntity) bool) []int32 {
	var eids []int32
	for eid, it := range h.items {
		if pick(it) {
			eids = append(eids, eid)
		}
	}
	slices.Sort(eids)
	return eids
}
