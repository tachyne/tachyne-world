package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// buildPortal lights a 2x3 portal sheet on an obsidian frame at (x,y,z) in
// dim (the sheet runs along x, or along z with axisZ), with somewhere to
// stand on both sides, and returns the sheet's bottom corner.
func buildPortal(h *hub, dim, x, y, z int) blockPos { return buildPortalAxis(h, dim, x, y, z, false) }

func buildPortalAxis(h *hub, dim, x, y, z int, axisZ bool) blockPos {
	w := h.worldFor(dim)
	dx, dz, st := 1, 0, portalX
	if axisZ {
		dx, dz, st = 0, 1, portalZ
	}
	for dy := -1; dy <= 3; dy++ { // frame sides
		w.SetBlock(x-dx, y+dy, z-dz, worldgen.Obsidian)
		w.SetBlock(x+2*dx, y+dy, z+2*dz, worldgen.Obsidian)
	}
	for i := 0; i < 2; i++ {
		cx, cz := x+dx*i, z+dz*i
		w.SetBlock(cx, y-1, cz, worldgen.Obsidian) // floor
		w.SetBlock(cx, y+3, cz, worldgen.Obsidian) // lintel
		for dy := 0; dy < 3; dy++ {
			w.SetBlock(cx, y+dy, cz, st)
		}
		for _, o := range []int{-1, 1} { // somewhere to stand on both sides
			w.SetBlock(cx+dz*o, y-1, cz+dx*o, worldgen.Stone)
		}
	}
	return blockPos{x, y, z}
}

// voidPortalHub is a hub whose Nether is empty sky (the End's void away from
// its islands), so what the portal forcer builds there is predictable.
func voidPortalHub(t *testing.T) *hub {
	t.Helper()
	h := newHub(world.New(1))
	nw, err := world.NewEnd(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.nether = nw
	return h
}

// portalCells counts the lit portal blocks in a box of a dimension.
func portalCells(h *hub, dim, x0, y0, z0, x1, y1, z1 int) int {
	w := h.worldFor(dim)
	n := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				if isPortalBlock(w.At(x, y, z)) {
					n++
				}
			}
		}
	}
	return n
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// A mob standing in a portal goes through at once — no dwell, because vanilla
// only makes players wait. With no portal on the far side one is built
// (createPortal's fallback platform, in empty sky); the next traveller finds
// that one; the way back finds the portal they left by; and an entity that
// stays standing in the far portal never bounces back.
func TestNetherTripBuildsThenReusesPortal(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	over := buildPortal(h, dimOverworld, 3200, 200, 3200)

	m := h.spawnMob(players, entityZombie, 3200.5, 200, 3200.5)
	m.dim = dimOverworld
	h.updatePortalTravel(players)
	if m.dim != dimNether {
		t.Fatalf("the zombie should be in the nether, dim=%d", m.dim)
	}
	// 3200.5/8 = 400.06: the frame goes one block back along the axis
	// (x 399..400), at y 118 — the nine-under-the-roof cap on the platform.
	if !near(m.x, 399.5) || !near(m.y, 118) || !near(m.z, 400.5) {
		t.Fatalf("it should come out in the new portal's matching spot, got %.3f,%.3f,%.3f", m.x, m.y, m.z)
	}
	nw := h.nether
	if nw.At(399, 118, 400) != portalX || nw.At(400, 120, 400) != portalX {
		t.Fatal("the far portal should be lit along the entry's axis")
	}
	for _, c := range []blockPos{{398, 118, 400}, {401, 118, 400}, {399, 117, 400}, {400, 121, 400}, {399, 117, 399}, {400, 117, 401}} {
		if nw.At(c.x, c.y, c.z) != worldgen.Obsidian {
			t.Fatalf("frame and platform should be obsidian at %v, got %d", c, nw.At(c.x, c.y, c.z))
		}
	}
	if nw.At(399, 118, 399) != worldgen.Air || nw.At(400, 120, 401) != worldgen.Air {
		t.Fatal("the platform should have air over it on both sides")
	}
	if m.portalCool != entityPortalCooldown {
		t.Fatalf("it should carry the 300-tick cooldown, got %d", m.portalCool)
	}
	// Standing in the far portal keeps the cooldown full (setAsInsidePortal).
	for i := 0; i < 2*entityPortalCooldown; i++ {
		h.updatePortalTravel(players)
	}
	if m.dim != dimNether || m.portalCool != entityPortalCooldown {
		t.Fatalf("a mob left standing in the portal must stay, dim=%d cool=%d", m.dim, m.portalCool)
	}

	// A second traveller finds the portal the first one built.
	m2 := h.spawnMob(players, entityZombie, 3201.2, 200, 3200.5)
	m2.dim = dimOverworld
	h.updatePortalTravel(players)
	if m2.dim != dimNether || portalCells(h, dimNether, 380, 60, 380, 420, 127, 420) != 6 {
		t.Fatalf("the second trip should reuse the portal: dim=%d cells=%d", m2.dim,
			portalCells(h, dimNether, 380, 60, 380, 420, 127, 420))
	}
	// Further along the near sheet is further along the far one:
	// 0.3 + 1.4 × (1.2-0.3)/1.4 = 1.2 across.
	if !near(m2.x, 400.2) {
		t.Fatalf("the relative spot across the sheet should carry over, x=%.3f", m2.x)
	}

	// Home again: step the first zombie off and back on after its cooldown.
	m.x = 395.5
	for i := 0; i < entityPortalCooldown; i++ {
		h.updatePortalTravel(players)
	}
	m.x = 399.5
	h.updatePortalTravel(players)
	if m.dim != dimOverworld {
		t.Fatalf("with the cooldown spent the zombie goes back, dim=%d", m.dim)
	}
	// 399.5×8 = 3196: the 128-block search finds the portal it left by.
	if floorInt(m.x) < over.x || floorInt(m.x) > over.x+1 || !near(m.y, 200) || floorInt(m.z) != over.z {
		t.Fatalf("it should come out of the portal it went in by, got %.2f,%.2f,%.2f", m.x, m.y, m.z)
	}
	if n := portalCells(h, dimOverworld, 3180, 190, 3180, 3220, 215, 3220); n != 6 {
		t.Fatalf("no second overworld portal should be built, %d cells", n)
	}
}

// A portal already standing within 16 blocks of the scaled spot is the one
// used — here one turned the other way, so the traveller turns a quarter.
func TestNetherTripFindsNearbyPortal(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	buildPortal(h, dimOverworld, 3200, 200, 3200)
	far := buildPortalAxis(h, dimNether, 410, 150, 405, true)

	it := h.spawnItemIn(players, dimOverworld, int32(itemByName["diamond"]), 3, 3200.5, 200, 3200.5)
	it.dim, it.x, it.y, it.z = dimOverworld, 3200.5, 200, 3200.5
	it.vx, it.vy, it.vz = 0.1, 0, 0
	h.updatePortalTravel(players)
	if it.dim != dimNether {
		t.Fatalf("the drop should be in the nether, dim=%d", it.dim)
	}
	if floorInt(it.x) != far.x || floorInt(it.z) < far.z || floorInt(it.z) > far.z+1 || !near(it.y, 150) {
		t.Fatalf("it should come out of the standing portal, got %.2f,%.2f,%.2f", it.x, it.y, it.z)
	}
	if it.count != 3 {
		t.Fatalf("the stack must arrive whole, got %d", it.count)
	}
	// Turned a quarter: +x motion becomes +z (ROTATE_DELTA).
	if !near(it.vx, 0) || !near(it.vz, 0.1) {
		t.Fatalf("the motion turns with the portal: vx=%.3f vz=%.3f", it.vx, it.vz)
	}
	if n := portalCells(h, dimNether, 380, 60, 380, 430, 160, 430); n != 6 {
		t.Fatalf("no portal should have been built, %d cells", n)
	}

	// The search square: 16 into the Nether, 128 out of it.
	if _, ok := h.findClosestPortal(dimNether, blockPos{410 - 17, 150, 405}, true); ok {
		t.Fatal("a portal 17 blocks off is outside the Nether search")
	}
	if p, ok := h.findClosestPortal(dimNether, blockPos{410 - 16, 150, 405}, true); !ok || p.x != 410 {
		t.Fatalf("a portal 16 blocks off is inside it: %v %v", p, ok)
	}
	if _, ok := h.findClosestPortal(dimOverworld, blockPos{3201 + 129, 200, 3200}, false); ok {
		t.Fatal("the overworld search stops at 128")
	}
	if p, ok := h.findClosestPortal(dimOverworld, blockPos{3200 - 128, 40, 3200}, false); !ok || p.x != 3200 || p.y != 200 {
		t.Fatalf("…and reaches 128 at any height, closest cell lowest: %v %v", p, ok)
	}
}

// createPortal takes the closest spot with room for the frame and solid
// ground under it — preferring one with room on both sides.
func TestCreatePortalPicksClosestSpot(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	nw := h.nether
	for x := 510; x <= 519; x++ {
		for z := 500; z <= 509; z++ {
			nw.SetBlock(x, 59, z, worldgen.Stone)
		}
	}
	r, ok := h.createNetherPortal(players, dimNether, blockPos{500, 60, 500}, false)
	if !ok {
		t.Fatal("a portal should be built")
	}
	// The frame (x-1..x+2) must stand on the floor, with a free row either
	// side (z±1): the nearest such corner is (511,60,501).
	if r.min != (blockPos{511, 60, 501}) || r.w != 2 || r.h != 3 {
		t.Fatalf("wrong spot: %+v", r)
	}
	if nw.At(511, 60, 501) != portalX || nw.At(510, 60, 501) != worldgen.Obsidian || nw.At(511, 59, 501) != worldgen.Obsidian {
		t.Fatal("the portal and its frame should be in place")
	}
	if nw.At(511, 59, 500) != worldgen.Stone {
		t.Fatal("a spot that fits needs no platform")
	}
}

// Nowhere to stand: the portal goes on a small obsidian platform, at the
// traveller's height kept at least 70.
func TestCreatePortalFallbackPlatform(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	r, ok := h.createNetherPortal(players, dimNether, blockPos{600, 20, 600}, true)
	if !ok {
		t.Fatal("a portal should be built")
	}
	// Along z: one back along the axis, y raised to 70.
	if r.min != (blockPos{600, 70, 599}) {
		t.Fatalf("wrong fallback spot: %+v", r.min)
	}
	nw := h.nether
	for box := -1; box <= 1; box++ { // clockwise of SOUTH is WEST: the platform spans x±1
		for wd := 0; wd < 2; wd++ {
			if nw.At(600-box, 69, 599+wd) != worldgen.Obsidian {
				t.Fatalf("platform missing at %d,69,%d", 600-box, 599+wd)
			}
		}
	}
	if nw.At(600, 70, 599) != portalZ || nw.At(600, 72, 600) != portalZ || nw.At(600, 70, 598) != worldgen.Obsidian {
		t.Fatal("the portal should be lit along z in its frame")
	}
}

// A player's dwell resolves the far side on the hub: the exact spot, and the
// heading turned when the portals differ.
func TestPlayerPortalTripArrival(t *testing.T) {
	h := voidPortalHub(t)
	buildPortal(h, dimOverworld, 3200, 200, 3200)
	buildPortalAxis(h, dimNether, 400, 150, 400, true)
	pl := testTracked()
	pl.gamemode = gmCreative
	pl.x, pl.y, pl.z, pl.yaw = 3200.5, 200, 3200.5, 10
	players := map[int32]*tracked{1: pl}
	h.updatePortalDwell(players) // creative: a delay of 0 fires on the first tick
	if pl.p.pendingDim.Load() != dimNether || !pl.p.pendingAt {
		t.Fatalf("the trip should be flagged with a destination: dim=%d at=%v", pl.p.pendingDim.Load(), pl.p.pendingAt)
	}
	if p := pl.p.pendingPos; !near(p[0], 400.5) || !near(p[1], 150) || p[2] < 400 || p[2] > 402 {
		t.Fatalf("wrong arrival %v", p)
	}
	if pl.p.pendingYaw != 100 {
		t.Fatalf("an x portal into a z portal turns a quarter: yaw %v", pl.p.pendingYaw)
	}
}

// A falling block is an entity: it goes through a portal still falling.
func TestFallingBlockTakesThePortal(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	buildPortal(h, dimOverworld, 3200, 200, 3200)
	far := buildPortal(h, dimNether, 400, 150, 400)
	fb := &fallingBlock{eid: h.allocEID(), dim: dimOverworld, x: 3200.5, y: 201.2, z: 3200.5, vy: -0.3,
		state: worldgen.Sand, dropItem: true, hurtMax: fallDamageMaxDef}
	h.addFallingBlock(players, fb)
	h.updatePortalTravel(players)
	if fb.dim != dimNether || floorInt(fb.x) != far.x || floorInt(fb.z) != far.z {
		t.Fatalf("the sand should cross: dim %d at %.2f,%.2f,%.2f", fb.dim, fb.x, fb.y, fb.z)
	}
	if fb.vy != -0.3 || fb.portalCool != entityPortalCooldown {
		t.Fatalf("it keeps falling and takes the cooldown: vy %v cool %d", fb.vy, fb.portalCool)
	}
}

// A mob that rides something cannot take a portal (canUsePortal).
func TestRidingMobSkipsThePortal(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	buildPortal(h, dimOverworld, 3200, 200, 3200)
	m := h.spawnMob(players, entityZombie, 3200.5, 200, 3200.5)
	m.dim, m.mount = dimOverworld, 999
	h.updatePortalTravel(players)
	if m.dim != dimOverworld {
		t.Fatal("a passenger stays put")
	}
}

// Primed TNT and projectiles go through a portal like any other entity,
// fuse and flight intact; and a lit charge in a water current is carried
// along by it.
func TestPrimedTNTTakesThePortalAndTheCurrent(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	over := buildPortal(h, dimOverworld, 3200, 200, 3200)
	buildPortal(h, dimNether, 400, 150, 400)
	pt := h.spawnPrimedTNT(players, dimOverworld, over.x, over.y, over.z, 80)
	h.updatePortalTravel(players)
	if pt.dim != dimNether || floorInt(pt.x) < 400 || floorInt(pt.x) > 401 || pt.fuse != 80 {
		t.Fatalf("the charge should cross with its fuse: dim %d x %.1f fuse %d", pt.dim, pt.x, pt.fuse)
	}

	a := h.launchProjectileIn(players, entityArrow, dimOverworld, float64(over.x)+0.5, float64(over.y)+1.5, float64(over.z)+0.5, 0.3, 0, 0)
	h.updatePortalTravel(players)
	if a.dim != dimNether || a.vx != 0.3 {
		t.Fatalf("an arrow in flight crosses and keeps flying: dim %d vx %v", a.dim, a.vx)
	}

	h.world.ForceLoad(0, 0, 1)
	for x := -3; x <= 6; x++ {
		h.world.SetBlock(x, 179, 0, worldgen.Stone)
		h.world.SetBlock(x, 181, 0, worldgen.Air)
	}
	// A stream running east: a source at x=-3, then levels 1..7.
	h.world.SetBlock(-3, 180, 0, worldgen.WaterBase)
	for i := 1; i <= 7; i++ {
		h.world.SetBlock(-3+i, 180, 0, worldgen.WaterBase+uint32(i))
	}
	c := h.spawnPrimedTNT(players, dimOverworld, 0, 180, 0, 80)
	c.vx, c.vz, c.vy = 0, 0, 0
	for i := 0; i < 5; i++ {
		h.tntStep(players, c)
	}
	if c.x <= 0.5 {
		t.Fatalf("the current should carry the charge east, x=%.3f", c.x)
	}
}

// With the rule off nothing goes into the Nether.
func TestAllowNetherRuleStopsEntities(t *testing.T) {
	h := voidPortalHub(t)
	players := map[int32]*tracked{}
	buildPortal(h, dimOverworld, 3200, 200, 3200)
	h.rules.AllowNether = false
	m := h.spawnMob(players, entityZombie, 3200.5, 200, 3200.5)
	m.dim = dimOverworld
	h.updatePortalTravel(players)
	if m.dim != dimOverworld {
		t.Fatal("allow_nether off keeps the zombie home")
	}
}
