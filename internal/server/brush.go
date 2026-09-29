package server

import (
	"math/rand"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Archaeology: brushing a suspicious block until whatever was buried in it
// falls out. Ported from BrushableBlock + BrushableBlockEntity.
//
// The block entity in vanilla stores which loot table this cell was seeded
// with and the seed to roll it against. Neither is stored here: the world
// generator is deterministic, so "which structure buried this block" is a
// QUESTION rather than a record — the same trick structloot.go uses to fill a
// structure chest on first open. All that needs keeping is how far along a
// player is, which lives only as long as the brushing does.

const (
	brushCooldown  = 10 // ticks between two brush strokes that count
	brushResetAfte = 40 // ticks of not brushing before the dust settles back
	brushesToBreak = 10 // strokes to open it
	brushRetract   = 4  // ticks per stage lost once it starts settling
)

var (
	itemBrush            = int32(itemByName["brush"])
	suspiciousSandBase   = worldgen.BlockBase("suspicious_sand")
	suspiciousGravelBase = worldgen.BlockBase("suspicious_gravel")
)

// brushing is one player's progress on one block. It is hub state rather than
// world state: nothing about a half-brushed block survives a restart in
// vanilla either, since the count decays in seconds.
type brushing struct {
	count     int
	resetAt   uint64
	coolUntil uint64
	face      int // the face last struck — the loot pops out of this side
	dim       int // which world the block is in
}

// evBrush is a player's brush click on a block face (BrushItem.useOn), which
// starts the held use.
type evBrush struct {
	eid        int32
	x, y, z    int
	dx, dy, dz int  // the clicked face normal
	off        bool // used from the offhand (the packet's InteractionHand)
}

func (evBrush) isHubEvent() {}

// isSuspicious reports whether a state is a suspicious block, and what it
// leaves behind once it gives up its contents.
func suspiciousTurnsInto(state uint32) (uint32, bool) {
	switch {
	case state >= suspiciousSandBase && state <= suspiciousSandBase+3:
		return worldgen.Sand, true
	case state >= suspiciousGravelBase && state <= suspiciousGravelBase+3:
		return worldgen.Gravel, true
	}
	return 0, false
}

// dustedStage is the block state for a brushing count — vanilla's
// getCompletionState, which is deliberately uneven: the first stroke shows
// immediately and the last four all read as nearly-clean.
func dustedStage(count int) int {
	switch {
	case count == 0:
		return 0
	case count < 3:
		return 1
	case count < 6:
		return 2
	default:
		return 3
	}
}

// brushLootTable reports which archaeology table this cell was seeded with, by
// asking the generator what buried it. Empty when nothing did — a suspicious
// block a player placed themselves holds nothing, exactly as in vanilla.
func (h *hub) brushLootTable(pos blockPos) (string, bool) {
	g := h.world.Gen()
	if w := g.DesertWellIn(pos.x, pos.z); w.Exists {
		for _, s := range w.Sus {
			if pos.x == s[0] && pos.y == s[1] && pos.z == s[2] {
				return "archaeology/desert_well", true
			}
		}
	}
	if r := g.OceanRuinsIn(pos.x, pos.z); r.Exists {
		table := "archaeology/ocean_ruin_cold"
		if r.Warm {
			table = "archaeology/ocean_ruin_warm"
		}
		for _, p := range r.Pieces {
			for _, s := range p.Sus {
				if pos.x == s[0] && pos.y == s[1] && pos.z == s[2] {
					return table, true
				}
			}
		}
	}
	if d := g.DesertTempleIn(pos.x, pos.z); d.Exists {
		for _, s := range d.Sus {
			if pos.x == s[0] && pos.y == s[1] && pos.z == s[2] {
				return "archaeology/desert_pyramid", true
			}
		}
	}
	for _, t := range g.TrailRuinsNear(pos.x, pos.z) {
		for _, s := range g.TrailRuinsSus(t) {
			if pos.x == s.X && pos.y == s.Y && pos.z == s.Z && s.Table != "" {
				return s.Table, true
			}
		}
	}
	return "", false
}

// brushUseTicks is BrushItem.getUseDuration: a held use lasts ten seconds,
// and the client starts another if the button is still down.
const brushUseTicks = 200

// startBrushing is BrushItem.useOn: a click on a block with the brush in hand
// starts a held use when the player's look reaches a block
// (calculateHitResult), and does nothing else — the strokes come from the
// hold (tickBrushing), not from clicks.
func (h *hub) startBrushing(t *tracked, e evBrush) {
	if s := t.handStack(t.useSlot()); t.dead || s == nil || s.item != itemBrush {
		return
	}
	if _, _, ok := h.brushTarget(t); !ok {
		return
	}
	// The click lands before the next tick's entity update, which is the
	// use's first onUseTick.
	t.brushFrom, t.brushOff = h.tick.Load()+1, e.off
}

// stopBrushing is releaseUsingItem for a brush: the button came up, the hand
// changed, or the look left the block.
func stopBrushing(t *tracked) { t.brushFrom = 0 }

// brushTarget is BrushItem.calculateHitResult: the block the player's look
// strikes (outline shapes, fluids passed through) within their block
// interaction range, and the face it strikes.
func (h *hub) brushTarget(t *tracked) (pos, face blockPos, ok bool) {
	reach := t.playerAttrs().Value(attr.BlockInteractionRange)
	lx, ly, lz := lookVector(t.yaw, t.pitch)
	ex, ey, ez := t.x, t.y+t.eyeHeight(), t.z
	return h.clipOutlineFace(t.dim, ex, ey, ez, ex+lx*reach, ey+ly*reach, ez+lz*reach)
}

// tickBrushing is BrushItem.onUseTick for every player holding a brush use:
// the tick of the use counted from one, a stroke on the one before each
// backswing (% 10 == 5), and the use given up when the look no longer
// strikes a block or the brush is gone from the hand. It ends on its own
// after two hundred ticks (completeUsingItem).
func (h *hub) tickBrushing(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, t := range players {
		if t.brushFrom == 0 || now < t.brushFrom {
			continue
		}
		elapsed := now + 1 - t.brushFrom // getUseDuration − ticksRemaining + 1
		prev := t.useOffhand
		t.useOffhand = t.brushOff
		held := t.handStack(t.useSlot())
		t.useOffhand = prev
		if t.dead || held == nil || held.item != itemBrush || elapsed > brushUseTicks {
			stopBrushing(t)
			continue
		}
		pos, face, ok := h.brushTarget(t)
		if !ok {
			stopBrushing(t)
			continue
		}
		if elapsed%10 == 5 {
			t.useOffhand = t.brushOff
			h.brushStroke(players, t, pos, face)
			t.useOffhand = prev
		}
		if elapsed == brushUseTicks {
			stopBrushing(t)
		}
	}
}

// brushStroke is one stroke of the held use: the brushing sound for the
// others (the brusher's client plays its own), then BrushableBlockEntity.brush
// on a suspicious block. Strokes inside the block's cooldown do nothing but
// keep the dust from settling.
func (h *hub) brushStroke(players map[int32]*tracked, t *tracked, pos, face blockPos) {
	w := h.worldFor(t.dim)
	if w == nil {
		return
	}
	state := w.At(pos.x, pos.y, pos.z)
	turnsInto, ok := suspiciousTurnsInto(state)
	// BrushableBlock.getBrushSound: sand or gravel by the block, and
	// BRUSH_GENERIC on anything else.
	snd := "minecraft:item.brush.brushing.generic"
	switch {
	case ok && turnsInto == worldgen.Gravel:
		snd = "minecraft:item.brush.brushing.gravel"
	case ok:
		snd = "minecraft:item.brush.brushing.sand"
	}
	h.playSoundExcept(players, t.dim, t.p.eid, snd, sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	if !ok {
		return
	}
	if h.brushes == nil {
		h.brushes = map[blockPos]*brushing{}
	}
	b := h.brushes[pos]
	if b == nil {
		b = &brushing{face: faceIndex(face.x, face.y, face.z), dim: t.dim}
		h.brushes[pos] = b
	}
	now := h.tick.Load()
	b.resetAt = now + brushResetAfte
	if now < b.coolUntil {
		return
	}
	b.coolUntil = now + brushCooldown

	was := dustedStage(b.count)
	b.count++
	if b.count >= brushesToBreak {
		h.finishBrush(players, t, pos, state, turnsInto, b)
		// BrushItem: the brush wears once, when the block is finished
		// (BrushableBlockEntity.brush returns true), not on every stroke.
		h.applyToolWear(t, t.useSlot(), 1)
		return
	}
	if stage := dustedStage(b.count); stage != was {
		h.setBlockAt(players, t.dim, pos, suspiciousBase(state)+uint32(stage))
		h.brushDisplay(players, t.dim, pos, b) // the item it is uncovering
	}
}

// suspiciousBase strips the dusted stage back off a state.
func suspiciousBase(state uint32) uint32 {
	if state >= suspiciousGravelBase && state <= suspiciousGravelBase+3 {
		return suspiciousGravelBase
	}
	return suspiciousSandBase
}

// finishBrush drops what was buried and leaves ordinary ground behind.
func (h *hub) finishBrush(players map[int32]*tracked, t *tracked, pos blockPos, state, turnsInto uint32, b *brushing) {
	delete(h.brushes, pos)
	h.setBlockAt(players, t.dim, pos, turnsInto)
	h.levelEvent(players, t.dim, worldEventBrushDone, pos.x, pos.y, pos.z, int32(state)) // BrushableBlockEntity.brush: the block's break sound + particles

	name, ok := h.brushLootTable(pos)
	if !ok {
		return // nothing seeded this cell — it was just dirty sand
	}
	tbl, found := lootForChest(name)
	if !found {
		return
	}
	// The item comes out of the face the player was brushing, as vanilla
	// pushes it out along hitDirection.
	dx, dy, dz := faceNormal(b.face)
	ox := float64(pos.x+dx) + 0.5
	oy := float64(pos.y+dy) + 0.5
	oz := float64(pos.z+dz) + 0.5

	r := rand.New(rand.NewSource(chestSeed(h.world.Seed(), pos, name)))
	ctx := &lootCtx{rng: r.Intn, randf: r.Float64}
	for _, s := range h.evalChestStacks(tbl, ctx, 0) {
		if s.count > 0 {
			if it := h.spawnItemIn(players, t.dim, s.item, s.count, ox, oy, oz); it != nil {
				it.setFrom(s) // the desert well's suspicious stew keeps its effect
				h.refreshItemMeta(players, it)
			}
		}
	}
	h.advance(players, t, "player_generates_container_loot", advMatch{lootTable: name})
}

// tickBrushes lets a half-brushed block settle back if nobody keeps at it —
// vanilla's checkReset, which retracts two strokes at a time so a distracted
// player loses ground faster than they gained it.
func (h *hub) tickBrushes(players map[int32]*tracked) {
	if len(h.brushes) == 0 {
		return
	}
	now := h.tick.Load()
	for pos, b := range h.brushes {
		if now < b.resetAt {
			continue
		}
		was := dustedStage(b.count)
		if b.count -= 2; b.count < 0 {
			b.count = 0
		}
		b.resetAt = now + brushRetract
		if stage := dustedStage(b.count); stage != was {
			if w := h.worldFor(b.dim); w != nil {
				st := w.At(pos.x, pos.y, pos.z)
				if _, ok := suspiciousTurnsInto(st); ok {
					h.setBlockAt(players, b.dim, pos, suspiciousBase(st)+uint32(stage))
				}
			}
		}
		if b.count == 0 {
			delete(h.brushes, pos)
		}
	}
}

// faceIndex/faceNormal pack a clicked face normal into the usual 0..5 order.
func faceIndex(dx, dy, dz int) int {
	switch {
	case dy < 0:
		return 0
	case dy > 0:
		return 1
	case dz < 0:
		return 2
	case dz > 0:
		return 3
	case dx < 0:
		return 4
	default:
		return 5
	}
}

func faceNormal(i int) (int, int, int) {
	switch i {
	case 0:
		return 0, -1, 0
	case 1:
		return 0, 1, 0
	case 2:
		return 0, 0, -1
	case 3:
		return 0, 0, 1
	case 4:
		return -1, 0, 0
	default:
		return 1, 0, 0
	}
}
