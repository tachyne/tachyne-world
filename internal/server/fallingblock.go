package server

import (
	"encoding/binary"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Falling blocks are entities, as vanilla's are. A block with nothing under
// it (FallingBlock.tick, two ticks after it was placed or its neighbour
// changed) is lifted out of the world into a falling_block entity
// (FallingBlockEntity.fall), which drops under gravity — 0.04 a tick, with
// the air's 0.98 drag — and on touching down sets the block back into the
// world where it came to rest. When the block cannot go there — it came to
// rest inside a torch's or a flower's cell, on a slab, on snow layers, or
// somewhere it cannot stand — it breaks into its item instead. An anvil hurts
// whatever it lands on and may chip on the way; a stalactite's tip skewers;
// concrete powder sets the moment it enters water. Before this the block
// stepped one cell a tick with no entity, no acceleration and no chance of
// breaking.

const (
	fallDelayAfterPlace = 2    // FallingBlock.getDelayAfterPlace
	fallGravity         = 0.04 // FallingBlockEntity.getDefaultGravity
	fallAirDrag         = 0.98 // Entity.getAirDrag
	fallHalfWidth       = 0.49 // EntityType.FALLING_BLOCK: 0.98 × 0.98
	fallHeight          = 0.98
	fallDamageMaxDef    = 40  // FallingBlockEntity DEFAULT_MAX_FALL_DAMAGE
	fallTimeVoid        = 100 // ticks before a block out of the world gives up
	fallTimeMax         = 600 // …and before any block does
	anvilHurtPerBlock   = 2.0 // AnvilBlock.falling: setHurtsEntities(2, 40)
	anvilHurtMax        = 40
	stalactiteHurtFloor = 6 // SpeleothemBlock.spawnFallingStalactite: max(size, 6)
	stalactiteHurtMax   = 40
	collideEpsilon      = 1e-7 // Shapes.EPSILON
)

// fallEyeHeight is the EntityDimensions default eye height (0.85 of the
// height), where a blast pushes a falling block from.
const fallEyeHeight = 0.85 * fallHeight

// Level events a falling block fires on the way out.
const (
	worldEventAnvilLand      = 1031 // SOUND_ANVIL_LAND (AnvilBlock.onLand)
	levelEventDripstoneBreak = 1045 // PointedDripstoneBlock.getStalactiteLandingSound
)

var entityFallingBlock = entityID("falling_block")

// fallingBlock is one FallingBlockEntity. It falls straight down its column
// unless something gives it sideways motion — a blast, or the far side of a
// portal it came through turned.
type fallingBlock struct {
	eid          int32
	dim          int
	x, y, z      float64
	vx, vy, vz   float64
	stuckY       float64 // stuckSpeedMultiplier.y from the last tick's cobweb or powder snow (0 = none)
	stuckXZ      float64 // …and its x and z
	portalCool   int     // ticks before it may take a portal again (Entity.portalCooldown)
	state        uint32
	time         int
	fallDistance float64
	hurtEntities bool
	hurtPer      float64 // fallDamagePerDistance
	hurtMax      int     // fallDamageMax
	dropItem     bool
	cancelDrop   bool
	onGround     bool
}

// fallBlockState reports the blocks FallingBlock.tick drops: sand, red sand,
// gravel, the concrete powders, the anvils, the dragon egg and the
// suspicious blocks (BrushableBlock, which ticks the same way).
func fallBlockState(s uint32) bool { return worldgen.IsFalling(s) }

// fallScheduleTick is FallingBlock.onPlace / updateShape: a look two ticks
// from now at whether the block still has something under it. A block with
// a look already pending keeps that one.
func (h *hub) fallScheduleTick(dim int, pos blockPos) {
	h.inDim(dim, func() { h.scheduleTick(pos, fallDelayAfterPlace, tickNormal) })
}

// fallShapeUpdates runs from every block change: the block placed schedules
// its own look (onPlace) and each falling block beside the change schedules
// one (updateShape reaches all six).
func (h *hub) fallShapeUpdates(dim int, pos blockPos, state uint32) {
	if fallBlockState(state) {
		h.fallScheduleTick(dim, pos)
	}
	if isScaffolding(state) { // ScaffoldingBlock.onPlace: a look at itself next tick
		h.inDim(dim, func() { h.scheduleTick(pos, 1, tickNormal) })
	}
	w := h.worldFor(dim)
	for _, d := range allNeighbors {
		n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
		if !h.inWorldYIn(dim, n.y) {
			continue
		}
		// Never generate a chunk from here: a neighbour across a chunk edge
		// is read only if its chunk is loaded.
		if (n.x>>4 != pos.x>>4 || n.z>>4 != pos.z>>4) && !w.Loaded(int32(n.x>>4), int32(n.z>>4)) {
			continue
		}
		if fallBlockState(w.At(n.x, n.y, n.z)) {
			h.fallScheduleTick(dim, n)
		}
	}
}

// fallingBlockTick is FallingBlock.tick (and BrushableBlock.tick): free
// space below, and the block comes loose.
func (h *hub) fallingBlockTick(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	if !fallIsFree(h.worldFor(dim).At(pos.x, pos.y-1, pos.z)) || pos.y < worldgen.MinY {
		return
	}
	fb := h.fallBlock(players, dim, pos, state)
	switch {
	case worldgen.IsAnvil(state):
		fb.setHurtsEntities(anvilHurtPerBlock, anvilHurtMax) // AnvilBlock.falling
	case isSuspicious(state):
		fb.cancelDrop = true // BrushableBlock.tick: disableDrop
	}
}

// scaffoldTick is ScaffoldingBlock.tick: the distance and bottom flag are
// worked out again; at distance 7 a scaffold that was already at 7 comes
// loose as a falling block (FallingBlockEntity.fall), one that just lost
// its support breaks and drops (destroyBlock).
func (h *hub) scaffoldTick(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	ns, ok := scaffoldUpdated(h.worldFor(dim), pos, state)
	switch {
	case !ok && scaffoldDist(state) == 7:
		h.fallBlock(players, dim, pos, ns)
	case !ok:
		h.setBlockAt(players, dim, pos, worldgen.Air)
		h.dropLoose(players, dim, pos, state)
	case ns != state:
		h.setBlockAt(players, dim, pos, ns)
	}
}

// scaffoldDist is a scaffold state's DISTANCE.
func scaffoldDist(state uint32) int {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return 0
	}
	return atoi(worldgen.GetProperty(info, state, "distance"))
}

// isSuspicious reports suspicious sand and gravel (BrushableBlock).
func isSuspicious(s uint32) bool { return inRanges2(s, suspiciousRanges) }

var suspiciousRanges = blockRangeOK("suspicious_sand", "suspicious_gravel")

func (fb *fallingBlock) setHurtsEntities(per float64, max int) {
	fb.hurtEntities, fb.hurtPer, fb.hurtMax = true, per, max
}

// fallBlock is FallingBlockEntity.fall: the entity takes the block's place
// (its water, if it was waterlogged, stays behind) at the cell's centre.
func (h *hub) fallBlock(players map[int32]*tracked, dim int, pos blockPos, state uint32) *fallingBlock {
	carried, left := state, worldgen.Air
	if waterloggable(state) {
		if isWaterlogged(state) {
			left = worldgen.WaterBase // getFluidState().createLegacyBlock(): a source
		}
		carried = withWaterlogged(state, false)
	}
	fb := &fallingBlock{eid: h.allocEID(), dim: dim, x: float64(pos.x) + 0.5, y: float64(pos.y), z: float64(pos.z) + 0.5,
		state: carried, dropItem: true, hurtMax: fallDamageMaxDef}
	h.setBlockAt(players, dim, pos, left)
	h.addFallingBlock(players, fb)
	return fb
}

// addFallingBlock puts the entity in the world and on its viewers' screens:
// add_entity carries the block state as its data.
func (h *hub) addFallingBlock(players map[int32]*tracked, fb *fallingBlock) {
	h.fallingBlocks = append(h.fallingBlocks, fb)
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(fb.eid))
	add := entAdd(fb.eid, entityFallingBlock, uuid, fb.x, fb.y, fb.z, 0, 0)
	add.Data = int32(fb.state)
	add.VX, add.VY, add.VZ = fb.vx, fb.vy, fb.vz
	h.toNearbyEv(players, fb.dim, fb.x, fb.z, add)
}

// moveFallingBlock carries a falling block to another dimension (a portal):
// the old dimension's viewers see it go, the new one's see it arrive.
func (h *hub) moveFallingBlock(players map[int32]*tracked, fb *fallingBlock, dim int, x, y, z float64) {
	h.entityGone(players, fb.dim, fb.eid)
	fb.dim, fb.x, fb.y, fb.z = dim, x, y, z
	fb.portalCool = entityPortalCooldown
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(fb.eid))
	add := entAdd(fb.eid, entityFallingBlock, uuid, fb.x, fb.y, fb.z, 0, 0)
	add.Data = int32(fb.state)
	add.VX, add.VY, add.VZ = fb.vx, fb.vy, fb.vz
	h.toNearbyEv(players, fb.dim, fb.x, fb.z, add)
}

// tickFallingBlocks runs every falling block's FallingBlockEntity.tick, in
// the entity phase of the tick. A block whose chunk is not ticking waits.
func (h *hub) tickFallingBlocks(players map[int32]*tracked) {
	if len(h.fallingBlocks) == 0 {
		return
	}
	// A landing can set loose the next block (the one that stood on the
	// cell it fills is already falling; one beside it may be scheduled):
	// new entities join a fresh list, not the one being walked.
	current := h.fallingBlocks
	h.fallingBlocks = nil
	var keep []*fallingBlock
	for _, fb := range current {
		w := h.worldFor(fb.dim)
		if w == nil {
			continue
		}
		if !w.Ticking(int32(floorInt(fb.x)>>4), int32(floorInt(fb.z)>>4)) || h.fallingBlockStep(players, w, fb) {
			keep = append(keep, fb)
		}
	}
	h.fallingBlocks = append(keep, h.fallingBlocks...)
}

// fallingBlockStep is one FallingBlockEntity.tick. It reports whether the
// entity is still in the world afterwards.
func (h *hub) fallingBlockStep(players map[int32]*tracked, w *world.World, fb *fallingBlock) bool {
	if fb.state == worldgen.Air {
		h.discardFalling(players, fb)
		return false
	}
	fb.time++
	fb.vy -= fallGravity // applyGravity
	xo, yo, zo := fb.x, fb.y, fb.z
	h.fallMove(players, w, fb)
	h.fallInsideBlocks(players, w, fb, yo)
	if fb.x != xo || fb.y != yo || fb.z != zo {
		h.toNearbyEv(players, fb.dim, fb.x, fb.z, entMove(fb.eid, fb.x, fb.y, fb.z, 0, 0, fb.onGround))
	}
	pos := blockPos{floorInt(fb.x), floorInt(fb.y), floorInt(fb.z)}
	concrete := worldgen.IsConcretePowder(fb.state)
	stuck := concrete && worldgen.HoldsWater(w.At(pos.x, pos.y, pos.z))
	if concrete && fb.vy*fb.vy > 1 {
		// Faster than a block a tick it could skip a thin layer of water:
		// the path it took is searched for a water source.
		if hit, ok := fallClipWaterSource(w, pos.x, pos.z, yo, fb.y); ok {
			pos, stuck = blockPos{pos.x, hit, pos.z}, true
		}
	}
	alive := true
	if !fb.onGround && !stuck {
		if fb.time > fallTimeVoid && (pos.y <= worldgen.MinY || pos.y > w.Ceiling()-1) || fb.time > fallTimeMax {
			if fb.dropItem && h.rules.EntityDrops {
				h.fallDropItem(players, fb)
			}
			h.discardFalling(players, fb)
			alive = false
		}
	} else {
		cur := w.At(pos.x, pos.y, pos.z)
		fb.vx, fb.vy, fb.vz = fb.vx*0.7, fb.vy*-0.5, fb.vz*0.7 // multiply(0.7, -0.5, 0.7)
		if !isMovingPiston(cur) {
			alive = false
			h.fallingBlockLands(players, w, fb, pos, cur, concrete, stuck)
		}
	}
	fb.vx, fb.vy, fb.vz = fb.vx*fallAirDrag, fb.vy*fallAirDrag, fb.vz*fallAirDrag
	return alive
}

// fallingBlockLands is the landing half of FallingBlockEntity.tick.
func (h *hub) fallingBlockLands(players map[int32]*tracked, w *world.World, fb *fallingBlock, pos blockPos, cur uint32, concrete, stuck bool) {
	if fb.cancelDrop {
		h.discardFalling(players, fb)
		h.fallBrokenAfterFall(players, fb, pos)
		return
	}
	mayReplace := fallMayReplace(cur)
	wouldContinue := fallIsFree(w.At(pos.x, pos.y-1, pos.z)) && (!concrete || !stuck)
	wouldSurvive := fallSurvives(w, pos, fb.state) && !wouldContinue
	if !mayReplace || !wouldSurvive {
		h.discardFalling(players, fb)
		if fb.dropItem && h.rules.EntityDrops {
			h.fallBrokenAfterFall(players, fb, pos)
			h.fallDropItem(players, fb)
		}
		return
	}
	state := fb.state
	if waterloggable(state) && worldgen.HoldsWater(cur) {
		state = withWaterlogged(state, true)
	}
	h.setBlockAt(players, fb.dim, pos, state)
	// FallingBlockEntity.tick: with the block update, the trackers get
	// add_transient_block (26.3), so the landing shows no gap.
	h.toTracking(players, fb.eid, fb.dim, fb.x, fb.z, attachproto.TransientBlock{X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), State: int32(state)})
	h.discardFalling(players, fb)
	// Fallable.onLand.
	switch {
	case worldgen.IsAnvil(state):
		h.levelEvent(players, fb.dim, worldEventAnvilLand, pos.x, pos.y, pos.z, 0)
	case concrete:
		// ConcretePowderBlock.onLand: into water, or beside it, it sets.
		if powderSolidifies(w, pos, cur) {
			h.setBlockAt(players, fb.dim, pos, worldgen.ConcreteFor(state))
		}
	}
}

// fallBrokenAfterFall is Fallable.onBrokenAfterFall: the break the player
// hears (an anvil's clang, a stalactite's crash, a suspicious block's dust).
func (h *hub) fallBrokenAfterFall(players map[int32]*tracked, fb *fallingBlock, pos blockPos) {
	switch {
	case worldgen.IsAnvil(fb.state):
		h.levelEvent(players, fb.dim, worldEventAnvilBroken, pos.x, pos.y, pos.z, 0)
	case isStalactite(fb.state) || dripstoneAny(fb.state):
		h.levelEvent(players, fb.dim, levelEventDripstoneBreak, pos.x, pos.y, pos.z, 0)
	case isSuspicious(fb.state):
		// BrushableBlock.onBrokenAfterFall: the block's break particles at
		// the entity's centre.
		c := blockPos{floorInt(fb.x), floorInt(fb.y + fallHeight/2), floorInt(fb.z)}
		h.levelEvent(players, fb.dim, worldEventBlockBreak, c.x, c.y, c.z, int32(fb.state))
	}
}

// dripstoneAny reports a pointed dripstone of either direction.
func dripstoneAny(s uint32) bool { _, _, _, ok := dripstoneParts(s); return ok }

// fallDropItem is spawnAtLocation(block): the block's item where the entity
// is, with an item entity's usual toss.
func (h *hub) fallDropItem(players map[int32]*tracked, fb *fallingBlock) {
	name, ok := worldgen.StateName(fb.state)
	if !ok {
		return
	}
	item, ok := itemByName[name]
	if !ok || item == 0 {
		return
	}
	h.spawnItemAt(players, fb.dim, item, 1, fb.x, fb.y, fb.z, h.rng.Float64()*0.2-0.1, 0.2, h.rng.Float64()*0.2-0.1)
}

// discardFalling takes the entity out of the world for every viewer.
func (h *hub) discardFalling(players map[int32]*tracked, fb *fallingBlock) {
	h.entityGone(players, fb.dim, fb.eid)
}

// fallMove is Entity.move for a falling block: down (or up) first, stopped
// by the first collision shape its box meets (by the shape's real height:
// half a block up on a slab, a block and a half on a fence), then sideways,
// the larger way first, a way that is blocked losing its motion; then
// checkFallDamage — the distance fallen, and on touching down the block it
// landed on (fallOn) and the damage (causeFallDamage).
func (h *hub) fallMove(players map[int32]*tracked, w *world.World, fb *fallingBlock) {
	dx, dy, dz := fb.vx, fb.vy, fb.vz
	if fb.stuckY != 0 { // Entity.move: the stuck multiplier scales this move, and the motion is spent
		dx, dy, dz = dx*fb.stuckXZ, dy*fb.stuckY, dz*fb.stuckXZ
		fb.stuckY, fb.stuckXZ = 0, 0
		fb.vx, fb.vy, fb.vz = 0, 0, 0
	}
	x0, x1 := floorInt(fb.x-fallHalfWidth), floorInt(fb.x+fallHalfWidth-collideEpsilon)
	z0, z1 := floorInt(fb.z-fallHalfWidth), floorInt(fb.z+fallHalfWidth-collideEpsilon)
	moved := dy
	var onCell blockPos
	var onState uint32
	hit := false
	if dy < 0 {
		bottom, target := fb.y, fb.y+dy
		for bx := x0; bx <= x1; bx++ {
			for bz := z0; bz <= z1; bz++ {
				// The cell below the target too: a fence or wall reaches 1.5 up.
				for cy := floorInt(target) - 1; cy <= floorInt(bottom); cy++ {
					st := w.At(bx, cy, bz)
					_, hi, ok := fallCollision(st, fb.fallDistance)
					if !ok {
						continue
					}
					top := float64(cy) + hi
					if top <= bottom+collideEpsilon && top-bottom > moved {
						moved, onCell, onState, hit = top-bottom, blockPos{bx, cy, bz}, st, true
					}
				}
			}
		}
	} else if dy > 0 {
		head, target := fb.y+fallHeight, fb.y+fallHeight+dy
		for bx := x0; bx <= x1; bx++ {
			for bz := z0; bz <= z1; bz++ {
				for cy := floorInt(head); cy <= floorInt(target); cy++ {
					lo, _, ok := fallCollision(w.At(bx, cy, bz), fb.fallDistance)
					if !ok {
						continue
					}
					if floor := float64(cy) + lo; floor >= head-collideEpsilon && floor-head < moved {
						moved = floor - head
					}
				}
			}
		}
	}
	fb.y += moved
	vertical := moved != dy
	fb.onGround = vertical && dy < 0
	if vertical {
		fb.vy = 0 // Block.updateEntityMovementAfterFallOn
	}
	// Sideways, the larger component first (Direction.axisStepOrder).
	stepX := func() {
		if dx == 0 {
			return
		}
		if fallBoxBlocked(w, fb.x+dx, fb.y, fb.z, fb.fallDistance) {
			fb.vx = 0
		} else {
			fb.x += dx
		}
	}
	stepZ := func() {
		if dz == 0 {
			return
		}
		if fallBoxBlocked(w, fb.x, fb.y, fb.z+dz, fb.fallDistance) {
			fb.vz = 0
		} else {
			fb.z += dz
		}
	}
	if math.Abs(dx) < math.Abs(dz) {
		stepZ()
		stepX()
	} else {
		stepX()
		stepZ()
	}
	// Entity.checkFallDamage.
	if moved < 0 {
		fb.fallDistance -= moved
	}
	if fb.onGround && hit {
		if fb.fallDistance > 0 {
			h.fallOn(players, fb, onState, onCell)
		}
		fb.fallDistance = 0
	}
}

// fallBoxBlocked reports whether a falling block's box at (x,y,z) overlaps
// a block's collision shape.
func fallBoxBlocked(w *world.World, x, y, z, fallDistance float64) bool {
	x0, x1 := floorInt(x-fallHalfWidth), floorInt(x+fallHalfWidth-collideEpsilon)
	y0, y1 := floorInt(y)-1, floorInt(y+fallHeight-collideEpsilon) // the cell below: a fence reaches up into the box
	z0, z1 := floorInt(z-fallHalfWidth), floorInt(z+fallHalfWidth-collideEpsilon)
	for bx := x0; bx <= x1; bx++ {
		for by := y0; by <= y1; by++ {
			for bz := z0; bz <= z1; bz++ {
				lo, hi, ok := fallCollision(w.At(bx, by, bz), fallDistance)
				if ok && float64(by)+hi > y+collideEpsilon && float64(by)+lo < y+fallHeight-collideEpsilon {
					return true
				}
			}
		}
	}
	return false
}

// fallOn is the landed-on block's Block.fallOn for a falling block: most
// pass the whole fall on, a bed halves it, a stalagmite's tip adds two and a
// half blocks, and powder snow swallows it.
func (h *hub) fallOn(players map[int32]*tracked, fb *fallingBlock, on uint32, _ blockPos) {
	fd := fb.fallDistance
	switch {
	case isPowderSnow(on):
		return // PowderSnowBlock.fallOn: a sound for the living, nothing else
	case isStalagmiteTip(on):
		fd += dripFallBonus
	case isBedBlock(on) || inRanges2(on, fallReducers):
		fd *= 0.5 // fallDistanceReduction(0.5F)
	}
	h.fallingBlockHurts(players, fb, fd)
}

// fallReducers are the non-bed blocks that halve a fall (Blocks:
// fallDistanceReduction(0.5F)).
var fallReducers = blockRangeOK("shelf_mushroom")

// fallingBlockHurts is FallingBlockEntity.causeFallDamage: a block that
// hurts (an anvil, a stalactite's tip) deals its damage per block of the
// fall past the first, capped, to every living thing inside its box but
// creative and spectator players — and an anvil that hurt may wear a stage,
// or break outright past damaged.
func (h *hub) fallingBlockHurts(players map[int32]*tracked, fb *fallingBlock, fd float64) {
	if !fb.hurtEntities {
		return
	}
	n := math.Ceil(fd - 1)
	if n < 0 {
		return
	}
	dmg := math.Min(math.Floor(n*fb.hurtPer), float64(fb.hurtMax))
	dt := dtFallingBlock
	switch {
	case worldgen.IsAnvil(fb.state):
		dt = dtFallingAnvil
	case dripstoneAny(fb.state):
		dt = dtFallingStalactite
	}
	lo := [3]float64{fb.x - fallHalfWidth, fb.y, fb.z - fallHalfWidth}
	hi := [3]float64{fb.x + fallHalfWidth, fb.y + fallHeight, fb.z + fallHalfWidth}
	for _, t := range players {
		if t.dim != fb.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator {
			continue
		}
		if psBoxHits(lo, hi, t.x, t.y, t.z, playerWidth(t), playerHeight(t)) {
			h.damageOf(players, t, float32(dmg), dt)
		}
	}
	for _, m := range h.mobs {
		if m.dim != fb.dim || m.dying > 0 {
			continue
		}
		if b := m.box(); psBoxHits(lo, hi, m.x, m.y, m.z, b.w, b.h) {
			h.hurtMobOf(players, m, dmg, dt)
		}
	}
	if worldgen.IsAnvil(fb.state) && dmg > 0 && h.rng.Float64() < 0.05+n*0.05 {
		if worn := worldgen.AnvilDamaged(fb.state); worn == 0 {
			fb.cancelDrop = true // AnvilBlock.damage: past damaged, nothing is left
		} else {
			fb.state = worn
		}
	}
}

// fallInsideBlocks is applyEffectsFromBlocks for the cells the box swept:
// frogspawn is destroyed (FrogspawnBlock.entityInside), and a cobweb or
// powder snow catches the block (WebBlock / PowderSnowBlock.entityInside →
// makeStuckInBlock): its fall is reset and its next move slowed to 0.05
// (a web) or 1.5 (powder snow) of its speed.
func (h *hub) fallInsideBlocks(players map[int32]*tracked, w *world.World, fb *fallingBlock, yo float64) {
	lo, hi := math.Min(yo, fb.y), math.Max(yo, fb.y)+fallHeight
	bx, bz := floorInt(fb.x), floorInt(fb.z)
	for cy := floorInt(lo); cy <= floorInt(hi-collideEpsilon); cy++ {
		switch st := w.At(bx, cy, bz); {
		case st == frogspawnBlock:
			h.setBlockAt(players, fb.dim, blockPos{bx, cy, bz}, worldgen.Air)
			h.levelEvent(players, fb.dim, worldEventBlockBreak, bx, cy, bz, int32(frogspawnBlock))
		case st == cobwebState:
			fb.stuckY, fb.stuckXZ, fb.fallDistance = webStuckY, 0.25, 0
		case isPowderSnow(st):
			fb.stuckY, fb.stuckXZ, fb.fallDistance = powderSnowStuckY, powderSnowStuckXZ, 0
		}
	}
	fallBubbleColumns(w, fb)
}

// fallBubbleColumns is BubbleColumnBlock.entityInside for the column cells
// the block's box is in: an upward column lifts it (onInsideBubbleColumn,
// +0.06 up to 0.7 a tick), a whirlpool drags it (−0.03 down to −0.3), and
// the top cell of a column, with open air over it, throws it harder
// (onAboveBubbleColumn: +0.1 up to 1.8, or down to −0.9).
func fallBubbleColumns(w *world.World, fb *fallingBlock) {
	x0, x1 := floorInt(fb.x-fallHalfWidth+1e-5), floorInt(fb.x+fallHalfWidth-1e-5)
	y0, y1 := floorInt(fb.y+1e-5), floorInt(fb.y+fallHeight-1e-5)
	z0, z1 := floorInt(fb.z-fallHalfWidth+1e-5), floorInt(fb.z+fallHalfWidth-1e-5)
	for by := y0; by <= y1; by++ {
		for bx := x0; bx <= x1; bx++ {
			for bz := z0; bz <= z1; bz++ {
				st := w.At(bx, by, bz)
				if !worldgen.IsBubbleColumn(st) {
					continue
				}
				above := w.At(bx, by+1, bz)
				top := !worldgen.Collides(above) && !worldgen.HoldsWater(above) && !worldgen.IsLava(above)
				drag := st == worldgen.BubbleColumnDrag
				switch {
				case top && drag:
					fb.vy = math.Max(columnTopDownCap, fb.vy-columnDownStep)
				case top:
					fb.vy = math.Min(columnTopUpCap, fb.vy+columnTopUpStep)
				case drag:
					fb.vy = math.Max(columnDownCap, fb.vy-columnDownStep)
				default:
					fb.vy = math.Min(columnUpCap, fb.vy+columnUpStep)
				}
			}
		}
	}
}

// powderSnowStuckY is PowderSnowBlock's makeStuckInBlock(0.9, 1.5, 0.9).
const powderSnowStuckY = 1.5

// fallClipWaterSource is the concrete powder's clip (ClipContext.Fluid.
// SOURCE_ONLY) down the column from the last position to this one: the first
// cell with a water source in it, stopping at a block's collision shape.
func fallClipWaterSource(w *world.World, bx, bz int, from, to float64) (int, bool) {
	for cy := floorInt(from); cy >= floorInt(to); cy-- {
		st := w.At(bx, cy, bz)
		if isWaterSource(st) {
			return cy, true
		}
		if _, hi, ok := fallCollision(st, 0); ok && float64(cy)+hi > to {
			return 0, false // the ray met a block first
		}
	}
	return 0, false
}

// isWaterSource is a cell whose fluid is a water SOURCE: still water, a
// bubble column, or the water held by a waterlogged block or a sea plant.
func isWaterSource(s uint32) bool {
	return s == worldgen.WaterBase || worldgen.IsBubbleColumn(s) || (worldgen.HoldsWater(s) && !worldgen.IsWater(s))
}

// fallIsFree is FallingBlock.isFree: air, fire, a liquid or a replaceable
// block lets a falling block through.
func fallIsFree(s uint32) bool {
	return s == worldgen.Air || isFire(s) || worldgen.IsFluid(s) || inRanges2(s, replaceableRanges)
}

// fallMayReplace is BlockState.canBeReplaced with the falling block's empty
// placement context: whether the block may take this cell.
func fallMayReplace(s uint32) bool {
	switch {
	case isVineBlock(s):
		// VineBlock: while it has a face left free.
		info, ok := worldgen.InfoForState(s)
		if !ok {
			return true
		}
		faces := 0
		for _, f := range [...]string{"up", "north", "east", "south", "west"} {
			if worldgen.GetProperty(info, s, f) == "true" {
				faces++
			}
		}
		return faces < 5
	case isMultiface(s):
		return true // MultifaceBlock: any item that is not itself
	case inRanges2(s, snowLayerRange):
		return s == snowLayerRange[0][0] // SnowLayerBlock: one layer only
	}
	return s == worldgen.Air || inRanges2(s, replaceableRanges)
}

// replaceableRanges is vanilla's replaceable() set (Blocks): what a falling
// block passes through and may take the place of.
var (
	replaceableRanges = blockRangeOK("water", "lava", "short_grass", "fern", "dead_bush", "bush", "red_shrub",
		"short_dry_grass", "tall_dry_grass", "seagrass", "tall_seagrass", "fire", "soul_fire", "snow", "vine",
		"glow_lichen", "resin_clump", "light", "tall_grass", "large_fern", "structure_void", "void_air",
		"cave_air", "bubble_column", "warped_roots", "nether_sprouts", "crimson_roots", "leaf_litter",
		"hanging_roots")
	snowLayerRange = blockRangeOK("snow")
)

// fallSurvives is BlockState.canSurvive for the block about to be set down:
// a stalactite needs its ceiling; every other falling block stands anywhere.
func fallSurvives(w *world.World, pos blockPos, s uint32) bool {
	if dripstoneAny(s) {
		return supported(w, pos, s)
	}
	return true
}

// fallCollision is a block's collision shape as a falling block meets it,
// vertically: its bounds within the cell, or no collision at all. Powder
// snow holds a falling block up — full height, or 0.9 once it has fallen
// more than two and a half blocks (PowderSnowBlock.getCollisionShape).
func fallCollision(s uint32, fallDistance float64) (lo, hi float64, ok bool) {
	if isPowderSnow(s) {
		if fallDistance > 2.5 {
			return 0, 0.9, true
		}
		return 0, 1, true
	}
	return collisionBounds(s)
}

// spawnFallingStalactite is SpeleothemBlock.spawnFallingStalactite: every
// cell of the hanging column comes loose at once, top first, and the tip
// carries the column's length — hurting as if at least six long, per block
// fallen, at most forty.
func (h *hub) spawnFallingStalactite(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	w := h.worldFor(dim)
	p, st := pos, state
	for isStalactite(st) {
		fb := h.fallBlock(players, dim, p, st)
		if th, _, _, _ := dripstoneParts(st); th == dripTip || th == dripTipMerge {
			size := max(1+pos.y-p.y, stalactiteHurtFloor)
			fb.setHurtsEntities(float64(size), stalactiteHurtMax)
			break
		}
		p.y--
		st = w.At(p.x, p.y, p.z)
	}
}

// stalactiteShapeUpdate is SpeleothemBlock.updateShape for a hanging
// stalactite whose ceiling changed: unsupported, it lets go two ticks later
// (unless a tick is already pending).
func (h *hub) stalactiteShapeUpdate(dim int, pos blockPos) {
	h.inDim(dim, func() {
		if !h.hasScheduledTick(pos) {
			h.scheduleTick(pos, fallDelayAfterPlace, tickNormal)
		}
	})
}

// snapshotFalling saves the falling blocks in the air with the rest of the
// entities, so a restart mid-fall loses no block: vanilla saves a
// FallingBlockEntity with its chunk, and it picks up where it was.
func (h *hub) snapshotFalling() []savedFalling {
	out := make([]savedFalling, 0, len(h.fallingBlocks))
	for _, fb := range h.fallingBlocks {
		out = append(out, savedFalling{Dim: fb.dim, X: fb.x, Y: fb.y, Z: fb.z, VX: fb.vx, VY: fb.vy, VZ: fb.vz, FallDist: fb.fallDistance,
			State: fb.state, Time: fb.time, NoDrop: !fb.dropItem, Hurts: fb.hurtEntities, HurtPer: fb.hurtPer,
			HurtMax: fb.hurtMax, CancelDrop: fb.cancelDrop})
	}
	return out
}

// restoreFalling puts the saved falling blocks back in the air at boot.
func (h *hub) restoreFalling(saved []savedFalling) {
	for _, sf := range saved {
		if sf.State == worldgen.Air || h.worldFor(sf.Dim) == nil {
			continue
		}
		fb := &fallingBlock{eid: h.allocEID(), dim: sf.Dim, x: sf.X, y: sf.Y, z: sf.Z, vx: sf.VX, vy: sf.VY, vz: sf.VZ,
			fallDistance: sf.FallDist, state: sf.State, time: sf.Time, dropItem: !sf.NoDrop,
			hurtEntities: sf.Hurts, hurtPer: sf.HurtPer, hurtMax: sf.HurtMax, cancelDrop: sf.CancelDrop}
		if fb.hurtMax == 0 {
			fb.hurtMax = fallDamageMaxDef
		}
		h.fallingBlocks = append(h.fallingBlocks, fb) // boot-time: nobody is watching yet
	}
}
