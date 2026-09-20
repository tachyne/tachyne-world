package server

import "math"

// The two sounds a body makes: the splash when it hits water, and the thump
// when it hits the ground hard enough to hurt.
//
// Both are vanilla's and both were missing. A player or a mob entering water
// fired the SPLASH game event (so a sculk sensor heard it) but made no sound
// at all, and nothing that took fall damage made a sound either — landings
// were silent however far the drop.

const (
	// Entity.doWaterSplashEffect: the volume is the speed going in, and past a
	// quarter it is the heavier splash instead.
	splashHeavyAbove = 0.25
	splashSelfScale  = 0.2 // f: 0.2 riding nothing, 0.9 carrying a rider
	// LivingEntity.getFallDamageSound: over four points of damage it is the
	// big one.
	bigFallAbove = 4.0
)

// splashVolume is doWaterSplashEffect's own formula: horizontal motion counts
// for a fifth of vertical, so a belly-flop from height is loud and swimming
// in off the shallows is not.
func splashVolume(vx, vy, vz float64) float32 {
	v := math.Sqrt(vx*vx*0.2 + vy*vy + vz*vz*0.2)
	return float32(math.Min(1, v*splashSelfScale))
}

// splashSound is the sound for that volume. A PLAYER has two of its own —
// the ordinary splash and the high-speed one past a quarter — where every
// other entity uses the same generic splash for both, which is why the
// player case is the one that takes an argument at all.
func splashSound(vol float32, isPlayer bool) string {
	if !isPlayer {
		return "minecraft:entity.generic.splash"
	}
	if vol < splashHeavyAbove {
		return "minecraft:entity.player.splash"
	}
	return "minecraft:entity.player.splash.high_speed"
}

// fallDamageSound is GENERIC_BIG_FALL past four points of damage, else the
// small one — the same split vanilla makes.
func fallDamageSound(damage float64) string {
	if damage > bigFallAbove {
		return "minecraft:entity.generic.big_fall"
	}
	return "minecraft:entity.generic.small_fall"
}

// playSplash makes the noise of something entering water at that speed.
func (h *hub) playSplash(players map[int32]*tracked, dim int, x, y, z, vx, vy, vz float64, isPlayer bool) {
	vol := splashVolume(vx, vy, vz)
	if vol <= 0 {
		vol = 0.2 // stepping in gently still makes a sound
	}
	h.playSoundDim(players, dim, splashSound(vol, isPlayer), sndNeutral, x, y, z, vol,
		1+(h.rng.Float32()-h.rng.Float32())*0.4)
}

// playFallDamageSound is the thump of a landing that hurt.
func (h *hub) playFallDamageSound(players map[int32]*tracked, dim int, x, y, z, damage float64) {
	h.playSoundDim(players, dim, fallDamageSound(damage), sndNeutral, x, y, z, 1, 1)
}
