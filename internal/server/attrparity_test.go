package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// /attribute block_break_speed doubles how fast a dig advances: the crack
// stages another player sees move twice as fast.
func TestBlockBreakSpeedAttributeSpeedsDigging(t *testing.T) {
	progressAfter := func(set bool) float64 {
		h, pl, players := cmdHub()
		h.world.ForceLoad(0, 0, 1)
		pl.onGround = true
		h.world.SetBlock(1, 180, 0, worldgen.BlockBase("stone"))
		pl.inv.slots[pl.p.held] = invStack{item: itemByName["wooden_pickaxe"], count: 1}
		if set {
			h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: attr.BlockBreakSpeed, op: "base set", value: 2})
		}
		h.startDig(players, evDigStart{eid: pl.p.eid, x: 1, y: 180, z: 0})
		for i := 0; i < 5; i++ {
			h.tickDigCracks(players)
		}
		return h.digs[pl.p.eid].progress
	}
	base, fast := progressAfter(false), progressAfter(true)
	if base <= 0 || fast < base*1.99 || fast > base*2.01 {
		t.Fatalf("five ticks of digging: %v at block_break_speed 1, %v at 2; want double", base, fast)
	}
}

// flierRun spawns a parrot over a stone floor, optionally sets its
// FLYING_SPEED by /attribute, and returns how far it flew in 200 updates.
func flierRun(t *testing.T, set func(h *hub, players map[int32]*tracked)) (dist float64, m *mob) {
	t.Helper()
	h, _, players := cmdHub()
	h.world.ForceLoad(0, 0, 2)
	for x := -16; x < 32; x++ {
		for z := -16; z < 32; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	m = h.spawnSpecies(players, entityParrot, 0, 8.5, 182, 8.5)
	if set != nil {
		set(h, players)
	}
	for i := 0; i < 200; i++ {
		ox, oz := m.x, m.z
		h.updateMobs(players)
		dist += math.Hypot(m.x-ox, m.z-oz)
	}
	return dist, m
}

// /attribute flying_speed changes how fast a flier crosses the air, and a
// reset puts it back exactly where the species started.
func TestFlyingSpeedAttributeMovesFliers(t *testing.T) {
	base, _ := flierRun(t, nil)
	fast, _ := flierRun(t, func(h *hub, players map[int32]*tracked) {
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=parrot]", id: attr.FlyingSpeed, op: "base set", value: 0.8})
	})
	t.Logf("default %v, doubled %v", base, fast)
	if base <= 0 || fast < base*1.5 {
		t.Fatalf("a parrot flew %v at its own flying speed and %v at double it", base, fast)
	}
	reset, m := flierRun(t, func(h *hub, players map[int32]*tracked) {
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=parrot]", id: attr.FlyingSpeed, op: "base set", value: 0.8})
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=parrot]", id: attr.FlyingSpeed, op: "base reset"})
	})
	if reset != base {
		t.Fatalf("after a reset the parrot flew %v, not the %v it flies by default", reset, base)
	}
	if got := m.mobAttrs().Value(attr.FlyingSpeed); got != 0.4 {
		t.Fatalf("a parrot's flying speed resets to %v, want its species' 0.4", got)
	}
}

// A player on /attribute gravity 0 floats because their client floats them;
// the float check must not ground them. At half gravity it allows twice
// the time, as getMaximumFlyingTicks does.
func TestLowGravityPlayerIsNotGrounded(t *testing.T) {
	hover := func(gravity float64, ticks int) (grounded bool) {
		h := newHub(world.New(1))
		pl, players := walkSetup(h)
		pl.p.eid = 1
		surface := pl.y
		pl.y = surface + 20
		h.tick.Store(1000)
		pl.lastMoveTick = 1000
		if gravity >= 0 {
			h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: attr.Gravity, op: "base set", value: gravity})
		}
		for i := 1; i <= ticks/5; i++ {
			h.tick.Store(1000 + uint64(i*5))
			h.onMove(players, pl, evMove{eid: 1, x: 0.5, y: surface + 20, z: 0.5})
		}
		return pl.y < surface+2
	}
	if !hover(-1, 150) {
		t.Fatal("at normal gravity a 150-tick hover was not grounded")
	}
	if hover(0, 600) {
		t.Fatal("a player with no gravity was grounded for floating")
	}
	if hover(0.04, 150) {
		t.Fatal("at half gravity a 150-tick hover was grounded; the limit is 160")
	}
	if !hover(0.04, 200) {
		t.Fatal("at half gravity a 200-tick hover was not grounded")
	}
}

// A zombie on /attribute gravity 0 stays put when the ground under it is
// dug out; at the default it drops to the new floor.
func TestZeroGravityMobHangsInTheAir(t *testing.T) {
	drop := func(zero bool) float64 {
		h, _, players := cmdHub()
		h.world.ForceLoad(0, 0, 2)
		for x := -8; x < 24; x++ {
			for z := -8; z < 24; z++ {
				h.world.SetBlock(x, 170, z, worldgen.BlockBase("stone"))
			}
		}
		h.world.SetBlock(10, 179, 10, worldgen.BlockBase("stone"))
		m := h.spawnMob(players, entityPig, 10.5, 180, 10.5)
		m.vx, m.vz, m.reroute = 0, 0, 1000
		if zero {
			h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=pig]", id: attr.Gravity, op: "base set", value: 0})
		}
		h.world.SetBlock(10, 179, 10, 0) // dig it out
		h.updateMobs(players)
		return m.y
	}
	if y := drop(false); y > 172 {
		t.Fatalf("at normal gravity the pig stayed at y=%v over a dug-out floor", y)
	}
	if y := drop(true); y != 180 {
		t.Fatalf("with no gravity the pig fell to y=%v", y)
	}
}

// The arc of a leap follows GRAVITY, and Slow Falling caps it on the way
// down: the leap comes down later on both.
func TestLeapArcFollowsGravity(t *testing.T) {
	airTime := func(setup func(h *hub, players map[int32]*tracked, m *mob)) int {
		h, _, players := cmdHub()
		h.world.ForceLoad(0, 0, 2)
		for x := -8; x < 24; x++ {
			for z := -8; z < 24; z++ {
				h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
			}
		}
		m := h.spawnMob(players, entityWolf, 8.5, 180, 8.5)
		if setup != nil {
			setup(h, players, m)
		}
		m.leaping, m.leapVX, m.leapVY, m.leapVZ = true, 0, 0.4, 0
		for i := 1; i <= 200; i++ {
			h.leapFlight(players, m)
			if !m.leaping {
				return i
			}
		}
		return 200
	}
	base := airTime(nil)
	light := airTime(func(h *hub, players map[int32]*tracked, m *mob) {
		h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=wolf]", id: attr.Gravity, op: "base set", value: 0.02})
	})
	slow := airTime(func(h *hub, players map[int32]*tracked, m *mob) { m.startEffect(effSlowFalling, 0, 30) })
	if light <= base || slow <= base {
		t.Fatalf("a leap lasted %d updates at normal gravity, %d at a quarter, %d under Slow Falling", base, light, slow)
	}
}

// Slow Falling keeps a mob's fall distance at nothing, so the drop when its
// floor is dug out does not hurt it.
func TestSlowFallingMobTakesNoFallDamage(t *testing.T) {
	hurt := func(slow bool) bool {
		h, _, players := cmdHub()
		h.world.ForceLoad(0, 0, 2)
		for x := -8; x < 24; x++ {
			for z := -8; z < 24; z++ {
				h.world.SetBlock(x, 160, z, worldgen.BlockBase("stone"))
			}
		}
		h.world.SetBlock(10, 179, 10, worldgen.BlockBase("stone"))
		m := h.spawnMob(players, entityPig, 10.5, 180, 10.5)
		m.vx, m.vz, m.reroute = 0, 0, 1000
		if slow {
			m.startEffect(effSlowFalling, 0, 30)
		}
		before := m.health
		h.world.SetBlock(10, 179, 10, 0)
		h.updateMobs(players)
		return m.health < before
	}
	if !hurt(false) {
		t.Fatal("a nineteen-block drop did not hurt the pig")
	}
	if hurt(true) {
		t.Fatal("a pig under Slow Falling took fall damage")
	}
}

// /attribute scale grows a mob's box: a block that clears a zombie's head
// is refused over a zombie twice the size, and the change reaches the
// players watching so their clients draw it that size.
func TestScaleAttributeGrowsTheMob(t *testing.T) {
	obstructed := func(scale float64) (bool, bool) {
		h, pl, players := cmdHub()
		h.world.ForceLoad(0, 0, 2)
		m := h.spawnMob(players, entityZombie, 8.5, 180, 8.5)
		drainEvs(pl.p)
		if scale != 1 {
			h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=zombie]", id: attr.Scale, op: "base set", value: scale})
		}
		synced := false
		for _, ev := range drainEvs(pl.p) {
			if fr, ok := ev.(attachproto.EntityAttributes); ok && fr.EID == m.eid {
				for _, a := range fr.Attrs {
					if a.Name == string(attr.Scale) && a.Base == scale {
						synced = true
					}
				}
			}
		}
		h.publishBodies(players)
		return h.placeObstructed(dimOverworld, 8, 182, 8, worldgen.BlockBase("stone")), synced
	}
	if blocked, _ := obstructed(1); blocked {
		t.Fatal("a block over a normal zombie's head was refused")
	}
	blocked, synced := obstructed(2)
	if !blocked {
		t.Fatal("a block inside a double-size zombie was allowed")
	}
	if !synced {
		t.Fatal("the new scale was not sent to the watching player")
	}
}

// A player shrunk with /attribute scale may walk under a one-block gap;
// the server must not take their head for being inside the ceiling.
func TestSmallPlayerWalksUnderALowCeiling(t *testing.T) {
	moved := func(scale float64) bool {
		h := newHub(world.New(1))
		pl, players := walkSetup(h)
		h.world.ForceLoad(0, 0, 1)
		y := pl.y
		h.world.SetBlock(1, int(math.Floor(y))+1, 0, worldgen.BlockBase("stone")) // a ceiling one block up
		h.world.SetBlock(1, int(math.Floor(y)), 0, 0)
		if scale != 1 {
			h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: attr.Scale, op: "base set", value: scale})
		}
		h.tick.Store(101)
		h.onMove(players, pl, evMove{eid: 1, x: 1.5, y: y, z: 0.5, onGround: true})
		return pl.x == 1.5
	}
	if moved(1) {
		t.Fatal("a full-size player walked into a one-block gap")
	}
	if !moved(0.5) {
		t.Fatal("a half-size player was stopped at a one-block gap")
	}
}

// piglinFixture is a sword piglin on a stone floor with a gold-clad player
// beside it and a player in no gold ten blocks off.
func piglinFixture(t *testing.T) (*hub, map[int32]*tracked, *mob, *tracked, *tracked) {
	t.Helper()
	h, far, players := cmdHub()
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x < 24; x++ {
		for z := -8; z < 24; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	gold := cmdSecondPlayer(players, 2, "golden")
	gold.armor[3] = invStack{item: int32(itemByName["golden_helmet"]), count: 1}
	pg := h.spawnSpecies(players, entityPiglin, 0, 8.5, 180, 8.5)
	if pg == nil {
		t.Fatal("no piglin")
	}
	pg.held, pg.baby, pg.attackCD = int32(itemByName["golden_sword"]), false, 0
	gold.x, gold.y, gold.z = 9.5, 180, 8.5
	far.x, far.y, far.z = 18.5, 180, 8.5
	return h, players, pg, gold, far
}

// A piglin hunting a player in no gold does not cut down the gold-clad
// player standing beside it on the way.
func TestPiglinMeleeSparesGold(t *testing.T) {
	h, players, pg, gold, _ := piglinFixture(t)
	for i := 0; i < 6; i++ {
		pg.x, pg.y, pg.z = 8.5, 180, 8.5 // held in place beside the bystander
		h.updateMobs(players)
		if gold.health < 20 {
			t.Fatalf("the piglin hit the player in gold (update %d)", i)
		}
	}
	if !pg.hasTarget {
		t.Fatal("the piglin was not hunting the player in no gold")
	}
}

// Hit a piglin while wearing gold and it fights back: the one it is angry
// at is its target, gold or not.
func TestPiglinRetaliatesAgainstGold(t *testing.T) {
	h, players, pg, gold, far := piglinFixture(t)
	far.x = 400 // nobody else about
	h.attackMob(players, gold.p.eid, pg.eid)
	hp := gold.health
	for i := 0; i < 20 && gold.health >= hp; i++ {
		pg.x, pg.y, pg.z = 8.5, 180, 8.5
		pg.attackCD = 0
		h.updateMobs(players)
	}
	if gold.health >= hp {
		t.Fatal("the piglin never hit back at the player in gold who struck it")
	}
}
