package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A village's iron golem, as vanilla spawns it (Villager.spawnGolemIfNeeded).
// Two things ask for one:
//   - two villagers gossiping (Villager.gossip) ask with five: five villagers
//     within ten blocks who slept within a day and have not seen a golem
//     lately;
//   - a panicking villager (VillagerPanicTrigger) asks with three, once every
//     hundred ticks, while it is hurt or a hostile is near.
// The golem is placed as SpawnUtil.trySpawnMob places it (LEGACY_IRON_GOLEM):
// ten tries ±8 blocks around the villager, each scanning from six above to
// six below for a solid floor with air or liquid over it, then the golem's
// own obstruction check. It used to be dropped at the villager's own height
// with no floor at all, into walls and into the air.

const (
	golemPanicAgree     = 3   // VillagerPanicTrigger's villagersNeededToAgree
	golemPanicEvery     = 100 // …tried when timestamp % 100 == 0
	golemSpawnTries     = 10  // SpawnUtil.trySpawnMob attempts
	golemSpawnRangeXZ   = 8
	golemSpawnRangeY    = 6
	villagerSensorRange = 16.0 // NEAREST_LIVING_ENTITIES: what the hostiles sensor sees
)

// villagerHostileRange is VillagerHostilesSensor.ACCEPTABLE_DISTANCE_FROM_HOSTILES:
// the mobs a villager panics at, and how close each must be.
var villagerHostileRange = map[int]float64{
	entityDrowned:        8,
	entityEvoker:         12,
	entityHusk:           8,
	entityIllusioner:     12,
	entityPillager:       15,
	entityRavager:        12,
	entityVex:            8,
	entityVindicator:     10,
	entityZoglin:         10,
	entityZombie:         8,
	entityZombieVillager: 8,
}

// villagerNearestHostile is the NEAREST_HOSTILE memory: the closest mob on
// the list inside its own distance.
func (h *hub) villagerNearestHostile(m *mob) *mob {
	var best *mob
	bestD := villagerSensorRange + 1
	h.grid().nearby(m.dim, m.x, m.z, villagerSensorRange, func(o *mob) {
		r, ok := villagerHostileRange[o.etype]
		if !ok || o.dying > 0 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d <= r && d < bestD {
			best, bestD = o, d
		}
	})
	return best
}

// villagerPanicking is VillagerPanicTrigger's condition: hurt, or a hostile near.
func (h *hub) villagerPanicking(m *mob) bool {
	return m.panic > 0 || h.villagerNearestHostile(m) != nil
}

// spawnGolemIfNeeded is Villager.spawnGolemIfNeeded for the villager m.
func (h *hub) spawnGolemIfNeeded(players map[int32]*tracked, m *mob, need int) {
	now := h.tick.Load()
	if !h.wantsToSpawnGolem(m, now) {
		return
	}
	var box []*mob
	agree := 0
	h.grid().nearby(m.dim, m.x, m.z, golemAgreeBox*1.5, func(o *mob) {
		if o.etype != entityVillager || o.dying > 0 {
			return
		}
		if abs64(o.x-m.x) > golemAgreeBox || abs64(o.y-m.y) > golemAgreeBox || abs64(o.z-m.z) > golemAgreeBox {
			return
		}
		box = append(box, o)
		if agree < golemVillagersToAgree && h.wantsToSpawnGolem(o, now) {
			agree++ // vanilla limits the stream to five
		}
	})
	if agree < need {
		return
	}
	if h.placeVillageGolem(players, m) == nil {
		return
	}
	for _, o := range box { // GolemSensor.golemDetected for every villager in the box
		o.golemSeen = now + golemDetectedTicks
	}
}

// placeVillageGolem is SpawnUtil.trySpawnMob(IRON_GOLEM, MOB_SUMMONED, 10, 8,
// 6, LEGACY_IRON_GOLEM): nil if no try found a place.
func (h *hub) placeVillageGolem(players map[int32]*tracked, m *mob) *mob {
	w := h.worldFor(m.dim)
	sx, sy, sz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for i := 0; i < golemSpawnTries; i++ {
		x := sx + h.rng.Intn(2*golemSpawnRangeXZ+1) - golemSpawnRangeXZ
		z := sz + h.rng.Intn(2*golemSpawnRangeXZ+1) - golemSpawnRangeXZ
		if !w.Ticking(int32(x>>4), int32(z>>4)) {
			continue // never generate a chunk here
		}
		// moveToPossibleSpawnPosition: from six above, step down to six below.
		y := sy + golemSpawnRangeY
		above := w.At(x, y, z)
		found := false
		for dy := golemSpawnRangeY; dy >= -golemSpawnRangeY; dy-- {
			y--
			cur := w.At(x, y, z)
			if golemFloor(cur, above) {
				y++
				found = true
				break
			}
			above = cur
		}
		if !found || !golemUnobstructed(w.At(x, y-1, z), w.At(x, y, z), w.At(x, y+1, z), w.At(x, y+2, z)) {
			continue
		}
		g := h.spawnMob(players, entityIronGolem, float64(x)+0.5, float64(y), float64(z)+0.5)
		if g == nil {
			return nil // plugin-cancelled spawn
		}
		g.health = 100
		g.setKBResist(1) // IronGolem KNOCKBACK_RESISTANCE
		g.behavior = golemBehavior{}
		g.home = blockPos{sx, sy, sz}
		return g
	}
	return nil
}

// golemFloor is SpawnUtil.Strategy.LEGACY_IRON_GOLEM: a solid block (or
// powder snow) that is not glass, a pane, leaves, ice, cactus, cobweb, TNT or
// a light block, with air or liquid above it.
func golemFloor(floor, above uint32) bool {
	if golemFloorExcluded[floor] || worldgen.IsLeaves(floor) {
		return false
	}
	if !(above == worldgen.Air || worldgen.IsFluid(above)) {
		return false
	}
	return worldgen.Collides(floor) || floor == powderSnowState
}

// golemUnobstructed is IronGolem.checkSpawnObstruction: something to stand on
// below, and three clear cells for the body (no collision; the two above the
// feet also free of fluid).
func golemUnobstructed(below, feet, body, head uint32) bool {
	if !worldgen.Collides(below) {
		return false
	}
	clear := func(s uint32, fluidOK bool) bool {
		return !worldgen.Collides(s) && (fluidOK || !worldgen.IsFluid(s))
	}
	return clear(feet, true) && clear(body, false) && clear(head, false)
}

var powderSnowState = worldgen.BlockBase("powder_snow")

var golemFloorExcluded = func() map[uint32]bool {
	m := map[uint32]bool{}
	for _, name := range []string{
		"cobweb", "cactus", "glass_pane", "conduit", "ice", "tnt", "glowstone",
		"beacon", "sea_lantern", "frosted_ice", "tinted_glass", "glass",
	} {
		if lo, hi, ok := worldgen.BlockRangeOK(name); ok {
			for s := lo; s <= hi; s++ {
				m[s] = true
			}
		}
	}
	for _, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
		for _, suffix := range []string{"_stained_glass", "_stained_glass_pane"} {
			if lo, hi, ok := worldgen.BlockRangeOK(c + suffix); ok {
				for s := lo; s <= hi; s++ {
					m[s] = true
				}
			}
		}
	}
	return m
}()
