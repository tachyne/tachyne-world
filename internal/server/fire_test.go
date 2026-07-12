package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestLavaSetsAfterburnAndWaterClears(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	pl.food = 10
	players := map[int32]*tracked{1: pl}
	w.SetBlock(0, 70, 0, worldgen.LavaBase)
	h.survivalTick(players)
	if pl.fireSecs != lavaFireSecs {
		t.Fatalf("lava must set %ds of afterburn, got %d", lavaFireSecs, pl.fireSecs)
	}
	// Step out: afterburn ticks damage.
	w.SetBlock(0, 70, 0, worldgen.Air)
	before := pl.health
	h.survivalTick(players)
	if pl.health >= before || pl.fireSecs != lavaFireSecs-1 {
		t.Fatalf("afterburn should tick: health %v→%v fireSecs=%d", before, pl.health, pl.fireSecs)
	}
	// Dive into water: extinguished.
	w.SetBlock(0, 70, 0, worldgen.Water)
	h.survivalTick(players)
	if pl.fireSecs != 0 {
		t.Fatalf("water must extinguish, fireSecs=%d", pl.fireSecs)
	}
}

func TestFireBlockBurnsOutOnItsOwn(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	w.SetBlock(3, 70, 3, fireDefault)
	pos := blockPos{3, 70, 3}
	for i := 0; i < 64 && isFire(w.At(3, 70, 3)); i++ {
		h.updateFire(players, pos)
	}
	if isFire(w.At(3, 70, 3)) {
		t.Fatal("fire must burn out within a few checks")
	}
}

func TestTNTFuseAndChainReaction(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	pl := testTracked()
	lx, lz := h.findLand(30, 30)
	pl.x, pl.z = float64(lx), float64(lz)
	y := h.world.SurfaceFeet(lx, lz)
	pl.y = float64(y) + 20 // out of blast range
	players := map[int32]*tracked{1: pl}

	w.SetBlock(lx, y, lz, tntStateMax)   // the charge
	w.SetBlock(lx+2, y, lz, tntStateMax) // a neighbour to chain
	h.primeTNT(players, lx, y, lz, 3)
	if len(h.tnt) != 1 || w.At(lx, y, lz) != worldgen.Air {
		t.Fatalf("priming must swap the block for the entity: tnt=%d", len(h.tnt))
	}
	for i := 0; i < 4; i++ {
		h.updateTNT(players)
	}
	// The blast went off and chain-primed the neighbour (now an entity too).
	if w.At(lx+2, y, lz) != worldgen.Air {
		t.Fatal("chain TNT should have been primed (block gone)")
	}
	if len(h.tnt) != 1 {
		t.Fatalf("the neighbour should be ticking now, tnt=%d", len(h.tnt))
	}
	if w.At(lx, y-1, lz) != worldgen.Air {
		t.Fatal("the crater should have carved the ground")
	}
}

func TestExplosionRespectsBlastResistance(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	lx, lz := h.findLand(50, 50)
	y := h.world.SurfaceFeet(lx, lz)
	st, ok := worldgen.BlockID("obsidian"), true // obsidian (blast resistance 1200)
	if !ok {
		t.Fatal("obsidian must be placeable")
	}
	w.SetBlock(lx, y, lz, st)
	h.explodeAt(players, float64(lx)+0.5, float64(y)+1, float64(lz)+0.5, 3, 20)
	if w.At(lx, y, lz) != st {
		t.Fatal("obsidian must survive an explosion")
	}
}

// TestFireSpreadsAndConsumes verifies the FireBlock-derived spread: fire on a
// plank surface eventually consumes fuel (the plank below burns away or turns
// to fire) and the fire propagates (new fire blocks appear).
func TestFireSpreadsAndConsumes(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	h.rules.MobGriefing = true
	planks := worldgen.OakPlanks
	count := func() (n int) {
		for dx := -2; dx <= 2; dx++ {
			for dy := 0; dy <= 3; dy++ {
				for dz := -2; dz <= 2; dz++ {
					if w.At(100+dx, 70+dy, 100+dz) == planks {
						n++
					}
				}
			}
		}
		return
	}
	// A 5x5x2 block of planks; fire lit on top of the middle.
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 1; dy++ {
			for dz := -2; dz <= 2; dz++ {
				w.SetBlock(100+dx, 70+dy, 100+dz, planks)
			}
		}
	}
	before := count()
	fire := blockPos{100, 72, 100}
	w.SetBlock(fire.x, fire.y, fire.z, fireDefault)
	fires := map[blockPos]bool{fire: true}
	sawSpread := false
	for i := 0; i < 600; i++ {
		// Tick every live fire block (spread creates new ones).
		for p := range fires {
			if isFire(w.At(p.x, p.y, p.z)) {
				h.updateFire(players, p)
			}
		}
		// Discover any new fire blocks in the region and track them.
		for dx := -3; dx <= 3; dx++ {
			for dy := -1; dy <= 5; dy++ {
				for dz := -3; dz <= 3; dz++ {
					q := blockPos{100 + dx, 70 + dy, 100 + dz}
					if isFire(w.At(q.x, q.y, q.z)) && !fires[q] {
						fires[q] = true
						if q != fire {
							sawSpread = true
						}
					}
				}
			}
		}
	}
	if before-count() == 0 {
		t.Fatalf("fire consumed no planks (before=%d after=%d)", before, count())
	}
	if !sawSpread {
		t.Fatal("fire never spread to a new block")
	}
}
