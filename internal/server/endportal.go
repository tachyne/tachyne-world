package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The end portal: eyes of ender locate the stronghold and fill its frame
// ring; twelve eyes open the portal; standing in end-portal blocks travels
// to the End (and back — the return trip lands at the overworld spawn).

const (
	frameEyeStride = 4 // frame states: eye(2)×4 + facing(4); eye=true is LOW
)

var (
	itemEnderEye = itemByName["ender_eye"]

	entityEyeProj = entityID("eye_of_ender")
)

func isEndFrame(s uint32) bool {
	return s >= worldgen.EndPortalFrame && s <= worldgen.EndPortalFrame+7
}
func frameHasEye(s uint32) bool { return s < worldgen.EndPortalFrame+frameEyeStride }

// insertEye fills a frame with an eye of ender; twelve lit frames open the
// portal (server-checked against the generated ring — AUTHORITY: the click
// is a wish, the stronghold layout is the truth).
func (h *hub) insertEye(players map[int32]*tracked, t *tracked, pos blockPos, state uint32) {
	if frameHasEye(state) {
		return
	}
	h.setBlock(players, pos, state-frameEyeStride)
	h.playSound(players, "minecraft:block.end_portal_frame.fill", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	// Any complete 12-frame eyed ring around a 3x3 interior opens — the
	// stronghold's own ring and player-built rings alike (vanilla parity;
	// frame facing is not enforced). The clicked frame sits somewhere on the
	// ring, so its center is within 2 blocks on both axes.
	for cdx := -2; cdx <= 2; cdx++ {
		for cdz := -2; cdz <= 2; cdz++ {
			cx, cz := pos.x+cdx, pos.z+cdz
			if !h.ringComplete(cx, pos.y, cz) {
				continue
			}
			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					if worldgen.IsReplaceable(h.world.At(cx+dx, pos.y, cz+dz)) {
						h.setBlock(players, blockPos{cx + dx, pos.y, cz + dz}, worldgen.EndPortalBlock)
					}
				}
			}
			h.playSound(players, "minecraft:block.end_portal.spawn", sndBlock,
				float64(cx)+0.5, float64(pos.y)+0.5, float64(cz)+0.5, 1, 1)
			return
		}
	}
}

// endRingOffsets are the twelve frame positions around a 3x3 portal interior.
var endRingOffsets = [12][2]int{
	{-1, -2}, {0, -2}, {1, -2}, {-1, 2}, {0, 2}, {1, 2},
	{-2, -1}, {-2, 0}, {-2, 1}, {2, -1}, {2, 0}, {2, 1},
}

// ringComplete reports whether a full eyed frame ring surrounds (cx,y,cz).
func (h *hub) ringComplete(cx, y, cz int) bool {
	for _, d := range endRingOffsets {
		s := h.world.At(cx+d[0], y, cz+d[1])
		if !isEndFrame(s) || !frameHasEye(s) {
			return false
		}
	}
	return true
}

// throwEye launches an eye of ender drifting toward the nearest stronghold.
func (h *hub) throwEye(players map[int32]*tracked, t *tracked) {
	if t.inv == nil {
		return
	}
	slot := &t.inv.slots[t.p.heldSlot()]
	if slot.item != itemEnderEye || slot.count == 0 {
		return
	}
	// Nearest stronghold across this cell + neighbours.
	best, bd := worldgen.Stronghold{}, math.MaxFloat64
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			st := h.world.Gen().StrongholdIn(int(t.x)+dx*1536, int(t.z)+dz*1536)
			if !st.Exists {
				continue
			}
			if d := math.Hypot(float64(st.X)-t.x, float64(st.Z)-t.z); d < bd {
				best, bd = st, d
			}
		}
	}
	if !best.Exists {
		t.p.trySendEv(chatEv("The eye lies still — no stronghold nearby."))
		return
	}
	if t.gamemode != gmCreative {
		slot.count--
		if slot.count == 0 {
			*slot = invStack{}
		}
		h.sendSlot(t, t.p.heldSlot())
	}
	dx, dz := float64(best.X)-t.x, float64(best.Z)-t.z
	d := math.Hypot(dx, dz)
	a := h.launchProjectileIn(players, entityEyeProj, t.dim, t.x, t.y+1.6, t.z,
		dx/d*0.7, 0.25, dz/d*0.7)
	a.dmg = 0
	h.playSound(players, "minecraft:entity.ender_eye.launch", sndPlayer, t.x, t.y, t.z, 0.6, 1)
}

type evInsertEye struct {
	eid     int32
	x, y, z int
}

func (evInsertEye) isHubEvent() {}

type evThrowEye struct{ eid int32 }

func (evThrowEye) isHubEvent() {}

// updateEndPortalContact: standing in an end-portal block travels instantly.
func (h *hub) updateEndPortalContact(players map[int32]*tracked) {
	for _, t := range players {
		if t.p.pendingDim.Load() >= 0 {
			continue
		}
		feet := h.worldFor(t.dim).At(floorInt(t.x), floorInt(t.y+0.05), floorInt(t.z))
		if feet != worldgen.EndPortalBlock {
			continue
		}
		target := 2
		if t.dim == 2 {
			target = 0 // the End's exit portal goes home
		}
		t.p.pendingFrom = dimPos{}
		t.p.pendingDestOK = false
		t.p.pendingDim.Store(int32(target))
	}
}
