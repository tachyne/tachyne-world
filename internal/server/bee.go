package server

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// hivesPathFor puts hives.json beside the spawn store's file ("" = in-memory).
func hivesPathFor(spawnPath string) string {
	if spawnPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(spawnPath), "hives.json")
}

// Real hive life (#138 M2): bees pollinate flowers and carry the nectar home,
// hives hold their occupants and fill with honey as nectar is delivered, and
// a sting costs the bee its life — vanilla's Bee/BeehiveBlockEntity numbers,
// run on the hub's 1 Hz survival step (tick constants divided by 20).
//
// SIMPLIFICATIONS, stated plainly: the state machine is seconds-grained; a
// bee's in-flight trip state (nectar, goal) is not persisted — a restart
// sends it foraging afresh — while HIVE OCCUPANTS are (hives.json). Hives are
// known through generation seeding, harvests and persistence, plus a small
// local scan when a homeless bee goes looking; there is no global block
// index to subscribe to.

const (
	beeMaxOccupants   = 3   // BeehiveBlockEntity.MAX_OCCUPANTS
	beeOccupySecs     = 120 // MIN_OCCUPATION_TICKS_NECTAR / 20
	beeOccupyIdleSecs = 30  // MIN_OCCUPATION_TICKS_NECTARLESS / 20
	beeReenterSecs    = 20  // MIN_TICKS_BEFORE_REENTERING_HIVE / 20
	beePollinateSecs  = 20  // ~BeePollinateGoal's successful visit
	beeStingDieSecs   = 60  // STING_DEATH_COUNTDOWN / 20
	beeSeekScan       = 8   // local box scanned for a flower or hive
	beeGoalSpeed      = 0.2 // steer factor toward the current goal
	beeGoalReach      = 1.8 // close enough to a flower / hive front

	// Bee.TOO_FAR_DISTANCE: past this the hive stops counting as its own and
	// the bee drops it (Bee.dropHive), becoming homeless until it finds another.
	beeTooFar = 48.0
	// BeeWanderGoal.getWanderThreshold: 48 - 24 with a hive or a saved flower,
	// 48 - 16 without. Past it, wandering is biased back toward the hive
	// instead of following the bee's nose — the soft leash that keeps a colony
	// near its nest.
	beeWanderLeashHome = 24.0
	beeWanderLeashFree = 32.0
	// BeeGoToHiveGoal: give up after MAX_TRAVELLING_TICKS and blacklist the
	// hive, up to MAX_BLACKLISTED_TARGETS. The counter ticks once per second,
	// with the rest of updateBee's clocks — measuring it in mob-updates made
	// the deadline twenty minutes instead of two.
	beeTravelGiveUp = 2400 / survivalTickN
	beeMaxBanned    = 3
	// The goal only computes a real route once it is CLOSE — further out it
	// just heads the right way (pathfindRandomlyTowards). That is what keeps
	// the node budget honest: A* is never asked to solve a 48-block detour,
	// only the last stretch, where the trees actually are.
	beeDirectRange = 16.0
	// pathfindDirectlyTowards: setMaxVisitedNodesMultiplier(10) — the close-in
	// search is allowed ten times the ordinary budget.
	beeDirectBudget = flyPathBudget * 10

	// Bee.isTiredOfLookingForNectar: 3600 ticks out with nothing to show for
	// it and the bee goes home anyway. Without this a bee somewhere with no
	// flowers never returns at all — it is not night, it has no nectar, so
	// nothing else in the condition ever fires.
	beeTiredSecs = 3600 / 20
	// BeeLocateHiveGoal.start: remainingCooldownBeforeLocatingNewHive = 200,
	// so a homeless bee looks for somewhere to live every ten seconds rather
	// than on every pass. The scan is not cheap and vanilla does not run it hot.
	beeLocateCD = 200 / survivalTickN
	// BeehiveBlockEntity.emptyAllLivingFromHive: robbing a SEDATED hive does
	// not anger its bees, it bars them from going back in for 400 ticks.
	beeStayOutSecs    = 400 / 20
	beeGoalKindNone   = 0
	beeGoalKindFlower = 1
	beeGoalKindHive   = 2
)

// hiveOccupant is one bee inside a hive.
type hiveOccupant struct {
	SecsLeft int  `json:"secs"`
	Nectar   bool `json:"nectar"`
}

// hiveStore persists hive occupants across restarts — hives.json, the same
// atomic-write shape as the spawn store.
type hiveStore struct {
	mu    sync.Mutex
	path  string
	m     map[string][]hiveOccupant
	dirty bool
}

func newHiveStore(path string) *hiveStore {
	s := &hiveStore{path: path, m: map[string][]hiveOccupant{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &s.m)
		}
	}
	return s
}

func hiveKey(p blockPos) string { return fmt.Sprintf("%d,%d,%d", p.x, p.y, p.z) }

func (s *hiveStore) save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.path == "" {
		return
	}
	data, _ := json.MarshalIndent(s.m, "", " ")
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
	s.dirty = false
}

// hivesLoad rebuilds the hub's live hive map from the store at boot.
func (h *hub) hivesLoad() {
	if h.hives == nil {
		h.hives = map[blockPos][]hiveOccupant{}
	}
	if h.hivestore == nil {
		return
	}
	for k, occ := range h.hivestore.m {
		var p blockPos
		if _, err := fmt.Sscanf(k, "%d,%d,%d", &p.x, &p.y, &p.z); err == nil {
			h.hives[p] = occ
		}
	}
}

// hivesMark records the live map into the store (called from the same 1 Hz
// pass that mutates it).
func (h *hub) hivesMark() {
	if h.hivestore == nil {
		return
	}
	h.hivestore.mu.Lock()
	h.hivestore.m = make(map[string][]hiveOccupant, len(h.hives))
	for p, occ := range h.hives {
		h.hivestore.m[hiveKey(p)] = occ
	}
	h.hivestore.dirty = true
	h.hivestore.mu.Unlock()
}

// registerHive makes a hive known to the bees (idempotent).
func (h *hub) registerHive(p blockPos) {
	if h.hives == nil {
		h.hives = map[blockPos][]hiveOccupant{}
	}
	if _, ok := h.hives[p]; !ok {
		h.hives[p] = nil
	}
}

// beeBehavior steers a bee toward its current goal; without one it wanders.
type beeBehavior struct{}

func (beeBehavior) name() string { return "bee" }
func (beeBehavior) steer(h *hub, m *mob) (float64, float64) {
	if m.beeGoalKind != beeGoalKindNone {
		g := m.beeGoal
		if dist3(m.x, m.y, m.z, float64(g.x)+0.5, float64(g.y)+0.5, float64(g.z)+0.5) > beeDirectRange {
			// Still a way out: head the right way and let the cruise altitude
			// stand, exactly as pathfindRandomlyTowards does.
			m.flyClearPath()
			return beeHeadFor(m, g)
		}
		// Close in, where the leaves and the trunk are: fly the real route.
		if h.flyToBudget(m, g, beeDirectBudget) {
			return flySteer(m, beeGoalSpeed*2)
		}
		return 0, 0 // no way through; updateBee blacklists on the next pass
	}
	m.flyClearPath()
	return beeWander(h, m)
}

// beeHeadFor is the long-range half of the errand: point at the goal and go.
func beeHeadFor(m *mob, g blockPos) (float64, float64) {
	dx, dz := float64(g.x)+0.5-m.x, float64(g.z)+0.5-m.z
	if d := math.Hypot(dx, dz); d > 0.01 {
		return dx / d * beeGoalSpeed * 2, dz / d * beeGoalSpeed * 2
	}
	return 0, 0
}

// beeWander is BeeWanderGoal.findPos: ordinary wandering, except that a bee
// which has drifted past its leash has the wander biased back toward the hive.
// It is a pull, not a wall — the bee still meanders, it just meanders home.
func beeWander(h *hub, m *mob) (float64, float64) {
	vx, vz := wanderBehavior{}.steer(h, m)
	if !m.beeHasHome {
		return vx, vz
	}
	leash := beeWanderLeashFree
	if m.beeHasHome {
		leash = beeWanderLeashHome
	}
	dx, dz := float64(m.beeHome.x)+0.5-m.x, float64(m.beeHome.z)+0.5-m.z
	if d := math.Hypot(dx, dz); d > leash && d > 0.01 {
		home := m.moveSpeed()
		return dx / d * home, dz / d * home
	}
	return vx, vz
}

// updateBees is the 1 Hz hive-and-bee pass, replacing the old proximity
// stand-in: honey now rises only when an occupant that came home CARRYING
// NECTAR finishes its stay and leaves.
func (h *hub) updateBees(players map[int32]*tracked) {
	if h.hives == nil {
		h.hives = map[blockPos][]hiveOccupant{}
	}
	day, raining := h.dayLight(), h.raining
	changed := false
	for pos, occ := range h.hives {
		cur := h.world.At(pos.x, pos.y, pos.z)
		if !isBeeHome(cur) {
			// The hive block is gone: its bees tumble out where it stood.
			for range occ {
				h.beeOut(players, pos, false)
			}
			delete(h.hives, pos)
			changed = true
			continue
		}
		for i := 0; i < len(occ); {
			occ[i].SecsLeft--
			changed = true
			if occ[i].SecsLeft > 0 || !day || raining {
				i++
				continue
			}
			if occ[i].Nectar {
				if lvl := honeyLevel(cur); lvl < beeMaxHoney {
					h.setBlockAt(players, 0, pos, withHoney(cur, lvl+1))
					cur = h.world.At(pos.x, pos.y, pos.z)
				}
			}
			h.beeOut(players, pos, false)
			occ = append(occ[:i], occ[i+1:]...)
		}
		h.hives[pos] = occ
	}
	for _, m := range h.mobs {
		if m.etype != entityBee || m.dim != 0 || m.dying > 0 {
			continue
		}
		h.updateBee(players, m, day, raining)
	}
	if changed {
		h.hivesMark()
	}
	if h.hivestore != nil {
		h.hivestore.save()
	}
}

// beeOut spawns a bee just outside a hive, fresh from inside.
func (h *hub) beeOut(players map[int32]*tracked, pos blockPos, angry bool) *mob {
	m := h.spawnAnimal(players, entityBee, pos.x, pos.z)
	if m == nil {
		return nil
	}
	m.x, m.y, m.z = float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+1.2 // the south face
	m.beeHome, m.beeHasHome = pos, true
	m.beeNoEnter = beeReenterSecs
	if angry {
		m.anger = beeAngerSecs
	}
	return m
}

// updateBee runs one bee's second: sting death, pollination, and the trip
// home with nectar (or out of the rain and the dark).
func (h *hub) updateBee(players map[int32]*tracked, m *mob, day, raining bool) {
	if m.beeStingDie > 0 {
		if m.beeStingDie--; m.beeStingDie <= 0 {
			h.hurtMobOf(players, m, float64(m.health), dtGeneric)
			return
		}
	}
	if m.beeNoEnter > 0 {
		m.beeNoEnter--
	}
	if m.beeStayOut > 0 {
		m.beeStayOut--
	}
	// customServerAiStep: the give-up clock runs whenever the bee is
	// empty-handed, and setHasNectar resets it the moment it is not.
	if m.beeNectar {
		m.beeNoNectar = 0
	} else {
		m.beeNoNectar++
	}
	h.syncBeeLook(players, m)
	// The pollen coat at work — even while flying home, and regardless of a
	// fight: the nectar drips (vanilla aiStep, 5% per tick) and crops below
	// get a boost until ten have grown this trip.
	if m.beeNectar && m.beeCropsGrown < beeCropBoostMax {
		drips := int32(0)
		for i := 0; i < 20; i++ {
			if h.rng.Float64() < 0.05 {
				drips++
			}
		}
		if drips > 0 {
			h.spawnParticles(players, particleNectar, m.x, m.y+0.4, m.z, 0.3, 0, drips)
		}
		if m.beeHasHome && isBeeHome(h.world.At(m.beeHome.x, m.beeHome.y, m.beeHome.z)) &&
			h.rng.Float64() < beeCropBoostProb {
			h.beeGrowCropsBelow(players, m)
		}
	}
	if m.anger > 0 || m.panic > 0 {
		m.beeGoalKind = beeGoalKindNone
		return // fighting or fleeing outranks the errand
	}
	// Hovering at a flower.
	if m.beePollinate > 0 {
		if !worldgen.IsFlower(h.world.At(m.beeGoal.x, m.beeGoal.y, m.beeGoal.z)) {
			m.beePollinate, m.beeGoalKind = 0, beeGoalKindNone
			return // the flower is gone
		}
		if m.beePollinate--; m.beePollinate <= 0 {
			m.beeNectar = true
			m.beeGoalKind = beeGoalKindNone
		}
		return
	}
	// Bee.aiStep: a hive left far behind stops being this bee's hive at all.
	if m.beeHasHome && dist3(m.x, m.y, m.z,
		float64(m.beeHome.x)+0.5, float64(m.beeHome.y)+0.5, float64(m.beeHome.z)+0.5) > beeTooFar {
		m.beeHasHome, m.beeTravel = false, 0
		m.flyClearPath()
	}
	// Home time. Bee.wantsToEnterHive is a good deal more than "has nectar":
	// a bee that has been out too long gives up and goes back, and one that is
	// mid-pollination, dying of its sting, angry, or barred after a robbery
	// does not go at all — nor does any bee whose hive is on fire.
	if h.beeWantsHive(m, day, raining) {
		// Bee.isHiveValid: a hive stops being one when the block is gone. Being
		// FULL is not the same thing and must not drop it here — that is what
		// sent a bee with nectar wandering off instead of flying home.
		if m.beeHasHome && !isBeeHome(h.world.At(m.beeHome.x, m.beeHome.y, m.beeHome.z)) {
			m.beeHasHome, m.beeTravel = false, 0
			m.flyClearPath()
		}
		// BeeLocateHiveGoal.canBeeUse: only a bee with NO hive goes looking, and
		// only once every remainingCooldownBeforeLocatingNewHive. Searching every
		// second also meant running the local block scan every second.
		if !m.beeHasHome {
			if m.beeLocateCD > 0 {
				m.beeLocateCD--
			} else {
				m.beeLocateCD = beeLocateCD
				if p, ok := h.findHiveFor(m); ok {
					m.beeHome, m.beeHasHome, m.beeTravel = p, true, 0
				}
			}
		}
		if m.beeHasHome {
			// BeeGoToHiveGoal.canBeeUse does not care whether the hive is full:
			// the bee flies home either way and finds out when it gets there.
			m.beeGoal, m.beeGoalKind = m.beeHome, beeGoalKindHive
			if m.beeNoEnter <= 0 && h.beeNear(m, m.beeHome) {
				// BeeEnterHiveGoal.canBeeUse: arriving to find the hive full
				// drops it (hivePos = null) — the bee does not queue. It is not
				// blacklisted: a busy hive is a perfectly good hive, so the
				// locate cooldown lets it try again rather than writing it off.
				if !h.hiveHasRoom(m.beeHome) {
					m.beeHasHome, m.beeTravel = false, 0
					m.beeGoalKind = beeGoalKindNone
					m.flyClearPath()
					return
				}
				h.enterHive(players, m)
				return
			}
			// BeeGoToHiveGoal.tick: the trip has a deadline, and a hive that
			// cannot be routed to FROM CLOSE UP is written off. Failing to
			// path from far away means nothing — the goal does not even try
			// until it is inside beeDirectRange.
			near := dist3(m.x, m.y, m.z, float64(m.beeHome.x)+0.5,
				float64(m.beeHome.y)+0.5, float64(m.beeHome.z)+0.5) <= beeDirectRange
			if m.beeTravel++; m.beeTravel > beeTravelGiveUp ||
				(near && !h.beeCanReach(m, m.beeHome)) {
				h.beeBanHive(m, m.beeHome)
			}
		} else {
			m.beeGoalKind = beeGoalKindNone
		}
		return
	}
	// Foraging: on a goal, close the distance; otherwise sometimes go look.
	if m.beeGoalKind == beeGoalKindFlower {
		if h.beeNear(m, m.beeGoal) {
			m.beePollinate = beePollinateSecs
		} else if !worldgen.IsFlower(h.world.At(m.beeGoal.x, m.beeGoal.y, m.beeGoal.z)) {
			m.beeGoalKind = beeGoalKindNone
			m.flyClearPath()
		}
		return
	}
	if h.rng.Intn(4) == 0 {
		if p, ok := h.findFlowerFor(m); ok {
			m.beeGoal, m.beeGoalKind = p, beeGoalKindFlower
		}
	}
}

// beeWantsHive is Bee.wantsToEnterHive. The first four are hard refusals —
// whatever else is true, a bee doing one of these is not going home — and the
// rest are the reasons it would want to.
func (h *hub) beeWantsHive(m *mob, day, raining bool) bool {
	if m.beeStayOut > 0 || m.beePollinate > 0 || m.beeStingDie > 0 || m.anger > 0 {
		return false
	}
	// isNightOrRaining is measured on the SKY, so it is the overworld's
	// business: a dimension with no sky never sends its bees in on the hour.
	dark := m.dim == dimOverworld && (!day || raining)
	want := m.beeNoNectar > beeTiredSecs || dark || m.beeNectar
	return want && !h.hiveOnFire(m)
}

// hiveOnFire is BeehiveBlockEntity.isFireNearby: an actual fire block in the
// 3x3x3 around the hive. NOT a campfire — a campfire under a hive sedates it,
// which is a different rule living in campfireUnder.
func (h *hub) hiveOnFire(m *mob) bool {
	if !m.beeHasHome {
		return false
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if isFire(w.At(m.beeHome.x+dx, m.beeHome.y+dy, m.beeHome.z+dz)) {
					return true
				}
			}
		}
	}
	return false
}

// beeCanReach reports whether a route to the goal exists right now. The path
// is computed by the steer as the bee flies; this only asks whether the last
// attempt found anything, so it costs nothing extra.
func (h *hub) beeCanReach(m *mob, goal blockPos) bool {
	return m.flyGoal != goal || len(m.flyPath) > 0
}

// beeBanHive is BeeGoToHiveGoal's blacklist: give up on a hive that cannot be
// reached and look for another. Vanilla remembers three; a fourth pushes the
// oldest out.
func (h *hub) beeBanHive(m *mob, pos blockPos) {
	for _, p := range m.beeBanned {
		if p == pos {
			return
		}
	}
	m.beeBanned = append(m.beeBanned, pos)
	if n := len(m.beeBanned) - beeMaxBanned; n > 0 {
		m.beeBanned = append(m.beeBanned[:0], m.beeBanned[n:]...)
	}
	m.beeHasHome, m.beeTravel, m.beeGoalKind = false, 0, beeGoalKindNone
	m.flyClearPath()
}

// beeHiveBanned reports whether this bee has written a hive off.
func (m *mob) beeHiveBanned(p blockPos) bool {
	for _, b := range m.beeBanned {
		if b == p {
			return true
		}
	}
	return false
}

func (h *hub) beeNear(m *mob, p blockPos) bool {
	return math.Hypot(float64(p.x)+0.5-m.x, float64(p.z)+0.5-m.z) <= beeGoalReach &&
		math.Abs(float64(p.y)+0.5-m.y) <= 2
}

func (h *hub) hiveHasRoom(p blockPos) bool {
	if !isBeeHome(h.world.At(p.x, p.y, p.z)) {
		return false
	}
	return len(h.hives[p]) < beeMaxOccupants
}

// enterHive folds a live bee into the hive's occupant list.
func (h *hub) enterHive(players map[int32]*tracked, m *mob) {
	stay := beeOccupyIdleSecs
	if m.beeNectar {
		stay = beeOccupySecs
	}
	h.registerHive(m.beeHome)
	m.beeTravel, m.beeBanned, m.beeNoNectar = 0, nil, 0
	m.flyClearPath()
	h.hives[m.beeHome] = append(h.hives[m.beeHome], hiveOccupant{SecsLeft: stay, Nectar: m.beeNectar})
	h.hivesMark()
	h.removeMob(players, m)
}

// findHiveFor looks for a home: any registered hive with room within reach,
// else a small local scan (a player-built hive the registry has not met).
//
// Only hives with SPACE are candidates — that much is
// BeeLocateHiveGoal.findNearbyHivesWithSpace, and it is the one place fullness
// belongs. A bee that already has a hive keeps it even once it fills up.
func (h *hub) findHiveFor(m *mob) (blockPos, bool) {
	bestD, banD := math.MaxFloat64, math.MaxFloat64
	var best, ban blockPos
	found, banned := false, false
	for p := range h.hives {
		if len(h.hives[p]) >= beeMaxOccupants || !isBeeHome(h.world.At(p.x, p.y, p.z)) {
			continue
		}
		d := dist3(m.x, m.y, m.z, float64(p.x), float64(p.y), float64(p.z))
		if d > 48 {
			continue
		}
		if m.beeHiveBanned(p) {
			if d < banD { // remembered separately: it is still somewhere to live
				banD, ban, banned = d, p, true
			}
			continue
		}
		if d < bestD {
			bestD, best, found = d, p, true
		}
	}
	if found {
		return best, true
	}
	// BeeLocateHiveGoal.start: when every hive with space is blacklisted, the
	// blacklist is CLEARED and the nearest taken anyway. Without that a bee
	// whose only hive it once failed to reach is homeless for ever, even after
	// whatever blocked the way is gone.
	if banned {
		m.beeBanned = nil
		return ban, true
	}
	bx, by, bz := int(m.x), int(m.y), int(m.z)
	for dy := -beeSeekScan; dy <= beeSeekScan; dy++ {
		for dx := -beeSeekScan; dx <= beeSeekScan; dx++ {
			for dz := -beeSeekScan; dz <= beeSeekScan; dz++ {
				p := blockPos{bx + dx, by + dy, bz + dz}
				if isBeeHome(h.world.At(p.x, p.y, p.z)) && len(h.hives[p]) < beeMaxOccupants &&
					!m.beeHiveBanned(p) {
					h.registerHive(p)
					return p, true
				}
			}
		}
	}
	return blockPos{}, false
}

// findFlowerFor looks for a #flowers block to work: the nearest within five
// blocks (BeePollinateGoal's reach, scanned exhaustively), then a handful of
// wider random samples for the stragglers.
func (h *hub) findFlowerFor(m *mob) (blockPos, bool) {
	bx, by, bz := int(m.x), int(m.y), int(m.z)
	bestD := math.MaxFloat64
	var best blockPos
	found := false
	for dy := -3; dy <= 3; dy++ {
		for dx := -5; dx <= 5; dx++ {
			for dz := -5; dz <= 5; dz++ {
				p := blockPos{bx + dx, by + dy, bz + dz}
				if !worldgen.IsFlower(h.world.At(p.x, p.y, p.z)) {
					continue
				}
				if d := float64(dx*dx + dy*dy + dz*dz); d < bestD {
					bestD, best, found = d, p, true
				}
			}
		}
	}
	if found {
		return best, true
	}
	for i := 0; i < 24; i++ {
		p := blockPos{bx + h.rng.Intn(2*beeSeekScan+1) - beeSeekScan,
			by + h.rng.Intn(9) - 4,
			bz + h.rng.Intn(2*beeSeekScan+1) - beeSeekScan}
		if worldgen.IsFlower(h.world.At(p.x, p.y, p.z)) {
			return p, true
		}
	}
	return blockPos{}, false
}

// robHive throws every occupant out angry at the robber — the price of
// harvesting without smoke.
func (h *hub) robHive(players map[int32]*tracked, t *tracked, pos blockPos) {
	h.releaseHiveBees(players, pos, t)
}

// releaseHiveBees empties a hive's occupants into the world: angry at t when a
// robbery or a bare break sets them off, calm when t is nil — a dispenser's
// shears or bottle has nobody to blame (BeeReleaseStatus.BEE_RELEASED).
func (h *hub) releaseHiveBees(players map[int32]*tracked, pos blockPos, t *tracked) {
	occ := h.hives[pos]
	for range occ {
		if m := h.beeOut(players, pos, t != nil); m != nil && t != nil {
			h.provoke(m, t)
		}
	}
	if len(occ) > 0 {
		h.hives[pos] = nil
		h.hivesMark()
	}
}

// The client-visible bee: vanilla syncs a flags byte (bit 2 rolling, bit 4
// stung, bit 8 nectar — the pollen coat on the texture) and the remaining
// anger time (>0 = red eyes) as entity metadata. Both are diffed once a
// second in syncBeeLook, so every code path that changes the state is covered.
const (
	metaIndexBeeFlags = 17 // Bee DATA_FLAGS_ID (byte), after AgeableMob's baby(16)
	metaIndexBeeAnger = 18 // Bee DATA_REMAINING_ANGER_TIME (VarInt)
	beeFlagStung      = 0x04
	beeFlagNectar     = 0x08
)

func beeFlagsMeta(eid int32, flags uint8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexBeeFlags)
	b = protocol.AppendVarInt(b, 0) // type: byte
	b = protocol.AppendU8(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}

func beeAngerMeta(eid int32, anger int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexBeeAnger)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(anger))
	return protocol.AppendU8(b, itemMetaEnd)
}

// syncBeeLook sends the appearance metadata when it changed since last send.
func (h *hub) syncBeeLook(players map[int32]*tracked, m *mob) {
	flags := uint8(0)
	if m.beeStingDie > 0 {
		flags |= beeFlagStung
	}
	if m.beeNectar {
		flags |= beeFlagNectar
	}
	if flags != m.beeSentFlags {
		m.beeSentFlags = flags
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(beeFlagsMeta(m.eid, flags)))
	}
	if angry := m.anger > 0; angry != m.beeSentAngry {
		m.beeSentAngry = angry
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(beeAngerMeta(m.eid, m.anger)))
	}
}

// Crop pollination (BeeGrowCropGoal): a pollen-laden bee with a valid hive
// boosts #bee_growables it flies over, up to ten crops per trip. The goal is
// flag-free in vanilla, so it runs concurrently with the flight home.
const (
	beeCropBoostMax = 10 // NumCropsGrownSincePollination budget
	// Vanilla attempts the two-below check once per ~30 active ticks, and the
	// goal is up ~70% of eligible time (canBeeUse refuses at random 30%):
	// per second that is 1-(1-0.7/30)^20 ≈ 0.376.
	beeCropBoostProb = 0.376
)

// beeGrowCropsBelow ports BeeGrowCropGoal.tick: each of the two cells under
// the bee advances one stage if it holds a #bee_growables block — crops and
// stems age, a berry bush ripens, cave vines grow their glow berries. Pitcher
// crops are in the tag but are not CropBlocks, so vanilla (and this) skips them.
func (h *hub) beeGrowCropsBelow(players map[int32]*tracked, m *mob) {
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for i := 1; i <= 2; i++ {
		p := blockPos{bx, by - i, bz}
		next, ok := beeGrowTarget(h.world.At(p.x, p.y, p.z))
		if !ok {
			continue
		}
		h.setBlockAt(players, 0, p, next)
		// Level event 2011: the client shows a green burst over the plant.
		h.spawnParticles(players, particleHappyVillager,
			float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, 0.5, 0, 15)
		m.beeCropsGrown++
	}
}

var (
	torchflowerBlock                   = worldgen.BlockBase("torchflower")
	caveVinesLo, caveVinesHi           = worldgen.BlockRange("cave_vines")
	caveVinesPlantLo, caveVinesPlantHi = worldgen.BlockRange("cave_vines_plant")
)

// beeGrowTarget maps a #bee_growables state to its one-stage-grown form.
func beeGrowTarget(s uint32) (uint32, bool) {
	for _, r := range cropRanges { // wheat / carrots / potatoes / beetroots
		if s >= r[0] && s < r[1] {
			return s + 1, true
		}
	}
	// Torchflower: getMaxAge is 2 though AGE stops at 1, so the step past the
	// last crop state swaps in the flower itself (TorchflowerCropBlock).
	if s >= torchflowerCropMin && s < torchflowerCropMax {
		return s + 1, true
	}
	if s == torchflowerCropMax {
		return torchflowerBlock, true
	}
	if (s >= melonStemBase && s < melonStemBase+7) ||
		(s >= pumpkinStemBase && s < pumpkinStemBase+7) {
		return s + 1, true
	}
	if s >= berryBase && s < berryBase+3 {
		return s + 1, true
	}
	return caveVineBerried(s)
}

// caveVineBerried returns the berries=true form of a berry-less cave-vine
// state; berries=true is the first of each pair (see vines.go headAt).
func caveVineBerried(s uint32) (uint32, bool) {
	switch {
	case s >= caveVinesLo && s <= caveVinesHi:
		if (s-caveVinesLo)%2 == 1 {
			return s - 1, true
		}
	case s >= caveVinesPlantLo && s <= caveVinesPlantHi:
		if (s-caveVinesPlantLo)%2 == 1 {
			return s - 1, true
		}
	}
	return s, false
}

// Silk-Touched hives travel with their bees (BeehiveBlock.playerDestroy +
// the silk loot path): the occupants and honey level ride the dropped item
// under a hiveID — the shulker-box indirection — and a later placement
// restores them. Persisted with the box contents in containers.json.

// hiveStow is what one carried hive item holds.
type hiveStow struct {
	Honey int            `json:"honey,omitempty"`
	Occ   []hiveOccupant `json:"occ,omitempty"`
}

// stowHiveItem moves a broken hive's occupants + honey level under a fresh
// hiveID for the dropped item to carry (0 = nothing worth carrying).
func (h *hub) stowHiveItem(pos blockPos, honey int) int32 {
	occ := h.hives[pos]
	delete(h.hives, pos)
	h.hivesMark()
	if len(occ) == 0 && honey == 0 {
		return 0
	}
	if h.hiveItems == nil {
		h.hiveItems = map[int32]hiveStow{}
	}
	h.nextHiveID++
	h.hiveItems[h.nextHiveID] = hiveStow{Honey: honey, Occ: occ}
	return h.nextHiveID
}

// dropBeeHome replaces the ordinary loot path for a broken hive or nest:
// Silk Touch keeps the bees and honey on the dropped item; anything else
// spills the occupants angry at the breaker — and a bee nest without Silk
// Touch drops nothing at all (its loot table is silk-only).
func (h *hub) dropBeeHome(players map[int32]*tracked, by int32, state uint32, pos blockPos) {
	t := players[by]
	item := int32(itemByName["beehive"])
	if beeHomeBase(state) == beeNestMin {
		item = int32(itemByName["bee_nest"])
	}
	if t != nil && heldStack(t).enchLvl(enchSilkTouch) > 0 {
		if it := h.spawnItem(players, item, 1, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5); it != nil {
			it.hiveID = h.stowHiveItem(pos, honeyLevel(state))
		}
		return
	}
	// EMERGENCY release. Whether the occupants come out ANGRY is the smoke's
	// decision (BeehiveBlockEntity.emptyAllLivingFromHive): a sedated hive's
	// bees have nobody to blame, and are simply barred from going back in
	// while they settle.
	if sedated := h.campfireUnder(pos); sedated {
		h.releaseHiveBees(players, pos, nil)
		for _, m := range h.mobs {
			if m.etype == entityBee && m.beeHasHome && m.beeHome == pos {
				m.beeStayOut = beeStayOutSecs
			}
		}
	} else {
		h.releaseHiveBees(players, pos, t)
		if t != nil {
			h.angerBees(players, t, pos)
		}
	}
	if beeHomeBase(state) != beeNestMin {
		h.spawnItem(players, item, 1, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
	}
}

// restoreBeeHome is the other half: a placed hive takes back the bees and
// honey its stack carried.
func (h *hub) restoreBeeHome(players map[int32]*tracked, pos blockPos, hiveID int32) {
	if hiveID == 0 {
		return
	}
	st, ok := h.hiveItems[hiveID]
	if !ok {
		return
	}
	delete(h.hiveItems, hiveID)
	h.registerHive(pos)
	if len(st.Occ) > 0 {
		h.hives[pos] = append(h.hives[pos], st.Occ...)
		h.hivesMark()
	}
	if cur := h.world.At(pos.x, pos.y, pos.z); isBeeHome(cur) && st.Honey > 0 {
		h.setBlockAt(players, 0, pos, withHoney(cur, st.Honey))
	}
}

// isBeeFood is the #bee_food item tag — bees court over any flower.
func isBeeFood(item int32) bool {
	if item == 0 {
		return false
	}
	for _, n := range beeFoodNames {
		if item == int32(itemByName[n]) {
			return true
		}
	}
	return false
}

var beeFoodNames = []string{
	"allium", "azure_bluet", "blue_orchid", "cactus_flower", "cherry_leaves",
	"chorus_flower", "cornflower", "dandelion", "flowering_azalea",
	"flowering_azalea_leaves", "lilac", "lily_of_the_valley",
	"mangrove_propagule", "open_eyeblossom", "orange_tulip", "oxeye_daisy",
	"peony", "pink_petals", "pink_tulip", "pitcher_plant", "poppy",
	"red_tulip", "rose_bush", "spore_blossom", "sunflower", "torchflower",
	"white_tulip", "wildflowers", "wither_rose",
}
