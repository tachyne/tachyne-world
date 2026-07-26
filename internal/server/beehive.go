package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Beehives and bee nests. The block fills with honey while bees work it, and
// at full you harvest: shears cut three honeycomb, a bottle draws honey. Either
// way the bees come out angry — unless there is a campfire smoking underneath,
// which is the whole reason anyone builds one under a hive.
//
// SIMPLIFICATION, stated plainly: vanilla fills a hive when a bee that went out
// for nectar comes home, and tachyne's bee has no hive AI at all — it is a
// plain flyer. So honey here rises while bees are simply NEAR the hive in fair
// daylight. The harvest half is vanilla's; the production half is a stand-in
// until the bee gets its pollination behaviour (#121).

const (
	beeMaxHoney       = 5   // BeehiveBlock.MAX_HONEY_LEVELS
	beeHoneycombYield = 3   // shearing a full hive
	beeWorkRange      = 8.0 // how close a bee must be to count as working it
	beeFillChance     = 60  // 1-in-N per second per working bee
	beeCampfireDepth  = 5   // vanilla looks this far below for calming smoke
	beeAngerSecs      = 30  // how long a robbed hive's bees stay cross
)

var (
	beehiveMin, beehiveMax = worldgen.BlockRange("beehive")
	beeNestMin, beeNestMax = worldgen.BlockRange("bee_nest")
)

// isBeeHome reports whether a state is a beehive or a bee nest.
func isBeeHome(s uint32) bool {
	return (s >= beehiveMin && s <= beehiveMax) || (s >= beeNestMin && s <= beeNestMax)
}

// beeHomeBase is the first state of whichever of the two this is.
func beeHomeBase(s uint32) uint32 {
	if s >= beeNestMin && s <= beeNestMax {
		return beeNestMin
	}
	return beehiveMin
}

// honeyLevel reads the block's honey_level. The 24 states are facing(4) x
// honey_level(6) with the level varying fastest, so it is the low digit.
func honeyLevel(s uint32) int {
	if !isBeeHome(s) {
		return 0
	}
	return int((s - beeHomeBase(s)) % (beeMaxHoney + 1))
}

// withHoney returns the same block at a different honey level, facing intact.
func withHoney(s uint32, level int) uint32 {
	if !isBeeHome(s) || level < 0 || level > beeMaxHoney {
		return s
	}
	base := beeHomeBase(s)
	facing := (s - base) / (beeMaxHoney + 1)
	return base + facing*(beeMaxHoney+1) + uint32(level)
}

// updateBeehives fills the hives near players. Runs on the 1 Hz survival step.
func (h *hub) updateBeehives(players map[int32]*tracked) {
	if h.raining || !h.dayLight() {
		return // bees stay in when it rains, and at night
	}
	seen := map[blockPos]bool{}
	for _, m := range h.mobs {
		if m.etype != entityBee || m.dim != 0 || m.dying > 0 {
			continue
		}
		pos, ok := h.beeHomeNear(m)
		if !ok || seen[pos] {
			continue
		}
		seen[pos] = true
		cur := h.world.At(pos.x, pos.y, pos.z)
		if lvl := honeyLevel(cur); lvl < beeMaxHoney && h.rng.Intn(beeFillChance) == 0 {
			h.setBlockAt(players, 0, pos, withHoney(cur, lvl+1))
		}
	}
}

// beeHomeNear finds a hive close to a bee — the stand-in for "this bee lives
// here and has just come home with nectar".
func (h *hub) beeHomeNear(m *mob) (blockPos, bool) {
	r := int(beeWorkRange)
	bx, by, bz := int(m.x), int(m.y), int(m.z)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				p := blockPos{bx + dx, by + dy, bz + dz}
				if isBeeHome(h.world.At(p.x, p.y, p.z)) {
					return p, true
				}
			}
		}
	}
	return blockPos{}, false
}

// harvestBeeHome is the right-click: shears cut honeycomb, a bottle draws
// honey, and either empties the hive. Reports whether it did anything.
func (h *hub) harvestBeeHome(players map[int32]*tracked, t *tracked, pos blockPos) bool {
	cur := h.world.At(pos.x, pos.y, pos.z)
	if !isBeeHome(cur) || t.inv == nil {
		return false
	}
	if honeyLevel(cur) < beeMaxHoney {
		return false // not ready; vanilla just does nothing
	}
	held := heldStack(t)
	var give invStack
	switch held.item {
	case int32(itemByName["shears"]):
		give = invStack{item: int32(itemByName["honeycomb"]), count: beeHoneycombYield}
		h.applyToolWear(t, t.p.heldSlot(), 1)
	case int32(itemByName["glass_bottle"]):
		give = invStack{item: int32(itemByName["honey_bottle"]), count: 1}
		slot := t.p.heldSlot()
		if held.count--; held.count <= 0 {
			held = invStack{}
		}
		t.inv.slots[slot] = held
		h.sendSlot(t, slot)
	default:
		return false
	}

	changed, left := t.inv.addStack(give)
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	if left > 0 {
		h.spawnItem(players, give.item, left, t.x, t.y, t.z)
	}
	h.setBlockAt(players, 0, pos, withHoney(cur, 0))
	h.playSound(players, "minecraft:block.beehive.shear", sndBlock,
		float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, 1, 1)

	// Robbing a hive turns the bees on you — unless smoke is keeping them calm.
	if !h.campfireUnder(pos) {
		h.angerBees(players, t, pos)
	}
	return true
}

// dayLight reports whether it is daytime — bees work in daylight and stay in
// at night.
func (h *hub) dayLight() bool {
	dt := h.dayTime.Load() % dayLengthTicks
	return dt < 12000
}

// campfireUnder looks for a LIT campfire in the few blocks below a hive: the
// smoke is what lets you take the honey and walk away.
func (h *hub) campfireUnder(pos blockPos) bool {
	for dy := 1; dy <= beeCampfireDepth; dy++ {
		s := h.world.At(pos.x, pos.y-dy, pos.z)
		if isCampfireBlock(s) {
			return boolProp(s, "lit")
		}
	}
	return false
}

// angerBees sets the hive's neighbours on the robber.
func (h *hub) angerBees(players map[int32]*tracked, t *tracked, pos blockPos) {
	for _, m := range h.mobs {
		if m.etype != entityBee || m.dim != 0 || m.dying > 0 {
			continue
		}
		if dist3(m.x, m.y, m.z, float64(pos.x), float64(pos.y), float64(pos.z)) > beeWorkRange*2 {
			continue
		}
		h.provoke(m, t)
		m.anger = beeAngerSecs
	}
}

// evHarvestHive asks the hub to run a right-click on a hive.
type evHarvestHive struct {
	eid     int32
	x, y, z int
}

func (evHarvestHive) isHubEvent() {}
