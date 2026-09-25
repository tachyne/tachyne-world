package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Buckets — scooping and pouring the world's fluids (the vanilla BucketItem
// model). A full bucket empties into the clicked/offset cell (water fizzles
// away in the nether); an empty bucket scoops the first fluid SOURCE along
// the player's look ray. Cauldron clicks route to useCauldron instead.

var (
	itemBucketSnow               = int32(itemByName["powder_snow_bucket"])
	powderSnowMin, powderSnowMax = worldgen.BlockRange("powder_snow")
)

// isPowderSnow: the block an empty bucket can scoop and a powder-snow bucket
// pours back (vanilla's only SolidBucketItem).
func isPowderSnow(s uint32) bool { return s >= powderSnowMin && s <= powderSnowMax }

const bucketReach = 4.5 // vanilla player block-interaction range

type evBucketEmpty struct {
	eid, slot int32
	x, y, z   int // the cell the fluid goes into (the clicked face's neighbour)
	// cx,cy,cz is the cell actually CLICKED. BucketItem.emptyContents puts
	// water INTO a waterloggable block rather than beside it, so the handler
	// has to be able to see what was under the cursor as well as next to it.
	cx, cy, cz int
}

type evBucketFill struct{ eid, slot int32 }

func (evBucketEmpty) isHubEvent() {}
func (evBucketFill) isHubEvent()  {}

// bucketEmpty pours a full bucket's fluid into a world cell.
func (h *hub) bucketEmpty(players map[int32]*tracked, t *tracked, slot int32, x, y, z int, cx, cy, cz int) {
	if (int(slot) != t.p.heldSlot() && slot != offhandSlot) || t.inv == nil {
		return // BucketItem.useOn pours the bucket in the hand used
	}
	h.vib(t.dim, freqFluidPlace, x, y, z, t.p.eid)
	hs := t.handStack(int(slot))
	held := hs.item
	mobBucket := isMobBucket(held) // MobBucketItem: water content + a mob to release
	if held != itemBucketH2O && held != itemBucketLav && held != itemBucketSnow && !mobBucket {
		return
	}
	w := h.worldFor(t.dim)
	if held == itemSulfurCubeBucket {
		// A MobBucketItem whose content is no fluid: nothing is poured (and
		// nothing boils off in the nether) — the cube just comes out.
		if ts := w.At(x, y, z); !worldgen.IsReplaceable(ts) && ts != worldgen.Air &&
			!worldgen.IsWater(ts) && !worldgen.IsLava(ts) {
			return
		}
		st := *hs
		h.swapBucket(t, slot, itemBucket)
		h.releaseSulfurBucket(players, t.dim, st, x, y, z)
		return
	}
	mobData := *hs // the bucketed mob's variant, age, health and name, read before the swap
	// LiquidBlockContainer: a water bucket emptied onto a slab, stair, fence,
	// sign or any other waterloggable block fills THAT block instead of the
	// cell beside it. Without this the water went next door and the slab
	// stayed dry, which is not how anyone expects a bucket to behave.
	if held == itemBucketH2O {
		if st := w.At(cx, cy, cz); waterloggable(st) && !isWaterlogged(st) {
			h.setBlockLive(players, t.dim, cx, cy, cz, withWaterlogged(st, true))
			h.playSoundDim(players, t.dim, "minecraft:item.bucket.empty", sndBlock,
				float64(cx)+0.5, float64(cy)+0.5, float64(cz)+0.5, 1, 1)
			h.swapBucket(t, slot, itemBucket)
			return
		}
	}
	if ts := w.At(x, y, z); !worldgen.IsReplaceable(ts) && ts != worldgen.Air &&
		!worldgen.IsWater(ts) && !worldgen.IsLava(ts) {
		return // cell filled in since the click
	}
	if held == itemBucketSnow {
		// SolidBucketItem.useOn: the bucket simply places its block, in every
		// dimension — powder snow is not a fluid and nothing boils it off.
		h.setBlockLive(players, t.dim, x, y, z, powderSnowBlock)
		h.playSoundDim(players, t.dim, "minecraft:item.bucket.empty_powder_snow", sndBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
		h.swapBucket(t, slot, itemBucket)
		return
	}
	if held != itemBucketLav && t.dim == 1 {
		// The nether boils water off the moment it leaves the bucket — and a
		// bucketed mob still comes out (checkExtraContent runs regardless).
		h.playSoundDim(players, t.dim, "minecraft:block.fire.extinguish", sndBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 0.5, 2.6+(h.rng.Float32()-h.rng.Float32())*0.8)
		h.swapBucket(t, slot, itemBucket)
		if mobBucket {
			h.releaseBucketMob(players, t.dim, mobData, x, y, z)
		}
		return
	}
	fluid, snd := worldgen.WaterBase, "minecraft:item.bucket.empty"
	if held == itemBucketLav {
		fluid, snd = worldgen.LavaBase, "minecraft:item.bucket.empty_lava"
	}
	h.setBlockLive(players, t.dim, x, y, z, fluid)
	if !mobBucket { // a mob bucket's empty sound is the mob's own (playEmptySound override)
		h.playSoundDim(players, t.dim, snd, sndBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	}
	h.swapBucket(t, slot, itemBucket)
	if mobBucket {
		h.releaseBucketMob(players, t.dim, mobData, x, y, z)
	}
}

// bucketFill scoops with an empty bucket: walk the look ray from the eyes
// (lower when crouching) to the first fluid SOURCE; flowing fluid is passed
// through, and any block with an outline shape — grass, a torch, a flower,
// not only a solid — stops the ray (getPlayerPOVHitResult clips OUTLINE).
func (h *hub) bucketFill(players map[int32]*tracked, t *tracked, slot int32) {
	if s := t.handStack(int(slot)); s == nil || s.item != itemBucket {
		return // BucketItem.use scoops with the bucket in the hand used
	}
	h.vibAt(t.dim, freqFluidPickup, t.x, t.y, t.z, t.p.eid)
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	eye := playerEyeStand
	if t.p.sneaking {
		eye = playerEyeSneak
	}
	ox, oy, oz := t.x, t.y+eye*t.scale(), t.z
	w := h.worldFor(t.dim)
	last := blockPos{int(math.Floor(ox)), int(math.Floor(oy)), int(math.Floor(oz))}
	for d := 0.0; d <= bucketReach; d += 0.1 {
		p := blockPos{int(math.Floor(ox + dx*d)), int(math.Floor(oy + dy*d)), int(math.Floor(oz + dz*d))}
		if p == last && d > 0 {
			continue
		}
		last = p
		st := w.At(p.x, p.y, p.z)
		switch {
		case st == worldgen.WaterBase || worldgen.IsBubbleColumn(st): // a source (BubbleColumnBlock is a BucketPickup too) — scoop it
			h.setBlockLive(players, t.dim, p.x, p.y, p.z, worldgen.Air)
			h.playSoundDim(players, t.dim, "minecraft:item.bucket.fill", sndBlock,
				float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, 1, 1)
			h.giveFilled(players, t, slot, itemBucketH2O)
			return
		case st == worldgen.LavaBase:
			h.setBlockLive(players, t.dim, p.x, p.y, p.z, worldgen.Air)
			h.playSoundDim(players, t.dim, "minecraft:item.bucket.fill_lava", sndBlock,
				float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, 1, 1)
			h.giveFilled(players, t, slot, itemBucketLav)
			return
		case isPowderSnow(st):
			// PowderSnowBlock is a BucketPickup: an empty bucket scoops the
			// whole block back up. It has to be tested BEFORE the solid stop,
			// because powder snow is one.
			h.setBlockLive(players, t.dim, p.x, p.y, p.z, worldgen.Air)
			h.playSoundDim(players, t.dim, "minecraft:item.bucket.fill_powder_snow", sndBlock,
				float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, 1, 1)
			h.giveFilled(players, t, slot, itemBucketSnow)
			return
		case waterloggable(st) && isWaterlogged(st):
			// SimpleWaterloggedBlock.pickupBlock: the ray stops at the water
			// source in a waterlogged slab, stair or fence, and the bucket
			// takes it, leaving the block dry.
			h.setBlockLive(players, t.dim, p.x, p.y, p.z, withWaterlogged(st, false))
			h.playSoundDim(players, t.dim, "minecraft:item.bucket.fill", sndBlock,
				float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, 1, 1)
			h.giveFilled(players, t, slot, itemBucketH2O)
			return
		case !outlineEmpty(st):
			return // hit a block's outline before any source
		}
		// air / flowing fluid: the source-only ray passes through
	}
}

// outlineEmpty reports whether a state has no outline shape, so an OUTLINE
// clip passes through its cell: air, liquid blocks, and the light block.
func outlineEmpty(st uint32) bool {
	if st == worldgen.Air || worldgen.IsFluid(st) {
		return true
	}
	switch name, _ := worldgen.StateName(st); name {
	case "air", "cave_air", "void_air", "light":
		return true
	}
	return false
}

// swapBucket replaces a (non-stacking, count-1) full bucket with its result.
// Creative keeps the original — vanilla infinite materials.
func (h *hub) swapBucket(t *tracked, slot int32, to int32) {
	if t.gamemode == gmCreative {
		return
	}
	*t.handStack(int(slot)) = invStack{item: to, count: 1} // a hotbar slot or the offhand
	h.sendHandSlot(t, int(slot))
}

// giveFilled turns one empty bucket (which stack) into a filled one: the last
// of the stack swaps in place, otherwise the stack shrinks and the filled
// bucket lands wherever the inventory has room (or at the player's feet).
func (h *hub) giveFilled(players map[int32]*tracked, t *tracked, slot int32, item int32) {
	h.giveFilledStack(players, t, slot, invStack{item: item, count: 1})
}

// giveFilledStack is ItemUtils.createFilledResult: creative keeps what it
// used and gains the filled item only if it has none like it yet.
func (h *hub) giveFilledStack(players map[int32]*tracked, t *tracked, slot int32, st invStack) {
	if t.gamemode == gmCreative {
		if !t.holdsLike(st) {
			changed, _ := t.inv.addStack(st)
			for _, sl := range changed {
				h.sendSlot(t, sl)
			}
		}
		return
	}
	cur := t.handStack(int(slot)) // a hotbar slot or the offhand
	if cur == nil {
		return
	}
	if cur.count <= 1 {
		*cur = st
		h.sendHandSlot(t, int(slot))
		return
	}
	cur.count--
	h.sendHandSlot(t, int(slot))
	changed, left := t.inv.addStack(st)
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	if left > 0 {
		if it := h.spawnItemIn(players, t.dim, st.item, left, t.x, t.y, t.z); it != nil {
			it.setFrom(st)
			h.refreshItemMeta(players, it)
		}
	}
}

// waterloggable / isWaterlogged / withWaterlogged are the SimpleWaterloggedBlock
// trio: whether a block carries the property at all, whether it is currently
// holding water, and the state with that flag set.
func waterloggable(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && info.HasProperty("waterlogged")
}

func isWaterlogged(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && worldgen.GetProperty(info, state, "waterlogged") == "true"
}

func withWaterlogged(state uint32, on bool) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	v := "false"
	if on {
		v = "true"
	}
	return worldgen.SetProperty(info, state, "waterlogged", v)
}
