package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Beacon, on the vanilla model: a pyramid of iron/gold/diamond/emerald/
// netherite blocks under the beacon sets its tier (1-4); an open column to
// the sky activates it; the menu takes one payment item and a power choice;
// every 80 ticks the beacon re-scans the pyramid and re-applies its effects
// to players in range (levels*10+10 blocks, ambient refresh 9+levels*2 s).
// The client renders the beam itself — it recomputes the pyramid from the
// chunk blocks once the beacon block entity (empty NBT) rides the chunk.

const menuBeacon = 9 // vanilla menu registration order (crafting 12 anchor)

var beaconState = worldgen.BlockBase("beacon") // single state

// beaconBaseStates are the pyramid blocks (#minecraft:beacon_base_blocks).
var beaconBaseStates = func() map[uint32]bool {
	m := map[uint32]bool{}
	for _, n := range []string{"iron_block", "gold_block", "diamond_block", "emerald_block", "netherite_block"} {
		m[worldgen.BlockBase(n)] = true
	}
	return m
}()

// beaconPayment are the accepted payment items (#minecraft:beacon_payment_items).
var beaconPayment = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"iron_ingot", "gold_ingot", "emerald", "diamond", "netherite_ingot"} {
		m[int32(itemByName[n])] = true
	}
	return m
}()

// beaconEffectTier is the pyramid tier each power unlocks at. Regeneration
// is tier 4 = secondary-only (a tier-4 primary is rejected, vanilla).
var beaconEffectTier = map[int32]int{
	effSpeed: 1, effHaste: 1,
	effResistance: 2, effJumpBoost: 2,
	effStrength: 3,
	effRegen:    4,
}

// beacon is one placed beacon's live state. The powers use the menu's
// property encoding (mob_effect id + 1, 0 = none); levels is the last
// 80-tick pyramid scan (0 = inactive).
type beacon struct {
	primary, secondary int32
	levels             int
}

type evOpenBeacon struct {
	eid     int32
	x, y, z int
}
type evSetBeacon struct {
	eid                int32
	primary, secondary int32
}

func (evOpenBeacon) isHubEvent() {}
func (evSetBeacon) isHubEvent()  {}

// beaconLevels computes the pyramid tier: layer s (1-4) at y-s must be a
// (2s+1)² square of base blocks (vanilla updateBase).
func beaconLevels(w *world.World, x, y, z int) int {
	levels := 0
	for step := 1; step <= 4; step++ {
		for dx := -step; dx <= step; dx++ {
			for dz := -step; dz <= step; dz++ {
				if !beaconBaseStates[w.At(x+dx, y-step, z+dz)] {
					return levels
				}
			}
		}
		levels = step
	}
	return levels
}

// beaconSkyOpen reports whether the beam reaches the sky: no full solid
// block in the column above (vanilla lets the beam pass anything with
// light dampening < 15; a full cube approximates the cut).
func beaconSkyOpen(w *world.World, x, y, z int) bool {
	top := worldgen.MinY + w.Ceiling()
	for cy := y + 1; cy < top; cy++ {
		if s := w.At(x, cy, z); s != 0 && worldgen.IsSolidFull(s) && !isBeamPassable(s) {
			return false
		}
	}
	return true
}

// isBeamPassable covers the full-cube blocks the vanilla beam still passes:
// the glass family (dampening 0) — stained glass tints, we just pass.
var glassStates = func() map[uint32]bool {
	m := map[uint32]bool{worldgen.BlockBase("glass"): true, worldgen.BlockBase("tinted_glass"): true}
	for _, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink",
		"gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
		m[worldgen.BlockBase(c+"_stained_glass")] = true
	}
	return m
}()

func isBeamPassable(s uint32) bool { return glassStates[s] }

// beaconsOnBlockChange keeps the beacon set current: placing a beacon
// registers it, breaking one drops it with the deactivate sound. Mirrors
// rodIndexOnBlockChange (overworld-only, like the container sims).
func (h *hub) beaconsOnBlockChange(players map[int32]*tracked, x, y, z int, state uint32) {
	pos := blockPos{x, y, z}
	if state == beaconState {
		if h.beacons[pos] == nil {
			h.beacons[pos] = &beacon{}
		}
		return
	}
	if b := h.beacons[pos]; b != nil {
		delete(h.beacons, pos)
		if b.levels > 0 {
			h.playSound(players, "minecraft:block.beacon.deactivate", sndBlock,
				float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
		}
	}
}

// openBeacon opens the beacon menu: one payment slot (t.anvil[0], reclaimed
// on close like the anvil) + the three container properties.
func (h *hub) openBeacon(t *tracked, x, y, z int) {
	if t.inv == nil || h.beacons[blockPos{x, y, z}] == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.reclaimEnchant(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind, t.winPos = h.nextWin, winBeacon, blockPos{x, y, z}

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuBeacon), Title: "Beacon"})
	h.sendBeaconWindow(t)
}

// sendBeaconWindow pushes the beacon window (payment slot + inventory) and
// the three menu properties: 0 power level, 1 primary, 2 secondary.
func (h *hub) sendBeaconWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 37) // 0 payment, 1-27 main, 28-36 hotbar
	slots = append(slots, stackEv(t.anvil[0]))
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
	b := h.beacons[t.winPos]
	if b == nil {
		return
	}
	prop := func(p, v int32) {
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: p, Value: v})
	}
	prop(0, int32(b.levels))
	prop(1, b.primary)
	prop(2, b.secondary)
}

// beaconValidEffects mirrors vanilla validateEffects: every chosen power's
// tier must be within the pyramid's, a secondary needs tier 4 and must be
// the tier-4 power or a copy of the primary, and the tier-4 power can never
// be primary.
func beaconValidEffects(levels int, primary, secondary int32) bool {
	tier := func(enc int32) int {
		if enc == 0 {
			return 0
		}
		t, ok := beaconEffectTier[enc-1]
		if !ok {
			return 99 // not a beacon power: fails every tier gate
		}
		return t
	}
	pt, st := tier(primary), tier(secondary)
	if secondary != 0 && levels < 4 {
		return false
	}
	if pt > levels || st > levels || pt >= 4 {
		return false
	}
	return st == 0 || st >= 4 || secondary == primary
}

// onSetBeacon applies the menu's confirm click: validate the choice against
// the live pyramid, consume the payment, store the powers.
func (h *hub) onSetBeacon(players map[int32]*tracked, t *tracked, primary, secondary int32) {
	if t.winKind != winBeacon {
		return
	}
	b := h.beacons[t.winPos]
	pay := &t.anvil[0]
	if b == nil || pay.item == 0 || pay.count == 0 || !beaconPayment[pay.item] ||
		!beaconValidEffects(b.levels, primary, secondary) {
		h.sendBeaconWindow(t) // AUTHORITY: resync instead of applying
		return
	}
	b.primary, b.secondary = primary, secondary
	if pay.count--; pay.count <= 0 { // vanilla consumes the payment in every mode
		*pay = invStack{}
	}
	h.playSound(players, "minecraft:block.beacon.power_select", sndBlock,
		float64(t.winPos.x)+0.5, float64(t.winPos.y)+0.5, float64(t.winPos.z)+0.5, 1, 1)
	h.sendBeaconWindow(t)
}

// beaconTick runs every 80 ticks (vanilla cadence): re-scan each pyramid,
// play the activation transitions, fire construct_beacon on tier growth,
// and re-apply the chosen effects to players in range.
func (h *hub) beaconTick(players map[int32]*tracked) {
	w := h.worldFor(0)
	for pos, b := range h.beacons {
		cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
		levels := 0
		if beaconSkyOpen(w, pos.x, pos.y, pos.z) {
			levels = beaconLevels(w, pos.x, pos.y, pos.z)
		}
		prev := b.levels
		b.levels = levels
		if prev == 0 && levels > 0 {
			h.playSound(players, "minecraft:block.beacon.activate", sndBlock, cx, cy, cz, 1, 1)
		} else if prev > 0 && levels == 0 {
			h.playSound(players, "minecraft:block.beacon.deactivate", sndBlock, cx, cy, cz, 1, 1)
		}
		if levels > prev { // vanilla: the trigger fires on tier growth, nearby players
			for _, t := range players {
				if t.dim == 0 && absF(t.x-cx) <= 10 && absF(t.y-cy) <= 10 && absF(t.z-cz) <= 10 {
					h.advance(players, t, "construct_beacon", advMatch{level: levels})
				}
			}
		}
		if levels == 0 {
			continue
		}
		h.playSound(players, "minecraft:block.beacon.ambient", sndBlock, cx, cy, cz, 1, 1)
		if b.primary == 0 {
			continue
		}
		rng := float64(levels*10 + 10)
		amp := 0
		if levels >= 4 && b.secondary == b.primary {
			amp = 1 // tier-4 double-up: primary at level II
		}
		secs := 9 + levels*2
		for _, t := range players {
			if t.dim != 0 || absF(t.x-cx) > rng || absF(t.z-cz) > rng || t.y < float64(pos.y)-rng {
				continue // vanilla box: ±range horizontally, down range, up to the sky
			}
			h.applyEffect(players, t, b.primary-1, amp, secs)
			if levels >= 4 && b.secondary != 0 && b.secondary != b.primary {
				h.applyEffect(players, t, b.secondary-1, 0, secs)
			}
		}
	}
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
