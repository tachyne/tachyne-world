package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
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
	h.setBlockAt(players, t.dim, pos, state-frameEyeStride)
	h.playSoundDim(players, t.dim, "minecraft:block.end_portal_frame.fill", sndBlock,
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
						h.setBlockAt(players, t.dim, blockPos{cx + dx, pos.y, cz + dz}, worldgen.EndPortalBlock)
					}
				}
			}
			// LevelEvent 1038: the whole dimension hears the portal open.
			h.playSoundGlobal(players, dimOverworld, "minecraft:block.end_portal.spawn", sndBlock,
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
	slot := t.handStack(t.useSlot())
	if slot == nil || slot.item != itemEnderEye || slot.count == 0 {
		return
	}
	// findNearestMapStructure(#eye_of_ender_located) in the thrower's own
	// dimension: only the overworld has strongholds; elsewhere the eye stays
	// in the hand.
	if t.dim != dimOverworld {
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
			if d := math.Hypot(float64(st.LocX)-t.x, float64(st.LocZ)-t.z); d < bd {
				best, bd = st, d
			}
		}
	}
	if !best.Exists {
		t.p.trySendEv(chatEv("The eye lies still — no stronghold nearby."))
		return
	}
	if t.gamemode != gmCreative {
		h.consumeUsed(t)
	}
	// EnderEyeItem: the eye is signalled to the structure's locate position
	// (the start chunk's corner at y 0), as /locate reports it — not the
	// portal room. It starts from the thrower's middle.
	e := h.spawnEye(players, t.dim, t.x, t.y+0.9, t.z, float64(best.LocX), 0, float64(best.LocZ))
	h.vibAt(t.dim, freqProjectileShoot, e.x, e.y, e.z, t.p.eid)
	pitch := 0.33 + h.rng.Float32()*(0.5-0.33)
	h.playSoundDim(players, t.dim, "minecraft:entity.ender_eye.launch", sndNeutral, t.x, t.y, t.z, 1, pitch)
}

type evInsertEye struct {
	eid     int32
	x, y, z int
	off     bool // used from the offhand (the packet's InteractionHand)
}

func (evInsertEye) isHubEvent() {}

type evThrowEye struct {
	eid int32
	off bool
}

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
		if t.wonGame {
			continue // the credits are rolling; the respawn request takes them home
		}
		t.p.pendingFrom = dimPos{}
		if t.dim == dimEnd {
			// The Bedrock gateway has no credits to roll (its client would
			// never ask to respawn afterwards), so Bedrock players go home.
			if !t.seenCredits && !t.p.bedrock {
				// EndPortalBlock.entityInside → showEndCredits: the first
				// walk out plays the End poem and credits; the player waits
				// here until their client finishes and asks to respawn.
				t.seenCredits, t.wonGame = true, true
				t.p.trySendEv(attachproto.GameEvent{Event: gameEventWinGame, Value: 1})
				continue
			}
			h.leaveEndHome(players, t)
			continue
		}
		t.p.pendingDestOK = false
		t.p.pendingDim.Store(int32(dimEnd))
	}
}

// gameEventWinGame is ClientboundGameEventPacket.WIN_GAME: value 1 rolls the
// End poem and credits.
const gameEventWinGame = 4

// leaveEndHome sends a player out of the End by its exit portal. Home is
// vanilla's findRespawnPositionAndUseSpawnBlock: their own bed or charged
// anchor if it still stands, else the world spawn. A walk out is not a
// death, so an anchor is read without being spent.
func (h *hub) leaveEndHome(players map[int32]*tracked, t *tracked) {
	sx, sy, sz, sdim := h.respawnPointCharging(players, t, false)
	t.p.pendingFrom = dimPos{}
	t.p.pendingDest = blockPos{floorInt(sx), floorInt(sy), floorInt(sz) - 1}
	t.p.pendingDestOK = true
	t.p.pendingDim.Store(int32(sdim))
}
