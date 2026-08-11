package server

import "math"

// Mob crowding — the shove that keeps two mobs from standing in one another.
//
// Ported from LivingEntity.pushEntities + Entity.push. Every living entity in
// vanilla, every tick, collects the pushable entities its bounding box touches
// and nudges each of them apart; a bee cannot rest inside another bee, a herd
// of cows spreads out instead of collapsing to a point, and a pen packed past
// the cramming limit starts crushing what is in it.
//
// tachyne had none of this. The only separation anywhere in the engine was the
// herd-flocking term in behavior.go, which is cow/sheep cohesion and which most
// species (bees included) never use — so mobs overlapped exactly and stayed
// that way.
//
// Two things are deliberately NOT vanilla here, both for reasons outside this
// file:
//
//   - Players are not pushed. In vanilla the shove lands in the player's own
//     deltaMovement; here the client owns its position and the server only
//     validates it (movement.go), so there is no server-side velocity to add
//     to. Pushing a player needs a wire frame the engine does not have yet.
//   - The impulse is kept out of m.vx/m.vz. Vanilla's yaw comes from the move
//     control — where the mob is walking — not from deltaMovement, so a mob
//     being shoved does not turn to face the shove. Our yaw IS derived from
//     m.vx/m.vz, so the push rides its own accumulator and the mob keeps
//     looking where it is going.

const (
	// Entity.push adds 0.05 per tick to the pushed entity's velocity. m.pushX
	// is a per-STEP displacement and a step is mobMoveInterval ticks, so the
	// same separation rate costs that much per update.
	pushImpulse = 0.05 * mobMoveInterval

	// Vanilla's shove decays with ordinary movement friction. This is the same
	// momentum factor the steering velocity carries in updateMobs.
	pushFriction = 0.85

	// Below this the accumulator is noise; zeroing it keeps a settled crowd
	// from drifting forever on the tail of an old shove.
	pushEpsilon = 1e-4

	// The bucket edge used to find touching mobs. Wider than the widest
	// ordinary mob (the ghast, 4.0), so a 3x3 neighbourhood always covers the
	// pair. The dragon is 16 wide and is excluded from the pass entirely.
	pushCell = 4

	crammingDamage = 6.0 // damageSources().cramming()

	// Vanilla rolls a 1-in-4 chance PER TICK, but a hurt entity is then
	// invulnerable for 20 ticks, so a crushed mob actually takes a hit about
	// every second. Our mobs have no invulnerability window, so the cadence is
	// spelled out here instead: one roll per update against a cooldown.
	crammingCD = 20 / mobMoveInterval
)

// mobBox is a species' bounding box: width (the full edge, not the half) and
// height, from the vanilla EntityType registrations. The engine's m.y is the
// FEET, so the box is [x-w/2, x+w/2] x [y, y+h] x [z-w/2, z+w/2].
type mobBox struct{ w, h float64 }

// mobBoxes is every living species the engine can spawn, at its vanilla size.
var mobBoxes = map[int]mobBox{
	entityAllay: {0.35, 0.6}, entityArmadillo: {0.7, 0.65}, entityAxolotl: {0.75, 0.42},
	entityBat: {0.5, 0.9}, entityBee: {0.55, 0.5}, entityBlaze: {0.6, 1.8},
	entityBogged: {0.6, 1.99}, entityBreeze: {0.6, 1.77}, entityCamel: {1.7, 2.375},
	entityCamelHusk: {1.7, 2.375}, entityCat: {0.6, 0.7}, entityCaveSpider: {0.7, 0.5},
	entityChicken: {0.4, 0.7}, entityCod: {0.5, 0.3}, entityCopperGolem: {0.49, 0.98},
	entityCow: {0.9, 1.4}, entityCreaking: {0.9, 2.7}, entityCreeper: {0.6, 1.7},
	entityDolphin: {0.9, 0.6}, entityDonkey: {1.3964844, 1.5}, entityDrowned: {0.6, 1.95},
	entityElderGuardian: {1.9975, 1.9975}, entityEnderDragon: {16, 8},
	entityEnderman: {0.6, 2.9}, entityEndermite: {0.4, 0.3}, entityEvoker: {0.6, 1.95},
	entityFox: {0.6, 0.7}, entityFrog: {0.5, 0.5}, entityGhast: {4, 4},
	entityGiant: {3.6, 12}, entityGlowSquid: {0.8, 0.8}, entityGoat: {0.9, 1.3},
	entityGuardian: {0.85, 0.85}, entityHappyGhast: {4, 4}, entityHoglin: {1.3964844, 1.4},
	entityHorse: {1.3964844, 1.6}, entityHusk: {0.6, 1.95}, entityIllusioner: {0.6, 1.95},
	entityIronGolem: {1.4, 2.7}, entityLlama: {0.9, 1.87}, entityMagmaCube: {0.52, 0.52},
	entityMooshroom: {0.9, 1.4}, entityMule: {1.3964844, 1.6}, entityNautilus: {0.875, 0.95},
	entityOcelot: {0.6, 0.7}, entityPanda: {1.3, 1.25}, entityParched: {0.6, 1.99},
	entityParrot: {0.5, 0.9}, entityPhantom: {0.9, 0.5}, entityPig: {0.9, 0.9},
	entityPiglin: {0.6, 1.95}, entityPiglinBrute: {0.6, 1.95}, entityPillager: {0.6, 1.95},
	entityPolarBear: {1.4, 1.4}, entityPufferfish: {0.7, 0.7}, entityRabbit: {0.49, 0.6},
	entityRavager: {1.95, 2.2}, entitySalmon: {0.7, 0.4}, entitySheep: {0.9, 1.3},
	entityShulker: {1, 1}, entitySilverfish: {0.4, 0.3}, entitySkeleton: {0.6, 1.99},
	entitySkeletonHorse: {1.3964844, 1.6}, entitySlime: {0.52, 0.52},
	entitySniffer: {1.9, 1.75}, entitySnowGolem: {0.7, 1.9}, entitySpider: {1.4, 0.9},
	entitySquid: {0.8, 0.8}, entityStray: {0.6, 1.99}, entityStrider: {0.9, 1.7},
	entityTadpole: {0.4, 0.3}, entityTraderLlama: {0.9, 1.87},
	entityTropicalFish: {0.5, 0.4}, entityTurtle: {1.2, 0.4}, entityVex: {0.4, 0.8},
	entityVillager: {0.6, 1.95}, entityVindicator: {0.6, 1.95},
	entityWanderingTrader: {0.6, 1.95}, entityWarden: {0.9, 2.9}, entityWitch: {0.6, 1.95},
	entityWither: {0.9, 3.5}, entityWitherSkeleton: {0.7, 2.4}, entityWolf: {0.6, 0.85},
	entityZoglin: {1.3964844, 1.4}, entityZombie: {0.6, 1.95},
	entityZombieHorse: {1.3964844, 1.6}, entityZombieNautilus: {0.875, 0.95},
	entityZombieVillager: {0.6, 1.95}, entityZombifiedPiglin: {0.6, 1.95},
}

// defaultMobBox is the humanoid box, used for anything not in the table so a
// new species collides sensibly from the day it is added.
var defaultMobBox = mobBox{0.6, 1.95}

// box returns this mob's bounding box, scaled for its age and (for the cube
// mobs) its size.
func (m *mob) box() mobBox {
	b, ok := mobBoxes[m.etype]
	if !ok {
		b = defaultMobBox
	}
	switch {
	case m.etype == entitySlime || m.etype == entityMagmaCube:
		// AbstractCubeMob.getDefaultDimensions: the registered box scaled by
		// the cube's size, so a big slime really is a big box.
		if s := float64(m.size); s > 0 {
			b.w, b.h = b.w*s, b.h*s
		}
	case m.baby:
		// AgeableMob: every baby box in vanilla is the adult scaled by half
		// (a cow's 0.9x1.4 becomes exactly 0.45x0.7).
		b.w, b.h = b.w/2, b.h/2
	}
	return b
}

// vehicle reports whether something is riding this mob. Entity.push refuses to
// move a vehicle on either side of the shove — a ridden horse holds its ground.
func (m *mob) vehicle() bool {
	return m.rider != 0 || m.mobRider != 0 || len(m.riders) > 0
}

// pushable is Entity.isPushable across the species that override it, plus the
// tachyne states that stand in for vanilla's.
//
// Being unpushable does NOT take a mob out of the pass. Vanilla's list is the
// pushable entities AROUND the pusher, and the pusher runs it whatever it is —
// so a bat shoves a cow out of its way and is never shoved back. Excluding an
// unpushable mob entirely would quietly cost that half.
func (m *mob) pushable() bool {
	switch {
	case m.dying > 0 || m.health <= 0:
		return false // LivingEntity.isPushable: isAlive()
	case m.mount != 0:
		return false // a passenger is glued to its vehicle's position anyway
	case m.statik:
		return false // anchored to its wall (shulker): it shoves, it does not budge
	case m.spawnInvuln > 0:
		return false // a wither charging its spawn is held in place
	case m.sleeping:
		return false // vanilla's sleeping box is a 0.2 stub inside the bed
	case m.etype == entityBat:
		return false // Bat.isPushable: false, flatly
	case m.etype == entityCreaking:
		return !m.frozen && !m.vehicle() // Creaking.isPushable: super && canMove()
	}
	// Warden.isPushable also refuses while digging or emerging; tachyne has no
	// emerge/dig animation state yet (warden.go), so there is nothing to test.
	return !m.vehicle()
}

// overlaps reports whether two mobs' bounding boxes intersect — AABB.intersects,
// which is strict on every axis.
func (m *mob) overlaps(o *mob) bool {
	mb, ob := m.box(), o.box()
	return math.Abs(m.x-o.x) < (mb.w+ob.w)/2 &&
		math.Abs(m.z-o.z) < (mb.w+ob.w)/2 &&
		m.y < o.y+ob.h && o.y < m.y+mb.h
}

// shove is Entity.push(Entity): the pusher `a` and the pushed `b` are driven
// apart horizontally. Each side earns its own half — vanilla re-tests both
// before applying either impulse, which is how an unpushable pusher clears a
// path without moving itself.
//
// The arithmetic looks wrong and is not. Vanilla takes the LARGER of the two
// axis gaps, square-roots it, and divides both components by that — so the
// impulse SHRINKS as two mobs converge, and a pair sharing a position to within
// 0.01 gets no impulse at all. That is why crammed mobs ooze apart slowly
// instead of springing, and why a perfectly stacked pair needs something else
// to jog it loose. Reproduced exactly, including the guard.
func shove(a, b *mob) {
	xa, za := a.x-b.x, a.z-b.z
	dd := math.Max(math.Abs(xa), math.Abs(za)) // Mth.absMax
	if dd < 0.01 {
		return
	}
	dd = math.Sqrt(dd)
	xa, za = xa/dd, za/dd
	pow := math.Min(1/dd, 1)
	xa, za = xa*pow*pushImpulse, za*pow*pushImpulse
	if b.pushable() { // always true off the neighbour list; stated, not assumed
		b.pushX, b.pushZ = b.pushX-xa, b.pushZ-za
	}
	if a.pushable() {
		a.pushX, a.pushZ = a.pushX+xa, a.pushZ+za
	}
}

// pushCellOf buckets a coordinate. Floor, not truncation — integer division
// folds -3..3 into one oversized cell around the origin.
func pushCellOf(v float64) int { return int(math.Floor(v / pushCell)) }

// pushMobs is one pass of LivingEntity.pushEntities over every mob: decay the
// standing impulse, find the crowd each mob is standing in, shove it apart, and
// crush whatever is packed past the cramming limit.
//
// Vanilla runs pushEntities from BOTH members of a touching pair (each entity
// ticks its own), and each call nudges both — so an ordinary mob-on-mob pair is
// shoved twice per tick. Looping over every mob here reproduces that exactly;
// it is not a double-count to be optimised away.
func (h *hub) pushMobs(players map[int32]*tracked) {
	// cells holds the PUSHABLE mobs — vanilla's getPushableEntities is the
	// neighbour list, filtered. pushers holds everything that runs the pass,
	// which is every living mob whether or not it can itself be moved.
	cells := make(map[[3]int][]*mob, len(h.mobs))
	pushers := make([]*mob, 0, len(h.mobs))
	for _, m := range h.mobs {
		m.pushX, m.pushZ = m.pushX*pushFriction, m.pushZ*pushFriction
		if math.Abs(m.pushX) < pushEpsilon {
			m.pushX = 0
		}
		if math.Abs(m.pushZ) < pushEpsilon {
			m.pushZ = 0
		}
		if m.cramCD > 0 {
			m.cramCD--
		}
		if m == h.dragon || m.dying > 0 || m.health <= 0 {
			continue // the dragon flies on its own physics (updateDragon)
		}
		pushers = append(pushers, m)
		if !m.pushable() {
			continue
		}
		cx, cz := pushCellOf(m.x), pushCellOf(m.z)
		k := [3]int{m.dim, cx, cz}
		cells[k] = append(cells[k], m)
	}
	for _, m := range pushers {
		cx, cz := pushCellOf(m.x), pushCellOf(m.z)
		crowd := 0
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				for _, o := range cells[[3]int{m.dim, cx + dx, cz + dz}] {
					if o == m || !m.overlaps(o) {
						continue
					}
					crowd++
					shove(m, o)
				}
			}
		}
		// maxEntityCramming: vanilla hurts an entity sharing its box with
		// maxCramming-1 others or more. Rare outside a packed pen, and exactly
		// what makes one work.
		if crowd > maxEntityCramming-1 && m.cramCD == 0 && h.rng.Intn(4) == 0 {
			m.cramCD = crammingCD
			h.hurtMobOf(players, m, crammingDamage, dtCramming)
		}
	}
}
