package server

import "math"

// Idle head tracking — LookAtPlayerGoal and RandomLookAroundGoal, the pair
// almost every vanilla mob carries at the bottom of its goal list. A mob
// standing about rolls a small chance each tick to watch a nearby player
// for a couple of seconds, or to turn its head somewhere random for one.
// The HEAD turns, not the body: vanilla's LookControl aims the head and
// lets the body follow only when the mob walks.

const (
	lookPlayerTicksMin = 40 // LookAtPlayerGoal.start: 40 + rand(40)
	lookPlayerTicksMax = 80
	lookAroundTicksMin = 20 // RandomLookAroundGoal.start: 20 + rand(20)
	lookAroundTicksMax = 40
	// lookChance is both goals' 0.02 per tick, compounded over the two ticks
	// one mob update covers.
	lookChance = 1 - (1-0.02)*(1-0.02)
)

// lookRanges are the vanilla LookAtPlayerGoal distances, by species: six
// blocks for the farm animals and the golems, eight for most hostiles, ten
// for the cats and the rabbit, fifteen for a pillager. A species that is
// absent takes the default below; one mapped to zero has neither goal
// (the squid and the fish, the shulker, the dragon) and never turns its
// head on its own.
var lookRanges = map[string]float64{
	"cow": 6, "mooshroom": 6, "pig": 6, "sheep": 6, "chicken": 6, "horse": 6, "donkey": 6,
	"mule": 6, "skeleton_horse": 6, "zombie_horse": 6, "camel": 6, "camel_husk": 6, "llama": 6, "trader_llama": 6,
	"polar_bear": 6, "iron_golem": 6, "snow_golem": 6, "copper_golem": 6, "dolphin": 6, "ravager": 6,
	"panda":   6, // PandaLookAtPlayerGoal(this, Player, 6)
	"sniffer": 6, // SnifferAi: SetEntityLookTarget(PLAYER, 6)
	"tadpole": 6, // TadpoleAi: SetEntityLookTargetSometimes(PLAYER, 6, 30-60)
	"cat":     10, "ocelot": 10, "rabbit": 10,
	"fox":      24, // FoxLookAtPlayerGoal(this, Player, 24)
	"pillager": 15,
	"evoker":   3, "illusioner": 3, "vindicator": 3, "vex": 3, // LookAtPlayerGoal(this, Player, 3, 1.0)
	"squid": 0, "glow_squid": 0, "cod": 0, "salmon": 0, "tropical_fish": 0, "pufferfish": 0,
	"shulker": 0, "ender_dragon": 0, "nautilus": 0, "zombie_nautilus": 0,
	"giant": 0, // Giant registers no goals at all
}

// lookRange is how far a species watches a player from, zero for one that
// has no look goals at all.
func lookRange(m *mob) float64 {
	if d, ok := lookRanges[entityNameByID[m.etype]]; ok {
		return d
	}
	return 8 // the LookAtPlayerGoal default every other mob is given
}

// idleLook runs the two look goals and leaves the mob's head yaw where they
// point it. A mob that is walking, hunting or has no look goals simply
// looks where its body faces.
func (h *hub) idleLook(players map[int32]*tracked, m *mob) {
	d := lookRange(m)
	if d == 0 || m.dying > 0 {
		m.headYaw = m.yaw
		return
	}
	if m.etype == entityFox && m.foxFlags&(foxFlagSleeping|foxFlagSitting) != 0 {
		return // asleep (FoxLookControl does not tick), or perched with its own look
	}
	// A fox's look goal neither starts nor carries on while it is face down
	// in snow or intent on prey.
	foxBusy := m.etype == entityFox && m.foxFlags&(foxFlagFaceplanted|foxFlagInterested) != 0
	if foxBusy {
		m.lookTicks = 0
	}
	if m.lookTicks > 0 {
		m.lookTicks -= mobMoveInterval
		switch {
		case m.lookEID != 0:
			if t := players[m.lookEID]; t != nil && t.dim == m.dim {
				m.headYaw = yawToward(m.x, m.z, t.x, t.z)
				return
			}
			m.lookTicks = 0 // whoever it was watching is gone
		default:
			m.headYaw = yawToward(0, 0, m.lookDX, m.lookDZ)
			return
		}
	}
	m.headYaw = m.yaw // nothing to watch: the head sits with the body
	if m.hasTarget {
		return // hunting: the chase already points it where it is going
	}
	// LookAtPlayerGoal first, as vanilla adds it first at the same priority.
	if t := h.nearestPlayerIn(players, m.dim, m.x, m.z, d); t != nil && !foxBusy && h.rng.Float64() < lookChance {
		m.lookEID = t.p.eid
		m.lookTicks = int32(lookPlayerTicksMin + h.rng.Intn(lookPlayerTicksMax-lookPlayerTicksMin))
		return
	}
	if m.etype == entityFox {
		return // the fox has no RandomLookAroundGoal
	}
	if h.rng.Float64() < lookChance { // RandomLookAroundGoal
		a := h.rng.Float64() * 2 * math.Pi
		m.lookEID, m.lookDX, m.lookDZ = 0, math.Cos(a), math.Sin(a)
		m.lookTicks = int32(lookAroundTicksMin + h.rng.Intn(lookAroundTicksMax-lookAroundTicksMin))
	}
}

// yawToward is the yaw that points from one spot to another.
func yawToward(fromX, fromZ, toX, toZ float64) float32 {
	return float32(math.Atan2(-(toX-fromX), toZ-fromZ) * 180 / math.Pi)
}

// nearestPlayerIn is the closest player in a dimension within range.
func (h *hub) nearestPlayerIn(players map[int32]*tracked, dim int, x, z, r float64) *tracked {
	var best *tracked
	bestD2 := r * r
	for _, t := range players {
		if t.dim != dim || t.dead {
			continue
		}
		if d2 := (t.x-x)*(t.x-x) + (t.z-z)*(t.z-z); d2 < bestD2 {
			best, bestD2 = t, d2
		}
	}
	return best
}

// alertsKin is HurtByTargetGoal.setAlertOthers: the species whose neighbours
// join in when one of them is struck.
var alertsKin = func() map[int]bool {
	m := map[int]bool{}
	for _, n := range []string{"dolphin", "wolf", "rabbit", "panda", "bee", "shulker", "endermite",
		"ravager", "vex", "silverfish", "blaze", "zombie", "husk", "drowned", "zombie_villager",
		"zombified_piglin", "evoker", "illusioner", "pillager", "vindicator"} {
		if id, ok := entityByName[n]; ok { // a species this version does not have is simply absent
			m[id] = true
		}
	}
	return m
}()

// alertKin rouses the struck mob's neighbours. Vanilla alerts everything of
// the same CLASS within the follow range (a box ten blocks tall), skipping
// any that is already fighting — which for the zombie family means a husk's
// cry reaches zombies and drowned, but never a zombified piglin, whose
// grudge is its own.
func (h *hub) alertKin(m *mob, t *tracked) {
	if t == nil || !alertsKin[m.etype] {
		return
	}
	// The search box is AABB.unitCubeFromLowerCorner(position).inflate(r,
	// 10, r): a square r out on each side (plus the unit cube), 10 below and
	// 11 above, met by the other mob's own box — not a sphere.
	r := m.followRange()
	h.grid().nearby(m.dim, m.x, m.z, r*math.Sqrt2+2, func(o *mob) {
		if o.eid == m.eid || o.dying > 0 || o.targetEID != 0 || !sameKin(m.etype, o.etype) {
			return
		}
		b := o.box()
		hw := b.w / 2
		if o.x+hw < m.x-r || o.x-hw > m.x+1+r || o.z+hw < m.z-r || o.z-hw > m.z+1+r ||
			o.y+b.h < m.y-10 || o.y > m.y+11 {
			return
		}
		if o.retaliates || !o.hostile {
			h.provoke(o, t) // wolves, bees, pandas: the pack turns
			return
		}
		if o.etype == entityZombifiedPiglin {
			h.zombifiedPiglinAngerAt(o, t) // the call starts its grudge too
			return
		}
		o.targetEID, o.unseenTicks, o.anger = t.p.eid, 0, spiderAnger
		o.hasTarget, o.tx, o.tz = true, t.x, t.z
	})
}

// sameKin reports whether a b answers a's call: getEntitiesOfClass of the
// struck mob's own class, so the same species, and for a plain zombie every
// Zombie subclass — husks, drowned and zombie villagers (zombified piglins
// are the one the goal excludes by name). A husk's cry reaches only husks.
func sameKin(a, b int) bool {
	if a == b {
		return true
	}
	return a == entityZombie && zombieKind(b)
}
