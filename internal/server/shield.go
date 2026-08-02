package server

import "math"

// Shields — the vanilla blocking path (LivingEntity.applyItemBlocking + the
// BLOCKS_ATTACKS component a shield carries). Raising a shield after a short
// delay blocks damage from the front arc; the shield takes durability scaled by
// the hit, and the knockback still lands.
//
// Which hits a shield stops is not a property of the shield but of the damage:
// the component names a damage-type tag it is bypassed by, and for the vanilla
// shield that tag is bypasses_shield. So the question is asked once, in hurtBy,
// against the type — not at each place that deals damage. It used to be asked
// at three of them, which is why a shield stopped a bite, an arrow and another
// player and nothing else: not an explosion, not a fireball, not a wither
// skull, not a llama's spit.

const (
	// The shield's BLOCKS_ATTACKS component, as vanilla configures it.
	shieldDelay = 5    // block_delay_seconds 0.25 → ticks the shield must be up
	shieldArc   = 90.0 // horizontal_blocking_angle, in degrees either side
	// damage_reductions[0] is base 0 + factor 1, i.e. the whole hit.
	shieldReduceBase   = 0.0
	shieldReduceFactor = 1.0
	// item_damage: below the threshold the shield takes nothing, at or above
	// it floor(base + factor × blocked). A shield shrugs off small hits for
	// free and wears fast against big ones.
	shieldWearThreshold = 3.0
	shieldWearBase      = 1.0
	shieldWearFactor    = 1.0
)

var (
	itemShield = itemByName["shield"] // minecraft:shield item id
)

// dmgFrom is where a hit came from, when that is known. Vanilla resolves the
// blocking angle against the source position and treats a hit with no position
// as coming from everywhere at once (the angle resolves to 180°, outside any
// arc), so the zero value means exactly "unblockable" — which is what you want
// for starving, drowning or falling.
type dmgFrom struct {
	x, z float64
	ok   bool
}

// from names a source position for a hit.
func from(x, z float64) dmgFrom { return dmgFrom{x: x, z: z, ok: true} }

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
	// Written as an addition rather than "now - blockingSince >= delay": these
	// are unsigned, so a now that trails blockingSince wraps to a colossal
	// number and reads as "up for ages".
	return t.blockingSince != 0 && now >= t.blockingSince+shieldDelay
}

// facesSource reports whether a hit from (srcX,srcZ) falls inside the arc the
// player faces — vanilla acos(dirToSource · viewVector) ≤ the blocking angle.
func (t *tracked) facesSource(srcX, srcZ float64) bool {
	dx, dz := srcX-t.x, srcZ-t.z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return true // on top of them — count as front
	}
	yaw := float64(t.yaw) * math.Pi / 180
	// Minecraft look vector in the XZ plane: (-sin yaw, cos yaw).
	lookX, lookZ := -math.Sin(yaw), math.Cos(yaw)
	cos := (lookX*dx + lookZ*dz) / d
	return math.Acos(math.Max(-1, math.Min(1, cos))) <= shieldArc*math.Pi/180
}

// shieldBlocked returns how much of a hit the player's raised shield eats.
//
// Zero covers every way a shield can fail: not raised, not raised long enough,
// the attacker is behind them, or the damage is of a sort no shield stops.
func (h *hub) shieldBlocked(t *tracked, amount float32, dt dmgType, src dmgFrom) float32 {
	if amount <= 0 || dt.has(tagBypassesShield) {
		return 0
	}
	if !t.isBlockingShield(h.tick.Load()) {
		return 0
	}
	if !src.ok || !t.facesSource(src.x, src.z) {
		return 0
	}
	blocked := shieldReduceBase + shieldReduceFactor*amount
	return float32(math.Max(0, math.Min(float64(blocked), float64(amount))))
}

// shieldBlockFX plays the block sound and wears the shield by what it stopped.
// Vanilla scales the wear with the hit rather than charging a flat point, so a
// shield survives a swarm of weak blows and not a charged one.
func (h *hub) shieldBlockFX(players map[int32]*tracked, t *tracked, blocked float32) {
	h.playSound(players, "minecraft:item.shield.block", sndPlayer, t.x, t.y, t.z, 0.8, 0.9)
	if n := shieldWear(blocked); n > 0 {
		// Called on the hub goroutine, so this wears the stack directly. It
		// used to h.post the wear event to itself, which is the one thing a
		// hub-goroutine caller must not do — the events channel is buffered,
		// and a full one would have the hub waiting on itself.
		h.applyToolWear(t, t.p.heldSlot(), n)
	}
}

// shieldWear is the shield's item_damage function: nothing below the
// threshold, floor(base + factor × blocked) at or above it.
func shieldWear(blocked float32) int {
	if blocked < shieldWearThreshold {
		return 0
	}
	return int(math.Floor(shieldWearBase + shieldWearFactor*float64(blocked)))
}
