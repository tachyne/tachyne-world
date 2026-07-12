package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/worldgen"
)

const metaIndexCopperWeather = 16 // copper golem WEATHERING_COPPER_STATE index

// copperWeatherMeta emits the golem's oxidation stage (0 unaffected → 3 oxidized)
// as a plain INT at index 16 — a 770-safe placeholder, since 770 has no
// WEATHERING_COPPER_STATE serializer. The gateway restores the real value-type
// for clients that have the copper golem (protocol.FixCopperGolemMeta); older
// clients render a substituted Frog and drop the metadata entirely.
func copperWeatherMeta(eid, stage int32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCopperWeather)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, stage)
	return protocol.AppendU8(b, itemMetaEnd)
}

// Oxidation lifecycle (vanilla CopperGolem). An unwaxed golem ages through
// unaffected → exposed → weathered → oxidized, one stage every 504000-552000
// ticks; once fully oxidized it has a 0.58%/tick chance to freeze into a copper
// golem statue block. Honeycomb waxes it (stops oxidation); an axe un-waxes or
// scrapes off a stage. The intermediate weather-state VISUAL is entity metadata
// (a new 1.21.9 value-type) — deferred; the golem renders unaffected until it
// becomes the statue block (which renders correctly, being a block state).
const (
	copperWeatherFrom  = 504000 // WEATHERING_TICK_FROM..TO
	copperWeatherTo    = 552000
	copperStatueChance = 0.0058 // canTurnToStatue: nextFloat() <= 0.0058
)

var (
	itemHoneycomb = itemByName["honeycomb"]
	axeItems      = func() map[int32]bool {
		m := map[int32]bool{}
		for _, mat := range []string{"wooden", "stone", "copper", "iron", "golden", "diamond", "netherite"} {
			if id, ok := itemByName[mat+"_axe"]; ok {
				m[id] = true
			}
		}
		return m
	}()
)

func (h *hub) copperWeatherDelay() uint64 {
	return uint64(copperWeatherFrom + h.rng.Intn(copperWeatherTo-copperWeatherFrom+1))
}

// updateCopperGolems ages copper golems (1 Hz): each unwaxed golem advances an
// oxidation stage on its schedule, and once oxidized may freeze into a statue.
func (h *hub) updateCopperGolems(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, m := range h.mobs {
		if m.etype != entityCopperGolem || m.dying > 0 {
			continue
		}
		if !m.waxed { // waxing halts oxidation but not item sorting
			if m.oxidizeAt == 0 { // unset → schedule the first step
				m.oxidizeAt = now + h.copperWeatherDelay()
			}
			if m.oxidation < 3 && now >= m.oxidizeAt {
				m.oxidation++
				if m.oxidation < 3 {
					m.oxidizeAt = now + h.copperWeatherDelay()
				}
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(copperWeatherMeta(m.eid, int32(m.oxidation))))
			}
			if m.oxidation >= 3 {
				bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
				if h.world.At(bx, by, bz) == worldgen.Air && h.rng.Float32() <= copperStatueChance {
					h.copperGolemToStatue(players, m, bx, by, bz)
					continue
				}
			}
		}
		h.copperGolemSort(players, m) // sort items between chests
	}
}

// copperGolemToStatue freezes an oxidized golem into a statue block (random pose,
// facing from its yaw) and despawns it.
func (h *hub) copperGolemToStatue(players map[int32]*tracked, m *mob, x, y, z int) {
	statue := worldgen.BlockID("oxidized_copper_golem_statue")
	state := statue
	if info, ok := worldgen.InfoForState(statue); ok {
		poses := []string{"standing", "sitting", "running", "star"}
		state = worldgen.SetProperty(info, statue, "copper_golem_pose", poses[h.rng.Intn(len(poses))])
		state = worldgen.SetProperty(info, state, "facing", yawFacing(m.yaw))
	}
	h.setBlock(players, blockPos{x, y, z}, state)
	h.despawnMob(players, m)
	h.playSound(players, "minecraft:entity.copper_golem.become_statue", sndNeutral, m.x, m.y, m.z, 1, 1)
}

// tryCopperGolem: honeycomb waxes the golem (stops oxidation); an axe un-waxes or
// scrapes off one oxidation stage. Returns true if the interaction was consumed.
func (h *hub) tryCopperGolem(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityCopperGolem || m.dying > 0 {
		return false
	}
	held := heldStack(t).item
	switch {
	case held == itemHoneycomb && !m.waxed:
		m.waxed = true
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.playSound(players, "minecraft:item.honeycomb.wax_on", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	case axeItems[held] && m.waxed:
		m.waxed = false
		h.playSound(players, "minecraft:item.axe.wax_off", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	case axeItems[held] && m.oxidation > 0:
		m.oxidation--
		m.oxidizeAt = h.tick.Load() + h.copperWeatherDelay()
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(copperWeatherMeta(m.eid, int32(m.oxidation))))
		h.playSound(players, "minecraft:item.axe.scrape", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	return false
}

// yawFacing maps a Minecraft yaw to a horizontal facing (Direction.fromYRot).
func yawFacing(yaw float32) string {
	switch int(math.Floor(float64(yaw)/90+0.5)) & 3 {
	case 0:
		return "south"
	case 1:
		return "west"
	case 2:
		return "north"
	default:
		return "east"
	}
}

// Copper golem construction (1.21.9). Vanilla CarvedPumpkinBlock builds a copper
// golem from the pattern {"^","#"} — a carved pumpkin (or jack o'lantern) on top
// of any copper block (BlockTags.COPPER). On completion the copper block becomes
// a copper chest facing the pumpkin, and the golem spawns where the pumpkin was.

var (
	carvedPumpkinBase = worldgen.BlockBase("carved_pumpkin") // facing(4)
	jackLanternBase   = worldgen.BlockBase("jack_o_lantern") // facing(4)
)

// copperBlockStates: the eight plain copper blocks (4 oxidation × waxed) — the
// "block of copper of any oxidation stage" that forms a golem.
var copperBlockStates = map[uint32]bool{
	worldgen.BlockID("copper_block"): true, worldgen.BlockID("exposed_copper"): true,
	worldgen.BlockID("weathered_copper"): true, worldgen.BlockID("oxidized_copper"): true,
	worldgen.BlockID("waxed_copper_block"): true, worldgen.BlockID("waxed_exposed_copper"): true,
	worldgen.BlockID("waxed_weathered_copper"): true, worldgen.BlockID("waxed_oxidized_copper"): true,
}

func isCarvedPumpkin(state uint32) bool {
	return (state >= carvedPumpkinBase && state < carvedPumpkinBase+4) ||
		(state >= jackLanternBase && state < jackLanternBase+4)
}

// checkCopperGolemBuild runs after a block is placed (mirrors checkWitherBuild):
// a carved pumpkin on top of a copper block builds a copper golem — the pumpkin
// is consumed and the golem spawns there, the copper block below becomes a copper
// chest facing the pumpkin.
func (h *hub) checkCopperGolemBuild(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	if dim != 0 || !isCarvedPumpkin(state) || !copperBlockStates[h.world.At(x, y-1, z)] {
		return
	}
	facing := "north"
	if info, ok := worldgen.InfoForState(state); ok {
		if f := worldgen.GetProperty(info, state, "facing"); f != "" {
			facing = f // the chest faces the pumpkin's facing direction
		}
	}
	chest := worldgen.BlockID("copper_chest")
	if info, ok := worldgen.InfoForState(chest); ok {
		chest = worldgen.SetProperty(info, chest, "facing", facing)
	}
	h.setBlock(players, blockPos{x, y, z}, worldgen.Air) // pumpkin consumed; golem spawns here
	h.setBlock(players, blockPos{x, y - 1, z}, chest)    // copper block -> copper chest
	m := h.spawnSpecies(players, entityCopperGolem, 0, float64(x)+0.5, float64(y)+0.05, float64(z)+0.5)
	if m == nil {
		return
	}
	m.yaw, m.syaw = 0, 0 // vanilla snapTo yaw 0
	h.playSound(players, "minecraft:block.copper.place", sndNeutral, float64(x), float64(y), float64(z), 1, 1)
}

// Item sorting (vanilla TransportItemsBetweenContainers). The golem carries
// items OUT of copper chests and into wooden/trapped chests within a 65×17×65
// box, up to 16 at a time, on a 60–100 tick cooldown between transports.
const (
	copperSortRangeH = 65   // horizontal search distance
	copperSortRangeV = 17   // vertical search distance
	copperSortMax    = 16   // items moved per transport
	copperSortCDMin  = 60   // transport cooldown (nextInt(60,100))
	copperSortCDMax  = 100  //
	copperSortReach  = 2.5  // close enough to interact with a container
	copperSortSpeed  = 0.22 // steer factor toward the goal
)

// copperGolemBehavior steers the golem toward the container it's transporting
// to/from; it idles (wanders) when it has no goal.
type copperGolemBehavior struct{}

func (copperGolemBehavior) name() string { return "copper_golem" }
func (copperGolemBehavior) steer(h *hub, m *mob) (float64, float64) {
	if m.sortHasGoal {
		dx := float64(m.sortGoal.x) + 0.5 - m.x
		dz := float64(m.sortGoal.z) + 0.5 - m.z
		if math.Hypot(dx, dz) > copperSortReach {
			return dx * copperSortSpeed, dz * copperSortSpeed
		}
		return 0, 0
	}
	return wanderBehavior{}.steer(h, m)
}

func (h *hub) golemNear(m *mob, p blockPos) bool {
	return math.Hypot(float64(p.x)+0.5-m.x, float64(p.z)+0.5-m.z) <= copperSortReach &&
		math.Abs(float64(p.y)-m.y) <= 2
}

func (h *hub) inSortRange(m *mob, p blockPos) bool {
	return math.Abs(float64(p.x)+0.5-m.x) <= copperSortRangeH &&
		math.Abs(float64(p.z)+0.5-m.z) <= copperSortRangeH &&
		math.Abs(float64(p.y)-m.y) <= copperSortRangeV
}

// copperGolemSort runs the golem's transport state machine (1 Hz): empty-handed
// it takes a stack from a copper chest; carrying, it deposits into a wooden or
// trapped chest.
func (h *hub) copperGolemSort(players map[int32]*tracked, m *mob) {
	if m.dying > 0 {
		return
	}
	if m.sortCD > 0 {
		m.sortCD--
		return
	}
	if m.carrying.item == 0 {
		pos, slot := h.findCopperSource(m)
		if slot < 0 {
			m.sortHasGoal = false
			return
		}
		m.sortGoal, m.sortHasGoal = pos, true
		if h.golemNear(m, pos) {
			st := &h.chests[pos].slots[slot]
			n := st.count
			if n > copperSortMax {
				n = copperSortMax
			}
			m.carrying = invStack{item: st.item, count: n, dmg: st.dmg, ench: st.ench}
			if st.count -= n; st.count == 0 {
				*st = invStack{}
			}
			m.sortCD = copperSortCDMin + h.rng.Intn(copperSortCDMax-copperSortCDMin+1)
			m.sortHasGoal = false
		}
		return
	}
	pos, ok := h.findSortTarget(m)
	if !ok {
		m.sortHasGoal = false // nowhere to put it — keep carrying
		return
	}
	m.sortGoal, m.sortHasGoal = pos, true
	if h.golemNear(m, pos) {
		m.carrying = depositIntoChest(h.chests[pos], m.carrying)
		m.sortCD = copperSortCDMin + h.rng.Intn(copperSortCDMax-copperSortCDMin+1)
		m.sortHasGoal = false
	}
}

// findCopperSource returns the position + slot of the nearest copper chest in
// range holding items (slot -1 = none).
func (h *hub) findCopperSource(m *mob) (blockPos, int) {
	best, bestPos, bestSlot := math.MaxFloat64, blockPos{}, -1
	for pos, c := range h.chests {
		if !isCopperChest(h.world.At(pos.x, pos.y, pos.z)) || !h.inSortRange(m, pos) {
			continue
		}
		for i, st := range c.slots {
			if st.item != 0 && st.count > 0 {
				if d := math.Hypot(float64(pos.x)-m.x, float64(pos.z)-m.z); d < best {
					best, bestPos, bestSlot = d, pos, i
				}
				break
			}
		}
	}
	return bestPos, bestSlot
}

// findSortTarget returns the nearest wooden/trapped chest in range with room for
// the golem's carried stack.
func (h *hub) findSortTarget(m *mob) (blockPos, bool) {
	best, bestPos, found := math.MaxFloat64, blockPos{}, false
	for pos := range h.chests {
		s := h.world.At(pos.x, pos.y, pos.z)
		if isCopperChest(s) || !isChestBlock(s) || !h.inSortRange(m, pos) {
			continue
		}
		if !chestHasRoom(h.chests[pos], m.carrying) {
			continue
		}
		if d := math.Hypot(float64(pos.x)-m.x, float64(pos.z)-m.z); d < best {
			best, bestPos, found = d, pos, true
		}
	}
	return bestPos, found
}

// chestHasRoom reports whether a stack can be (partly) placed in a chest.
func chestHasRoom(c *chest, s invStack) bool {
	for _, slot := range c.slots {
		if slot.item == 0 || (slot.item == s.item && slot.dmg == s.dmg && slot.ench == s.ench && slot.count < 64) {
			return true
		}
	}
	return false
}

// depositIntoChest stacks a carried stack into a chest, returning the leftover.
func depositIntoChest(c *chest, s invStack) invStack {
	for i := range c.slots { // top up matching stacks first
		d := &c.slots[i]
		if d.item == s.item && d.dmg == s.dmg && d.ench == s.ench && d.count < 64 {
			room := 64 - d.count
			n := min(room, s.count)
			d.count += n
			if s.count -= n; s.count == 0 {
				return invStack{}
			}
		}
	}
	for i := range c.slots { // then empty slots
		if c.slots[i].item == 0 {
			c.slots[i] = s
			return invStack{}
		}
	}
	return s // no room — keep carrying
}
