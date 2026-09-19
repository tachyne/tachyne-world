package server

// The nautilus as a mount (AbstractNautilus, 1.21.11): tamed with a
// pufferfish (one try in three), saddled, and ridden under water — where
// its rider breathes, on the Breath of the Nautilus the mount grants and
// refreshes — with a dash on the jump key like the camel's. The riding
// client applies the dash impulse itself (executeRidersJump on the local
// authority); the server plays the dash, keeps the forty-tick cooldown and
// signals when it is ready again.

const (
	nautilusDashCooldown = 40 // AbstractNautilus.executeRidersJump: dashCooldown = 40
	nautilusBreathTicks  = 60 // applyEffects: 60 ticks, refreshed every 40
)

// nautilusDashStart is handleStartJump for the nautilus the player rides.
func (h *hub) nautilusDashStart(players map[int32]*tracked, t *tracked) {
	m := h.mobs[t.ridingEID]
	if m == nil || m.etype != entityNautilus || m.dying > 0 || !m.saddled || m.dashCD > 0 {
		return
	}
	m.dashCD, m.dashing = nautilusDashCooldown, true
	snd := "minecraft:entity.nautilus.dash_land"
	if h.inWater(m.dim, m.x, m.y, m.z) {
		snd = "minecraft:entity.nautilus.dash"
	}
	h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
	h.vibAt(m.dim, freqEntityAction, m.x, m.y, m.z, m.eid)
}

// nautilusDashTick runs the cooldown and plays the ready cue when it ends.
func (h *hub) nautilusDashTick(players map[int32]*tracked, m *mob) {
	if m.dashCD == 0 {
		return
	}
	if m.dashCD -= mobMoveInterval; m.dashCD <= 0 {
		m.dashCD, m.dashing = 0, false
		snd := "minecraft:entity.nautilus.dash_ready_land"
		if h.inWater(m.dim, m.x, m.y, m.z) {
			snd = "minecraft:entity.nautilus.dash_ready"
		}
		h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
	}
}

// nautilusBreath is AbstractNautilus.applyEffects, on the survival step:
// the rider carries Breath of the Nautilus while mounted.
func (h *hub) nautilusBreath(players map[int32]*tracked, m *mob) {
	t := players[m.rider]
	if t == nil || t.dead {
		return
	}
	if t.hasEffect(effBreathOfTheNautilus) == 0 || h.tick.Load()%40 == 0 {
		h.applyEffect(players, t, effBreathOfTheNautilus, 0, nautilusBreathTicks/20)
	}
}
