package server

import "sort"

// A villager's LOOK_TARGET and INTERACTION_TARGET memories.
//
//   - LOOK_TARGET is set by the look RunOne of its activity (the "full" one
//     of IDLE, PLAY and MEET, the "minimal" one of WORK and REST), by
//     InteractWith and SocializeAtBell (the villager or cat it goes up to)
//     and by WorkAtPoi (its job site). LookAtTargetSink(45, 90) holds it
//     while the target stays in sight and erases it when it stops.
//     SetWalkTargetFromLookTarget walks the villager to it.
//   - INTERACTION_TARGET is set by InteractWith and SocializeAtBell. In IDLE
//     and MEET a GateBehavior around TradeWithVillager erases it when that
//     stops: at once for a cat, after TradeWithVillager's 60 ticks (or when
//     the villager drops out of sight) for a villager. SocializeAtBell only
//     starts without one; a player being shown trades counts as one too.

const (
	vLookRange      = 8.0 // SetEntityLookTarget(.., 8.0F)
	vLookWalkClose  = 2   // SetWalkTargetFromLookTarget(speedModifier, 2)
	vTradeWithTicks = 60  // TradeWithVillager: a Behavior's default 60-tick run
	vWorkAtTicks    = 60  // WorkAtPoi: the same
	vWorkAtReach    = 1.73
	vLookHoldMin    = 30 // the look RunOne's DoNothing(30, 60)
	vLookHoldMax    = 60
)

// villagerLook is LOOK_TARGET: a mob or player followed (EntityTracker), or
// a block (BlockPosTracker).
type villagerLook struct {
	set     bool
	eid     int32 // the mob or player (0 for a block)
	player  bool
	x, y, z float64 // a block's centre
}

// setLookMob, setLookPlayer and setLookBlock set LOOK_TARGET.
func (m *mob) setLookMob(o *mob) { m.vLook = villagerLook{set: true, eid: o.eid} }

func (m *mob) setLookPlayer(t *tracked) {
	m.vLook = villagerLook{set: true, eid: t.p.eid, player: true}
}

func (m *mob) setLookBlock(p blockPos) {
	m.vLook = villagerLook{set: true, x: float64(p.x) + 0.5, y: float64(p.y) + 0.5, z: float64(p.z) + 0.5}
}

// setInteraction sets INTERACTION_TARGET. TradeWithVillager, next in the
// IDLE and MEET gate, keeps a villager for its 60 ticks; anything else is
// erased as soon as the gate stops.
func (h *hub) setInteraction(m, o *mob) {
	m.vInteract = o.eid
	m.vInteractUntil = h.tick.Load()
	if o.etype == entityVillager {
		m.vInteractUntil += vTradeWithTicks
	}
}

// villagerSeesPlayer: a player in NEAREST_VISIBLE_LIVING_ENTITIES — alive,
// seen by anyone (no spectator), within the follow range and in sight.
func (h *hub) villagerSeesPlayer(m *mob, t *tracked) bool {
	if t == nil || t.dead || t.dim != m.dim || t.gamemode == gmSpectator {
		return false
	}
	r := m.followRange()
	return dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) <= r*r && h.mobSees(m, t)
}

// villagerSeesMob: the same for a mob.
func (h *hub) villagerSeesMob(m, o *mob) bool {
	if o == nil || o == m || o.dying > 0 || o.dim != m.dim {
		return false
	}
	r := m.followRange()
	return dist3sq(o.x, o.y, o.z, m.x, m.y, m.z) <= r*r && h.mobSeesMob(m, o)
}

// villagerLookPos is where LOOK_TARGET is, and whether it is still visible
// (EntityTracker.isVisibleBy; a block always is).
func (h *hub) villagerLookPos(players map[int32]*tracked, m *mob) (x, y, z float64, ok bool) {
	l := m.vLook
	switch {
	case !l.set:
		return 0, 0, 0, false
	case l.eid == 0:
		return l.x, l.y, l.z, true
	case l.player:
		t := players[l.eid]
		if !h.villagerSeesPlayer(m, t) {
			return 0, 0, 0, false
		}
		return t.x, t.y, t.z, true
	}
	o := h.mobs[l.eid]
	if !h.villagerSeesMob(m, o) {
		return 0, 0, 0, false
	}
	return o.x, o.y, o.z, true
}

// lookCategory is the target's MobCategory for SetEntityLookTarget: the
// golems and the villager are MISC (-1), every other mob its spawn category.
func lookCategory(o *mob) int {
	switch o.etype {
	case entityVillager, entityIronGolem, entitySnowGolem, entityCopperGolem:
		return -1
	}
	return mobSpawnCategory(o)
}

// closestVisibleMob is NearestVisibleLivingEntities.findClosest for mobs:
// the nearest within r that pred accepts and the villager can see (sight
// tested nearest first, a bounded number of times).
func (h *hub) closestVisibleMob(m *mob, r float64, pred func(o *mob) bool) *mob {
	var cands []*mob
	h.grid().nearby(m.dim, m.x, m.z, r, func(o *mob) {
		if o != m && o.dying == 0 && pred(o) && dist3sq(o.x, o.y, o.z, m.x, m.y, m.z) <= r*r {
			cands = append(cands, o)
		}
	})
	sort.Slice(cands, func(i, j int) bool {
		di, dj := dist3sq(cands[i].x, cands[i].y, cands[i].z, m.x, m.y, m.z), dist3sq(cands[j].x, cands[j].y, cands[j].z, m.x, m.y, m.z)
		if di != dj {
			return di < dj
		}
		return cands[i].eid < cands[j].eid
	})
	for i, o := range cands {
		if i >= vSeeChecks {
			break
		}
		if h.mobSeesMob(m, o) {
			return o
		}
	}
	return nil
}

// closestVisiblePlayer is the same for players.
func (h *hub) closestVisiblePlayer(players map[int32]*tracked, m *mob, r float64) *tracked {
	var best *tracked
	bestD := r * r
	for _, t := range players {
		if t.dead || t.dim != m.dim || t.gamemode == gmSpectator {
			continue
		}
		if d := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); d <= bestD && h.mobSees(m, t) {
			if best == nil || d < bestD || (d == bestD && t.p.eid < best.p.eid) {
				best, bestD = t, d
			}
		}
	}
	return best
}

// villagerLookTick runs each mob update for a villager awake and not
// trading: LookAtTargetSink, the IDLE/MEET gate's hold on
// INTERACTION_TARGET, and the look RunOne of its activity.
func (h *hub) villagerLookTick(players map[int32]*tracked, m *mob) {
	if m.etype != entityVillager || m.dying > 0 {
		return
	}
	now := h.tick.Load()
	act := h.villagerActivity(m)
	// INTERACTION_TARGET: the GateBehavior(TradeWithVillager) of IDLE and MEET.
	if m.vInteract != 0 && (act == vsRoam || act == vsGather) {
		o := h.mobs[m.vInteract]
		if now >= m.vInteractUntil || o == nil || o.etype != entityVillager || !h.villagerSeesMob(m, o) {
			m.vInteract, m.vInteractUntil = 0, 0
		}
	}
	// LookAtTargetSink(45, 90): starts on a LOOK_TARGET, stops at its end or
	// once the target is out of sight, and erases it.
	if m.vLook.set {
		if m.vLookSink == 0 {
			m.vLookSink = now + uint64(lookSinkMin+h.rng.Intn(lookSinkMax-lookSinkMin+1))
		}
		if _, _, _, ok := h.villagerLookPos(players, m); !ok || now >= m.vLookSink {
			m.vLook, m.vLookSink = villagerLook{}, 0
		}
	} else {
		m.vLookSink = 0
	}
	if m.sleeping || now < m.vLookHold {
		return // asleep, or the RunOne's DoNothing is running
	}
	hold := func() bool {
		m.vLookHold = now + uint64(vLookHoldMin+h.rng.Intn(vLookHoldMax-vLookHoldMin+1))
		return true
	}
	// SetEntityLookTarget: only with no LOOK_TARGET.
	lookMob := func(pred func(o *mob) bool) bool {
		if m.vLook.set {
			return false
		}
		if o := h.closestVisibleMob(m, vLookRange, pred); o != nil {
			m.setLookMob(o)
			return true
		}
		return false
	}
	lookPlayer := func() bool {
		if m.vLook.set {
			return false
		}
		if t := h.closestVisiblePlayer(players, m, vLookRange); t != nil {
			m.setLookPlayer(t)
			return true
		}
		return false
	}
	ofType := func(et int) func(o *mob) bool { return func(o *mob) bool { return o.etype == et } }
	ofCat := func(c int) func(o *mob) bool { return func(o *mob) bool { return lookCategory(o) == c } }
	if act == vsWork || act == vsSleep {
		// getMinimalLookBehavior: VILLAGER 2, PLAYER 2, DoNothing(30, 60) 8.
		h.runOne([]int{2, 2, 8}, func(i int) bool {
			switch i {
			case 0:
				return lookMob(ofType(entityVillager))
			case 1:
				return lookPlayer()
			}
			return hold()
		})
		return
	}
	// getFullLookBehavior: CAT 8, VILLAGER 2, PLAYER 2, the CREATURE,
	// WATER_CREATURE, AXOLOTLS, UNDERGROUND_WATER_CREATURE, WATER_AMBIENT and
	// MONSTER categories 1 each, DoNothing(30, 60) 2.
	cats := []int{catCreature, catWaterCreature, catAxolotls, catUndergroundWater, catWaterAmbient, catMonster}
	h.runOne([]int{8, 2, 2, 1, 1, 1, 1, 1, 1, 2}, func(i int) bool {
		switch {
		case i == 0:
			return lookMob(ofType(entityCat))
		case i == 1:
			return lookMob(ofType(entityVillager))
		case i == 2:
			return lookPlayer()
		case i >= 3 && i <= 8:
			return lookMob(ofCat(cats[i-3]))
		}
		return hold()
	})
}

// walkFromVillagerLook is SetWalkTargetFromLookTarget(speedModifier, 2):
// with no walk and a LOOK_TARGET, a walk to it — following the mob or
// player, or to the block. It reports whether it started.
func (h *hub) walkFromVillagerLook(m *mob, play bool) bool {
	l := m.vLook
	if !l.set || m.vWalk.set {
		return false
	}
	switch {
	case l.eid == 0:
		h.setWalk(m, l.x, l.y, l.z, vSpeedBase, vLookWalkClose, play)
	case l.player:
		t := h.playersRef[l.eid]
		if t == nil {
			return false
		}
		m.vWalk = villagerWalk{set: true, x: t.x, y: t.y, z: t.z, eid: l.eid, player: true,
			speed: vSpeedBase, close: vLookWalkClose, until: h.walkLimit(play)}
	default:
		o := h.mobs[l.eid]
		if o == nil {
			return false
		}
		m.vWalk = villagerWalk{set: true, x: o.x, y: o.y, z: o.z, eid: o.eid,
			speed: vSpeedBase, close: vLookWalkClose, until: h.walkLimit(play)}
	}
	return true
}

// villagerHeadLook points a villager's head for LookAtTargetSink: at its
// LOOK_TARGET, or with the body when it has none. A villager holding up a
// trade to a player keeps the head ShowTradesToPlayer gave it.
func (h *hub) villagerHeadLook(players map[int32]*tracked, m *mob) {
	if m.showTrades.player != 0 {
		return
	}
	if x, _, z, ok := h.villagerLookPos(players, m); ok {
		m.headYaw = yawToward(m.x, m.z, x, z)
		return
	}
	m.headYaw = m.yaw
}
