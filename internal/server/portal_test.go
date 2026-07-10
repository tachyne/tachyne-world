package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// buildFrame erects a standard 4x5 obsidian frame (2x3 interior) at (x,y,z)
// being the interior's min corner, on the x axis.
func buildFrame(w *world.World, x, y, z int) {
	for i := -1; i <= 2; i++ {
		w.SetBlock(x+i, y-1, z, worldgen.Obsidian)
		w.SetBlock(x+i, y+3, z, worldgen.Obsidian)
	}
	for j := 0; j < 3; j++ {
		w.SetBlock(x-1, y+j, z, worldgen.Obsidian)
		w.SetBlock(x+2, y+j, z, worldgen.Obsidian)
		w.SetBlock(x, y+j, z, worldgen.Air)
		w.SetBlock(x+1, y+j, z, worldgen.Air)
	}
}

func TestPortalFrameDetection(t *testing.T) {
	w := world.New(1)
	x, y, z := 100, 80, 100
	buildFrame(w, x, y, z)
	x0, y0, _, wid, hgt, state, ok := detectPortalFrame(w, x+1, y+1, z)
	if !ok {
		t.Fatal("valid frame not detected")
	}
	if x0 != x || y0 != y || wid != 2 || hgt != 3 || state != portalX {
		t.Fatalf("frame geometry wrong: x0=%d y0=%d w=%d h=%d state=%d", x0, y0, wid, hgt, state)
	}
	// Broken frame (missing top) must not light.
	w.SetBlock(x, y+3, z, worldgen.Air)
	if _, _, _, _, _, _, ok := detectPortalFrame(w, x+1, y+1, z); ok {
		t.Fatal("frame with a missing top block must not validate")
	}
}

func TestPortalDwellFlagsSwitch(t *testing.T) {
	ow := world.New(1)
	nw, _ := world.NewNether(1, nil)
	h := newHub(ow)
	h.nether = nw
	pl := testTracked()
	pl.gamemode = gmSurvival
	x, y, z := 50, 80, 50
	buildFrame(ow, x, y, z)
	for j := 0; j < 3; j++ { // lit portal
		ow.SetBlock(x, y+j, z, portalX)
		ow.SetBlock(x+1, y+j, z, portalX)
	}
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+0.5
	players := map[int32]*tracked{1: pl}
	// The dwell pass runs once a second: 80 ticks → 4 passes in survival.
	for i := 0; i < portalDwellTicks/survivalTickN-1; i++ {
		h.updatePortalDwell(players)
	}
	if pl.p.pendingDim.Load() != -1 {
		t.Fatal("switch must not fire before the dwell completes")
	}
	h.updatePortalDwell(players)
	if pl.p.pendingDim.Load() != 1 {
		t.Fatalf("dwell complete should flag the nether, got %d", pl.p.pendingDim.Load())
	}
	// Stepping out resets the count.
	pl.p.pendingDim.Store(-1)
	pl.x += 5
	h.updatePortalDwell(players)
	if pl.portalTicks != 0 {
		t.Fatal("leaving the portal must reset the dwell")
	}
}

func TestPortalLatchStopsBounce(t *testing.T) {
	ow := world.New(1)
	nw, _ := world.NewNether(1, nil)
	h := newHub(ow)
	h.nether = nw
	pl := testTracked()
	pl.gamemode = gmSurvival
	x, y, z := 60, 80, 60
	buildFrame(ow, x, y, z)
	for j := 0; j < 3; j++ {
		ow.SetBlock(x, y+j, z, portalX)
		ow.SetBlock(x+1, y+j, z, portalX)
	}
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+0.5
	pl.portalLatch = true // just arrived through this portal
	players := map[int32]*tracked{1: pl}
	for i := 0; i < 20; i++ {
		h.updatePortalDwell(players)
	}
	if pl.p.pendingDim.Load() != -1 {
		t.Fatal("a latched portal must never re-trigger while stood in")
	}
	// Step off: latch clears; step back in: dwell works again.
	pl.x += 5
	h.updatePortalDwell(players)
	if pl.portalLatch {
		t.Fatal("stepping off must clear the latch")
	}
	pl.x -= 5
	for i := 0; i < portalDwellTicks/survivalTickN; i++ {
		h.updatePortalDwell(players)
	}
	if pl.p.pendingDim.Load() != 1 {
		t.Fatal("after stepping off, the portal must work again")
	}
}

func TestNearestEditedFindsDistantPortal(t *testing.T) {
	w := world.New(1)
	x, y, z := 300, 80, 300
	buildFrame(w, x, y, z)
	for j := 0; j < 3; j++ {
		w.SetBlock(x, y+j, z, portalX)
		w.SetBlock(x+1, y+j, z, portalX)
	}
	// 8:1 drift can put the return point ~100 blocks away — must still link.
	px, py, pz, ok := w.NearestEdited(x+100, y, z-60, 128, isPortalBlock)
	if !ok {
		t.Fatal("128-block edit scan should find the portal")
	}
	for isPortalBlock(w.At(px, py-1, pz)) {
		py--
	}
	if py != y || pz != z {
		t.Fatalf("wrong portal base: (%d,%d,%d)", px, py, pz)
	}
	if _, _, _, ok := w.NearestEdited(x+500, y, z, 128, isPortalBlock); ok {
		t.Fatal("beyond radius must not match")
	}
}

func TestOrphanPortalBlocksPop(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	x, y, z := 400, 80, 400
	buildFrame(w, x, y, z)
	for j := 0; j < 3; j++ {
		w.SetBlock(x, y+j, z, portalX)
		w.SetBlock(x+1, y+j, z, portalX)
	}
	// Demolish a frame pillar: the sheet must cascade away.
	w.SetBlock(x-1, y, z, worldgen.Air)
	w.SetBlock(x-1, y+1, z, worldgen.Air)
	w.SetBlock(x-1, y+2, z, worldgen.Air)
	h.scheduleAround(blockPos{x - 1, y, z}, 1)
	stepTicks(h, players, 12)
	for j := 0; j < 3; j++ {
		if isPortalBlock(w.At(x, y+j, z)) || isPortalBlock(w.At(x+1, y+j, z)) {
			t.Fatalf("orphaned portal blocks must pop (col y+%d: %d %d)",
				j, w.At(x, y+j, z), w.At(x+1, y+j, z))
		}
	}
	// And a stray portal edit is never a link target.
	w.SetBlock(x+5, y, z+5, portalX) // floating stray
	px, py, pz, ok := w.NearestEdited(x, y, z, 64, isPortalBlock)
	_ = px
	_ = py
	_ = pz
	if ok {
		// found the stray — the arrival path's integrity check must reject it
		if w.At(px, py-1, pz) == worldgen.Obsidian {
			t.Fatal("stray should have no obsidian footing")
		}
	}
}

func TestPortalLinkRegistryRoundTrip(t *testing.T) {
	ow := world.New(1)
	nw, _ := world.NewNether(1, nil)
	h := newHub(ow)
	h.nether = nw
	// Overworld portal A and nether portal B, linked.
	buildFrame(ow, 500, 80, 500)
	for j := 0; j < 3; j++ {
		ow.SetBlock(500, 80+j, 500, portalX)
		ow.SetBlock(501, 80+j, 500, portalX)
	}
	ny := nw.Gen().NetherFloor(62, 62)
	for dx := -1; dx <= 2; dx++ { // nether-side frame at (62,ny,62)
		nw.SetBlock(62+dx, ny-1, 62, worldgen.Obsidian)
		nw.SetBlock(62+dx, ny+3, 62, worldgen.Obsidian)
	}
	for j := 0; j < 3; j++ {
		nw.SetBlock(61, ny+j, 62, worldgen.Obsidian)
		nw.SetBlock(64, ny+j, 62, worldgen.Obsidian)
		nw.SetBlock(62, ny+j, 62, portalX)
		nw.SetBlock(63, ny+j, 62, portalX)
	}
	a := dimPos{0, portalBaseKey(ow, 500, 81, 500)}
	b := dimPos{1, portalBaseKey(nw, 62, ny+1, 62)}
	h.portalLinks[a] = b
	h.portalLinks[b] = a

	pl := testTracked()
	pl.gamemode = gmCreative
	pl.x, pl.y, pl.z = 500.5, 80, 500.5
	players := map[int32]*tracked{1: pl}
	h.updatePortalDwell(players) // creative: fires next pass
	if pl.p.pendingDim.Load() != 1 || !pl.p.pendingDestOK {
		t.Fatalf("linked travel should carry the destination: dim=%d ok=%v",
			pl.p.pendingDim.Load(), pl.p.pendingDestOK)
	}
	if pl.p.pendingDest != b.pos {
		t.Fatalf("destination should be the linked nether portal base: %+v want %+v", pl.p.pendingDest, b.pos)
	}
	// A broken link target falls back to coordinate travel.
	for j := 0; j < 3; j++ {
		nw.SetBlock(62, ny+j, 62, worldgen.Air)
		nw.SetBlock(63, ny+j, 62, worldgen.Air)
	}
	pl.p.pendingDim.Store(-1)
	pl.portalTicks = 0
	h.updatePortalDwell(players)
	if pl.p.pendingDestOK {
		t.Fatal("a demolished link target must not be used")
	}
}

func TestNetherPortalDemolishCascades(t *testing.T) {
	ow := world.New(1)
	nw, _ := world.NewNether(1, nil)
	h := newHub(ow)
	h.nether = nw
	players := map[int32]*tracked{}
	x, y, z := 70, 60, 70
	for dx := -1; dx <= 2; dx++ {
		nw.SetBlock(x+dx, y-1, z, worldgen.Obsidian)
		nw.SetBlock(x+dx, y+3, z, worldgen.Obsidian)
	}
	for j := 0; j < 3; j++ {
		nw.SetBlock(x-1, y+j, z, worldgen.Obsidian)
		nw.SetBlock(x+2, y+j, z, worldgen.Obsidian)
		nw.SetBlock(x, y+j, z, portalX)
		nw.SetBlock(x+1, y+j, z, portalX)
	}
	// Break one frame pillar IN THE NETHER: the sheet must pop despite the
	// dim-0-only scheduler.
	nw.SetBlock(x-1, y, z, worldgen.Air)
	h.onBlock(players, evBlock{x: x - 1, y: y, z: z, dim: 1, state: worldgen.Air, by: 1})
	for j := 0; j < 3; j++ {
		if isPortalBlock(nw.At(x, y+j, z)) || isPortalBlock(nw.At(x+1, y+j, z)) {
			t.Fatalf("nether portal sheet must cascade-pop on frame break (y+%d)", j)
		}
	}
}
