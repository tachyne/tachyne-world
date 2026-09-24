package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Everything that hurts a boat or a minecart goes through one door,
// VehicleEntity.hurtServer: a player's blow, an arrow or any other
// projectile, a blast, lava and fire. Each blow rocks the vehicle and adds
// ten times its worth to the damage that drains away a point a tick; past
// 40 the vehicle breaks. Nothing is turned away by type — a vehicle is not
// fire-immune and has no invulnerability window — except a mob's blast when
// mobGriefing is off (VehicleEntity.ignoreExplosion).

// vehHit is a damage source as a vehicle reads it.
type vehHit struct {
	dmg float64
	dt  dmgType
	// by is the causing player (DamageSource.getEntity), nil when none.
	by *tracked
	// proj is the direct entity when it is a projectile.
	proj *arrowEntity
}

// Vehicle boxes (EntityType sizes): a minecart is 0.98 × 0.7, a boat or raft
// 1.375 × 0.5625.
func (v *vehicle) box() (w, ht float64) {
	if v.isBoat() {
		return 1.375, 0.5625
	}
	return 0.98, 0.7
}

// damageVehicle is VehicleEntity.hurtServer. It reports whether the vehicle
// is still there afterwards.
func (h *hub) damageVehicle(players map[int32]*tracked, v *vehicle, src vehHit) bool {
	if h.vehicles[v.eid] != v {
		return false // already gone (isRemoved)
	}
	if v.etype == entityTntMinecart && src.proj != nil && isAbstractArrow(src.proj.etype) && projectileOnFire(src.proj) {
		// MinecartTNT.hurtServer: a burning arrow sets it off on the spot,
		// the blast growing with the arrow's speed.
		a := src.proj
		h.explodeCart(players, v, a.vx*a.vx+a.vy*a.vy+a.vz*a.vz)
		if h.vehicles[v.eid] != v {
			return false
		}
	}
	v.hurtDirFlip = !v.hurtDirFlip
	v.hurtTime = vehHurtTicks
	v.damage += src.dmg * 10
	var srcEID int32
	if src.by != nil {
		srcEID = src.by.p.eid
	}
	h.vibAt(v.dim, freqEntityDamage, v.x, v.y, v.z, srcEID)
	creative := src.by != nil && src.by.gamemode == gmCreative
	if (creative || v.damage <= vehBreakDamage) && !h.sourceDestroysVehicle(v, src) {
		if creative {
			h.discardVehicle(players, v)
			return false
		}
		h.toTracking(players, v.eid, v.dim, v.x, v.z, metaEv(vehicleHurtMeta(v)))
		return true
	}
	h.destroyVehicle(players, v, src)
	return h.vehicles[v.eid] == v
}

// sourceDestroysVehicle is shouldSourceDestroy: only a TNT cart has one —
// anything that would light it destroys it (and so lights it) whatever the
// blow was worth.
func (h *hub) sourceDestroysVehicle(v *vehicle, src vehHit) bool {
	return v.etype == entityTntMinecart && src.ignitesTNT()
}

// ignitesTNT is MinecartTNT.damageSourceIgnitesTnt: a projectile when it is
// burning, otherwise any fire or explosion damage.
func (src vehHit) ignitesTNT() bool {
	if src.proj != nil {
		return projectileOnFire(src.proj)
	}
	return src.dt.has(tagIsFire) || src.dt.has(tagIsExplosion)
}

// destroyVehicle is VehicleEntity.destroy(level, source). A TNT cart that
// is moving, or destroyed by something that lights it, starts a short
// random fuse instead of dropping (MinecartTNT.destroy).
func (h *hub) destroyVehicle(players map[int32]*tracked, v *vehicle, src vehHit) {
	if v.etype == entityTntMinecart && (src.ignitesTNT() || v.vx*v.vx+v.vz*v.vz >= 0.01) {
		h.lightBrokenCart(players, v)
		return
	}
	h.breakVehicle(players, v)
}

// lightBrokenCart is the lighting half of MinecartTNT.destroy: primeFuse,
// then a fuse of 0-38 ticks in place of the usual 80.
func (h *hub) lightBrokenCart(players map[int32]*tracked, v *vehicle) {
	if v.fuse >= 0 {
		return
	}
	fuse := h.rng.Intn(20) + h.rng.Intn(20)
	h.primeCart(players, v, fuse)
	v.fuse = fuse // set even when tnt_explodes kept the fuse unlit: it then burns out quietly
}

// isAbstractArrow: the projectiles vanilla builds on AbstractArrow.
func isAbstractArrow(etype int) bool {
	return etype == entityArrow || etype == entitySpectralArrow || etype == entityTrident
}

// projectileOnFire is Projectile.isOnFire: a Flame arrow, and the blaze's and
// ghast's fireballs (which burn as they fly).
func projectileOnFire(a *arrowEntity) bool {
	return a.fire || a.etype == entitySmallFireball || a.etype == entityLargeFireball
}

// arrowHitsVehicle is a projectile meeting a boat or a cart
// (ProjectileUtil's entity hit: every vehicle is pickable). It never strikes
// the vehicle its own shooter is riding. The hit is the projectile's
// onHitEntity: an arrow deals its damage, a fireball its own, a snowball or
// an egg nothing (the vehicle still rocks), a burning arrow or a small
// fireball sets a boat alight for five seconds.
func (h *hub) arrowHitsVehicle(players map[int32]*tracked, a *arrowEntity, px, py, pz float64) bool {
	if a.pearl || a.xpBottle || a.breath {
		return false
	}
	for _, v := range h.vehicles {
		if v.dim != a.dim || (a.shooter != 0 && (v.rider == a.shooter || v.mobRider == a.shooter)) {
			continue
		}
		w, ht := v.box()
		r := w/2 + 0.3 // the entity box inflated by 0.3 (ProjectileUtil)
		if math.Abs(px-v.x) > r || math.Abs(pz-v.z) > r || py < v.y-0.3 || py > v.y+ht+0.3 {
			continue
		}
		if a.splash {
			return true // a thrown potion bursts against it and hurts nothing
		}
		dmg := float64(a.dmg)
		if a.etype == entitySnowball || a.etype == entityEggProj || a.knock > 0 {
			dmg = 0 // Snowball/ThrownEgg deal nothing; a wind charge's point is handled below
		}
		if a.knock > 0 {
			dmg = windChargeHitDamage
		}
		if projectileOnFire(a) && (isAbstractArrow(a.etype) || a.etype == entitySmallFireball) {
			v.igniteVehicle(vehArrowBurnTicks)
		}
		var by *tracked
		if s := players[a.shooter]; s != nil {
			by = s
		}
		h.damageVehicle(players, v, vehHit{dmg: dmg, dt: projectileDamageOf(a), by: by, proj: a})
		if a.knock > 0 {
			h.windBurstR(players, a.dim, px, py, pz, a.shooter, windChargeBurstRadius(a))
		}
		return true
	}
	return false
}

const (
	vehArrowBurnTicks = 100 // AbstractArrow/SmallFireball: igniteForSeconds(5)
	vehLavaBurnTicks  = 300 // lavaIgnite: igniteForSeconds(15)
	vehFireBurnTicks  = 160 // BaseFireBlock.fireIgnite: igniteForSeconds(8)
	vehLavaDamage     = 4   // Entity.lavaHurt
)

// igniteVehicle is igniteForTicks. Only a boat keeps a fire going: a
// minecart's tick never runs the base entity tick that burns it.
func (v *vehicle) igniteVehicle(ticks int) {
	if v.isBoat() && v.fireTicks < ticks {
		v.fireTicks = ticks
	}
}

// vehicleHazards is what the blocks a vehicle sits in do to it each tick,
// and a boat's afterburn. Lava hurts 4 twice a tick (a boat applies its
// block effects twice; a minecart once, and then its own tick hurts it in
// lava again), which breaks either within two ticks; fire hurts 1 (soul fire
// 2), twice a tick for a boat. A burning boat takes a point every second out
// of lava and water puts it out. It reports whether the vehicle survived.
func (h *hub) vehicleHazards(players map[int32]*tracked, v *vehicle) bool {
	w := h.worldFor(v.dim)
	if w == nil {
		return true
	}
	bw, ht := v.box()
	const eps = 1e-5
	inLava, inWater := false, false
	fireDmg := 0.0
	for x := floorInt(v.x - bw/2 + eps); x <= floorInt(v.x+bw/2-eps); x++ {
		for y := floorInt(v.y + eps); y <= floorInt(v.y+ht-eps); y++ {
			for z := floorInt(v.z - bw/2 + eps); z <= floorInt(v.z+bw/2-eps); z++ {
				st := w.At(x, y, z)
				switch {
				case worldgen.IsLava(st):
					inLava = true
				case worldgen.IsWater(st):
					inWater = true
				case st == soulFire:
					fireDmg = max(fireDmg, 2)
				case isFire(st):
					fireDmg = max(fireDmg, 1)
				}
			}
		}
	}
	applications := 2 // a boat runs its block effects twice a tick
	if !v.isBoat() {
		applications = 1
	}
	for i := 0; i < applications; i++ {
		if inLava {
			v.igniteVehicle(vehLavaBurnTicks)
			if !h.damageVehicle(players, v, vehHit{dmg: vehLavaDamage, dt: dtLava}) {
				return false
			}
		}
		if fireDmg > 0 {
			v.igniteVehicle(vehFireBurnTicks)
			if !h.damageVehicle(players, v, vehHit{dmg: fireDmg, dt: dtInFire}) {
				return false
			}
		}
		if inWater {
			v.fireTicks = min(v.fireTicks, 0) // WaterFluid: EXTINGUISH
		}
	}
	if inLava && !v.isBoat() { // AbstractMinecart.tick: isInLava → lavaHurt
		if !h.damageVehicle(players, v, vehHit{dmg: vehLavaDamage, dt: dtLava}) {
			return false
		}
	}
	if v.isBoat() && v.fireTicks > 0 { // Entity.baseTick's afterburn
		if v.fireTicks%20 == 0 && !inLava {
			if !h.damageVehicle(players, v, vehHit{dmg: 1, dt: dtOnFire}) {
				return false
			}
		}
		v.fireTicks--
	}
	if burning := v.fireTicks > 0; burning != v.burning {
		v.burning = burning
		h.toTracking(players, v.eid, v.dim, v.x, v.z, metaEv(fireMetadata(v.eid, burning)))
	}
	return true
}

// explosionHurtsVehicles is ServerExplosion.hurtEntities for the vehicles in
// reach: the blast damage (which is never below 1, so anything inside the
// radius at least rocks) and, for a cart, the shove from its eyes.
func (h *hub) explosionHurtsVehicles(players map[int32]*tracked, dim int, cx, cy, cz, power float64, dt dmgType) {
	if h.blastSrc.causerMob && !h.rules.MobGriefing {
		return // VehicleEntity.ignoreExplosion: a mob's blast with griefing off
	}
	var by *tracked
	if !h.blastSrc.causerMob {
		by = players[h.blastSrc.causer]
	}
	for _, v := range h.vehicles {
		if v.dim != dim || dist3(v.x, v.y, v.z, cx, cy, cz) > power*2 {
			continue
		}
		bw, ht := v.box()
		exposure := h.seenPercent(dim, cx, cy, cz, v.x-bw/2, v.y, v.z-bw/2, v.x+bw/2, v.y+ht, v.z+bw/2)
		impact := explosionImpact(power, cx, cy, cz, v.x, v.y, v.z, exposure)
		if !h.damageVehicle(players, v, vehHit{dmg: explosionDamage(power, impact), dt: dt, by: by}) || v.isBoat() {
			continue
		}
		ex, ey, ez := v.x-cx, v.y+ht*0.85-cy, v.z-cz
		if n := math.Sqrt(ex*ex + ey*ey + ez*ez); impact > 0 && n > 1e-9 {
			v.vx, v.vy, v.vz = v.vx+ex/n*impact, v.vy+ey/n*impact, v.vz+ez/n*impact
		}
	}
}
