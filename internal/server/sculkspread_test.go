package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// sculkPad lays a 25×25 floor high in the sky (y=180, clear of terrain),
// its chunks loaded, with a catalyst standing on it at (x,y,z). floor picks
// each floor block by its offset from the catalyst.
func sculkPad(t *testing.T, floor func(dx, dz int) uint32) (*hub, *world.World, map[int32]*tracked, int, int, int) {
	t.Helper()
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	h.playersRef = players
	x, y, z := 8, 180, 8
	w.ForceLoad(x, z, 2)
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, floor(dx, dz))
			for dy := 0; dy < 6; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	cat := catalystWith(false)
	w.SetBlock(x, y, z, cat)
	h.sculkIndexOnBlockChange(0, x, y, z, cat)
	return h, w, players, x, y, z
}

func allStone(int, int) uint32 { return worldgen.Stone }

// killNear kills a blaze (ten experience) a player hit, standing at
// (x,y,z), through the death animation to the moment its drops are rolled.
func killNear(t *testing.T, h *hub, players map[int32]*tracked, x, y, z float64) {
	t.Helper()
	m := h.spawnMobIn(players, entityBlaze, 0, x, y, z)
	if m == nil {
		t.Fatal("no blaze")
	}
	h.hurtByEID(m, 0) // a player's blow: it pays experience
	h.killMob(players, m)
	for i := 0; i < deathAnimTicks+2 && h.mobs[m.eid] != nil; i++ {
		h.tick.Add(1)
		h.updateMobs(players)
	}
	if h.mobs[m.eid] != nil {
		t.Fatal("the blaze never finished dying")
	}
}

// countFloor counts floor cells in a state.
func countFloor(w *world.World, x, y, z int, s uint32) int {
	n := 0
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			if w.At(x+dx, y-1, z+dz) == s {
				n++
			}
		}
	}
	return n
}

// A kill near a catalyst gives it the experience as charge, not orbs; the
// catalyst blooms; and the charge creeps out tick by tick — nothing turns at
// the moment of death, one block at most on the first step, and never more
// blocks than there was charge (a vein turning ground costs one).
func TestCatalystChargeSpreadsOverTicks(t *testing.T) {
	h, w, players, x, y, z := sculkPad(t, allStone)
	killNear(t, h, players, float64(x)+3.5, float64(y), float64(z)+0.5)
	if len(h.orbs) != 0 {
		t.Fatalf("the catalyst should take the experience, but %d orbs dropped", len(h.orbs))
	}
	if got := w.At(x, y, z); got != catalystWith(true) {
		t.Fatalf("the catalyst should bloom on the death: state %d", got)
	}
	sp := h.sculkSpread[simPos{blockPos: blockPos{x, y, z}}]
	if sp == nil || len(sp.cursors) != 1 {
		t.Fatalf("the death should leave one charge cursor: %+v", sp)
	}
	charge := sp.cursors[0].charge
	if charge != 10 || sp.cursors[0].pos != (blockPos{x + 3, y, z}) {
		t.Fatalf("cursor %+v: want the blaze's ten experience at its death cell", *sp.cursors[0])
	}
	sculk := worldgen.BlockBase("sculk")
	if n := countFloor(w, x, y, z, sculk); n != 0 {
		t.Fatalf("%d blocks turned at the moment of death; the charge travels", n)
	}
	stepSculk(h, players, 1)
	if n := countFloor(w, x, y, z, sculk); n > 1 {
		t.Fatalf("%d blocks turned on the first step", n)
	}
	stepSculk(h, players, 2000)
	n := countFloor(w, x, y, z, sculk)
	if n < 2 || n > charge {
		t.Fatalf("after the charge ran out %d blocks had turned, want 2..%d", n, charge)
	}
	if _, left := h.sculkSpread[simPos{blockPos: blockPos{x, y, z}}]; left {
		t.Fatal("a spent spreader should be dropped")
	}
	if w.At(x, y, z) != catalystWith(false) {
		t.Fatal("the bloom should have faded")
	}
}

// Only #sculk_replaceable turns: on a floor mixing stone, dirt, planks and
// stone bricks, every block that became sculk was stone or dirt, and the
// planks and bricks are all still there (veins may cling to them).
func TestCatalystChargeTurnsOnlyReplaceable(t *testing.T) {
	planks := worldgen.BlockBase("oak_planks")
	bricks := worldgen.BlockBase("stone_bricks")
	dirt := worldgen.BlockBase("dirt")
	mix := func(dx, dz int) uint32 {
		return [4]uint32{worldgen.Stone, planks, dirt, bricks}[(dx+dz+100)%4]
	}
	h, w, players, x, y, z := sculkPad(t, mix)
	for i := 0; i < 4; i++ { // four kills' worth of charge
		killNear(t, h, players, float64(x)+2.5, float64(y), float64(z)+0.5) // over dirt
	}
	stepSculk(h, players, 400)
	sculk := worldgen.BlockBase("sculk")
	turned := 0
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			was, now := mix(dx, dz), w.At(x+dx, y-1, z+dz)
			switch {
			case now == sculk:
				if !inRanges2(was, sculkReplaceable) {
					t.Fatalf("block %d at %+d,%+d is not #sculk_replaceable but turned", was, dx, dz)
				}
				turned++
			case (was == planks || was == bricks) && now != was:
				t.Fatalf("a built block at %+d,%+d changed to %d", dx, dz, now)
			}
		}
	}
	if turned == 0 {
		t.Fatal("the charge turned no stone or dirt at all")
	}
}

// Veins grow on faces: each vein has a face, and every face lies on a block
// that can hold it.
func TestCatalystVeinsClingToFaces(t *testing.T) {
	h, w, players, x, y, z := sculkPad(t, allStone)
	killNear(t, h, players, float64(x)+4.5, float64(y), float64(z)+0.5)
	stepSculk(h, players, 6) // mid-spread: veins are out
	veins := 0
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			for dy := -2; dy <= 1; dy++ {
				p := blockPos{x + dx, y + dy, z + dz}
				s := w.At(p.x, p.y, p.z)
				if !isSculkVein(s) {
					continue
				}
				veins++
				f := veinFaces(s)
				if f == 0 {
					t.Fatalf("a faceless vein at %+v", p)
				}
				for d := 0; d < 6; d++ {
					if n := p.off(d); f&(1<<d) != 0 && !holdsBlock(w.At(n.x, n.y, n.z)) {
						t.Fatalf("the vein at %+v has face %d on %d, which holds nothing", p, d, w.At(n.x, n.y, n.z))
					}
				}
			}
		}
	}
	if veins == 0 {
		t.Fatal("the spreading charge laid no veins")
	}
}

// A big death — a player of level 20 dying six blocks out — on a floor of
// earlier sculk: the charge has no fresh ground to eat, so it wanders the
// sculk and grows sensors and shriekers from it (fresh ground in reach
// would draw it off and spend it a point a block instead). A grown shrieker cannot summon a Warden, and a
// grown sensor listens.
func TestCatalystBigDeathGrowsSensorOrShrieker(t *testing.T) {
	field := func(int, int) uint32 { return worldgen.BlockBase("sculk") }
	h, w, players, x, y, z := sculkPad(t, field)
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = float64(x)+6.5, float64(y), float64(z)+0.5
	pl.xpLevel = 20
	h.damageOf(players, pl, 1000, dtGeneric)
	if !pl.dead || len(h.orbs) != 0 {
		t.Fatalf("the player should die and the catalyst take the experience: dead=%v orbs=%d", pl.dead, len(h.orbs))
	}
	delete(players, pl.p.eid) // gone to the death screen
	stepSculk(h, players, 3000)
	grown := 0
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			p := blockPos{x + dx, y, z + dz}
			s := w.At(p.x, p.y, p.z)
			switch {
			case isShrieker(s):
				if shriekerCanSummon(s) {
					t.Fatalf("a catalyst grew a shrieker that can summon at %+v", p)
				}
				grown++
			case isSculkSensor(s):
				if !h.sculkList[simPos{blockPos: p}] {
					t.Fatalf("the sensor grown at %+v does not listen", p)
				}
				grown++
			default:
				continue
			}
			if w.At(p.x, p.y-1, p.z) != worldgen.BlockBase("sculk") {
				t.Fatalf("growth at %+v is not standing on sculk", p)
			}
			if abs(dx)*abs(dx)+abs(dz)*abs(dz)+1 < sculkNoGrowthRadius*sculkNoGrowthRadius {
				t.Fatalf("growth at %+d,%+d is inside the no-growth radius", dx, dz)
			}
		}
	}
	if grown == 0 {
		t.Fatal("a hundred points of charge grew no sensor or shrieker")
	}
}

// SculkSpreader.addCursors: a thousand points to a cursor, thirty-two at most.
func TestSculkSpreaderCursorLimits(t *testing.T) {
	sp := &sculkSpreader{}
	sp.addCursors(blockPos{}, 2500)
	if len(sp.cursors) != 3 || sp.cursors[0].charge != 1000 || sp.cursors[2].charge != 500 {
		t.Fatalf("2500 split as %d cursors", len(sp.cursors))
	}
	sp.addCursors(blockPos{}, 100*1000)
	if len(sp.cursors) != sculkMaxCursors {
		t.Fatalf("%d cursors, want at most %d", len(sp.cursors), sculkMaxCursors)
	}
}

// A catalyst whose chunk is not loaded holds its charge: nothing spreads
// until a player's view brings it back.
func TestCatalystChargeWaitsForLoadedChunk(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	x, y, z := 4000, 180, 4000 // never loaded
	w.SetBlock(x, y, z, catalystWith(false))
	h.sculkIndexOnBlockChange(0, x, y, z, catalystWith(false))
	pos := simPos{blockPos: blockPos{x, y, z}}
	h.sculkSpread[pos] = &sculkSpreader{}
	h.sculkSpread[pos].addCursors(blockPos{x + 2, y, z}, 50)
	stepSculk(h, players, 20)
	if sp := h.sculkSpread[pos]; sp == nil || len(sp.cursors) != 1 || sp.cursors[0].charge != 50 || sp.cursors[0].updateDelay != 0 {
		t.Fatal("a catalyst in an unloaded chunk spent its charge")
	}
}
