package server

import (
	"encoding/binary"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Arrow projectiles — fired by skeletons (and later, players with bows). The
// hub owns them like mobs/items: spawned with an initial velocity, integrated
// each tick under gravity + drag, stuck on block contact, and despawned after
// a short lifetime. Hits flow through the normal player-damage path (armor,
// knockback, hurt flash).

const (
	arrowSpeed     = 1.6  // launch speed, blocks/tick (vanilla skeleton bow)
	arrowGravity   = 0.05 // blocks/tick² (vanilla projectile gravity)
	arrowDrag      = 0.99 // per-tick air drag
	arrowDamage    = 3    // hearts×2 before armor (vanilla skeleton ~1-4)
	arrowLifeTicks = 200  // flying or stuck, gone after 10 s (transient litter)
	arrowHitRadius = 0.5  // horizontal hit cylinder (player 0.3 + arrow slack)

	// Shulker bullet: a slow homing projectile that curves toward its victim
	// (vanilla ShulkerBullet steers its motion each tick) rather than flying a
	// fixed arc, and gives Levitation on a hit.
	shulkerBulletSpeed = 0.4 // target flight speed, blocks/tick
	shulkerBulletSteer = 0.2 // how hard it turns toward the target each tick

	parchedWeaknessSecs = 30 // Parched arrows: WEAKNESS 600 ticks (vanilla behavior)
	boggedPoisonSecs    = 5  // Bogged.getArrow: POISON 100 ticks
)

var (
	entityArrow         = entityID("arrow")          // minecraft:entity_type "arrow" (1.21.5)
	entitySpectralArrow = entityID("spectral_arrow") // …and the glowing one
	entityXPBottle      = entityID("experience_bottle")
)

type arrowEntity struct {
	eid        int32
	etype      int // projectile species — decides its damage type (projectileDamage)
	uuid       [16]byte
	x, y, z    float64
	vx, vy, vz float64
	dim        int  // dimension the projectile flies in
	stuck      bool // hit a block — hold position until despawn
	born       uint64
	sx, sy, sz float64 // last broadcast position (relative-move baseline)

	shooter    int32    // eid of who fired it (players skip their own fresh shots)
	ox, oz     float64  // launch point (target-block distance, advancements)
	weapon     int32    // the bow/crossbow that loosed it (0 = thrown/mob) — killed_by_arrow
	victims    []string // entity types this projectile has killed (piercing) — killed_by_arrow
	dmg        int      // damage on a hit (charge-scaled for player bows)
	noHitUntil uint64   // tick before which the shooter can't hit themselves
	playerShot bool     // player-fired: hits mobs, and is retrievable once stuck
	breaks     bool     // snowball/egg: shatters on impact instead of sticking
	mobShot    bool     // shot by a mob at mobs (a snow golem's snowball): may hit mobs other than its shooter
	breath     bool     // dragon fireball: bursts into a breath cloud where it lands
	dangerous  bool     // wither skull: the blue one — slower, and it chews through what the black one cannot
	egg        bool     // an egg: 1-in-8 chance to hatch a chick where it lands
	xpBottle   bool     // a bottle o' enchanting: shatters into experience orbs
	pearl      bool     // an ender pearl: teleports its thrower where it lands
	poison     int      // seconds of poison on a hit: a witch's splash, a bogged's arrow
	splash     bool     // a thrown potion: shatters on any impact into an AoE (see splashPotion)
	tipped     bool     // a tipped arrow: applies its potion's effects to the player it hits
	glow       int      // a spectral arrow: seconds of Glowing on what it hits
	potion     int8     // the potion kind a splash/lingering/tipped projectile carries
	lingering  bool     // a lingering potion: leaves an effect cloud instead of an instant splash
	fire       bool     // blaze fireball: sets its target burning
	wither     int      // wither skull: seconds of wither effect on a hit
	weaken     int      // parched arrow: seconds of weakness effect on a hit
	slow       int      // stray arrow: seconds of slowness effect on a hit

	homing   int32   // shulker bullet: eid of the target it curves toward (0 = straight)
	levitate int     // shulker bullet: seconds of Levitation applied on a hit
	explode  int     // ghast/wither fireball: explosion power on impact (0 = none)
	knock    float64 // wind charge: pure knockback impulse, no damage
	punch    int     // bow Punch enchant: +0.6/level extra hit knockback

	pierce   int            // crossbow piercing: remaining pass-throughs (0 = stop on first mob)
	hitMobs  map[int32]bool // mobs already struck (piercing: never the same one twice; nil when not piercing)
	noPickup bool           // multishot side bolts / creative tridents: fly + hit, never retrievable

	loyalty     int      // thrown-trident loyalty: >0 flies back to the thrower instead of sticking
	impaling    int      // thrown-trident impaling: bonus damage to #sensitive_to_impaling mobs
	channeling  bool     // thrown-trident channeling: a storm bolt on whatever it hits under open sky
	returning   bool     // a loyal trident on its way home (no collisions, steers to the owner)
	pickupStack invStack // the exact stack a retrieved/returned projectile restores (0 item = plain arrow)
}

// spawnArrow fires an arrow exactly like vanilla's performRangedAttack
// (vanilla 1.21.5): aim at 1/3 of the target's height with the gravity lob
// folded into the direction (dy += horizontalDist × 0.2), then shoot at 1.6
// with difficulty-scaled spread — normalize + triangle(0, 0.0172275 × inacc)
// noise per axis, inacc = 14 − 4×difficulty (easy 10, normal 6, hard 2).
// Vanilla skeletons MISS; aimbot skeletons were harder than vanilla hard.
func (h *hub) spawnArrow(players map[int32]*tracked, m *mob, t *tracked) {
	h.spawnArrowAt(players, m, t.x, t.y+0.6, t.z) // a player's bbox is 1.8 → getY(1/3) = y+0.6
}

// spawnArrowAt looses a mob's arrow at a point (the target's getY(1/3)). It is
// a mob's shot, so it can strike other mobs: a skeleton's arrow kills the
// creeper that drops a music disc, and a stray one starts a fight.
func (h *hub) spawnArrowAt(players map[int32]*tracked, m *mob, tx, ty, tz float64) {
	ox, oy, oz := m.x, m.y+1.4, m.z
	dx, dy, dz := tx-ox, ty-oy, tz-oz
	dy += math.Hypot(dx, dz) * 0.2
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	dev := 0.0172275 * float64(14-4*h.rules.Difficulty)
	tri := func() float64 { return dev * (h.rng.Float64() - h.rng.Float64()) }
	vx := (dx/d + tri()) * arrowSpeed
	vy := (dy/d + tri()) * arrowSpeed
	vz := (dz/d + tri()) * arrowSpeed
	a := h.launchProjectileIn(players, entityArrow, m.dim, ox, oy, oz, vx, vy, vz)
	a.shooter, a.dmg, a.mobShot = m.eid, arrowDamage, true
	// ProjectileUtil.getMobArrow carries the bow's enchantments: Power adds
	// 0.5·lvl+0.5 to the base damage (before ×speed), Punch its knockback,
	// Flame sets the arrow alight.
	bow := m.heldStack()
	if pl := bow.enchLvl(enchPower); pl > 0 {
		a.dmg += int(math.Ceil((0.5*float64(pl) + 0.5) * arrowSpeed))
	}
	a.punch = bow.enchLvl(enchPunch)
	if bow.enchLvl(enchFlame) > 0 {
		a.fire = true
	}
	if m.etype == entityParched {
		a.weaken = parchedWeaknessSecs // vanilla Parched: WEAKNESS, 600 ticks
	}
	if m.etype == entityStray {
		a.slow = 30 // vanilla Stray: an arrow of SLOWNESS, 600 ticks
	}
	if m.etype == entityBogged {
		a.poison = boggedPoisonSecs // Bogged.getArrow: POISON, 100 ticks
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid)) // the draw is visible
}

// launchProjectileIn launches into an explicit dimension.
func (h *hub) launchProjectileIn(players map[int32]*tracked, etype, dim int, x, y, z, vx, vy, vz float64) *arrowEntity {
	eid := h.allocEID()
	a := &arrowEntity{eid: eid, etype: etype, dim: dim, x: x, y: y, z: z, vx: vx, vy: vy, vz: vz,
		born: h.tick.Load(), sx: x, sy: y, sz: z, ox: x, oz: z}
	binary.BigEndian.PutUint32(a.uuid[12:], uint32(eid))
	h.vibAt(dim, freqProjectileShoot, x, y, z, 0)
	if etype == entityWindCharge { // a gust, not a dart: shoves, bursts on contact, never sticks
		a.knock, a.breaks = 1.5, true
	}
	h.arrows[eid] = a
	add := entAdd(eid, etype, a.uuid, x, y, z, arrowYaw(a), arrowPitch(a))
	add.VX, add.VY, add.VZ = vx, vy, vz // the launch arc, ahead of the first move
	h.toNearbyEv(players, dim, x, z, add)
	return a
}

// hurtingSpeed is AbstractHurtingProjectile's launch speed: the direction
// scaled by its acceleration power, from which it works up to 1.9 a tick.
const hurtingSpeed = 0.1

// hurtingMotion is AbstractHurtingProjectile.applyInertia's constants for
// the self-propelled projectiles (fireballs, wither skulls, dragon
// fireballs): the push along the flight each tick and the inertia it is
// then scaled by, 0.8 in water. A wind charge coasts (no push, inertia 1).
func hurtingMotion(a *arrowEntity, water bool) (accel, inertia float64, ok bool) {
	switch a.etype {
	case entityLargeFireball, entitySmallFireball, entityDragonFireball, entityWitherSkull:
		if water {
			return hurtingSpeed, 0.8, true
		}
		if a.dangerous {
			return hurtingSpeed, 0.73, true // WitherSkull.getInertia: a blue skull drags
		}
		return hurtingSpeed, 0.95, true
	case entityWindCharge:
		return 0, 1, true
	}
	return 0, 0, false
}

// retrieveProjectile restores a stuck projectile to a survival player's
// inventory: a thrown trident hands back its exact stack (enchantments intact),
// everything else hands back a plain arrow.
func (h *hub) retrieveProjectile(t *tracked, a *arrowEntity) (changed []int, left int) {
	if a.pickupStack.item != 0 {
		return t.inv.addStack(a.pickupStack)
	}
	return t.inv.add(itemArrowAmmo, 1)
}

// updateArrows integrates every arrow one tick: move, collide, hit, expire.
// Runs every tick (arrows are fast; a 2-tick cadence would tunnel walls).
func (h *hub) updateArrows(players map[int32]*tracked) {
	now := h.tick.Load()
	for eid, a := range h.arrows {
		if a.returning { // a loyal trident flying home — no collisions, steers to its owner
			if h.updateReturningTrident(players, a) {
				delete(h.arrows, eid)
				h.entityGone(players, a.dim, eid)
			}
			continue
		}
		if now-a.born >= arrowLifeTicks {
			delete(h.arrows, eid)
			h.entityGone(players, a.dim, eid)
			continue
		}
		if a.stuck {
			if a.playerShot && !a.noPickup { // stuck player projectiles are retrievable
				for _, t := range players {
					if t.gamemode != gmSurvival || t.dead || t.inv == nil {
						continue
					}
					if math.Abs(a.x-t.x) > 1 || math.Abs(a.z-t.z) > 1 || math.Abs(a.y-t.y) > 1.5 {
						continue
					}
					changed, left := h.retrieveProjectile(t, a)
					if left == 0 {
						for _, sl := range changed {
							h.sendSlot(t, sl)
						}
						delete(h.arrows, eid)
						h.entityGone(players, a.dim, eid)
						h.playSoundDim(players, a.dim, "minecraft:entity.item.pickup", sndPlayer, a.x, a.y, a.z, 0.4, 1.5)
						break
					}
				}
			}
			continue
		}
		// Shulker bullet: curve toward its live target each tick (it homes on its
		// victim rather than flying a fixed arc). If the target is gone it keeps
		// its heading; either way a homing bullet never falls.
		if a.homing != 0 {
			if tgt := players[a.homing]; tgt != nil && !tgt.dead && tgt.dim == a.dim {
				dx, dy, dz := tgt.x-a.x, (tgt.y+1)-a.y, tgt.z-a.z
				if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d > 1e-6 {
					a.vx += (dx/d*shulkerBulletSpeed - a.vx) * shulkerBulletSteer
					a.vy += (dy/d*shulkerBulletSpeed - a.vy) * shulkerBulletSteer
					a.vz += (dz/d*shulkerBulletSpeed - a.vz) * shulkerBulletSteer
				}
			}
		}

		// Sample the step every half block along the tick's move (at least
		// its midpoint and endpoint): a single endpoint test at 1.6
		// blocks/tick can pass clean through a one-block wall, and a bow's
		// 3-a-tick arrow through a player. Samples measure from where the
		// tick began, so the move is exactly one velocity a tick.
		hit := false
		bx, by, bz := a.x, a.y, a.z
		n := 2
		if sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz); sp > 1 {
			n = int(math.Ceil(sp / 0.5))
		}
		for i := 1; i <= n; i++ {
			f := float64(i) / float64(n)
			px, py, pz := bx+a.vx*f, by+a.vy*f, bz+a.vz*f
			if h.arrowHitsPlayer(players, a, px, py, pz) ||
				((a.playerShot || a.mobShot) && h.arrowHitsMob(players, a, px, py, pz)) {
				hit = true
				break
			}
			if worldgen.Collides(h.worldFor(a.dim).At(int(math.Floor(px)), int(math.Floor(py)), int(math.Floor(pz)))) {
				h.vibAt(a.dim, freqProjectileLand, px, py, pz, a.shooter)
				if a.breaks { // snowballs/eggs shatter
					hit = true
					if a.knock > 0 { // a wind charge bursts a quarter block off the face it struck
						h.windBurstR(players, a.dim, a.x-a.vx*0.25, a.y-a.vy*0.25, a.z-a.vz*0.25, a.shooter, windChargeBurstRadius(a))
						break
					}
					h.spawnParticles(players, a.dim, particlePoof, a.x, a.y, a.z, 0.1, 0.05, 6)
					if a.pearl {
						h.pearlLand(players, a)
					}
					if a.egg && h.rng.Intn(8) == 0 { // the classic egg-machine gamble
						chick := h.spawnAnimal(players, entityChicken, int(a.x), int(a.z))
						if chick != nil {
							chick.baby, chick.growLeft = true, growUpTicks
							h.toTracking(players, chick.eid, 0, chick.x, chick.z, metaEv(babyMeta(chick.eid, true)))
						}
					}
					break
				}
				bp := blockPos{int(math.Floor(px)), int(math.Floor(py)), int(math.Floor(pz))}
				h.projectileHitBlock(players, a, bp, h.worldFor(a.dim).At(bp.x, bp.y, bp.z))
				if a.loyalty > 0 { // a loyal trident bounces off the wall and flies home
					a.returning = true
				} else {
					a.stuck = true // freeze just short of the face it struck
				}
				h.playSoundDim(players, a.dim, "minecraft:entity.arrow.hit", sndNeutral, a.x, a.y, a.z, 1, 1.2)
				break
			}
			a.x, a.y, a.z = px, py, pz
		}
		if hit {
			if a.xpBottle { // a bottle o' enchanting pays out where it broke
				h.breakXPBottle(players, a)
			}
			if a.breath { // a dragon fireball bursts into its breath
				h.spawnBreathCloud(a.dim, a.x, a.y, a.z)
			}
			if a.splash { // a thrown potion shatters into its area-of-effect
				h.splashPotion(players, a.dim, a.x, a.y, a.z, a.potion, a.lingering)
			}
			if a.explode > 0 { // ghast/wither fireball detonates on impact
				by := ""
				if s := players[a.shooter]; s != nil {
					by = s.p.name
				} else if m := h.mobs[a.shooter]; m != nil {
					by = mobDisplayName(m.etype)
				}
				var opts []blastOpt
				// LargeFireball: a ghast's blast lights what it clears, when
				// mobGriefing lets it change the world at all.
				if a.etype == entityLargeFireball && h.rules.MobGriefing {
					opts = append(opts, withBlastFire())
				}
				if a.dangerous {
					// WitherSkull.getBlockExplosionResistance: a blue skull
					// holds everything the wither may break to 0.8, which is
					// what lets it eat through obsidian a black one bounces off.
					opts = append(opts, withResistCap(witherSkullResistCap))
				}
				h.explodeBy(players, a.dim, a.x, a.y, a.z, a.explode+2, float64(a.explode), blastMob, by, opts...)
			}
			if a.loyalty > 0 { // a loyal trident returns after striking rather than vanishing
				a.returning = true
				continue
			}
			delete(h.arrows, eid)
			h.entityGone(players, a.dim, eid)
			continue
		}
		if accel, inertia, ok := hurtingMotion(a, h.inWater(a.dim, a.x, a.y, a.z)); ok {
			if !a.stuck { // AbstractHurtingProjectile.applyInertia: self-propelled, no gravity
				if sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz); sp > 1e-9 {
					a.vx += a.vx / sp * accel
					a.vy += a.vy / sp * accel
					a.vz += a.vz / sp * accel
				}
				a.vx, a.vy, a.vz = a.vx*inertia, a.vy*inertia, a.vz*inertia
			}
		} else if !a.stuck && a.homing == 0 { // homing bullets steer themselves, no gravity/drag
			a.vy -= arrowGravity
			a.vx, a.vy, a.vz = a.vx*arrowDrag, a.vy*arrowDrag, a.vz*arrowDrag
		}
		if a.x != a.sx || a.y != a.sy || a.z != a.sz {
			a.sx, a.sy, a.sz = a.x, a.y, a.z
			h.toNearbyEv(players, a.dim, a.x, a.z,
				entMove(eid, a.x, a.y, a.z, arrowYaw(a), arrowPitch(a), false))
		}
	}
}

// arrowHitsPlayer tests a sample point against every huntable player's hitbox
// and applies the hit (damage through armor, knockback along the shot).
func (h *hub) arrowHitsPlayer(players map[int32]*tracked, a *arrowEntity, px, py, pz float64) bool {
	now := h.tick.Load()
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead || t.dim != a.dim {
			continue // arrows pass through creative/spectator observers + other dims
		}
		if a.playerShot && t.p.eid == a.shooter && now < a.noHitUntil {
			continue // fresh shots clear their own archer
		}
		// A player's shot at another player is PvP, and obeys the same rule
		// the melee path does — otherwise switching pvp off would stop fists
		// and leave bows working.
		if a.playerShot && t.p.eid != a.shooter && !h.rules.PvP {
			if _, byPlayer := players[a.shooter]; byPlayer {
				continue
			}
		}
		ddx, ddz := px-t.x, pz-t.z
		if ddx*ddx+ddz*ddz > arrowHitRadius*arrowHitRadius {
			continue
		}
		if py < t.y-0.1 || py > t.y+1.9 {
			continue
		}
		if a.knock > 0 { // wind charge: a shove, no damage (vanilla breeze)
			h.knockback(t, a.x, a.z)
			t.launchCause = "wind_charge" // fall_after_explosion, until the next landing
			h.windBurstR(players, a.dim, px, py, pz, a.shooter, windChargeBurstRadius(a))
			return true
		}
		if a.dmg > 0 {
			// Whoever loosed it gets the credit, player or mob.
			shot := deathCause{}
			byMob := false
			if s := players[a.shooter]; s != nil {
				shot.by = s.p.name
			} else if m := h.mobs[a.shooter]; m != nil {
				shot.by = mobDisplayName(m.etype)
				byMob = true // a skeleton's arrow scales with difficulty; a player's does not
			}
			// A piercing bolt goes through a raised shield as though it were
			// not there, which is what naming no source position means here.
			src := from(a.x, a.z)
			if a.pierce > 0 {
				src = dmgFrom{}
			}
			src.byMob = byMob
			landed := h.hurtFrom(players, t, float32(a.dmg), projectileDamageOf(a), shot, src)
			h.knockback(t, a.x, a.z) // the shove lands even off a shield
			if !landed {
				return true // caught on the shield: no venom, no thorns, no fire
			}
			if t.dead {
				h.witherSkullHeal(a) // a wither feeds on what its skull kills
			}
			h.thornsAgainstShooter(players, t, a.shooter)
			if a.poison > 0 {
				h.applyEffect(players, t, effPoison, 0, a.poison)
			}
			if a.wither > 0 {
				h.applyEffect(players, t, effWither, 0, a.wither)
			}
			if a.weaken > 0 {
				h.applyEffect(players, t, effWeakness, 0, a.weaken)
			}
			if a.slow > 0 {
				h.applyEffect(players, t, effSlowness, 0, a.slow)
			}
			if a.levitate > 0 { // shulker bullet: LEVITATION I (vanilla 10 s)
				h.applyEffect(players, t, effLevitation, 0, a.levitate)
			}
			if a.glow > 0 { // SpectralArrow.doPostHurtEffects
				h.applyEffect(players, t, effGlowing, 0, a.glow)
			}
			if a.tipped { // tipped arrow: its brewed potion effects transfer on a hit
				// POTION_DURATION_SCALE on tipped_arrow is 0.125 — an arrow
				// gives an eighth of the bottle's duration, not all of it.
				for _, e := range potionEffects(a.potion) {
					ticks := int(math.Round(float64(e.ticks) * tippedArrowScale))
					if ticks < 1 && e.ticks > 0 {
						ticks = 1
					}
					h.applyEffectTicks(players, t, e.id, e.amp, ticks)
				}
			}
			if a.fire {
				h.setBurning(players, t, 5)
			}
		}
		return true
	}
	return false
}

// tippedArrowScale is the tipped arrow's POTION_DURATION_SCALE: an eighth
// of the potion it was dipped in (a lingering cloud's is a quarter).
const tippedArrowScale = 0.125

// arrowHitsMob tests a sample point against mob hitboxes (player shots only)
// and applies the hit through the normal attack bookkeeping.
func (h *hub) arrowHitsMob(players map[int32]*tracked, a *arrowEntity, px, py, pz float64) bool {
	for _, m := range h.mobs {
		if m.dying > 0 || m.dim != a.dim || (a.mobShot && m.eid == a.shooter) {
			continue
		}
		part := ""
		if m == h.dragon {
			// The dragon is eight boxes, not one, and its own is far too big
			// for the arrow cylinder everything else uses.
			p, ok := dragonPartAt(m, px, py, pz)
			if !ok {
				continue
			}
			part = p
		} else {
			ddx, ddz := px-m.x, pz-m.z
			if ddx*ddx+ddz*ddz > arrowHitRadius*arrowHitRadius || py < m.y-0.1 || py > m.y+2 {
				continue
			}
		}
		if a.hitMobs != nil && a.hitMobs[m.eid] {
			continue // piercing bolt already struck this mob — pass through
		}
		if m.etype == entityShulker && m.shulkerClosed() && a.etype != entityShulkerBullet {
			continue // a closed shell: arrows glance off (Shulker.hurtServer)
		}
		if m.etype == entityEnderman {
			// Vanilla EnderMan.hurtServer: projectiles NEVER land — the
			// enderman teleports out from under them, taking no damage.
			h.endermanTeleport(players, m)
			continue
		}
		if m.etype == entityWarden && a.shooter != 0 {
			h.wardenAngerAt(m, a.shooter, wardenAngerShot) // PROJECTILE_ANGER
		}
		if a.knock > 0 { // a wind charge: one point of damage, a shove and the burst
			if a.playerShot {
				m.hitByPlayer = true
			}
			h.windChargeShoveMob(players, a, m)
			m.hurtKind(windChargeHitDamage, dtWindCharge)
			if m.health <= 0 {
				h.killMob(players, m)
			}
			h.windBurstR(players, a.dim, px, py, pz, a.shooter, windChargeBurstRadius(a))
			return true
		}
		if m.hasBody() { // a sulfur cube's block: its own knockback, a burning arrow lights TNT
			h.cubeStruckByProjectile(players, a, m, float64(projectileHitDamage(a, m)), projectileDamageOf(a))
			return true
		}
		if dmg0 := projectileHitDamage(a, m); dmg0 > 0 {
			if a.playerShot {
				m.hitByPlayer = true
			}
			if d := math.Hypot(a.vx, a.vz); d > 1e-6 && m.kbScale() > 0 { // ride the arrow's momentum
				kbp := (0.5 + 0.6*float64(a.punch)) * m.kbScale() // Punch adds 0.6/level
				m.vx, m.vz, m.kb, m.reroute = a.vx/d*kbp, a.vz/d*kbp, 3, 0
				h.mobKnockVelocity(players, m)
			}
			if shooter := players[a.shooter]; shooter != nil && a.playerShot {
				h.traderLlamasDefend(m, shooter)
			}
			if m.retaliates && a.playerShot {
				if shooter := players[a.shooter]; shooter != nil {
					h.provoke(m, shooter)
				}
			} else if !m.hostile && panicsAt(m, projectileDamageOf(a)) {
				m.panic, m.fleeX, m.fleeZ = panicTicks, a.x, a.z
			} else {
				m.anger = spiderAnger
				if shooter := players[a.shooter]; shooter != nil && a.playerShot {
					m.targetEID, m.unseenTicks = shooter.p.eid, 0 // HurtByTargetGoal
				}
			}
			if hurt, _, _ := h.mobSoundsFor(m); hurt != "" {
				h.playSoundDim(players, a.dim, hurt, sndNeutral, m.x, m.y, m.z, 1, h.hurtPitch())
			}
			dmg := dmg0
			if a.impaling > 0 && sensitiveToImpaling[m.etype] {
				// Impaling bites #sensitive_to_impaling (= #aquatic) since
				// 1.17 — the mobs of the sea, wet or dry — not anything
				// standing in the rain.
				dmg += int(math.Ceil(2.5 * float64(a.impaling)))
			}
			if a.mobShot {
				m.lastAttacker = a.shooter // a mob's arrow counts as its blow (the creeper's disc)
			}
			hit := float64(dmg)
			if part != "" {
				// EnderDragon.hurt: anywhere but the head is worth a quarter.
				hit = dragonPartDamage(part, hit)
			}
			m.hurtKind(hit, projectileDamageOf(a))
			m.lastDirect = a.etype // the blow's direct entity (the ghast's disc asks for its own fireball)
			if a.playerShot {
				if s := players[a.shooter]; s != nil {
					h.advance(players, s, "player_hurt_entity", advMatch{damageDirect: advEntityName[a.etype],
						damageTags: map[string]bool{"is_projectile": true}, dealt: float64(dmg), mainhand: heldStack(s).item})
				}
			}
			h.arrowEffectsOnMob(players, a, m) // poison/wither/slowness/tipped brew
			if m.etype == entityShulker && a.etype == entityShulkerBullet && m.health > 0 {
				h.shulkerBulletHit(players, m) // hitByShulkerBullet: a teleport, maybe a new shulker
			}
			h.channelingStrike(players, a, m.dim, m.x, m.y, m.z, m)
			if a.playerShot { // shot by a living entity → may call reinforcements
				h.zombieReinforce(players, m, players[a.shooter])
			}
			if m.health <= 0 {
				h.witherSkullHeal(a)
				h.killMob(players, m)
				a.victims = append(a.victims, advEntityName[m.etype])
				if a.playerShot {
					if shooter := players[a.shooter]; shooter != nil {
						h.advance(players, shooter, "player_killed_entity", advMatch{entity: advEntityName[m.etype]})
						h.advance(players, shooter, "killed_by_arrow", advMatch{item: a.weapon, victims: a.victims})
						h.incStat(shooter, attachproto.StatKilled, int32(m.etype), 1)
						h.incCustom(shooter, "mob_kills", 1)
						h.sbCriteria(players, "totalKillCount", shooter.p.name, 1, false)
					}
				}
			}
		}
		// A piercing bolt records the mob and keeps flying until its pierces run
		// out; every other arrow stops on the first mob it strikes.
		if a.hitMobs != nil {
			a.hitMobs[m.eid] = true
			if a.pierce > 0 {
				a.pierce--
				return false
			}
		}
		return true
	}
	return false
}

func arrowYaw(a *arrowEntity) float32 {
	return float32(math.Atan2(-a.vx, a.vz) * 180 / math.Pi)
}

func arrowPitch(a *arrowEntity) float32 {
	return float32(-math.Atan2(a.vy, math.Hypot(a.vx, a.vz)) * 180 / math.Pi)
}

// projectileDamage is the vanilla damage type each projectile deals on a
// direct hit. Everything used to land as `arrow`, which meant a fireball's
// burn, a spit and a wither skull were all absorbed, enchanted against and
// counted exactly like an arrow — and the fireball type, which armour does
// NOT protect against the way it protects against arrows, was never dealt at
// all.
//
// Taken from each projectile's onHitEntity in vanilla. The two owner-dependent
// cases are resolved at hit time in projectileDamageOf, not here.
func projectileDamage(etype int) dmgType {
	switch etype {
	case entityTrident:
		return dtTrident
	case entitySnowball, entityEggProj:
		return dtThrown // Snowball/ThrownEgg: damageSources().thrown
	case entitySmallFireball, entityLargeFireball:
		return dtFireball // blaze bolt and ghast fireball alike
	case entityWitherSkull:
		return dtWitherSkull
	case entityShulkerBullet:
		return dtMobProjectile
	case entityLlamaSpit:
		return dtSpit
	case entityWindCharge:
		return dtWindCharge
	case entityPearlProj:
		return dtEnderPearl
	}
	return dtArrow
}

// projectileDamageOf is projectileDamage with the owner known, which two
// vanilla projectiles need:
//
//   - DamageSources.fireball returns UNATTRIBUTED_FIREBALL when the fireball
//     has no owner, and FIREBALL when it does.
//   - WitherSkull.onHitEntity only deals witherSkull damage when its owner is
//     a living entity; an ownerless skull deals plain magic instead.
func projectileDamageOf(a *arrowEntity) dmgType {
	dt := projectileDamage(a.etype)
	if a.shooter != 0 {
		return dt
	}
	switch dt {
	case dtFireball:
		return dtUnattributedFireball
	case dtWitherSkull:
		return dtMagic
	}
	return dt
}

// witherSkullHeal is the last clause of WitherSkull.onHitEntity: when a skull
// KILLS what it hits, its owner heals 5. Only on a kill — a skull that merely
// wounds runs the post-attack effects instead — and only for a skull with a
// living owner, which is the same condition that decides whether it deals
// wither_skull damage or plain magic.
//
// It is what lets a wither out-heal a drawn-out fight by killing whatever else
// is nearby, so leaving it out made the boss meaningfully easier.
func (h *hub) witherSkullHeal(a *arrowEntity) {
	if a.etype != entityWitherSkull || a.shooter == 0 {
		return
	}
	owner := h.mobs[a.shooter]
	if owner == nil || owner.dying > 0 {
		return
	}
	owner.health = min(owner.health+witherSkullHealHP, owner.maxHP())
}

// WitherSkull.onHitEntity: livingOwner.heal(5.0F).
const witherSkullHealHP = 5

// projectileHitDamage is what a projectile does to the mob it strikes: its
// own damage, except a snowball, which does nothing to anyone but a blaze
// (Snowball.onHitEntity: 3 to a blaze, 0 otherwise).
func projectileHitDamage(a *arrowEntity, m *mob) int {
	if a.etype == entitySnowball {
		if m.etype == entityBlaze {
			return 3
		}
		return 0
	}
	// WitherBoss.hurtServer: once it is below half health the wither shrugs
	// off arrows and wind charges entirely. That rule is what gives the fight
	// its shape — the second half cannot be sniped from a hole, you have to
	// come within reach of it.
	if m.etype == entityWither && witherPowered(m) &&
		(a.etype == entityArrow || a.etype == entitySpectralArrow || a.etype == entityWindCharge) {
		return 0
	}
	return a.dmg
}
