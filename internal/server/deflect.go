package server

// A player's swing at a projectile in #redirectable_projectile (a ghast's
// fireball, a wind charge) sends it back along the player's look
// (Player.deflectProjectile → Projectile.deflect with AIM_DEFLECT): the
// projectile becomes the player's own, so a ghast killed by its returned
// fireball is a player's kill by a fireball, which is what its music disc
// and Return to Sender ask for.

// reflectedFireballDamage is what a ghast takes from a large fireball a
// player is behind (Ghast.isReflectedFireball: the fireball as the direct
// entity, a player as the cause) — the hit or its blast — which no ghast
// survives.
const reflectedFireballDamage = 1000

// redirectable is the #redirectable_projectile tag.
func redirectable(etype int) bool {
	return etype == entityLargeFireball || etype == entityWindCharge
}

// deflectProjectile is the swing; it reports whether the projectile took it.
func (h *hub) deflectProjectile(players map[int32]*tracked, t *tracked, a *arrowEntity) bool {
	if t == nil || a == nil || a.stuck || !redirectable(a.etype) {
		return false
	}
	a.vx, a.vy, a.vz = lookVector(t.yaw, t.pitch) // AIM_DEFLECT: the attacker's look, unit speed
	a.shooter, a.playerShot, a.mobShot = t.p.eid, true, false
	a.noHitUntil = h.tick.Load() + 5 // it leaves its new owner before it can strike them
	h.playSoundDim(players, t.dim, "minecraft:entity.player.attack.nodamage", sndPlayer, t.x, t.y, t.z, 1, 1)
	return true
}

// blowSourceName names who dealt a mob's last blow for the loot tables'
// source_entity checks: "player" for a player, the species for a mob.
func (h *hub) blowSourceName(players map[int32]*tracked, m *mob) string {
	if m.lastAttacker == 0 {
		return ""
	}
	if players[m.lastAttacker] != nil {
		return "player"
	}
	if k := h.mobs[m.lastAttacker]; k != nil {
		return advEntityName[k.etype]
	}
	return ""
}
