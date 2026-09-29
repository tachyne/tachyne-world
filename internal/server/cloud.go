package server

import (
	"encoding/binary"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Area-effect clouds — vanilla's AreaEffectCloud, a real entity: a flat disc
// half a block tall that the clients are told about (the tracker spawns it)
// and draw themselves from its synced radius, waiting flag and particle. The
// lingering potion, the charged-with-effects creeper and the dragon (its
// fireball and its sitting flame) leave one.
//
// Its server tick is vanilla's: nothing happens while it waits out its
// wait time, then its radius changes by radiusPerTick every tick, and every
// fifth tick each living thing in its box within the radius that it has not
// dosed within reapplicationDelay ticks takes its effects — instant ones at
// half strength, timed ones scaled by potionDurationScale — and the radius
// and duration change by radiusOnUse and durationOnUse for each one dosed.
// It goes when its duration (counted after the wait) runs out or its
// radius falls under half a block.

var entityAreaEffectCloud = entityID("area_effect_cloud")

const (
	cloudApplyEvery    = 5    // TIME_BETWEEN_APPLICATIONS
	cloudMaxRadius     = 32.0 // setRadius clamps to 0..32
	cloudMinRadius     = 0.5  // under this a shrinking cloud is gone
	cloudHeight        = 0.5  // EntityDimensions.scalable(radius × 2, 0.5)
	cloudWaitDefault   = 20   // DEFAULT_WAIT_TIME
	cloudReapply       = 20   // DEFAULT_REAPPLICATION_DELAY
	cloudInstantScale  = 0.5  // applyInstantaneousEffect(…, 0.5)
	cloudInfinite      = -1   // INFINITE_DURATION
	cloudLingerRadius  = 3.0  // ThrownLingeringPotion.onHitAsPotion
	cloudLingerTicks   = 600  // …its duration
	cloudLingerWait    = 10   // …and wait time
	cloudLingerOnUse   = -0.5 // …and radiusOnUse
	cloudCreeperRadius = 2.5  // Creeper.spawnLingeringCloud
	cloudCreeperTicks  = 300

	// The dragon's two clouds: the sitting flame and the fireball's burst.
	breathRadius       = 5.0 // DragonSittingFlamingPhase: radius 5, 200 ticks, Harming I
	breathTicks        = 200
	fireballRadius     = 3.0 // DragonFireball.onHit: radius 3 growing to 7 over 600 ticks, Harming II
	fireballRadiusTo   = 7.0
	fireballCloudTicks = 600

	// AreaEffectCloud's synced fields after Entity's eight — the same on 26.2
	// and 26.3.
	metaIndexCloudRadius   = 8  // DATA_RADIUS: FLOAT
	metaIndexCloudWaiting  = 9  // DATA_WAITING: BOOLEAN
	metaIndexCloudParticle = 10 // DATA_PARTICLE: PARTICLE
	metaTypeParticle       = 17 // EntityDataSerializers.PARTICLE (canonical 770; 16 from 1.21.6)

	// cloudParticleSynced gates DATA_PARTICLE. The gateways' metadata
	// translation knows the PARTICLES list (effect swirls) but not the single
	// PARTICLE serializer, and a type it does not know passes the whole frame
	// through untranslated — serializer 17 is PARTICLES on 26.x, which would
	// disconnect the client. Until it does, the clients draw the default
	// white ENTITY_EFFECT and the radius and waiting fields go out alone.
	cloudParticleSynced = false

	particleDragonBreath = 7 // canonical 770 id (PowerParticleOption on 26.x: the gateway adds the power)
)

// effectCloud is one area-effect cloud.
type effectCloud struct {
	eid           int32
	uuid          [16]byte
	dim           int
	x, y, z       float64
	age           int // tickCount
	duration      int // cloudInfinite, or ticks after the wait
	waitTime      int
	reapplyDelay  int
	durationOnUse int
	radius        float32
	radiusOnUse   float32
	radiusPerTick float32
	effects       []potEffect // PotionContents' effects, unscaled
	durScale      float32     // potionDurationScale
	color         int32       // the potion's colour, for the ENTITY_EFFECT particle
	breath        bool        // the DRAGON_BREATH particle rather than the potion's
	owner         int32       // who left it (setOwner), or 0
	ownerDragon   bool        // …and whether that is the dragon: a bottle fills only from its clouds
	victims       map[int32]int
	waiting       bool    // DATA_WAITING as the clients have it
	sentRadius    float32 // DATA_RADIUS as the clients have it
}

// addCloud gives a cloud its id and puts it in the world; the tracker spawns
// it for the players in range.
func (h *hub) addCloud(c *effectCloud) *effectCloud {
	c.eid = h.allocEID()
	binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
	c.radius = float32(math.Min(cloudMaxRadius, math.Max(0, float64(c.radius))))
	c.sentRadius = c.radius
	if c.victims == nil {
		c.victims = map[int32]int{}
	}
	h.clouds[c.eid] = c
	return c
}

// removeCloud is discard: the cloud leaves the world and every client.
func (h *hub) removeCloud(players map[int32]*tracked, c *effectCloud) {
	if h.clouds[c.eid] != c {
		return
	}
	delete(h.clouds, c.eid)
	h.entityGone(players, c.dim, c.eid)
}

// setCloudRadius is setRadius: clamped to 0..32, and the clients told.
func (h *hub) setCloudRadius(players map[int32]*tracked, c *effectCloud, r float32) {
	c.radius = float32(math.Min(cloudMaxRadius, math.Max(0, float64(r))))
	if c.radius != c.sentRadius {
		c.sentRadius = c.radius
		h.toTracking(players, c.eid, c.dim, c.x, c.z, metaEv(cloudRadiusMeta(c.eid, c.radius)))
	}
}

func cloudRadiusMeta(eid int32, r float32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCloudRadius)
	b = protocol.AppendVarInt(b, metaTypeFloat)
	b = protocol.AppendF32(b, r)
	return protocol.AppendU8(b, itemMetaEnd)
}

// cloudParticleMeta is DATA_PARTICLE: the dragon's breath at power 1, or the
// ENTITY_EFFECT particle in the potion's opaque colour (updateParticle).
func cloudParticleMeta(c *effectCloud) []byte {
	b := protocol.AppendVarInt(nil, c.eid)
	b = protocol.AppendU8(b, metaIndexCloudParticle)
	b = protocol.AppendVarInt(b, metaTypeParticle)
	if c.breath {
		b = protocol.AppendVarInt(b, particleDragonBreath)
	} else {
		b = protocol.AppendVarInt(b, particleEntityEffect)
		b = protocol.AppendI32(b, c.color|-0x1000000) // ARGB.opaque
	}
	return protocol.AppendU8(b, itemMetaEnd)
}

// showCloudTo spawns a cloud for one viewer with its synced state.
func (h *hub) showCloudTo(t *tracked, c *effectCloud) {
	t.p.trySendEv(entAdd(c.eid, entityAreaEffectCloud, c.uuid, c.x, c.y, c.z, 0, 0))
	t.p.trySendEv(metaEv(cloudRadiusMeta(c.eid, c.radius)))
	if c.waiting {
		t.p.trySendEv(metaEv(boolMeta(c.eid, metaIndexCloudWaiting, true)))
	}
	if cloudParticleSynced {
		t.p.trySendEv(metaEv(cloudParticleMeta(c)))
	}
}

// spawnPotionCloud is ThrownLingeringPotion.onHitAsPotion: radius 3 losing
// half a block per creature dosed and shrinking to nothing over its 600
// ticks, after a ten-tick wait; the potion's effects at a quarter of their
// duration (the lingering bottle's potion_duration_scale).
func (h *hub) spawnPotionCloud(dim int, x, y, z float64, kind int8) *effectCloud {
	effs := potionEffects(kind)
	if len(effs) == 0 {
		return nil // water/awkward: nothing to linger
	}
	return h.addCloud(&effectCloud{dim: dim, x: x, y: y, z: z,
		radius: cloudLingerRadius, radiusOnUse: cloudLingerOnUse, duration: cloudLingerTicks,
		waitTime: cloudLingerWait, reapplyDelay: cloudReapply,
		radiusPerTick: -cloudLingerRadius / cloudLingerTicks,
		effects:       effs, durScale: lingerFactor, color: potionColor(kind)})
}

// spawnCreeperCloud is Creeper.spawnLingeringCloud: a creeper carrying
// effects leaves them behind when it blows — radius 2.5, 300 ticks, the
// same shrink and quarter durations as a lingering potion.
func (h *hub) spawnCreeperCloud(m *mob) {
	if len(m.effects) == 0 {
		return
	}
	var effs []potEffect
	for id, e := range m.effects {
		effs = append(effs, potEffect{id: id, amp: e.amp, ticks: e.left})
	}
	h.addCloud(&effectCloud{dim: m.dim, x: m.x, y: m.y, z: m.z,
		radius: cloudCreeperRadius, radiusOnUse: cloudLingerOnUse, duration: cloudCreeperTicks,
		waitTime: cloudLingerWait, reapplyDelay: cloudReapply,
		radiusPerTick: -cloudCreeperRadius / cloudCreeperTicks,
		effects:       effs, durScale: lingerFactor, color: effectsColor(effs)})
}

// spawnBreathCloud is the dragon's sitting flame (DragonSittingFlamingPhase):
// radius 5 for 200 ticks, never shrinking, carrying Harming I.
func (h *hub) spawnBreathCloud(dim int, x, y, z float64) int32 {
	c := h.addCloud(&effectCloud{dim: dim, x: x, y: y, z: z,
		radius: breathRadius, duration: breathTicks, waitTime: cloudWaitDefault,
		reapplyDelay: cloudReapply, durScale: lingerFactor, breath: true, ownerDragon: true,
		effects: []potEffect{{id: effInstantDamage, amp: 0, ticks: 1}}})
	if h.dragon != nil {
		c.owner = h.dragon.eid
	}
	return c.eid
}

// spawnFireballCloud is DragonFireball.onHit's cloud: radius 3 growing to 7
// over its 600 ticks, carrying Harming II, and laid under the first living
// thing within four blocks of the burst (the burst's box grown 4, 2, 4)
// rather than at the burst itself.
func (h *hub) spawnFireballCloud(players map[int32]*tracked, a *arrowEntity) {
	x, y, z := a.x, a.y, a.z
	near := func(ex, ey, ez, hw, ht float64) bool {
		if ex+hw < a.x-4.5 || ex-hw > a.x+4.5 || ez+hw < a.z-4.5 || ez-hw > a.z+4.5 ||
			ey+ht < a.y-2 || ey > a.y+1+2 {
			return false
		}
		dx, dy, dz := ex-a.x, ey-a.y, ez-a.z
		return dx*dx+dy*dy+dz*dz < 16
	}
	placed := false
	for _, t := range players {
		if t.dim == a.dim && !t.dead && t.gamemode != gmSpectator && near(t.x, t.y, t.z, 0.3, 1.8) {
			x, y, z, placed = t.x, t.y, t.z, true
			break
		}
	}
	if !placed {
		for _, m := range h.mobs {
			if m.dim != a.dim || m.dying > 0 || m.eid == a.shooter {
				continue
			}
			if b := m.box(); near(m.x, m.y, m.z, b.w/2, b.h) {
				x, y, z = m.x, m.y, m.z
				break
			}
		}
	}
	h.addCloud(&effectCloud{dim: a.dim, x: x, y: y, z: z,
		radius: fireballRadius, duration: fireballCloudTicks, waitTime: cloudWaitDefault,
		radiusPerTick: (fireballRadiusTo - fireballRadius) / fireballCloudTicks,
		reapplyDelay:  cloudReapply, durScale: lingerFactor, breath: true,
		owner: a.shooter, ownerDragon: true,
		effects: []potEffect{{id: effInstantDamage, amp: 1, ticks: 1}}})
}

// updateClouds is AreaEffectCloud.serverTick for every cloud.
func (h *hub) updateClouds(players map[int32]*tracked) {
	for _, c := range h.clouds {
		c.age++ // tickCount
		if c.duration != cloudInfinite && c.age-c.waitTime >= c.duration {
			h.removeCloud(players, c)
			continue
		}
		wait := c.age < c.waitTime
		if wait != c.waiting {
			c.waiting = wait
			h.toTracking(players, c.eid, c.dim, c.x, c.z, metaEv(boolMeta(c.eid, metaIndexCloudWaiting, wait)))
		}
		if wait {
			continue
		}
		radius := c.radius
		if c.radiusPerTick != 0 {
			if radius += c.radiusPerTick; radius < cloudMinRadius {
				h.removeCloud(players, c)
				continue
			}
			h.setCloudRadius(players, c, radius)
			radius = c.radius
		}
		if c.age%cloudApplyEvery != 0 {
			continue
		}
		for eid, until := range c.victims {
			if c.age >= until {
				delete(c.victims, eid)
			}
		}
		if len(c.effects) == 0 {
			clear(c.victims)
			continue
		}
		h.doseCloud(players, c, radius)
	}
}

// doseCloud is the application half of serverTick: every living thing whose
// box meets the cloud's, within the radius of its centre on the level, and
// not dosed lately.
func (h *hub) doseCloud(players map[int32]*tracked, c *effectCloud, radius float32) {
	box := float64(radius) // the box is taken once; the reach test reads the radius as it changes
	inside := func(ex, ey, ez, hw, ht float64) bool {
		r := float64(radius)
		if ex+hw < c.x-box || ex-hw > c.x+box || ez+hw < c.z-box || ez-hw > c.z+box ||
			ey+ht <= c.y || ey >= c.y+cloudHeight {
			return false
		}
		dx, dz := ex-c.x, ez-c.z
		return dx*dx+dz*dz <= r*r
	}
	cause := deathCause{}
	if o := players[c.owner]; o != nil {
		cause.by, cause.byEID = o.p.name, o.p.eid
	} else if m := h.mobs[c.owner]; m != nil {
		cause.by = mobDisplayName(m.etype)
	}
	// dosed is the bookkeeping for one victim; it reports whether the cloud
	// is used up.
	dosed := func(eid int32) bool {
		c.victims[eid] = c.age + c.reapplyDelay
		if c.radiusOnUse != 0 {
			radius += c.radiusOnUse
			if radius < cloudMinRadius {
				h.removeCloud(players, c)
				return true
			}
			h.setCloudRadius(players, c, radius)
		}
		if c.durationOnUse != 0 && c.duration != cloudInfinite {
			if c.duration += c.durationOnUse; c.duration <= 0 {
				h.removeCloud(players, c)
				return true
			}
		}
		return false
	}
	for _, t := range players {
		// A spectator is never among the entities it finds.
		if t.dim != c.dim || t.gamemode == gmSpectator || t.dead || t.health <= 0 {
			continue
		}
		if _, seen := c.victims[t.p.eid]; seen || !inside(t.x, t.y, t.z, 0.3*t.scale(), 1.8*t.scale()) {
			continue
		}
		h.cloudDosePlayer(players, t, c, cause)
		if dosed(t.p.eid) {
			return
		}
	}
	for _, m := range h.mobs {
		if m.dim != c.dim || m.dying > 0 || m.health <= 0 {
			continue
		}
		if _, seen := c.victims[m.eid]; seen {
			continue
		}
		if b := m.box(); !inside(m.x, m.y, m.z, b.w/2, b.h) || !cloudAffects(m, c.effects) {
			continue
		}
		h.applyPotionAoEMobDur(players, m, c.effects, cloudInstantScale, cloudDur(c.durScale))
		if dosed(m.eid) {
			return
		}
	}
}

// cloudAffects is the canBeAffected half of the victim test: at least one
// of the cloud's effects can take on this mob (an undead shrugs off Poison
// and Regeneration).
func cloudAffects(m *mob, effs []potEffect) bool {
	for _, e := range effs {
		if !ignoresPoisonAndRegen(m.etype) || (e.id != effPoison && e.id != effRegen) {
			return true
		}
	}
	return false
}

// cloudDur is withScaledDuration: floor(duration × scale), never under a
// tick; an infinite effect stays infinite.
func cloudDur(scale float32) func(int) int {
	return func(ticks int) int {
		if ticks == effInfinite || ticks == 0 {
			return ticks
		}
		return max(int(math.Floor(float64(ticks)*float64(scale))), 1)
	}
}

// cloudDosePlayer applies a cloud's effects to one player: instant ones at
// half strength (Harming as magic from the cloud, credited to its owner),
// timed ones for their scaled duration.
func (h *hub) cloudDosePlayer(players map[int32]*tracked, t *tracked, c *effectCloud, cause deathCause) {
	dur := cloudDur(c.durScale)
	for _, e := range c.effects {
		switch e.id {
		case effInstantHealth:
			heal := float32(int(cloudInstantScale * float64(int(4)<<e.amp)))
			t.health = float32(math.Min(float64(t.maxHP()), float64(t.health+heal)))
			h.sendHealth(t)
		case effInstantDamage:
			if dmg := int(cloudInstantScale * float64(int(6)<<e.amp)); dmg > 0 {
				h.hurtBy(players, t, float32(dmg), dtIndirectMagic, cause)
			}
		default:
			if ticks := dur(e.ticks); ticks >= 1 || ticks == effInfinite {
				h.applyEffectTicks(players, t, e.id, e.amp, ticks)
			}
		}
	}
}
