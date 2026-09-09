package server

// Dolphins lead to treasure (vanilla Dolphin.mobInteract +
// DolphinSwimToTreasureGoal): fed a fish, a dolphin takes a bearing on
// the nearest shipwreck within fifty chunks (vanilla's #dolphin_located is
// shipwrecks and ocean ruins; the engine's oceans hold wrecks) and swims
// for it, giving up the errand once within four blocks — or at once when
// there is nothing within reach.

const (
	dolphinTreasureRadius = 50 * 16
	dolphinArrive         = 4.0
	dolphinLeadSpeed      = 1.3
)

var fishItems = func() map[int32]bool {
	out := map[int32]bool{}
	for _, n := range []string{"cod", "salmon", "tropical_fish", "pufferfish"} { // #minecraft:fishes
		if id, ok := itemByName[n]; ok {
			out[int32(id)] = true
		}
	}
	return out
}()

// tryFeedDolphin is Dolphin.mobInteract: a fish sets it off after treasure.
func (h *hub) tryFeedDolphin(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityDolphin || m.dying > 0 || !fishItems[heldStack(t).item] {
		return false
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.dolphin.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	if m.baby {
		return true // a calf just eats
	}
	if x, z, ok := h.worldFor(m.dim).Gen().NearestDolphinTreasure(int(m.x), int(m.z), dolphinTreasureRadius); ok && m.dim == 0 {
		m.gotFish, m.treasureX, m.treasureZ = true, x, z
		h.playSoundDim(players, m.dim, "minecraft:entity.dolphin.play", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
	return true
}

// dolphinStep swims a fed dolphin toward its wreck.
func (h *hub) dolphinStep(players map[int32]*tracked, m *mob) bool {
	if !m.gotFish {
		return false
	}
	tx, tz := float64(m.treasureX)+0.5, float64(m.treasureZ)+0.5
	dx, dz := tx-m.x, tz-m.z
	if dx*dx+dz*dz <= dolphinArrive*dolphinArrive {
		m.gotFish = false
		return false
	}
	h.steerTo(m, tx, tz, dolphinLeadSpeed)
	return true
}
