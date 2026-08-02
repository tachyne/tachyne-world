package server

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

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
		dx := float64(m.beeGoal.x) + 0.5 - m.x
		dz := float64(m.beeGoal.z) + 0.5 - m.z
		if math.Hypot(dx, dz) > beeGoalReach {
			return dx * beeGoalSpeed, dz * beeGoalSpeed
		}
		return 0, 0
	}
	return wanderBehavior{}.steer(h, m)
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
	// Home time: carrying nectar, or night, or rain.
	if m.beeNectar || !day || raining {
		if !m.beeHasHome || !h.hiveHasRoom(m.beeHome) {
			if p, ok := h.findHiveFor(m); ok {
				m.beeHome, m.beeHasHome = p, true
			}
		}
		if m.beeHasHome && h.hiveHasRoom(m.beeHome) {
			m.beeGoal, m.beeGoalKind = m.beeHome, beeGoalKindHive
			h.beeApproach(m)
			if m.beeNoEnter <= 0 && h.beeNear(m, m.beeHome) {
				h.enterHive(players, m)
			}
		} else {
			m.beeGoalKind = beeGoalKindNone
		}
		return
	}
	// Foraging: on a goal, close the distance; otherwise sometimes go look.
	if m.beeGoalKind == beeGoalKindFlower {
		h.beeApproach(m)
		if h.beeNear(m, m.beeGoal) {
			m.beePollinate = beePollinateSecs
		} else if !worldgen.IsFlower(h.world.At(m.beeGoal.x, m.beeGoal.y, m.beeGoal.z)) {
			m.beeGoalKind = beeGoalKindNone
		}
		return
	}
	if h.rng.Intn(4) == 0 {
		if p, ok := h.findFlowerFor(m); ok {
			m.beeGoal, m.beeGoalKind = p, beeGoalKindFlower
		}
	}
}

// beeApproach nudges a flying bee's altitude toward its goal — the steer
// primitive covers the ground plane, this covers the tree.
func (h *hub) beeApproach(m *mob) {
	dy := float64(m.beeGoal.y) + 0.5 - m.y
	switch {
	case dy > 0.3:
		m.y += math.Min(dy, 0.5)
	case dy < -0.3:
		m.y += math.Max(dy, -0.5)
	}
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
	h.hives[m.beeHome] = append(h.hives[m.beeHome], hiveOccupant{SecsLeft: stay, Nectar: m.beeNectar})
	h.hivesMark()
	h.removeMob(players, m)
}

// findHiveFor looks for a home: any registered hive with room within reach,
// else a small local scan (a player-built hive the registry has not met).
func (h *hub) findHiveFor(m *mob) (blockPos, bool) {
	bestD := math.MaxFloat64
	var best blockPos
	found := false
	for p := range h.hives {
		if len(h.hives[p]) >= beeMaxOccupants || !isBeeHome(h.world.At(p.x, p.y, p.z)) {
			continue
		}
		d := dist3(m.x, m.y, m.z, float64(p.x), float64(p.y), float64(p.z))
		if d < bestD && d <= 48 {
			bestD, best, found = d, p, true
		}
	}
	if found {
		return best, true
	}
	bx, by, bz := int(m.x), int(m.y), int(m.z)
	for dy := -beeSeekScan; dy <= beeSeekScan; dy++ {
		for dx := -beeSeekScan; dx <= beeSeekScan; dx++ {
			for dz := -beeSeekScan; dz <= beeSeekScan; dz++ {
				p := blockPos{bx + dx, by + dy, bz + dz}
				if isBeeHome(h.world.At(p.x, p.y, p.z)) && len(h.hives[p]) < beeMaxOccupants {
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
	occ := h.hives[pos]
	for range occ {
		if m := h.beeOut(players, pos, true); m != nil {
			h.provoke(m, t)
		}
	}
	if len(occ) > 0 {
		h.hives[pos] = nil
		h.hivesMark()
	}
}
