package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bells — BellBlock.onHit / attemptToRing and BellBlockEntity.onHit. A bell
// rings when struck on a proper side (a floor bell along its facing axis, a
// wall bell across it, a ceiling bell from any side; never from above or
// below, nor high on the block), when its redstone input rises, or when a
// projectile hits it: the bell sound at volume 2 and a block event (action
// 1, the strike direction) that swings it on every client. The block
// entity's swing clock sends villagers to hide and makes raiders glow.

var (
	bellRange       = blockRange("bell")
	bellRegistryID  = func() int32 { id, _ := worldgen.BlockRegistryID("bell"); return int32(id) }()
	bellSoundVolume = float32(2)
)

func isBell(s uint32) bool { return inRanges2(s, bellRange) }

type evRingBell struct {
	eid     int32
	x, y, z int
	dir     int32   // the face struck (0 down … 5 east)
	hitY    float32 // cursor height within the block (isProperHit's clickY)
}

func (evRingBell) isHubEvent() {}

// bellProperHit is BellBlock.isProperHit.
func bellProperHit(state uint32, dir int32, hitY float32) bool {
	if dir <= 1 || hitY > 0.8124 {
		return false
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return false
	}
	facing := worldgen.GetProperty(info, state, "facing")
	facingAxisZ := facing == "north" || facing == "south"
	clickAxisZ := dir == 2 || dir == 3
	switch worldgen.GetProperty(info, state, "attachment") {
	case "floor":
		return facingAxisZ == clickAxisZ
	case "single_wall", "double_wall":
		return facingAxisZ != clickAxisZ
	case "ceiling":
		return true
	}
	return false
}

// dirParam is Direction.get3DDataValue for the face ids the connection uses.
func dirParam(dir int32) uint8 {
	if dir < 0 || dir > 5 {
		return 2
	}
	return uint8(dir)
}

// onRingBell is a player striking the bell.
func (h *hub) onRingBell(players map[int32]*tracked, e evRingBell) {
	t := players[e.eid]
	if t == nil || t.dead {
		return
	}
	state := h.worldFor(t.dim).At(e.x, e.y, e.z)
	if !isBell(state) || !bellProperHit(state, e.dir, e.hitY) {
		return
	}
	h.ringBell(players, t.dim, blockPos{e.x, e.y, e.z}, e.dir)
	h.incCustom(t, "bell_ring", 1)
}

// ringBell is attemptToRing: the sound and the swing, from a direction (-1 =
// the bell's own facing, as redstone and explosions ring it).
func (h *hub) ringBell(players map[int32]*tracked, dim int, pos blockPos, dir int32) bool {
	state := h.worldFor(dim).At(pos.x, pos.y, pos.z)
	if !isBell(state) {
		return false
	}
	if dir < 0 {
		dir = 2
		if info, ok := worldgen.InfoForState(state); ok {
			switch worldgen.GetProperty(info, state, "facing") {
			case "south":
				dir = 3
			case "west":
				dir = 4
			case "east":
				dir = 5
			}
		}
	}
	h.toNearbyEv(players, dim, float64(pos.x), float64(pos.z), attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 1, Param: dirParam(dir), Block: bellRegistryID})
	h.playSoundDim(players, dim, "minecraft:block.bell.use", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, bellSoundVolume, 1)
	h.vib(dim, freqBlockChange, pos.x, pos.y, pos.z, 0) // BellBlock.attemptToRing: sculk hears the bell
	h.bellStruck(players, simPos{dim: dim, blockPos: pos})
	return true
}

// BellBlockEntity: the swing clock and the resonation. A strike (the block
// event, triggerEvent 1) takes stock of the living things within 48 blocks —
// at most once every 60 ticks — and sends villagers within 32 to hide. Five
// ticks into the swing, a #raiders mob within 32 blocks makes the bell
// resonate; forty ticks after that every raider within 48 glows for three
// seconds (serverTick → makeRaidersGlow).
const (
	bellRaiderRange   = 48  // SEARCH_RADIUS / HIGHLIGHT_RAIDERS_RADIUS
	bellVillagerRange = 32  // HEAR_BELL_RADIUS: villagers within 32 blocks hear it
	bellResonateRange = 32  // areRaidersNearby
	bellSwingTicks    = 50  // shaking ends
	bellResonateAt    = 5   // ticks into the swing before it can resonate
	bellResonateTicks = 40  // resonation length before the raiders light up
	bellRescanTicks   = 60  // updateEntities re-reads the neighbourhood this often
	bellGlowTicks     = 60  // MobEffects.GLOWING, 60
	villagerHideTicks = 300 // SetHiddenState.create(15 s, …)
)

type bellEntity struct {
	shaking    bool
	ticks      int
	resonating bool
	resTicks   int
	scanned    bool
	lastScan   uint64
	nearby     []int32 // mob eids in the 48-block box at the last scan
}

// isRaiderType is #raiders: the illagers, the ravager and the witch, in a
// raid or not.
func isRaiderType(etype int) bool {
	return isIllager(etype) || etype == entityRavager || etype == entityWitch
}

// bellStruck is BellBlockEntity.triggerEvent(1): updateEntities, then the
// swing and the resonation restart.
func (h *hub) bellStruck(players map[int32]*tracked, sp simPos) {
	if h.bells == nil {
		h.bells = map[simPos]*bellEntity{}
	}
	be := h.bells[sp]
	if be == nil {
		be = &bellEntity{}
		h.bells[sp] = be
	}
	now := h.tick.Load()
	cx, cy, cz := float64(sp.x)+0.5, float64(sp.y)+0.5, float64(sp.z)+0.5
	if !be.scanned || now > be.lastScan+bellRescanTicks {
		be.scanned, be.lastScan, be.nearby = true, now, be.nearby[:0]
		// AABB(pos).inflate(48): a box, not a sphere.
		h.grid().nearby(sp.dim, cx, cz, bellRaiderRange*1.5, func(m *mob) {
			if m.x >= float64(sp.x)-bellRaiderRange && m.x <= float64(sp.x)+1+bellRaiderRange &&
				m.y >= float64(sp.y)-bellRaiderRange && m.y <= float64(sp.y)+1+bellRaiderRange &&
				m.z >= float64(sp.z)-bellRaiderRange && m.z <= float64(sp.z)+1+bellRaiderRange {
				be.nearby = append(be.nearby, m.eid)
			}
		})
	}
	for _, eid := range be.nearby {
		if m := h.mobs[eid]; m != nil && m.dying == 0 && m.etype == entityVillager &&
			dist3(m.x, m.y, m.z, cx, cy, cz) < bellVillagerRange { // HEARD_BELL_TIME → hide for 15 s
			m.hideUntil = now + villagerHideTicks
		}
	}
	be.resTicks, be.ticks, be.shaking = 0, 0, true
}

// tickBells is BellBlockEntity.serverTick for every bell that has been rung.
func (h *hub) tickBells(players map[int32]*tracked) {
	for sp, be := range h.bells {
		w := h.worldFor(sp.dim)
		if w == nil || !isBell(w.At(sp.x, sp.y, sp.z)) {
			delete(h.bells, sp)
			continue
		}
		if be.shaking {
			be.ticks++
		}
		if be.ticks >= bellSwingTicks {
			be.shaking, be.ticks = false, 0
		}
		cx, cy, cz := float64(sp.x)+0.5, float64(sp.y)+0.5, float64(sp.z)+0.5
		if be.ticks >= bellResonateAt && be.resTicks == 0 && h.bellRaidersWithin(be, cx, cy, cz, bellResonateRange) {
			be.resonating = true
			h.playSoundDim(players, sp.dim, "minecraft:block.bell.resonate", sndBlock, cx, cy, cz, 1, 1)
		}
		if be.resonating {
			if be.resTicks < bellResonateTicks {
				be.resTicks++
			} else {
				for _, eid := range be.nearby { // makeRaidersGlow
					if m := h.mobs[eid]; m != nil && m.dying == 0 && m.dim == sp.dim && isRaiderType(m.etype) &&
						dist3(m.x, m.y, m.z, cx, cy, cz) < bellRaiderRange {
						h.applyMobEffectTicks(players, m, effGlowing, 0, bellGlowTicks)
					}
				}
				be.resonating = false
			}
		}
		if !be.shaking && !be.resonating && h.tick.Load() > be.lastScan+bellRescanTicks {
			delete(h.bells, sp) // idle, and its neighbourhood list is stale anyway
		}
	}
}

// bellRaidersWithin is areRaidersNearby over the strike's entity list.
func (h *hub) bellRaidersWithin(be *bellEntity, cx, cy, cz, r float64) bool {
	for _, eid := range be.nearby {
		if m := h.mobs[eid]; m != nil && m.dying == 0 && isRaiderType(m.etype) && dist3(m.x, m.y, m.z, cx, cy, cz) < r {
			return true
		}
	}
	return false
}
