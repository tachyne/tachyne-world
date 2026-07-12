package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone tier 1b: repeaters, comparators, observers, pressure plates and
// daylight detectors. Repeater/comparator/observer are in the orientable
// property table (Get/SetProperty work); plates and detectors have their tiny
// layouts inlined like the torch.
//
// Direction conventions (minecraft.wiki):
//   repeater/comparator `facing` = output→input = toward the placing player,
//     so input cell = pos+facing, output cell = pos-facing.
//   observer `facing` = the watched direction = the player's look direction;
//     it pulses 15 for 2 ticks out of its back (pos-facing) when the watched
//     block changes.

const (
	observerPulseTicks = 2
)

var (
	repeaterMin   = worldgen.BlockBase("repeater")
	repeaterMax   = worldgen.BlockBase("repeater") + 63
	comparatorMin = worldgen.BlockBase("comparator")
	comparatorMax = worldgen.BlockBase("comparator") + 15
	observerMin   = worldgen.BlockBase("observer")
	observerMax   = worldgen.BlockBase("observer") + 11

	stonePlateOn  = worldgen.BlockBase("stone_pressure_plate") // powered is the low bit, true first
	stonePlateOff = worldgen.BlockBase("stone_pressure_plate") + 1
	oakPlateOn    = worldgen.BlockBase("oak_pressure_plate")
	oakPlateOff   = worldgen.BlockBase("oak_pressure_plate") + 1
	lightPlateMin = worldgen.BlockBase("light_weighted_pressure_plate") // base + power 0-15
	lightPlateMax = worldgen.BlockBase("light_weighted_pressure_plate") + 15
	heavyPlateMin = worldgen.BlockBase("heavy_weighted_pressure_plate")
	heavyPlateMax = worldgen.BlockBase("heavy_weighted_pressure_plate") + 15

	daylightMin = worldgen.BlockBase("daylight_detector") // inverted(2) × power(16); inverted=true is the LOW block
	daylightMax = worldgen.BlockBase("daylight_detector") + 31
)

func isRepeater(s uint32) bool   { return s >= repeaterMin && s <= repeaterMax }
func isComparator(s uint32) bool { return s >= comparatorMin && s <= comparatorMax }
func isObserver(s uint32) bool   { return s >= observerMin && s <= observerMax }
func isDaylight(s uint32) bool   { return s >= daylightMin && s <= daylightMax }

func isPlate(s uint32) bool {
	return s == stonePlateOn || s == stonePlateOff || s == oakPlateOn || s == oakPlateOff ||
		(s >= lightPlateMin && s <= heavyPlateMax)
}

// platePower is the signal a plate emits in its current state.
func platePower(s uint32) int {
	switch {
	case s == stonePlateOn || s == oakPlateOn:
		return 15
	case s >= lightPlateMin && s <= lightPlateMax:
		return int(s - lightPlateMin)
	case s >= heavyPlateMin && s <= heavyPlateMax:
		return int(s - heavyPlateMin)
	}
	return 0
}

// plateWith returns the plate state for a number of entities standing on it.
func plateWith(s uint32, count int) uint32 {
	p := count
	if p > 15 {
		p = 15
	}
	switch {
	case s == stonePlateOn || s == stonePlateOff:
		if p > 0 {
			return stonePlateOn
		}
		return stonePlateOff
	case s == oakPlateOn || s == oakPlateOff:
		if p > 0 {
			return oakPlateOn
		}
		return oakPlateOff
	case s >= lightPlateMin && s <= lightPlateMax:
		return lightPlateMin + uint32(p)
	case s >= heavyPlateMin && s <= heavyPlateMax:
		return heavyPlateMin + uint32(p)
	}
	return s
}

// daylight detector state math: power is the fastest-varying property.
func daylightPower(s uint32) int     { return int((s - daylightMin) % 16) }
func daylightInverted(s uint32) bool { return (s-daylightMin)/16 == 0 }
func daylightWith(inverted bool, power int) uint32 {
	inv := uint32(1)
	if inverted {
		inv = 0
	}
	return daylightMin + inv*16 + uint32(power)
}

// stateFacing reads the facing property of an orient-table block.
func stateFacing(s uint32) string {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return "north"
	}
	return worldgen.GetProperty(info, s, "facing")
}

// repeaterDelay is the repeater's configured delay in game ticks.
func repeaterDelay(s uint32) int {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return 2
	}
	d := worldgen.GetProperty(info, s, "delay")
	return 2 * int(d[0]-'0')
}

// obsDelta is the observer's watch direction as a 3D delta (6-way facing).
func obsDelta(s uint32) (int, int, int) {
	switch stateFacing(s) {
	case "up":
		return 0, 1, 0
	case "down":
		return 0, -1, 0
	}
	dx, dz := facingDelta(stateFacing(s))
	return dx, 0, dz
}

// updateRepeater: reads the cell behind (facing side), flips `powered` after
// the configured delay, emits 15 out the front. rsDue holds the pending flip.
func (h *hub) updateRepeater(players map[int32]*tracked, pos blockPos, state uint32) {
	dx, dz := facingDelta(stateFacing(state))
	in := h.emitPower(pos.x+dx, pos.y, pos.z+dz, pos.x, pos.y, pos.z) > 0
	cur := boolProp(state, "powered")
	if in == cur {
		delete(h.rsDue, pos) // input settled back before the flip landed
		return
	}
	now := h.tick.Load()
	due, pendingFlip := h.rsDue[pos]
	if !pendingFlip {
		h.rsDue[pos] = now + uint64(repeaterDelay(state))
		h.schedule(pos, uint64(repeaterDelay(state)))
		return
	}
	if now >= due {
		delete(h.rsDue, pos)
		h.setBlock(players, pos, setBoolProp(state, "powered", in))
		h.scheduleAround(pos, 1)
	}
}

// updateComparator: output = rear if rear >= strongest side (compare mode),
// or rear - strongest side (subtract). The level lives in h.compOut (vanilla
// keeps it in a block entity).
func (h *hub) updateComparator(players map[int32]*tracked, pos blockPos, state uint32) {
	dx, dz := facingDelta(stateFacing(state))
	rear := h.emitPower(pos.x+dx, pos.y, pos.z+dz, pos.x, pos.y, pos.z)
	if sig := h.containerSignal(blockPos{pos.x + dx, pos.y, pos.z + dz}); sig > rear {
		rear = sig // comparators measure container fullness through their back
	}
	sdx, sdz := facingDelta(leftOf(stateFacing(state)))
	side := h.emitPower(pos.x+sdx, pos.y, pos.z+sdz, pos.x, pos.y, pos.z)
	if s2 := h.emitPower(pos.x-sdx, pos.y, pos.z-sdz, pos.x, pos.y, pos.z); s2 > side {
		side = s2
	}
	out := 0
	info, _ := worldgen.InfoForState(state)
	if worldgen.GetProperty(info, state, "mode") == "subtract" {
		if out = rear - side; out < 0 {
			out = 0
		}
	} else if rear >= side {
		out = rear
	}
	if h.compOut[pos] == out && boolProp(state, "powered") == (out > 0) {
		return
	}
	h.compOut[pos] = out
	h.setBlock(players, pos, setBoolProp(state, "powered", out > 0))
	h.scheduleAround(pos, 1)
}

// updateObserver: pulse 15 out the back for 2 ticks when the watched block
// changes state (obsSeen remembers the last look).
func (h *hub) updateObserver(players map[int32]*tracked, pos blockPos, state uint32) {
	now := h.tick.Load()
	if boolProp(state, "powered") {
		if at, ok := h.obsPulse[pos]; !ok || now >= at+observerPulseTicks {
			delete(h.obsPulse, pos)
			h.setBlock(players, pos, setBoolProp(state, "powered", false))
			h.scheduleAround(pos, 1)
		}
		return
	}
	dx, dy, dz := obsDelta(state)
	watched := h.world.At(pos.x+dx, pos.y+dy, pos.z+dz)
	prev, seen := h.obsSeen[pos]
	h.obsSeen[pos] = watched
	if seen && watched != prev {
		h.obsPulse[pos] = now
		h.setBlock(players, pos, setBoolProp(state, "powered", true))
		h.schedule(pos, observerPulseTicks)
		h.scheduleAround(pos, 1)
	}
}

// updateDaylight follows the sky: power tracks the day curve (inverted flips
// it), self-rescheduling on a slow cadence while the detector exists.
func (h *hub) updateDaylight(players map[int32]*tracked, pos blockPos, state uint32) {
	day := h.dayTime.Load() % 24000
	power := 0
	if day < 12000 { // rough day curve: 0 at dawn/dusk, 15 at noon
		mid := int64(day) - 6000
		if mid < 0 {
			mid = -mid
		}
		power = int(15 * (6000 - mid) / 6000)
	}
	if daylightInverted(state) {
		power = 15 - power
	}
	if daylightPower(state) != power {
		h.setBlock(players, pos, daylightWith(daylightInverted(state), power))
		h.scheduleAround(pos, 1)
	}
	h.schedule(pos, 100)
}

// updatePlates is the per-tick occupancy scan: entities standing on plates
// press them; empty pressed plates release. platesOn tracks what's pressed.
func (h *hub) updatePlates(players map[int32]*tracked) {
	occupied := map[blockPos]int{}
	feet := func(x, y, z float64) {
		pos := blockPos{floorInt(x), floorInt(y + 0.01), floorInt(z)}
		if isPlate(h.world.At(pos.x, pos.y, pos.z)) {
			occupied[pos]++
		}
	}
	for _, t := range players {
		feet(t.x, t.y, t.z)
	}
	for _, m := range h.mobs {
		feet(m.x, m.y, m.z)
	}
	for pos, n := range occupied {
		s := h.world.At(pos.x, pos.y, pos.z)
		if ns := plateWith(s, n); ns != s {
			if platePower(s) == 0 {
				h.playSound(players, "minecraft:block.stone_pressure_plate.click_on", sndBlock,
					float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.4, 0.8)
			}
			h.setBlock(players, pos, ns)
			h.scheduleAround(pos, 1)
			h.platesOn[pos] = true
		} else if platePower(s) > 0 {
			h.platesOn[pos] = true
		}
	}
	for pos := range h.platesOn {
		if occupied[pos] > 0 {
			continue
		}
		delete(h.platesOn, pos)
		s := h.world.At(pos.x, pos.y, pos.z)
		if isPlate(s) && platePower(s) > 0 {
			h.setBlock(players, pos, plateWith(s, 0))
			h.playSound(players, "minecraft:block.stone_pressure_plate.click_off", sndBlock,
				float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.4, 0.7)
			h.scheduleAround(pos, 1)
		}
	}
}

// useRedstone1b handles right-clicks: cycle repeater delay, toggle comparator
// mode, flip a daylight detector.
func (h *hub) useRedstone1b(players map[int32]*tracked, pos blockPos, state uint32) bool {
	switch {
	case isRepeater(state):
		info, _ := worldgen.InfoForState(state)
		d := worldgen.GetProperty(info, state, "delay")[0] - '0'
		next := d%4 + 1
		h.setBlock(players, pos, worldgen.SetProperty(info, state, "delay", string(rune('0'+next))))
		h.playSound(players, "minecraft:block.lever.click", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, 1.2)
	case isComparator(state):
		info, _ := worldgen.InfoForState(state)
		mode := "subtract"
		if worldgen.GetProperty(info, state, "mode") == "subtract" {
			mode = "compare"
		}
		h.setBlock(players, pos, worldgen.SetProperty(info, state, "mode", mode))
		h.playSound(players, "minecraft:block.comparator.click", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, 1.1)
		h.schedule(pos, 1)
	case isDaylight(state):
		h.setBlock(players, pos, daylightWith(!daylightInverted(state), daylightPower(state)))
		h.schedule(pos, 1)
	default:
		return false
	}
	return true
}

func floorInt(v float64) int {
	i := int(v)
	if v < 0 && float64(i) != v {
		i--
	}
	return i
}
