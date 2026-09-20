package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The trial-chamber ominous effects: four harmful effects that do nothing at
// all while they run and everything at the moment you are hurt or killed.
// Ports of WindChargedMobEffect, WeavingMobEffect, OozingMobEffect and
// InfestedMobEffect, whose whole behaviour lives in onMobRemoved/onMobHurt.
//
// Their SOURCE — the ominous bottle and the trial spawner — does not exist
// yet, so today they arrive only through /effect. That is deliberate: the
// mechanics are the part that is hard to get right, and wiring a spawner to
// them later is then a one-line grant.

const (
	effTrialOmen   = 33
	effRaidOmen    = 34
	effWindCharged = 35
	effWeaving     = 36
	effOozing      = 37
	effInfested    = 38

	oozingSlimeSize  = 2   // OozingMobEffect.SLIME_SIZE
	oozingSlimeCount = 2   // the effect's fixed spawnedCount
	oozingCrowdR     = 2.0 // RADIUS_TO_CHECK_SLIMES
	infestedChance   = 0.1 // InfestedMobEffect chanceToSpawn
	weavingCubeR     = 1   // BlockPos.randomInCube radius
	weavingTries     = 15  // how many candidate cells vanilla walks

	maxEntityCramming = 24 // the max_entity_cramming default
)

// ominousOnDeath runs the three death-triggered ominous effects. Called from
// the death branch while the player is still at the spot they died.
func (h *hub) ominousOnDeath(players map[int32]*tracked, t *tracked) {
	if t.hasEffect(effWindCharged) > 0 {
		h.windChargedBurst(players, t)
	}
	if t.hasEffect(effWeaving) > 0 {
		h.weaveCobwebs(players, t)
	}
	if t.hasEffect(effOozing) > 0 {
		h.oozeSlimes(players, t)
	}
}

// windChargedBurst is the gust a wind-charged victim leaves behind: vanilla
// explodes with the wind-charge damage calculator, which deals no damage and
// breaks nothing — it only shoves. Radius 3-5, as vanilla rolls it.
func (h *hub) windChargedBurst(players map[int32]*tracked, t *tracked) {
	h.windChargedBurstAt(players, t.dim, t.x, t.y, t.z, t.p.eid, 0)
}

// windChargedBurstAt is the burst itself, for a player or a mob. skipPlayer
// and skipMob keep the victim out of its own gust.
func (h *hub) windChargedBurstAt(players map[int32]*tracked, dim int, cx, cy, cz float64, skipPlayer, skipMob int32) {
	radius := 3.0 + h.rng.Float64()*2
	y := cy + 0.9 // mid-body, as vanilla uses half the bounding-box height
	h.spawnParticles(players, dim, particlePoof, cx, y, cz, float32(radius/2), 0.1, 40)
	h.playSoundDim(players, dim, "minecraft:entity.breeze_wind_charge.burst", sndNeutral, cx, y, cz, 1, 1)

	now := h.tick.Load()
	for _, o := range players {
		if o.p.eid == skipPlayer || o.dim != dim || o.dead {
			continue
		}
		dx, dy, dz := o.x-cx, o.y-y, o.z-cz
		d := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if d > radius || d < 1e-6 {
			continue
		}
		power := (radius - d) / radius * maceKnockPower
		o.p.trySendEv(attachproto.Velocity{EID: o.p.eid, VX: dx / d * power, VY: 0.4, VZ: dz / d * power})
		o.spinUntil = now + windBurstGrace // let the launch past the speed check
	}
	for _, m := range h.mobs {
		if m.eid == skipMob || m.dying > 0 || m.dim != dim || m.kbScale() <= 0 {
			continue
		}
		dx, dz := m.x-cx, m.z-cz
		d := math.Hypot(dx, dz)
		if d > radius || d < 1e-6 {
			continue
		}
		power := (radius - d) / radius * maceKnockPower * m.kbScale()
		m.vx, m.vz, m.kb, m.reroute = dx/d*power, dz/d*power, 3, 0
		h.mobKnockVelocity(players, m)
	}
}

// weaveCobwebs strings 2-3 cobwebs around where a weaving victim fell. Vanilla
// walks up to 15 random cells in a 1-block cube and takes the ones that are
// replaceable AND sitting on a solid top face, so webs never hang in the air.
func (h *hub) weaveCobwebs(players map[int32]*tracked, t *tracked) {
	h.weaveCobwebsAt(players, t.dim, t.x, t.y, t.z)
}

// weaveCobwebsAt is the placement itself, for a player or a mob.
func (h *hub) weaveCobwebsAt(players map[int32]*tracked, dim int, cx, cy, cz float64) {
	if !h.rules.MobGriefing {
		return // vanilla gates the block placement on mobGriefing for non-players
	}
	want := 2 + h.rng.Intn(2) // randomBetweenInclusive(2, 3)
	web, _ := worldgen.BlockRange("cobweb")
	if web == 0 {
		return
	}
	bx, by, bz := int(math.Floor(cx)), int(math.Floor(cy)), int(math.Floor(cz))
	w := h.worldFor(dim)
	placed := map[blockPos]bool{}
	for i := 0; i < weavingTries && len(placed) < want; i++ {
		p := blockPos{
			bx + h.rng.Intn(2*weavingCubeR+1) - weavingCubeR,
			by + h.rng.Intn(2*weavingCubeR+1) - weavingCubeR,
			bz + h.rng.Intn(2*weavingCubeR+1) - weavingCubeR,
		}
		if placed[p] || !worldgen.IsReplaceable(w.At(p.x, p.y, p.z)) {
			continue
		}
		if !worldgen.IsSolidFull(w.At(p.x, p.y-1, p.z)) {
			continue // nothing sturdy underneath
		}
		placed[p] = true
		h.setBlockAt(players, dim, p, web)
	}
}

// oozeSlimes splits a size-2 slime or two out of an oozing victim. Vanilla
// caps the count by maxEntityCramming minus the slimes already crowding the
// spot, so a stack of oozing deaths cannot run away with itself.
func (h *hub) oozeSlimes(players map[int32]*tracked, t *tracked) {
	h.oozeSlimesAt(players, t.dim, t.x, t.y, t.z)
}

// oozeSlimesAt is the split itself, for a player or a mob.
func (h *hub) oozeSlimesAt(players map[int32]*tracked, dim int, cx, cy, cz float64) {
	near := 0
	for _, m := range h.mobs {
		if m.etype != entitySlime || m.dim != dim {
			continue
		}
		if math.Abs(m.x-cx) <= oozingCrowdR && math.Abs(m.y-cy) <= oozingCrowdR &&
			math.Abs(m.z-cz) <= oozingCrowdR {
			near++
		}
	}
	// Vanilla caps this against the maxEntityCramming game rule; we have no
	// such rule, so its default of 24 stands in — the crowd check is what
	// actually matters, and it is the same check.
	want := oozingSlimeCount
	if room := maxEntityCramming - near; room < want {
		want = room
	}
	for i := 0; i < want; i++ {
		s := h.spawnMobIn(players, entitySlime, dim, cx, cy+0.5, cz)
		if s == nil {
			continue // plugin-cancelled
		}
		s.hostile, s.behavior = true, Behavior(hostileBehavior{})
		s.size = oozingSlimeSize
		s.applyCubeSize()
		h.toTracking(players, s.eid, s.dim, s.x, s.z, metaEv(slimeMeta(s.eid, s.size)))
	}
}

// infestOnHurt is InfestedMobEffect.onMobHurt: a 10% chance per hit taken to
// burst 1-2 silverfish out of the victim. Unlike the other three this fires on
// damage, not death — being infested makes every fight worse, not just the
// last one.
func (h *hub) infestOnHurt(players map[int32]*tracked, t *tracked) {
	if t.hasEffect(effInfested) == 0 || h.rng.Float64() > infestedChance {
		return
	}
	h.infestBurst(players, t.dim, t.x, t.y, t.z)
}

// infestOnMobHurt is the same for a mob: vanilla's onMobHurt is
// LivingEntity-wide, so an infested zombie bursts too.
func (h *hub) infestOnMobHurt(players map[int32]*tracked, m *mob) {
	if _, on := m.effects[effInfested]; !on || h.rng.Float64() > infestedChance {
		return
	}
	h.infestBurst(players, m.dim, m.x, m.y, m.z)
}

// infestBurst spawns the silverfish.
func (h *hub) infestBurst(players map[int32]*tracked, dim int, cx, cy, cz float64) {
	for i, n := 0, 1+h.rng.Intn(2); i < n; i++ {
		s := h.spawnMobIn(players, entitySilverfish, dim, cx, cy+0.9, cz)
		if s == nil {
			continue
		}
		h.applySpecies(players, s)
		h.playSoundDim(players, dim, "minecraft:entity.silverfish.hurt", sndHostile, cx, cy, cz, 1, 1)
	}
}

// ominousOnMobDeath is the LivingEntity half of the three death-triggered
// ominous effects: vanilla runs them for anything that dies carrying one, not
// only for players, which is what makes an ominous trial's mobs leave webs,
// slimes and gusts behind them.
func (h *hub) ominousOnMobDeath(players map[int32]*tracked, m *mob) {
	if len(m.effects) == 0 {
		return
	}
	if _, on := m.effects[effWindCharged]; on {
		h.windChargedBurstAt(players, m.dim, m.x, m.y, m.z, 0, m.eid)
	}
	if _, on := m.effects[effWeaving]; on {
		h.weaveCobwebsAt(players, m.dim, m.x, m.y, m.z)
	}
	if _, on := m.effects[effOozing]; on {
		h.oozeSlimesAt(players, m.dim, m.x, m.y, m.z)
	}
}
