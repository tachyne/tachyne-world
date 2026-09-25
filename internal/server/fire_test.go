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
	h.playerContactTick(players) // lava's hit (and its ignite) is the contact pass
	if pl.fireSecs != lavaFireSecs {
		t.Fatalf("lava must set %ds of afterburn, got %d", lavaFireSecs, pl.fireSecs)
	}
	// Step out: afterburn ticks damage.
	w.SetBlock(0, 70, 0, worldgen.Air)
	before := pl.health
	h.tick.Add(survivalTickN) // the afterburn tick is a second later, past the lava's cooldown
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
	// Fire only spreads or burns out within fire_spread_radius_around_player
	// of somebody, so the fixture needs a witness.
	pl := testTracked()
	pl.x, pl.y, pl.z = 3.5, 70, 3.5
	players := map[int32]*tracked{1: pl}
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
	h.explodeAt(players, float64(lx)+0.5, float64(y)+1, float64(lz)+0.5, 3, 3, blastMob)
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
	pl := testTracked() // fire needs a player within the spread radius
	pl.x, pl.y, pl.z = 100.5, 70, 100.5
	players := map[int32]*tracked{1: pl}
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

// fire_spread_radius_around_player replaced the boolean doFireTick in 1.21.9:
// a fire out of every player's reach neither spreads nor burns out, -1 lets it
// burn anywhere, and 0 is the old doFireTick=false.
func TestFireSpreadRadiusRule(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players := map[int32]*tracked{1: pl}
	near, far := blockPos{4, 70, 0}, blockPos{4000, 70, 0}

	if h.rules.FireSpreadRadius != defaultFireSpreadRadius {
		t.Errorf("the default radius is %d, want %d", h.rules.FireSpreadRadius, defaultFireSpreadRadius)
	}
	if !h.canSpreadFireAround(players, near) {
		t.Error("a fire four blocks from a player is well within the default radius")
	}
	if h.canSpreadFireAround(players, far) {
		t.Error("a fire four thousand blocks away is not")
	}
	h.rules.FireSpreadRadius = -1
	if !h.canSpreadFireAround(players, far) {
		t.Error("-1 means everywhere")
	}
	h.rules.FireSpreadRadius = 0
	if h.canSpreadFireAround(players, near) {
		t.Error("0 means nowhere — the old doFireTick=false")
	}
	h.rules.FireSpreadRadius = defaultFireSpreadRadius
	if h.canSpreadFireAround(map[int32]*tracked{}, near) {
		t.Error("with nobody online there is nobody to be near")
	}
	pl.dim = 1 // the same coordinates in another dimension are not nearby
	if h.canSpreadFireAround(players, near) {
		t.Error("a player in the Nether does not keep an overworld fire alive")
	}
}

// A world saved before the switch stored a boolean doFireTick. A stored false
// has to survive as radius 0, or a server that deliberately turned fire off
// would come back with it burning again.
func TestLegacyFireTickMigrates(t *testing.T) {
	off := false
	on := true
	for _, tc := range []struct {
		name   string
		legacy *bool
		want   int
	}{
		{"doFireTick: false becomes radius 0", &off, 0},
		{"doFireTick: true keeps the default", &on, defaultFireSpreadRadius},
		{"a world with neither keeps the default", nil, defaultFireSpreadRadius},
	} {
		dir := t.TempDir()
		h := newHub(world.New(1))
		h.rules = defaultRules()
		h.rules.LegacyFireTick = tc.legacy
		h.rulesPath = dir + "/rules.json"
		h.saveRules()
		// Re-read into a fresh hub, the way a restart does.
		h2 := newHub(world.New(1))
		h2.rules = defaultRules()
		h2.rulesPath = h.rulesPath
		h2.loadRules()
		if h2.rules.FireSpreadRadius != tc.want {
			t.Errorf("%s: radius %d, want %d", tc.name, h2.rules.FireSpreadRadius, tc.want)
		}
		if h2.rules.LegacyFireTick != nil {
			t.Errorf("%s: the legacy key must not survive the load", tc.name)
		}
	}
}

// A bed detonating where it must not leaves the crater burning; TNT never
// does. Vanilla lights one cleared cell in three, where the cell is air and
// what is under it is solid.
func TestBadRespawnBlastLightsFires(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	dim := 1 // the Nether, where a bed explodes
	w := h.worldFor(dim)
	cx, cy, cz := 300, 70, 300
	// A BEDROCK floor: the blast cannot take it, so every cleared cell above
	// it keeps something solid underneath and the fire roll is the only
	// variable left. (On a stone floor the crater eats its own footing and
	// the test turns flaky.)
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			w.SetBlock(cx+dx, cy-1, cz+dz, worldgen.Bedrock)
			for dy := 0; dy < 5; dy++ {
				w.SetBlock(cx+dx, cy+dy, cz+dz, worldgen.Air)
			}
		}
	}
	// Something for the blast to clear, standing on the floor. Fifty-odd
	// cells at one-in-three apiece leaves no realistic chance of none.
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			w.SetBlock(cx+dx, cy, cz+dz, worldgen.BlockBase("dirt"))
		}
	}
	h.explodeTyped(players, dim, float64(cx)+0.5, float64(cy)+0.5, float64(cz)+0.5,
		badRespawnPower, badRespawnPower, blastBlock, dtBadRespawnPoint, deathCause{}, withBlastFire())

	fires := 0
	for dx := -6; dx <= 6; dx++ {
		for dy := 0; dy < 4; dy++ {
			for dz := -6; dz <= 6; dz++ {
				if isFire(w.At(cx+dx, cy+dy, cz+dz)) {
					fires++
				}
			}
		}
	}
	if fires == 0 {
		t.Fatal("a bad-respawn blast should leave fires behind")
	}
}

// TNT leaves no fire: vanilla only sets the flag for a bad respawn point and
// a ghast's fireball.
func TestTNTBlastLeavesNoFire(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	cx, cy, cz := 340, 70, 340
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			w.SetBlock(cx+dx, cy-1, cz+dz, worldgen.Bedrock)
			w.SetBlock(cx+dx, cy, cz+dz, worldgen.BlockBase("dirt"))
		}
	}
	h.explodeIn(players, 0, float64(cx)+0.5, float64(cy)+0.5, float64(cz)+0.5, tntRadius, tntRadius, blastTNT)
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			if isFire(w.At(cx+dx, cy, cz+dz)) {
				t.Fatal("TNT must not light fires")
			}
		}
	}
}
