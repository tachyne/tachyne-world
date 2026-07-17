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
	itemBucketSnow = itemByName["powder_snow_bucket"]
)

const bucketReach = 4.5 // vanilla player block-interaction range

type evBucketEmpty struct {
	eid, slot int32
	x, y, z   int
}

type evBucketFill struct{ eid, slot int32 }

func (evBucketEmpty) isHubEvent() {}
func (evBucketFill) isHubEvent()  {}

// bucketEmpty pours a full bucket's fluid into a world cell.
func (h *hub) bucketEmpty(players map[int32]*tracked, t *tracked, slot int32, x, y, z int) {
	if int(slot) != t.p.heldSlot() || t.inv == nil {
		return
	}
	held := t.inv.slots[slot].item
	if held != itemBucketH2O && held != itemBucketLav {
		return
	}
	w := h.worldFor(t.dim)
	if ts := w.At(x, y, z); !worldgen.IsReplaceable(ts) && ts != worldgen.Air &&
		!worldgen.IsWater(ts) && !worldgen.IsLava(ts) {
		return // cell filled in since the click
	}
	if held == itemBucketH2O && t.dim == 1 {
		// The nether boils water off the moment it leaves the bucket.
		h.playSoundDim(players, t.dim, "minecraft:block.fire.extinguish", sndBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 0.5, 2.6+(h.rng.Float32()-h.rng.Float32())*0.8)
		h.swapBucket(t, slot, itemBucket)
		return
	}
	fluid, snd := worldgen.WaterBase, "minecraft:item.bucket.empty"
	if held == itemBucketLav {
		fluid, snd = worldgen.LavaBase, "minecraft:item.bucket.empty_lava"
	}
	h.setBlockLive(players, t.dim, x, y, z, fluid)
	h.playSoundDim(players, t.dim, snd, sndBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	h.swapBucket(t, slot, itemBucket)
}

// bucketFill scoops with an empty bucket: walk the look ray to the first
// fluid SOURCE (flowing fluid is passed through, a solid stops the ray).
func (h *hub) bucketFill(players map[int32]*tracked, t *tracked, slot int32) {
	if int(slot) != t.p.heldSlot() || t.inv == nil || t.inv.slots[slot].item != itemBucket {
		return
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	ox, oy, oz := t.x, t.y+1.5, t.z
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
		case st == worldgen.WaterBase: // a source — scoop it
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
		case worldgen.Collides(st):
			return // hit a solid before any source
		}
		// air / flowing fluid: the source-only ray passes through
	}
}

// swapBucket replaces a (non-stacking, count-1) full bucket with its result.
// Creative keeps the original — vanilla infinite materials.
func (h *hub) swapBucket(t *tracked, slot int32, to int32) {
	if t.gamemode == gmCreative {
		return
	}
	t.inv.slots[slot] = invStack{item: to, count: 1}
	h.sendSlot(t, int(slot))
}

// giveFilled turns one empty bucket (which stack) into a filled one: the last
// of the stack swaps in place, otherwise the stack shrinks and the filled
// bucket lands wherever the inventory has room (or at the player's feet).
func (h *hub) giveFilled(players map[int32]*tracked, t *tracked, slot int32, item int32) {
	h.giveFilledStack(players, t, slot, invStack{item: item, count: 1})
}

func (h *hub) giveFilledStack(players map[int32]*tracked, t *tracked, slot int32, st invStack) {
	if t.gamemode == gmCreative {
		return // creative scoops without inventory changes
	}
	cur := t.inv.slots[slot]
	if cur.count <= 1 {
		t.inv.slots[slot] = st
		h.sendSlot(t, int(slot))
		return
	}
	cur.count--
	t.inv.slots[slot] = cur
	h.sendSlot(t, int(slot))
	changed, left := t.inv.addStack(st)
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	if left > 0 {
		if it := h.spawnItemIn(players, t.dim, st.item, left, t.x, t.y, t.z); it != nil {
			it.dmg, it.ench = st.dmg, st.ench
		}
	}
}
