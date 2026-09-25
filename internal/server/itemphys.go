package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Dropped items move as vanilla's ItemEntity does: they fall under gravity,
// slide with their velocity until friction stops them (or a wall does),
// tumble off ledges, float up through water, ride bubble columns, and are
// carried by flowing water (Entity.updateFluidHeightAndDoFluidPushing with
// the water current scale). A block's drops pop out with vanilla's random
// offset and hop, so the drops of neighbouring blocks land near enough to
// merge, and a tossed item arcs from the player's eyes.

const (
	itemGravity          = 0.04   // Entity.applyGravity (default gravity)
	itemAirDrag          = 0.98   // Entity.getAirDrag
	itemFluidDrag        = 0.99   // ItemEntity.setUnderwaterMovement: horizontal ×0.99
	itemFluidLift        = 5.0e-4 // setFluidMovement: +0.0005 while vy < 0.06
	itemFluidLiftCap     = 0.06
	waterFlowScale       = 0.014 // Entity: applyCurrentTo(WATER, 0.014)
	columnUpStep         = 0.06  // onInsideBubbleColumn: min(0.7, vy + 0.06)
	columnUpCap          = 0.7
	columnDownStep       = 0.03 // max(-0.3, vy - 0.03)
	columnDownCap        = -0.3
	slimeBounceNonLiving = 0.8 // SlimeBlock.bounceUp: 1.0 for a living thing, 0.8 otherwise
	defaultFriction      = 0.6 // Block.getFriction default
	itemSleepSpeed       = 1e-4
)

// blockFriction is BlockBehaviour.getFriction: ice is slick, slime grips.
func blockFriction(state uint32) float64 {
	switch {
	case inRanges2(state, slickIce):
		return 0.98
	case inRanges2(state, blueIce):
		return 0.989
	case inRanges2(state, slimeBlock):
		return 0.8
	}
	return defaultFriction
}

var (
	slickIce   = blockRangeOK("ice", "packed_ice", "frosted_ice")
	blueIce    = blockRangeOK("blue_ice")
	slimeBlock = blockRangeOK("slime_block")
)

// tickItems runs every dropped item's physics for one tick.
func (h *hub) tickItems(players map[int32]*tracked) {
	for eid, it := range h.items {
		w := h.worldFor(it.dim)
		if w == nil {
			continue
		}
		h.tickItem(players, w, it)
		// CactusBlock.entityInside hurts the item 1 a tick; an ItemEntity
		// has 5 health, so five ticks on (or against) a cactus and it is gone.
		if h.boxTouchesCactus(it.dim, it.x, it.y, it.z, itemHalfHeight, 2*itemHalfHeight) {
			if h.hurtItem(players, eid, it, 1, itemHurtOther) {
				continue
			}
		}
		// Entity.lavaHurt: 4 a tick in lava, with the burn's hiss;
		// BaseFireBlock.entityInside: fire's 1 (soul fire's 2) a tick.
		cell := w.At(floorInt(it.x), floorInt(it.y), floorInt(it.z))
		switch {
		case worldgen.IsLava(cell):
			if !fireResistantItem[it.item] {
				h.playSoundDim(players, it.dim, "minecraft:entity.generic.burn", sndNeutral, it.x, it.y, it.z, 0.4, 2+h.rng.Float32()*0.4)
			}
			h.hurtItem(players, eid, it, 4, itemHurtFire)
		case cell == soulFire:
			h.hurtItem(players, eid, it, 2, itemHurtFire)
		case isFire(cell):
			h.hurtItem(players, eid, it, 1, itemHurtFire)
		}
	}
}

// itemHealth is ItemEntity's health: 5.
const itemHealth = 5

// How an item is being hurt, for ItemStack.canBeHurtBy.
const (
	itemHurtOther = iota
	itemHurtFire
	itemHurtExplosion
)

// fireResistantItem: the items built with Item.Properties.fireResistant
// (damage_resistant #is_fire) — the netherite family, ancient debris and
// the nether star — which float through lava and fire untouched.
var fireResistantItem = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"ancient_debris", "netherite_block", "netherite_ingot", "netherite_scrap",
		"netherite_sword", "netherite_shovel", "netherite_pickaxe", "netherite_axe", "netherite_hoe",
		"netherite_helmet", "netherite_chestplate", "netherite_leggings", "netherite_boots",
		"nether_star", "netherite_horse_armor", "netherite_spear", "netherite_nautilus_armor"} {
		if id, ok := itemByName[n]; ok {
			m[int32(id)] = true
		}
	}
	return m
}()

// hurtItem is ItemEntity.hurtServer: a fire-resistant item ignores fire and
// lava, a nether star ignores blasts, and anything else loses health and is
// gone at 5. Reports whether the item was destroyed.
func (h *hub) hurtItem(players map[int32]*tracked, eid int32, it *itemEntity, amount float64, how int) bool {
	switch {
	case how == itemHurtFire && fireResistantItem[it.item],
		how == itemHurtExplosion && it.item == int32(itemByName["nether_star"]):
		return false
	}
	if it.hurt += amount; it.hurt < itemHealth {
		return false
	}
	delete(h.items, eid)
	h.entityGone(players, it.dim, eid)
	return true
}

// itemGrounded reports an item resting exactly on the block under it.
func itemGrounded(w *world.World, it *itemEntity) bool {
	fx, fy, fz := int(math.Floor(it.x)), int(math.Floor(it.y)), int(math.Floor(it.z))
	return it.y == float64(fy) && worldgen.Collides(w.At(fx, fy-1, fz))
}

func (h *hub) tickItem(players map[int32]*tracked, w *world.World, it *itemEntity) {
	fx, fy, fz := int(math.Floor(it.x)), int(math.Floor(it.y)), int(math.Floor(it.z))
	cell := w.At(fx, fy, fz)
	below := w.At(fx, fy-1, fz)
	ox, oy, oz := it.x, it.y, it.z
	inWater := worldgen.IsWater(cell) && !worldgen.IsBubbleColumn(cell)
	// The engine floats an item ON a water surface (the cell above is air)
	// instead of vanilla's bob just under it; a surfaced item still counts as
	// in the water below for the current.
	// A geyser's lift carries it out through the surface the engine would
	// otherwise float it on (potentsulfur.go).
	lifted := it.geyser
	it.geyser = false
	afloat := !lifted && !worldgen.IsWater(cell) && it.y == float64(fy) && worldgen.IsWater(below) && !worldgen.IsBubbleColumn(below)
	grounded := it.y == float64(fy) && worldgen.Collides(below)
	// The top cell of a column, with nothing over it, is onAboveBubbleColumn:
	// the harder push that throws an item clear of the surface (or the
	// stronger pull of a whirlpool).
	colTop := worldgen.IsBubbleColumn(cell) && !worldgen.Collides(w.At(fx, fy+1, fz)) &&
		!worldgen.HoldsWater(w.At(fx, fy+1, fz)) && !worldgen.IsLava(w.At(fx, fy+1, fz))
	switch {
	case cell == worldgen.BubbleColumnUp && colTop:
		it.vy = math.Min(columnTopUpCap, it.vy+columnTopUpStep)
	case cell == worldgen.BubbleColumnUp:
		it.vy = math.Min(columnUpCap, it.vy+columnUpStep)
	case cell == worldgen.BubbleColumnDrag && colTop:
		it.vy = math.Max(columnTopDownCap, it.vy-columnDownStep)
	case cell == worldgen.BubbleColumnDrag:
		it.vy = math.Max(columnDownCap, it.vy-columnDownStep)
	case inWater:
		if it.vy < itemFluidLiftCap {
			it.vy += itemFluidLift
		}
		it.vx *= itemFluidDrag
		it.vz *= itemFluidDrag
	case afloat:
		it.vy = 0
	case grounded:
		// the floor cancels gravity (vanilla zeroes vy on the vertical collision)
	default:
		it.vy -= itemGravity
	}
	if inWater || afloat {
		wp := blockPos{fx, fy, fz}
		if afloat {
			wp.y--
		}
		if cx, cy, cz, ok := h.fluidFlow(it.dim, wp); ok {
			it.vx += cx * waterFlowScale
			it.vy += cy * waterFlowScale
			it.vz += cz * waterFlowScale
		}
	}

	// makeStuckInBlock: a cobweb (0.25, 0.05, 0.25) or, for anything not
	// alive, powder snow (0.9, 1.5, 0.9) scales this tick's move, and the
	// motion is spent by it.
	stuckX, stuckY := 1.0, 1.0
	switch top := w.At(fx, int(math.Floor(it.y+2*itemHalfHeight-1e-7)), fz); {
	case cell == cobwebState || top == cobwebState:
		stuckX, stuckY = 0.25, webStuckY
	case isPowderSnow(cell) || isPowderSnow(top):
		stuckX, stuckY = powderSnowStuckXZ, 1.5
	}
	stuck := stuckX != 1 || stuckY != 1
	if stuck {
		it.vx, it.vy, it.vz = it.vx*stuckX, it.vy*stuckY, it.vz*stuckX
	}

	// ItemEntity.tick: an item inside a block (one was placed over it, or
	// it was pushed in) moves through it toward the nearest open side, with
	// collisions off until it is out (noPhysics + moveTowardsClosestSpace).
	noPhysics := false
	if mid := int(math.Floor(it.y + itemHalfHeight)); worldgen.Collides(w.At(fx, mid, fz)) {
		noPhysics = true
		h.itemTowardsClosestSpace(w, it, it.x, it.y+itemHalfHeight, it.z)
	}

	// Vertical move, landing on the first solid cell passed on the way down
	// or stopping under a ceiling on the way up.
	if noPhysics {
		it.y += it.vy
	} else if it.vy < 0 {
		ny := it.y + it.vy
		for cy := fy - 1; cy >= int(math.Floor(ny)); cy-- {
			if floorBlock := w.At(fx, cy, fz); worldgen.Collides(floorBlock) {
				v := it.vy
				ny, it.vy = float64(cy+1), 0
				if isSlimeBlock(floorBlock) {
					// SlimeBlock.updateEntityMovementAfterFallOn: anything
					// not alive bounces back at 0.8 of its speed.
					it.vy = -v * slimeBounceNonLiving
				}
				break
			}
		}
		it.y = ny
	} else if it.vy > 0 {
		ny := it.y + it.vy
		// A box whose top sits exactly on a face is still below it (1e-7,
		// vanilla's own collision epsilon), so the item resting under a
		// ceiling does not creep into it.
		if top := int(math.Floor(ny + 2*itemHalfHeight - 1e-7)); top != int(math.Floor(it.y+2*itemHalfHeight-1e-7)) && worldgen.Collides(w.At(fx, top, fz)) {
			ny, it.vy = float64(top)-2*itemHalfHeight, 0 // a ceiling meets the top of the box
		} else if nfy := int(math.Floor(ny)); nfy != fy {
			switch {
			case worldgen.Collides(w.At(fx, nfy, fz)):
				ny, it.vy = float64(nfy)-2*itemHalfHeight, 0 // a ceiling
			case inWater && !lifted && !worldgen.IsWater(w.At(fx, nfy, fz)):
				ny, it.vy = float64(nfy), 0 // surfaced: rest on the water (the engine's bob-free float)
			}
		}
		it.y = ny
	}
	// Horizontal moves stop at a wall (the axis's speed dies there).
	cy := int(math.Floor(it.y))
	if noPhysics {
		it.x += it.vx
		it.z += it.vz
	} else if it.vx != 0 {
		if nx := it.x + it.vx; worldgen.Collides(w.At(int(math.Floor(nx)), cy, fz)) {
			it.vx = 0
		} else {
			it.x = nx
		}
	}
	if it.vz != 0 && !noPhysics {
		if nz := it.z + it.vz; worldgen.Collides(w.At(fx, cy, int(math.Floor(nz)))) {
			it.vz = 0
		} else {
			it.z = nz
		}
	}

	if stuck {
		it.vx, it.vy, it.vz = 0, 0, 0
	}

	// Drag, and ground friction for a resting item.
	grounded = itemGrounded(w, it)
	hf := itemAirDrag
	if grounded {
		hf *= blockFriction(w.At(int(math.Floor(it.x)), int(math.Floor(it.y))-1, int(math.Floor(it.z))))
	}
	it.vx *= hf
	it.vz *= hf
	it.vy *= itemAirDrag
	if math.Abs(it.vx) < itemSleepSpeed {
		it.vx = 0
	}
	if math.Abs(it.vz) < itemSleepSpeed {
		it.vz = 0
	}
	if grounded && math.Abs(it.vy) < itemSleepSpeed {
		it.vy = 0
	}
	if it.x != ox || it.y != oy || it.z != oz {
		h.toTracking(players, it.eid, it.dim, it.x, it.z, entMove(it.eid, it.x, it.y, it.z, 0, 0, grounded || afloat))
	}
}

// flowHeight is FlowingFluid.getOwnHeight: amount/9, where a source or a
// falling cell is a full 8 and a flowing level n carries 8-n.
func flowHeight(state uint32) float64 {
	lvl := worldgen.FluidLevel(state, worldgen.WaterBase)
	amount := 8
	if lvl >= 1 && lvl <= 7 {
		amount = 8 - lvl
	}
	return float64(amount) / 9
}

// fluidFlow is FlowingFluid.getFlow for a water cell: the unit vector of
// the height differences to each horizontal neighbour (an open neighbour
// with water below it counts as a big drop), pulled down beside a solid
// face when the cell is falling. ok=false means still water.
func (h *hub) fluidFlow(dim int, pos blockPos) (x, y, z float64, ok bool) {
	w := h.worldFor(dim)
	st := w.At(pos.x, pos.y, pos.z)
	if !worldgen.IsWater(st) {
		return 0, 0, 0, false
	}
	own := flowHeight(st)
	var fx, fz float64
	for _, d := range horizNeighbors {
		nx, nz := pos.x+d.x, pos.z+d.z
		ns := w.At(nx, pos.y, nz)
		var dist float64
		switch {
		case worldgen.IsWater(ns):
			if nh := flowHeight(ns); nh > 0 {
				dist = own - nh
			}
		case worldgen.IsLava(ns):
			continue // another fluid does not affect the flow
		case !worldgen.Collides(ns):
			if bs := w.At(nx, pos.y-1, nz); worldgen.IsWater(bs) {
				if nh := flowHeight(bs); nh > 0 {
					dist = own - (nh - 0.8888889)
				}
			}
		}
		fx += float64(d.x) * dist
		fz += float64(d.z) * dist
	}
	var fy float64
	if worldgen.FluidLevel(st, worldgen.WaterBase) >= 8 { // falling beside a solid face: pulled down
		for _, d := range horizNeighbors {
			if worldgen.Collides(w.At(pos.x+d.x, pos.y, pos.z+d.z)) || worldgen.Collides(w.At(pos.x+d.x, pos.y+1, pos.z+d.z)) {
				if n := math.Hypot(fx, fz); n > 0 {
					fx, fz = fx/n, fz/n
				}
				fy = -6
				break
			}
		}
	}
	n := math.Sqrt(fx*fx + fy*fy + fz*fz)
	if n < 1e-9 {
		return 0, 0, 0, false
	}
	return fx / n, fy / n, fz / n, true
}

// itemHalfHeight is half the item's 0.25 box: the bounding box's middle,
// where vanilla measures which block the item is stuck in.
const itemHalfHeight = 0.125

// itemTowardsClosestSpace is Entity.moveTowardsClosestSpace: of the four
// sides and the top of the block at (x, y, z) that are not a full cube,
// the one nearest the item sets its speed on that axis to 0.1–0.3 outward;
// the other axes keep three quarters of theirs. Up wins when nothing is
// open, as in vanilla.
func (h *hub) itemTowardsClosestSpace(w *world.World, it *itemEntity, x, y, z float64) {
	px, py, pz := int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))
	dx, dy, dz := x-float64(px), y-float64(py), z-float64(pz)
	type side struct {
		off  [3]int
		dist float64
	}
	best := side{[3]int{0, 1, 0}, math.MaxFloat64}
	for _, s := range []side{ // NORTH, SOUTH, WEST, EAST, UP
		{[3]int{0, 0, -1}, dz}, {[3]int{0, 0, 1}, 1 - dz},
		{[3]int{-1, 0, 0}, dx}, {[3]int{1, 0, 0}, 1 - dx},
		{[3]int{0, 1, 0}, 1 - dy},
	} {
		if !worldgen.IsFullCube(w.At(px+s.off[0], py+s.off[1], pz+s.off[2])) && s.dist < best.dist {
			best = s
		}
	}
	speed := h.rng.Float64()*0.2 + 0.1
	vx, vy, vz := it.vx*0.75, it.vy*0.75, it.vz*0.75
	switch {
	case best.off[0] != 0:
		vx = float64(best.off[0]) * speed
	case best.off[1] != 0:
		vy = speed
	default:
		vz = float64(best.off[2]) * speed
	}
	it.vx, it.vy, it.vz = vx, vy, vz
}
