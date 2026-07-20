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
)

var (
	entityArrow = entityID("arrow") // minecraft:entity_type "arrow" (1.21.5)
)

type arrowEntity struct {
	eid        int32
	uuid       [16]byte
	x, y, z    float64
	vx, vy, vz float64
	dim        int  // dimension the projectile flies in
	stuck      bool // hit a block — hold position until despawn
	born       uint64
	sx, sy, sz float64 // last broadcast position (relative-move baseline)

	shooter    int32   // eid of who fired it (players skip their own fresh shots)
	dmg        int     // damage on a hit (charge-scaled for player bows)
	noHitUntil uint64  // tick before which the shooter can't hit themselves
	playerShot bool    // player-fired: hits mobs, and is retrievable once stuck
	breaks     bool    // snowball/egg: shatters on impact instead of sticking
	egg        bool    // an egg: 1-in-8 chance to hatch a chick where it lands
	pearl      bool    // an ender pearl: teleports its thrower where it lands
	poison     bool    // witch splash: poisons the player it hits
	splash     bool    // a thrown potion: shatters on any impact into an AoE (see splashPotion)
	potion     int8    // the potion kind a splash/lingering projectile carries
	lingering  bool    // a lingering potion: leaves an effect cloud instead of an instant splash
	fire       bool    // blaze fireball: sets its target burning
	wither     int     // wither skull: seconds of wither effect on a hit
	weaken     int     // parched arrow: seconds of weakness effect on a hit
	slow       int     // stray arrow: seconds of slowness effect on a hit
	homing     int32   // shulker bullet: eid of the target it curves toward (0 = straight)
	levitate   int     // shulker bullet: seconds of Levitation applied on a hit
	explode    int     // ghast/wither fireball: explosion power on impact (0 = none)
	knock      float64 // wind charge: pure knockback impulse, no damage
	punch      int     // bow Punch enchant: +0.6/level extra hit knockback

	pierce   int            // crossbow piercing: remaining pass-throughs (0 = stop on first mob)
	hitMobs  map[int32]bool // mobs already struck (piercing: never the same one twice; nil when not piercing)
	noPickup bool           // multishot side bolts / creative tridents: fly + hit, never retrievable

	loyalty     int      // thrown-trident loyalty: >0 flies back to the thrower instead of sticking
	impaling    int      // thrown-trident impaling: bonus damage to targets in water or rain
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
	ox, oy, oz := m.x, m.y+1.4, m.z
	dx, dy, dz := t.x-ox, (t.y+0.6)-oy, t.z-oz // a player's bbox is 1.8 → getY(1/3) = y+0.6
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
	a := h.launchArrow(players, ox, oy, oz, vx, vy, vz)
	a.shooter, a.dmg = m.eid, arrowDamage
	if m.etype == entityParched {
		a.weaken = parchedWeaknessSecs // vanilla Parched: WEAKNESS, 600 ticks
	}
	if m.etype == entityStray {
		a.slow = 30 // vanilla Stray: an arrow of SLOWNESS, 600 ticks
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid)) // the draw is visible
}

// launchArrow spawns an arrow projectile with a velocity; callers stamp
// shooter/damage on the returned entity.
func (h *hub) launchArrow(players map[int32]*tracked, x, y, z, vx, vy, vz float64) *arrowEntity {
	return h.launchProjectileIn(players, entityArrow, 0, x, y, z, vx, vy, vz)
}

// launchProjectile spawns any arrow-physics projectile (arrow/snowball/egg).
func (h *hub) launchProjectile(players map[int32]*tracked, etype int, x, y, z, vx, vy, vz float64) *arrowEntity {
	return h.launchProjectileIn(players, etype, 0, x, y, z, vx, vy, vz)
}

// launchProjectileIn launches into an explicit dimension.
func (h *hub) launchProjectileIn(players map[int32]*tracked, etype, dim int, x, y, z, vx, vy, vz float64) *arrowEntity {
	eid := h.allocEID()
	a := &arrowEntity{eid: eid, dim: dim, x: x, y: y, z: z, vx: vx, vy: vy, vz: vz,
		born: h.tick.Load(), sx: x, sy: y, sz: z}
	binary.BigEndian.PutUint32(a.uuid[12:], uint32(eid))
	h.arrows[eid] = a
	add := entAdd(eid, etype, a.uuid, x, y, z, arrowYaw(a), arrowPitch(a))
	add.VX, add.VY, add.VZ = vx, vy, vz // the launch arc, ahead of the first move
	h.toNearbyEv(players, dim, x, z, add)
	return a
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
				h.toNearbyEv(players, a.dim, a.x, a.z, entGone(eid))
			}
			continue
		}
		if now-a.born >= arrowLifeTicks {
			delete(h.arrows, eid)
			h.toNearbyEv(players, a.dim, a.x, a.z, entGone(eid))
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
						h.toNearbyEv(players, a.dim, a.x, a.z, entGone(eid))
						h.playSound(players, "minecraft:entity.item.pickup", sndPlayer, a.x, a.y, a.z, 0.4, 1.5)
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

		// Sample the step at its midpoint and endpoint: at 1.6 blocks/tick a
		// single endpoint test can pass clean through a one-block wall.
		hit := false
		for _, f := range [2]float64{0.5, 1.0} {
			px, py, pz := a.x+a.vx*f, a.y+a.vy*f, a.z+a.vz*f
			if h.arrowHitsPlayer(players, a, px, py, pz) ||
				(a.playerShot && h.arrowHitsMob(players, a, px, py, pz)) {
				hit = true
				break
			}
			if worldgen.Collides(h.worldFor(a.dim).At(int(math.Floor(px)), int(math.Floor(py)), int(math.Floor(pz)))) {
				if a.breaks { // snowballs/eggs shatter
					hit = true
					h.spawnParticles(players, particlePoof, a.x, a.y, a.z, 0.1, 0.05, 6)
					if a.pearl {
						h.pearlLand(players, a)
					}
					if a.egg && h.rng.Intn(8) == 0 { // the classic egg-machine gamble
						chick := h.spawnAnimal(players, entityChicken, int(a.x), int(a.z))
						if chick != nil {
							chick.baby, chick.growLeft = true, growUpTicks
							h.toNearbyEv(players, 0, chick.x, chick.z, metaEv(babyMeta(chick.eid, true)))
						}
					}
					break
				}
				if bp := (blockPos{int(math.Floor(px)), int(math.Floor(py)), int(math.Floor(pz))}); a.dim == 0 {
					if bs := h.world.At(bp.x, bp.y, bp.z); isTarget(bs) {
						h.hitTarget(players, bp, bs, px, py, pz, true) // arrows hold 20 ticks
					}
				}
				if a.loyalty > 0 { // a loyal trident bounces off the wall and flies home
					a.returning = true
				} else {
					a.stuck = true // freeze just short of the face it struck
				}
				h.playSound(players, "minecraft:entity.arrow.hit", sndNeutral, a.x, a.y, a.z, 1, 1.2)
				break
			}
			a.x, a.y, a.z = px, py, pz
		}
		if hit {
			if a.splash { // a thrown potion shatters into its area-of-effect
				h.splashPotion(players, a.dim, a.x, a.y, a.z, a.potion, a.lingering)
			}
			if a.explode > 0 { // ghast/wither fireball detonates on impact
				h.explodeAt(players, a.x, a.y, a.z, a.explode+2, a.explode*4)
			}
			if a.loyalty > 0 { // a loyal trident returns after striking rather than vanishing
				a.returning = true
				continue
			}
			delete(h.arrows, eid)
			h.toNearbyEv(players, a.dim, a.x, a.z, entGone(eid))
			continue
		}
		if !a.stuck && a.homing == 0 { // homing bullets steer themselves, no gravity/drag
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
		ddx, ddz := px-t.x, pz-t.z
		if ddx*ddx+ddz*ddz > arrowHitRadius*arrowHitRadius {
			continue
		}
		if py < t.y-0.1 || py > t.y+1.9 {
			continue
		}
		if a.knock > 0 { // wind charge: a shove, no damage (vanilla breeze)
			h.knockback(t, a.x, a.z)
			return true
		}
		if a.dmg > 0 {
			// A raised shield facing the incoming arrow stops it dead.
			if h.shieldBlocks(t, a.x, a.z) {
				h.shieldBlockFX(players, t)
				return true
			}
			h.damage(players, t, t.armorReduce(float32(a.dmg)))
			h.wearArmor(players, t, float32(a.dmg))
			h.knockback(t, a.x, a.z)
			if a.poison {
				h.applyEffect(players, t, effPoison, 0, 10)
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
			if a.fire {
				h.setBurning(players, t, 5)
			}
		}
		return true
	}
	return false
}

// arrowHitsMob tests a sample point against mob hitboxes (player shots only)
// and applies the hit through the normal attack bookkeeping.
func (h *hub) arrowHitsMob(players map[int32]*tracked, a *arrowEntity, px, py, pz float64) bool {
	for _, m := range h.mobs {
		if m.dying > 0 || m.dim != a.dim {
			continue
		}
		ddx, ddz := px-m.x, pz-m.z
		if ddx*ddx+ddz*ddz > arrowHitRadius*arrowHitRadius || py < m.y-0.1 || py > m.y+2 {
			continue
		}
		if a.hitMobs != nil && a.hitMobs[m.eid] {
			continue // piercing bolt already struck this mob — pass through
		}
		if m.etype == entityEnderman {
			// Vanilla EnderMan.hurtServer: projectiles NEVER land — the
			// enderman teleports out from under them, taking no damage.
			h.endermanTeleport(players, m)
			continue
		}
		if a.dmg > 0 {
			m.hitByPlayer = true
			if d := math.Hypot(a.vx, a.vz); d > 1e-6 && !m.noKB { // ride the arrow's momentum
				kbp := 0.5 + 0.6*float64(a.punch) // Punch adds 0.6/level
				m.vx, m.vz, m.kb, m.reroute = a.vx/d*kbp, a.vz/d*kbp, 3, 0
				h.mobKnockVelocity(players, m)
			}
			if m.retaliates && a.playerShot {
				if shooter := players[a.shooter]; shooter != nil {
					h.provoke(m, shooter)
				}
			} else if !m.hostile {
				m.panic, m.fleeX, m.fleeZ = panicTicks, a.x, a.z
			} else {
				m.anger = spiderAnger
			}
			if hurt, _, _ := mobSounds(m.etype); hurt != "" {
				h.playSound(players, hurt, sndNeutral, m.x, m.y, m.z, 1, h.hurtPitch())
			}
			dmg := a.dmg
			if a.impaling > 0 && (h.raining || h.inWater(m.dim, m.x, m.y, m.z)) {
				dmg += int(math.Ceil(2.5 * float64(a.impaling))) // trident impaling: +2.5/level in water or rain
			}
			m.hurt(float64(dmg))
			if a.playerShot { // shot by a living entity → may call reinforcements
				h.zombieReinforce(players, m, players[a.shooter])
			}
			if m.health <= 0 {
				h.killMob(players, m)
				if a.playerShot {
					if shooter := players[a.shooter]; shooter != nil {
						h.advance(players, shooter, "player_killed_entity", advMatch{entity: advEntityName[m.etype]})
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
