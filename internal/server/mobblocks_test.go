package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An updraft column carries a walker that would sink up to the surface
// and throws it clear of the water at the top.
func TestBubbleColumnLiftsASinker(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 196, worldgen.SoulSand, worldgen.Air)
	for y := 180; y <= 189; y++ {
		w.SetBlock(0, y, 0, worldgen.BubbleColumnUp)
	}
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	peak := z.y
	runMobs(h, players, 80, func() { peak = math.Max(peak, z.y) })
	if peak <= 190 {
		t.Fatalf("the column should carry the zombie out of the water: peak y=%.2f", peak)
	}
	if z.y < 186 {
		t.Errorf("the zombie should ride the top of the column, not the bed: y=%.2f", z.y)
	}
}

// A whirlpool over magma pulls down even a mob that swims for the surface.
func TestWhirlpoolDragsAFloaterDown(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 194, magmaBlockState, worldgen.Air)
	for y := 180; y <= 189; y++ {
		w.SetBlock(0, y, 0, worldgen.BubbleColumnDrag)
	}
	cow := h.spawnMob(players, entityCow, 0.5, 189, 0.5)
	runMobs(h, players, 200, nil)
	if cow.y > 186 {
		t.Errorf("the whirlpool should pull the cow under: y=%.2f", cow.y)
	}
}

// A mob caught in a cobweb over a drop sinks through it slowly, and the web
// takes the fall: it lands below unhurt.
func TestMobSinksThroughCobweb(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 188, worldgen.Stone, worldgen.Air)
	w.SetBlock(0, 185, 0, cobwebState)
	w.SetBlock(0, 186, 0, cobwebState)
	z := h.spawnMob(players, entityZombie, 0.5, 185, 0.5)
	hp := z.health
	runMobs(h, players, 1, nil)
	if z.y < 184.9 {
		t.Fatalf("a web holds the zombie: it dropped to y=%.3f in one update", z.y)
	}
	runMobs(h, players, 600, nil)
	if z.y != 180 {
		t.Fatalf("the zombie should have sunk through to the floor: y=%.3f", z.y)
	}
	if z.health != hp {
		t.Errorf("the web took the fall, but the zombie lost %d", hp-z.health)
	}
}

// A mob dropped onto slime takes no damage and bounces; walking on slime is
// slow.
func TestMobBouncesOnSlime(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 200, slimeMin, worldgen.Air)
	z := h.spawnMob(players, entityZombie, 0.5, 190, 0.5)
	hp := z.health
	peak := 0.0
	runMobs(h, players, 1, nil)
	runMobs(h, players, 30, func() { peak = math.Max(peak, z.y) })
	if z.health != hp {
		t.Errorf("slime catches a fall, but the zombie lost %d", hp-z.health)
	}
	if peak < 185 {
		t.Errorf("the zombie should bounce well up off the slime: peak y=%.2f", peak)
	}
	walker := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	if f := h.mobSpeedFactor(walker); f != slimeStepFactor {
		t.Errorf("walking on slime is slowed to %v, got %v", slimeStepFactor, f)
	}
}

// Landing blocks soften a mob's fall as they do a player's, and water on the
// way down takes the fall entirely.
func TestMobFallSoftenedByLandingBlockAndWater(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 200, worldgen.BlockBase("hay_block"), worldgen.Air)
	shaft(w, 10, 0, 180, 200, worldgen.Stone, worldgen.Air)
	for y := 180; y <= 182; y++ {
		w.SetBlock(10, y, 0, worldgen.WaterBase)
	}
	hay := h.spawnMob(players, entityZombie, 0.5, 191, 0.5)
	wet := h.spawnMob(players, entityZombie, 10.5, 195, 0.5)
	hp := hay.health
	runMobs(h, players, 2, nil)
	if hay.y != 180 {
		t.Fatalf("the zombie should be on the hay: y=%v", hay.y)
	}
	if got := hp - hay.health; got != 1 { // floor((11 − 3) × 0.2)
		t.Errorf("an 11-block fall onto hay costs 1, got %d", got)
	}
	if wet.health != hp {
		t.Errorf("a fall into water costs nothing, the zombie lost %d", hp-wet.health)
	}
}

// A mob falling against a honey block's side slides down it and lands
// unhurt.
func TestMobSlidesDownHoneyWall(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	w.ForceLoad(0, 0, 1)
	for x := -2; x <= 3; x++ {
		for z := -2; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	for y := 180; y <= 190; y++ {
		w.SetBlock(1, y, 0, honeyMin)
	}
	z := h.spawnMob(players, entityZombie, 0.75, 190, 0.5)
	hp := z.health
	runMobs(h, players, 1, nil)
	if z.y < 189 {
		t.Fatalf("the honey should hold the zombie to a slide: y=%.2f after one update", z.y)
	}
	runMobs(h, players, 120, nil)
	if z.y != 180 {
		t.Fatalf("the zombie should reach the floor: y=%.2f", z.y)
	}
	if z.health != hp {
		t.Errorf("a honey slide costs nothing at the bottom, the zombie lost %d", hp-z.health)
	}
}

// A player falling through a cobweb has the fall reset there: only the drop
// below the web counts.
func TestPlayerFallResetByCobweb(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w.ForceLoad(0, 0, 1)
	w.SetBlock(0, 179, 0, worldgen.Stone)
	w.SetBlock(0, 185, 0, cobwebState)
	pl.x, pl.y, pl.z = 0.5, 190, 0.5
	move := func(y float64, ground bool) {
		h.onFallAndExhaust(players, pl, evMove{eid: pl.p.eid, x: 0.5, y: y, z: 0.5, onGround: ground})
		pl.y = y
	}
	move(190, false)
	move(185.2, false) // in the web
	move(180, true)
	if got := 20 - pl.health; got != 2 { // floor(5.2 − 3)
		t.Errorf("a fall through a web counts from the web: lost %v, want 2", got)
	}
}
