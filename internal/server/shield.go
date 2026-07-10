package server

import "math"

// Shields — reimplemented from the vanilla 1.21.5 blocking path
// (LivingEntity.applyItemBlocking + the BLOCKS_ATTACKS component). Raising a
// shield (right-click hold, main hand) after a short delay blocks all damage
// from a ±90° front arc; the shield takes durability, and knockback still lands.

const (
	shieldDelay = 5 // block_delay: ticks the shield must be up before it blocks
)

var (
	itemShield = itemByName["shield"] // minecraft:shield item id
)

// evBlockStart raises a player's shield (they right-clicked holding one).
type evBlockStart struct{ eid int32 }

func (evBlockStart) isHubEvent() {}

// raiseShield records the tick a shield went up (if the player is actually
// holding one). isBlockingShield gates on the block delay.
func (h *hub) raiseShield(t *tracked) {
	if t.p.heldItem() == itemShield {
		t.blockingSince = h.tick.Load()
	}
}

// lowerShield drops the shield (release, hotbar switch, or a disabling hit).
func (h *hub) lowerShield(t *tracked) { t.blockingSince = 0 }

// isBlockingShield reports whether the raised shield is active (past the delay).
func (t *tracked) isBlockingShield(now uint64) bool {
	return t.blockingSince != 0 && now-t.blockingSince >= shieldDelay
}

// shieldBlocks reports whether a hit from (srcX,srcZ) is caught by the player's
// raised shield: the attacker must lie within the ±90° arc the player faces
// (vanilla acos(dirToSource · viewVector) ≤ 90°, i.e. their dot ≥ 0).
func (h *hub) shieldBlocks(t *tracked, srcX, srcZ float64) bool {
	if !t.isBlockingShield(h.tick.Load()) {
		return false
	}
	dx, dz := srcX-t.x, srcZ-t.z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return true // on top of them — count as front
	}
	yaw := float64(t.yaw) * math.Pi / 180
	// Minecraft look vector in the XZ plane: (-sin yaw, cos yaw).
	lookX, lookZ := -math.Sin(yaw), math.Cos(yaw)
	return (lookX*dx+lookZ*dz)/d >= 0
}

// shieldBlockFX plays the block sound and wears the shield down (the attack was
// caught, so it costs durability but no health).
func (h *hub) shieldBlockFX(players map[int32]*tracked, t *tracked) {
	h.playSound(players, "minecraft:item.shield.block", sndPlayer, t.x, t.y, t.z, 0.8, 0.9)
	h.post(evToolWear{eid: t.p.eid, slot: t.p.heldSlot()})
}
