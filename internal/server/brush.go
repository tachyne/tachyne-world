package server

import (
	"math/rand"

	"github.com/tachyne/tachyne-world/internal/worldgen"
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

// evBrush is a player's brush stroke on a block face.
type evBrush struct {
	eid        int32
	x, y, z    int
	dx, dy, dz int // the clicked face normal
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
	return "", false
}

// brush applies one stroke. Strokes inside the cooldown do nothing but keep
// the dust from settling, which is what makes brushing a hold rather than a
// race.
func (h *hub) brush(players map[int32]*tracked, t *tracked, e evBrush) {
	pos := blockPos{e.x, e.y, e.z}
	w := h.worldFor(t.dim)
	if w == nil {
		return
	}
	state := w.At(pos.x, pos.y, pos.z)
	turnsInto, ok := suspiciousTurnsInto(state)
	if !ok {
		return
	}
	if h.brushes == nil {
		h.brushes = map[blockPos]*brushing{}
	}
	b := h.brushes[pos]
	if b == nil {
		b = &brushing{face: faceIndex(e.dx, e.dy, e.dz), dim: t.dim}
		h.brushes[pos] = b
	}
	now := h.tick.Load()
	b.resetAt = now + brushResetAfte
	if now < b.coolUntil {
		return
	}
	b.coolUntil = now + brushCooldown
	h.playSound(players, "minecraft:item.brush.brushing.sand", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)

	was := dustedStage(b.count)
	b.count++
	if b.count >= brushesToBreak {
		h.finishBrush(players, t, pos, state, turnsInto, b)
		return
	}
	if stage := dustedStage(b.count); stage != was {
		h.setBlockAt(players, t.dim, pos, suspiciousBase(state)+uint32(stage))
	}
	h.applyToolWear(t, t.p.heldSlot(), 1)
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
	h.playSound(players, "minecraft:block.rooted_dirt.break", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)

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
			h.spawnItemIn(players, t.dim, s.item, s.count, ox, oy, oz)
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
