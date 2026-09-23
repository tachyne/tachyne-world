package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// poiDue loads a villager's surroundings, as a player's view would, and makes
// each of its AcquirePoi runs due now.
func poiDue(h *hub, m *mob) {
	h.worldFor(m.dim).ForceLoad(floorInt(m.x), floorInt(m.z), 3)
	for g := range m.poiAt {
		m.poiAt[g] = 1
	}
	if h.tick.Load() < 1 {
		h.tick.Store(1)
	}
}

// freeBed is a bed's head half, nobody in it, facing east (foot to the west).
func freeBed() uint32 {
	s := worldgen.BlockBase("red_bed")
	info, _ := worldgen.InfoForState(s)
	s = worldgen.SetProperty(info, s, "part", "head")
	s = worldgen.SetProperty(info, s, "occupied", "false")
	return worldgen.SetProperty(info, s, "facing", "east")
}

// poiFloor lays a stone floor in open air around (cx, cz) at y=179.
func poiFloor(h *hub, cx, cz, r int) {
	for x := cx - r; x <= cx+r; x++ {
		for z := cz - r; z <= cz+r; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			h.world.SetBlock(x, 180, z, worldgen.Air)
			h.world.SetBlock(x, 181, z, worldgen.Air)
		}
	}
}

// AcquirePoi(MEETING): a bell 40 blocks off (beyond the old ±16 scan) is
// found and claimed; breaking it lets the claim go.
func TestVillagerClaimsABellWithinFortyEight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	poiFloor(h, 20, 0, 26)
	m := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	bell := blockPos{40, 180, 0}
	h.world.SetBlock(bell.x, bell.y, bell.z, bellDefault)
	poiDue(h, m)
	h.world.ForceLoad(40, 0, 2)
	h.villagerMeetTick(players, m)
	if m.meet != bell {
		t.Fatalf("the villager should claim the bell 40 blocks off, got %+v", m.meet)
	}
	h.world.SetBlock(bell.x, bell.y, bell.z, worldgen.Air)
	h.villagerMeetTick(players, m)
	if m.meet != (blockPos{}) {
		t.Fatalf("a broken bell should be let go, still %+v", m.meet)
	}
}

// A bed the villager cannot path to is not claimed, and is marked to retry
// later rather than every run.
func TestUnreachableBedIsNotClaimed(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	poiFloor(h, 0, 0, 12)
	bed := blockPos{8, 180, 0}
	h.world.SetBlock(bed.x, bed.y, bed.z, freeBed())
	for x := 5; x <= 11; x++ { // a sealed stone box around it
		for z := -3; z <= 3; z++ {
			for y := 180; y <= 183; y++ {
				if x == 5 || x == 11 || z == -3 || z == 3 || y == 183 {
					h.world.SetBlock(x, y, z, worldgen.Stone)
				}
			}
		}
	}
	m := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	m.bed, m.home = blockPos{}, blockPos{}
	poiDue(h, m)
	h.villagerBedTick(players, m)
	if m.bed != (blockPos{}) {
		t.Fatalf("a bed sealed in stone was claimed: %+v", m.bed)
	}
	if r := m.poiRetries[poiHome][bed]; r == nil || r.next <= h.tick.Load() {
		t.Fatalf("the unreachable bed should carry a retry delay, got %+v", r)
	}
}

// SetWalkTargetFromBlockMemory: a claim the villager keeps failing to path to
// is let go after 1200 ticks for a bed, 200 for a bell, and kept before.
func TestUnreachableClaimIsLetGo(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMob(map[int32]*tracked{}, entityVillager, 0.5, 180, 0.5)
	m.bed, m.home, m.meet = blockPos{10, 180, 0}, blockPos{10, 180, 0}, blockPos{12, 180, 0}
	m.pathReached = false
	h.tick.Store(100)
	h.noteWalkToPoi(m, poiHome, m.bed)
	h.noteWalkToPoi(m, poiMeet, m.meet)
	h.tick.Store(100 + 250)
	h.noteWalkToPoi(m, poiHome, m.bed)
	h.noteWalkToPoi(m, poiMeet, m.meet)
	if m.meet != (blockPos{}) {
		t.Error("a bell unreachable for 250 ticks should be let go (limit 200)")
	}
	if m.bed == (blockPos{}) {
		t.Fatal("a bed unreachable for 250 ticks should still be held (limit 1200)")
	}
	h.tick.Store(100 + 1300)
	h.noteWalkToPoi(m, poiHome, m.bed)
	if m.bed != (blockPos{}) {
		t.Error("a bed unreachable for 1300 ticks should be let go")
	}
	// A path that reaches stops the clock.
	m.bed = blockPos{10, 180, 0}
	m.pathReached = false
	h.noteWalkToPoi(m, poiHome, m.bed)
	m.pathReached = true
	h.tick.Store(100 + 5000)
	h.noteWalkToPoi(m, poiHome, m.bed)
	if m.bed == (blockPos{}) || m.cantReachSince[poiHome] != 0 {
		t.Error("a reachable bed must keep its claim and clear the clock")
	}
}

// A bed upstairs: the ground floor, a staircase of full blocks up the side,
// and a second floor with the bed. A villager's plan to its bed climbs the
// stairs (search3D) and reaches it. The column search saw only one height
// per column and could not tell the floors apart.
func TestVillagerPlansUpstairsToItsBed(t *testing.T) {
	h := newHub(world.New(1))
	x, y, z := 5000, 180, 5000
	h.world.ForceLoad(x, z, 2)
	for dx := -2; dx <= 10; dx++ { // ground floor, cleared above
		for dz := -2; dz <= 4; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 8; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	for dx := 0; dx <= 8; dx++ { // the upper floor, one storey up (y+3), over the ground floor
		for dz := 0; dz <= 2; dz++ {
			if dx >= 6 && dz == 2 {
				continue // the stairwell
			}
			h.world.SetBlock(x+dx, y+3, z+dz, worldgen.Stone)
		}
	}
	for i := 0; i < 3; i++ { // a staircase of full blocks up to the upper floor
		h.world.SetBlock(x+8-i, y+i, z+2, worldgen.Stone)
	}
	bed := blockPos{x + 2, y + 4, z + 1}
	h.world.SetBlock(bed.x, bed.y, bed.z, freeBed())
	m := h.spawnMob(map[int32]*tracked{}, entityVillager, float64(x)+0.5, float64(y), float64(z)+0.5)
	h.pathSteerTo(m, bed, poiValidRange[poiHome])
	if !m.pathReached {
		t.Fatalf("no plan reached the bed upstairs (path %d steps)", len(m.path))
	}
	if !h.poiReachable(m, bed, poiValidRange[poiHome]) {
		t.Fatal("the bed upstairs should count as reachable")
	}
}
