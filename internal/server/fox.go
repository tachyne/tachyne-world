package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Foxes (vanilla Fox): by day, sheltered from the sky and with nobody
// about, a fox lies down and sleeps; it hunts chickens, rabbits, baby
// turtles on land and schooling fish, crouching in as it stalks; and it
// picks up whatever is lying around, carrying it in its mouth — food it
// eats after half a minute, the rest it keeps (and drops when it dies).

const (
	foxFlagSitting     = 0x01
	foxFlagCrouching   = 0x04
	foxFlagInterested  = 0x08
	foxFlagPouncing    = 0x10
	foxFlagSleeping    = 0x20
	foxFlagFaceplanted = 0x40
	foxFlagDefending   = 0x80
	metaIndexFoxFlags  = 18 // DATA_FLAGS_ID, after the variant at 17
	foxLandRange       = 10.0
	foxFishRange       = 20.0
	foxItemRange       = 8.0
	foxBiteRange       = 2.0
	foxStalkRange      = 6.0 // StalkPreyGoal: walks in while farther than this, crouches inside it
	foxEatAfter        = 600 // MIN_TICKS_BEFORE_EAT
	foxSleepWait       = 140 // WAIT_TIME_BEFORE_SLEEP
	foxAlertRange      = 12.0
	foxAlertHeight     = 6.0  // the alertable box: inflate(12, 6, 12)
	foxHuntSpeed       = 1.2  // FoxMeleeAttackGoal(1.2)
	foxStalkSpeed      = 1.5  // StalkPreyGoal's moveTo(target, 1.5)
	foxItemSpeed       = 1.2  // FoxSearchForItemsGoal's moveTo(item, 1.2)
	foxBiteEvery       = 20   // a bite a second, the melee cadence
	foxFullCrouch      = 25   // crouchAmount climbs 0.2 a tick to its 5.0 cap
	foxPounceAcross    = 0.8  // FoxPounceGoal.start: the leap's horizontal share
	foxPounceUp        = 0.9  // …and its lift
	foxFaceplantTicks  = 40   // FaceplantGoal: adjustedTickDelay(40)
	foxAggroOdds       = 0.05 // aiStep: a defending fox barks one tick in twenty
	metaTypeByteFox    = 0    // metadata value type: byte
)

func foxFlagsMeta(eid int32, flags uint8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexFoxFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = append(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) foxSetFlags(players map[int32]*tracked, m *mob, flags uint8) {
	if m.foxFlags == flags {
		return
	}
	m.foxFlags = flags
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(foxFlagsMeta(m.eid, flags)))
}

// foxClearStates is Fox.clearStates: up, awake, attentive to nothing.
func (h *hub) foxClearStates(players map[int32]*tracked, m *mob) {
	h.foxSetFlags(players, m, m.foxFlags&^(foxFlagInterested|foxFlagCrouching|foxFlagSitting|foxFlagSleeping|foxFlagDefending|foxFlagFaceplanted))
	m.foxCrouch, m.foxPerchLooks = 0, 0
}

// foxPreyRange: what a fox hunts, and from how far.
func foxPreyRange(o *mob) float64 {
	switch o.etype {
	case entityChicken, entityRabbit:
		return foxLandRange
	case entityCod, entitySalmon, entityTropicalFish:
		return foxFishRange
	case entityTurtle:
		if o.baby {
			return foxLandRange
		}
	}
	return 0
}

// foxStalks is STALKABLE_PREY: a chicken or a rabbit is crept up on and
// pounced at; anything else is simply run down and bitten.
func foxStalks(o *mob) bool { return o.etype == entityChicken || o.etype == entityRabbit }

// foxTamable is the TamableAnimal family: a tame one does not put a fox on
// its guard.
func foxTamable(etype int) bool {
	switch etype {
	case entityWolf, entityCat, entityParrot, entityNautilus, entityZombieNautilus:
		return true
	}
	return false
}

// foxAlertable is FoxBehaviorGoal.alertable with FoxAlertableEntitiesSelector:
// anything living within twelve blocks (six up or down) that is not a fox,
// a tame pet, one of the players it trusts, a sleeper or a sneaking player
// keeps a fox from lying down or sitting to look about. Chickens, rabbits
// and monsters always count; creative and spectator players never do.
func (h *hub) foxAlertable(players map[int32]*tracked, m *mob) bool {
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator || t.p.sneaking || foxTrusts(m, t) {
			continue
		}
		if math.Abs(t.y-m.y) <= foxAlertHeight && dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= foxAlertRange {
			return true
		}
	}
	alert := false
	h.grid().nearby(m.dim, m.x, m.z, foxAlertRange, func(o *mob) {
		if alert || o == m || o.dying > 0 || math.Abs(o.y-m.y) > foxAlertHeight || dist3(o.x, o.y, o.z, m.x, m.y, m.z) > foxAlertRange {
			return
		}
		switch {
		case o.etype == entityFox:
		case o.etype == entityChicken || o.etype == entityRabbit || isMonster(o):
			alert = true
		case foxTamable(o.etype):
			alert = !o.tamed
		default:
			alert = !o.sleeping
		}
	})
	return alert
}

// foxHasShelter is FoxBehaviorGoal.hasShelter: the sky cannot see the top
// of its box, and the spot is one it would walk to (grass underfoot, or
// light of twelve and up: Animal.getWalkTargetValue is not negative).
func (h *hub) foxHasShelter(m *mob) bool {
	x, y, z := floorInt(m.x), floorInt(m.y+m.box().h), floorInt(m.z)
	return !h.canSeeSky(m.dim, x, y, z) && h.foxWalkValue(m.dim, x, y, z) >= 0
}

// canSeeSky is BlockAndLightGetter.canSeeSky: full sky light at the cell.
func (h *hub) canSeeSky(dim, x, y, z int) bool {
	w := h.worldFor(dim)
	if w == nil || dim != dimOverworld {
		return false
	}
	sky, _ := w.LightAt(x, y, z)
	return sky >= 15
}

// foxWalkValue is Animal.getWalkTargetValue's sign: 10 over a grass block,
// otherwise the light's brightness less a half — negative below light 12.
func (h *hub) foxWalkValue(dim, x, y, z int) float64 {
	w := h.worldFor(dim)
	if w == nil {
		return 0
	}
	if below := w.At(x, y-1, z); below == worldgen.GrassBlock || below == worldgen.GrassBlock-1 {
		return 10
	}
	sky, block := w.LightAt(x, y, z)
	f := float64(h.rawBrightness(sky, block, -1)) / 15
	return f/(4-3*f) - 0.5
}

// foxTick is the fox's own tick and aiStep, run every update whatever goal
// has it: waking in the water, under a target or in a thunderstorm; the
// states another goal ended; eating what it carries; the defending bark
// and the faceplant's puffs of snow.
func (h *hub) foxTick(players map[int32]*tracked, m *mob) {
	if m.dying > 0 {
		return
	}
	if !m.foxHeld && m.foxFlags&(foxFlagSleeping|foxFlagSitting) != 0 {
		// A goal above the sleep or the perch took the fox over (a panic, a
		// flight, a mate): their stop() clears the states.
		h.foxSetFlags(players, m, m.foxFlags&^(foxFlagSleeping|foxFlagSitting|foxFlagCrouching|foxFlagInterested))
		m.foxPerchLooks = 0
	}
	m.foxHeld = false
	// The target goals run ahead of the goals proper, whatever the fox is
	// doing; mid-pounce it keeps the one it sprang at.
	if m.foxFlags&foxFlagPouncing == 0 || h.mobs[m.foxPrey] == nil {
		if prey := h.foxTarget(players, m); prey == nil {
			m.foxPrey, m.foxStalking = 0, false
		} else if prey.eid != m.foxPrey {
			m.foxPrey, m.foxStalking, m.foxCrouch = prey.eid, false, 0
		}
	}
	flags := m.foxFlags
	inWater := h.inWater(m.dim, m.x, m.y+0.2, m.z)
	if inWater {
		// FoxFloatGoal.start: clearStates.
		flags &^= foxFlagInterested | foxFlagCrouching | foxFlagSitting | foxFlagSleeping | foxFlagDefending | foxFlagFaceplanted
		m.foxCrouch = 0
	}
	if m.foxPrey != 0 || h.thundering {
		flags &^= foxFlagSleeping // Fox.tick: wakeUp
	}
	if p := h.mobs[m.foxPrey]; p == nil || p.dying > 0 {
		m.foxPrey = 0
		flags &^= foxFlagCrouching | foxFlagInterested // aiStep: no live target, no crouch
		m.foxCrouch = 0
		if flags&foxFlagDefending != 0 {
			flags &^= foxFlagDefending // setTarget(null) ends the defence
		}
	}
	h.foxSetFlags(players, m, flags)
	if m.foxFlags&foxFlagDefending != 0 {
		m.panic, m.panicHasT = 0, false // FoxPanicGoal.shouldPanic: never while defending
		for i := 0; i < mobMoveInterval; i++ {
			if h.rng.Float64() < foxAggroOdds {
				h.playSoundDim(players, m.dim, "minecraft:entity.fox.aggro", sndNeutral, m.x, m.y, m.z, 1, 1)
				break
			}
		}
	}
	if m.foxFlags&foxFlagFaceplanted != 0 {
		for i := 0; i < mobMoveInterval; i++ {
			if h.rng.Float32() < 0.2 {
				x, y, z := floorInt(m.x), floorInt(m.y), floorInt(m.z)
				h.levelEvent(players, m.dim, worldEventBlockBreak, x, y, z, int32(h.worldFor(m.dim).At(x, y, z)))
			}
		}
	}
	// Eating what it carries: a fox with food in its mouth, on the ground,
	// awake and after nothing, eats it past six hundred ticks.
	m.foxEatTicks += mobMoveInterval
	if m.held != 0 && foodPoints[m.held] > 0 && m.foxPrey == 0 && m.foxFlags&foxFlagSleeping == 0 && h.mobOnGround(m) {
		if m.foxEatTicks > foxEatAfter {
			m.held, m.foxEatTicks = 0, 0
			h.playSoundDim(players, m.dim, "minecraft:entity.fox.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusFoxEat))
			h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
		} else if m.foxEatTicks > foxEatAfter-40 && h.rng.Intn(10) == 0 {
			h.playSoundDim(players, m.dim, "minecraft:entity.fox.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusFoxEat))
		}
	}
}

// foxTarget picks what the fox is after this update: whatever hurt a
// player it trusts (DefendTrustedTargetGoal, which puts it on the
// defensive), else the nearest prey.
func (h *hub) foxTarget(players map[int32]*tracked, m *mob) *mob {
	if d := h.foxDefendTarget(players, m); d != nil {
		if m.foxFlags&foxFlagDefending == 0 || m.foxPrey != d.eid {
			// DefendTrustedTargetGoal.start: the bark, the defending flag, awake.
			h.playSoundDim(players, m.dim, "minecraft:entity.fox.aggro", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.foxSetFlags(players, m, (m.foxFlags|foxFlagDefending)&^foxFlagSleeping)
		}
		return d
	}
	if m.foxFlags&foxFlagDefending != 0 {
		h.foxSetFlags(players, m, m.foxFlags&^foxFlagDefending)
	}
	if m.baby {
		return nil
	}
	var prey *mob
	best := foxFishRange
	h.grid().nearby(m.dim, m.x, m.z, foxFishRange, func(o *mob) {
		if o == m || o.dying > 0 {
			return
		}
		r := foxPreyRange(o)
		if r == 0 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d <= r && d < best {
			prey, best = o, d
		}
	})
	return prey
}

// foxStep is the fox's goals from the stalk down to the sleep: stalking,
// pouncing and biting what it is after, running for shelter, and sleeping
// through a quiet day. Returns true while it owns the movement.
func (h *hub) foxStep(players map[int32]*tracked, m *mob) bool {
	if prey := h.mobs[m.foxPrey]; prey != nil && prey.dying == 0 {
		return h.foxHuntStep(players, m, prey)
	}
	if h.foxShelterStep(players, m) {
		return true
	}
	return h.foxSleepStep(players, m)
}

// foxHuntStep is StalkPreyGoal, FoxPounceGoal and FoxMeleeAttackGoal: a
// chicken or rabbit farther than six blocks is walked in on at 1.5; inside
// six, with a clear line, the fox crouches, and once fully down springs at
// it; anything else — and a crouch without a clear line — is run down at
// 1.2 and bitten.
func (h *hub) foxHuntStep(players map[int32]*tracked, m *mob, prey *mob) bool {
	d := dist3(prey.x, prey.y, prey.z, m.x, m.y, m.z)
	if m.foxFlags&foxFlagCrouching != 0 {
		m.foxCrouch += mobMoveInterval
		m.vx, m.vz = 0, 0
		m.yaw = yawToward(m.x, m.z, prey.x, prey.z)
		if m.foxCrouch < foxFullCrouch {
			m.foxHeld = true
			return true // settling down: crouched, nobody else moves it
		}
		if h.foxPathClear(m, prey) {
			h.foxPounce(players, m, prey)
			m.foxHeld = true
			return true
		}
		// FoxPounceGoal.canUse on a blocked line: stand up and go the long way.
		h.foxSetFlags(players, m, m.foxFlags&^(foxFlagCrouching|foxFlagInterested))
		m.foxCrouch = 0
	} else if foxStalks(prey) && m.foxFlags&foxFlagInterested == 0 {
		if d > foxStalkRange {
			m.foxStalking = true
			h.steerTo(m, prey.x, prey.z, foxStalkSpeed)
			return true
		}
		if m.foxStalking {
			// StalkPreyGoal.tick inside six, then stop(): with a clear line
			// it stays down, interested; without one it gets up again.
			m.foxStalking = false
			if h.foxPathClear(m, prey) {
				h.foxSetFlags(players, m, m.foxFlags|foxFlagCrouching|foxFlagInterested)
				m.foxCrouch = 0
				m.vx, m.vz = 0, 0
				m.foxHeld = true
				return true
			}
		}
	}
	// FoxMeleeAttackGoal: start clears the interest; it runs the target down
	// and bites.
	h.foxSetFlags(players, m, m.foxFlags&^foxFlagInterested)
	if d <= foxBiteRange {
		if h.tick.Load()%foxBiteEvery < uint64(mobMoveInterval) {
			h.foxBite(players, m, prey, true)
		}
		m.vx, m.vz = 0, 0
		return true
	}
	h.steerTo(m, prey.x, prey.z, foxHuntSpeed)
	return true
}

// foxBite is doHurtTarget; the melee goal's checkAndPerformAttack adds the
// bite sound.
func (h *hub) foxBite(players map[int32]*tracked, m *mob, prey *mob, sound bool) {
	prey.lastAttacker = m.eid
	prey.hurtKind(float64(m.attackDamage()), dtMobAttack)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	if sound {
		h.playSoundDim(players, m.dim, "minecraft:entity.fox.bite", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
	if prey.health <= 0 {
		h.killMob(players, prey)
	}
}

// foxPathClear is Fox.isPathClear: six points along the way to the target,
// each clear of anything solid one to three blocks up.
func (h *hub) foxPathClear(m *mob, prey *mob) bool {
	w := h.worldFor(m.dim)
	dx, dz := prey.x-m.x, prey.z-m.z
	for i := 0; i < 6; i++ {
		f := float64(i) / 6
		for j := 1; j < 4; j++ {
			s := w.At(floorInt(m.x+dx*f), floorInt(m.y+float64(j)), floorInt(m.z+dz*f))
			if !worldgen.IsReplaceable(s) && !worldgen.IsFluid(s) {
				return false
			}
		}
	}
	return true
}

// foxPounce is FoxPounceGoal.start: the leap, straight at the target.
func (h *hub) foxPounce(players map[int32]*tracked, m *mob, prey *mob) {
	h.foxSetFlags(players, m, (m.foxFlags|foxFlagPouncing)&^foxFlagInterested)
	dx, dy, dz := prey.x-m.x, prey.y-m.y, prey.z-m.z
	n := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if n < 1e-6 {
		n = 1
	}
	m.leaping = true
	m.leapVX, m.leapVY, m.leapVZ = dx/n*foxPounceAcross, foxPounceUp, dz/n*foxPounceAcross
	m.vx, m.vz = 0, 0
}

// foxPounceStep is FoxPounceGoal's tick and stop while the leap flies: it
// bites the target the moment it is within two blocks, and, landing in
// snow, ends up face down in it and forgets the target.
func (h *hub) foxPounceStep(players map[int32]*tracked, m *mob) bool {
	if m.foxFlags&foxFlagPouncing == 0 {
		return false
	}
	prey := h.mobs[m.foxPrey]
	if prey != nil && prey.dying == 0 && dist3(prey.x, prey.y, prey.z, m.x, m.y, m.z) <= foxBiteRange {
		h.foxBite(players, m, prey, false) // doHurtTarget each tick; the hurt cooldown spaces the blows
	}
	if m.leaping {
		m.foxHeld = true
		return true
	}
	// Landed: stop().
	flags := m.foxFlags &^ (foxFlagCrouching | foxFlagInterested | foxFlagPouncing)
	m.foxCrouch = 0
	if h.foxInSnow(m) {
		flags |= foxFlagFaceplanted
		m.foxFaceplant = foxFaceplantTicks
		m.foxPrey, m.foxStalking = 0, false
	}
	h.foxSetFlags(players, m, flags)
	return flags&foxFlagFaceplanted != 0
}

// foxInSnow: the snow layer it came down in — the cell at its feet, or the
// one below when the layer is deep enough to stand on.
func (h *hub) foxInSnow(m *mob) bool {
	w := h.worldFor(m.dim)
	x, y, z := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	if s := w.At(x, y, z); s >= snowLayer1 && s < snowLayer1+7 {
		return true
	}
	s := w.At(x, y-1, z)
	return s > snowLayer1 && s < snowLayer1+7 && worldgen.Collides(s)
}

// foxFaceplantStep is FaceplantGoal: forty ticks head down in the snow,
// going nowhere.
func (h *hub) foxFaceplantStep(players map[int32]*tracked, m *mob) bool {
	if m.foxFlags&foxFlagFaceplanted == 0 {
		return false
	}
	m.vx, m.vz = 0, 0
	if m.foxFaceplant -= mobMoveInterval; m.foxFaceplant <= 0 {
		h.foxSetFlags(players, m, m.foxFlags&^foxFlagFaceplanted)
	}
	m.foxHeld = true
	return true
}

// foxSleepStep is SleepGoal: on a quiet, sheltered day, after a little
// wait, the fox lies down — not in powder snow — and stays down while it
// stays quiet.
func (h *hub) foxSleepStep(players map[int32]*tracked, m *mob) bool {
	quiet := h.isDayTime() && !h.thundering && m.panic == 0 && m.loveTicks == 0 &&
		!h.mobInPowderSnow(m) && !h.inWater(m.dim, m.x, m.y+0.2, m.z) &&
		h.foxHasShelter(m) && !h.foxAlertable(players, m)
	if m.foxFlags&foxFlagSleeping != 0 {
		if !quiet {
			h.foxClearStates(players, m) // SleepGoal.stop
			m.foxSleepIn = h.rng.Intn(foxSleepWait)
			return false
		}
		m.vx, m.vz = 0, 0
		m.foxHeld = true
		return true
	}
	if !quiet {
		m.foxSleepIn = h.rng.Intn(foxSleepWait)
		return false
	}
	if m.foxSleepIn > 0 {
		m.foxSleepIn -= mobMoveInterval
		return false
	}
	// SleepGoal.start: no sitting, crouching or interest; down it goes.
	h.foxSetFlags(players, m, (m.foxFlags&^(foxFlagSitting|foxFlagCrouching|foxFlagInterested))|foxFlagSleeping)
	m.vx, m.vz = 0, 0
	m.foxHeld = true
	return true
}

// foxPickupStep is FoxSearchForItemsGoal and the pick-up: with nothing in
// its mouth, nothing hunted and nothing hurting it, a fox goes for whatever
// is lying within eight blocks.
func (h *hub) foxPickupStep(players map[int32]*tracked, m *mob) bool {
	if m.held != 0 || m.foxPrey != 0 || m.panic > 0 || m.foxFlags&(foxFlagSleeping|foxFlagSitting|foxFlagFaceplanted) != 0 {
		return false
	}
	now := h.tick.Load()
	var it *itemEntity
	bestD := foxItemRange
	for _, cand := range h.items {
		if cand.dim != m.dim || cand.count <= 0 || now < cand.noPickupUntil {
			continue
		}
		if d := dist3(cand.x, cand.y, cand.z, m.x, m.y, m.z); d < bestD {
			it, bestD = cand, d
		}
	}
	if it == nil {
		return false
	}
	if bestD <= 1.5 {
		h.foxHold(players, m, it.item)
		if it.count--; it.count <= 0 {
			delete(h.items, it.eid)
			h.entityGone(players, it.dim, it.eid)
		} else {
			h.refreshItemMeta(players, it)
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
		return true
	}
	h.steerTo(m, it.x, it.z, foxItemSpeed)
	return true
}

// foxHold puts an item in the fox's mouth: shown, kept (a guaranteed drop),
// and the eating clock starts over.
func (h *hub) foxHold(players map[int32]*tracked, m *mob, item int32) {
	m.held, m.foxEatTicks = item, 0
	m.persistent = true
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: 1}, invStack{}, m.gear))
}

// steerTo sets a walker's velocity straight at (x, z) at a speed multiple.
func (h *hub) steerTo(m *mob, x, z float64, speed float64) {
	dx, dz := x-m.x, z-m.z
	if hd := math.Hypot(dx, dz); hd > 1e-6 {
		sp := m.moveSpeed() * speed
		m.vx, m.vz = dx/hd*sp, dz/hd*sp
	}
	m.rest = 0
}
