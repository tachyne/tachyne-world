package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// LivingEntity.pushEntities: mobs standing in one another are shoved apart.
// Before this existed tachyne had no mob-vs-mob separation anywhere, so two
// bees (or two cows, or forty zombies) occupied the same point exactly.

func pushWorld(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	// Real ground, so the tests that drive the full updateMobs step see a
	// walkable destination rather than a rejected move into the void.
	airBox(t, h.world, -8, 70, -8, 120, 90, 120)
	return h, players
}

// putMob drops a mob at an exact spot with its AI quiet, so a test measures the
// shove and nothing else.
func putMob(t *testing.T, h *hub, players map[int32]*tracked, etype int, x, y, z float64) *mob {
	t.Helper()
	m := h.spawnMob(players, etype, x, y, z)
	if m == nil {
		t.Fatalf("no %d spawned", etype)
	}
	m.x, m.y, m.z = x, y, z
	m.rest = 1 << 20 // idle forever: no steering to confuse the measurement
	return m
}

func gap(a, b *mob) float64 { return math.Hypot(a.x-b.x, a.z-b.z) }

func TestOverlappingMobsAreShovedApart(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityCow, 100.5, 70, 100.5)
	b := putMob(t, h, players, entityCow, 100.7, 70, 100.5) // well inside a 0.9 box

	before := gap(a, b)
	for i := 0; i < 40; i++ {
		h.pushMobs(players)
		a.x, a.z = a.x+a.pushX, a.z+a.pushZ
		b.x, b.z = b.x+b.pushX, b.z+b.pushZ
	}
	after := gap(a, b)
	if after <= before {
		t.Errorf("two cows sharing a spot stayed %.3f apart (started %.3f)", after, before)
	}
	if after < 0.9 {
		t.Errorf("cows settled %.3f apart, still inside one another's 0.9-wide box", after)
	}
}

// The shove is symmetric: Entity.push moves BOTH parties, in opposite
// directions, by the same amount.
func TestShoveMovesBothMobsApart(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityPig, 0.5, 70, 0.5)
	b := putMob(t, h, players, entityPig, 0.9, 70, 0.5)

	h.pushMobs(players)
	if a.pushX >= 0 {
		t.Errorf("the mob on the -x side got pushX=%.4f, want it shoved further -x", a.pushX)
	}
	if b.pushX <= 0 {
		t.Errorf("the mob on the +x side got pushX=%.4f, want it shoved further +x", b.pushX)
	}
	if d := math.Abs(a.pushX + b.pushX); d > 1e-9 {
		t.Errorf("the shove is not symmetric: %.6f vs %.6f", a.pushX, b.pushX)
	}
}

// Mobs that are not touching are left alone — the pass must not turn into a
// long-range repulsion field.
func TestSeparatedMobsAreNotPushed(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityCow, 0.5, 70, 0.5)
	b := putMob(t, h, players, entityCow, 3.5, 70, 0.5) // 3 blocks: no box touches

	h.pushMobs(players)
	if a.pushX != 0 || a.pushZ != 0 || b.pushX != 0 || b.pushZ != 0 {
		t.Errorf("mobs 3 blocks apart were pushed: a=(%.4f,%.4f) b=(%.4f,%.4f)",
			a.pushX, a.pushZ, b.pushX, b.pushZ)
	}
}

// The boxes are three-dimensional. A bee hovering well above a cow shares its
// footprint but not its space, and vanilla does not push them apart.
func TestMobsStackedOutOfReachDoNotPush(t *testing.T) {
	h, players := pushWorld(t)
	cow := putMob(t, h, players, entityCow, 0.5, 70, 0.5) // 1.4 tall
	// Offset horizontally too: two mobs sharing a spot to within 0.01 are not
	// pushed at all (Entity.push's guard), which would mask the height test.
	bee := putMob(t, h, players, entityBee, 0.9, 74, 0.5) // four blocks up

	h.pushMobs(players)
	if bee.pushX != 0 || bee.pushZ != 0 || cow.pushX != 0 || cow.pushZ != 0 {
		t.Error("a bee hovering four blocks above a cow was shoved by it")
	}
	bee.y = 71 // now inside the cow's box
	h.pushMobs(players)
	if bee.pushX == 0 && bee.pushZ == 0 {
		t.Error("a bee inside a cow's box was not shoved out of it")
	}
}

// Entity.push takes the LARGER axis gap, square-roots it and divides by that,
// so the impulse SHRINKS as the pair converges and vanishes entirely inside
// 0.01. It reads like a bug and is vanilla's actual arithmetic; if this ever
// gets "fixed" into a real normalisation, crowds will spring apart instead of
// oozing, so the quirk is pinned here on purpose.
func TestShoveWeakensAsMobsConverge(t *testing.T) {
	h, players := pushWorld(t)
	far := func(d float64) float64 {
		a := putMob(t, h, players, entityPig, 0.5, 70, 0.5)
		b := putMob(t, h, players, entityPig, 0.5+d, 70, 0.5)
		shove(a, b)
		got := math.Abs(a.pushX)
		h.removeMob(players, a)
		h.removeMob(players, b)
		return got
	}
	wide, tight := far(0.8), far(0.05)
	if tight >= wide {
		t.Errorf("shove at 0.05 apart (%.5f) is not weaker than at 0.8 apart (%.5f)", tight, wide)
	}

	// Inside 0.01 vanilla gives up entirely.
	a := putMob(t, h, players, entityPig, 0.5, 70, 0.5)
	b := putMob(t, h, players, entityPig, 0.505, 70, 0.5)
	shove(a, b)
	if a.pushX != 0 || b.pushX != 0 {
		t.Errorf("mobs 0.005 apart were pushed (%.6f); vanilla's guard is 0.01", a.pushX)
	}
}

// Entity.push is horizontal. A shoved mob never gains or loses height from it.
func TestShoveIsHorizontalOnly(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityBee, 0.5, 70, 0.5)
	b := putMob(t, h, players, entityBee, 0.7, 70.2, 0.5)
	ay, by := a.y, b.y
	h.pushMobs(players)
	if a.y != ay || b.y != by {
		t.Errorf("the shove moved a mob vertically: %.4f→%.4f, %.4f→%.4f", ay, a.y, by, b.y)
	}
}

// Bees were the reported case: they could sit inside one another indefinitely.
func TestBeesDoNotShareASpace(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityBee, 0.5, 72, 0.5)
	b := putMob(t, h, players, entityBee, 0.52, 72, 0.5)
	for i := 0; i < 60; i++ {
		h.pushMobs(players)
		a.x, a.z = a.x+a.pushX, a.z+a.pushZ
		b.x, b.z = b.x+b.pushX, b.z+b.pushZ
	}
	if a.overlaps(b) {
		t.Errorf("two bees still overlap after 60 updates: %.3f apart, box 0.55", gap(a, b))
	}
}

// isPushable, species by species. Being unpushable only exempts a mob from
// RECEIVING the shove: vanilla's list is the pushable entities around the
// pusher, and the pusher runs the pass whatever it is — so a bat clears a cow
// out of its way and is never shoved back. The one that drops out entirely is
// a dead mob, which is not ticking any more.
func TestUnpushableMobsAreLeftAlone(t *testing.T) {
	cases := []struct {
		name   string
		etype  int
		setup  func(m *mob)
		shoves bool // does it still push what it is standing in?
	}{
		{"a bat", entityBat, nil, true},
		{"a ridden mount", entityCow, func(m *mob) { m.rider = 42 }, true},
		{"a watched creaking", entityCreaking, func(m *mob) { m.frozen = true }, true},
		{"a sleeping villager", entityVillager, func(m *mob) { m.sleeping = true }, true},
		{"a wither charging its spawn", entityWither, func(m *mob) { m.spawnInvuln = 40 }, true},
		{"a dead mob", entityCow, func(m *mob) { m.dying = 10 }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, players := pushWorld(t)
			m := putMob(t, h, players, c.etype, 0.5, 70, 0.5)
			if c.setup != nil {
				c.setup(m)
			}
			other := putMob(t, h, players, entityCow, 0.9, 70, 0.5)
			h.pushMobs(players)
			if m.pushX != 0 || m.pushZ != 0 {
				t.Errorf("%s was shoved (%.4f, %.4f)", c.name, m.pushX, m.pushZ)
			}
			shoved := other.pushX != 0 || other.pushZ != 0
			if shoved != c.shoves {
				t.Errorf("%s: cow beside it shoved = %v, want %v (%.4f, %.4f)",
					c.name, shoved, c.shoves, other.pushX, other.pushZ)
			}
		})
	}
}

// An unpushable creaking becomes pushable the moment nobody is watching it.
func TestCreakingIsPushableOnceUnwatched(t *testing.T) {
	h, players := pushWorld(t)
	c := putMob(t, h, players, entityCreaking, 0.5, 70, 0.5)
	putMob(t, h, players, entityCow, 0.6, 70, 0.5)

	c.frozen = true
	h.pushMobs(players)
	if c.pushX != 0 {
		t.Fatal("a frozen creaking was shoved")
	}
	c.frozen = false
	h.pushMobs(players)
	if c.pushX == 0 && c.pushZ == 0 {
		t.Error("an unwatched creaking was still not shoved")
	}
}

// Sizes come from the vanilla EntityType registrations, with babies at half and
// the cube mobs scaled by their size.
func TestMobBoxesMatchVanilla(t *testing.T) {
	h, players := pushWorld(t)
	check := func(name string, m *mob, w, ht float64) {
		t.Helper()
		if b := m.box(); math.Abs(b.w-w) > 1e-6 || math.Abs(b.h-ht) > 1e-6 {
			t.Errorf("%s box %.4fx%.4f, want %.4fx%.4f", name, b.w, b.h, w, ht)
		}
	}
	check("cow", putMob(t, h, players, entityCow, 0.5, 70, 0.5), 0.9, 1.4)
	check("bee", putMob(t, h, players, entityBee, 0.5, 70, 8.5), 0.55, 0.5)
	check("ravager", putMob(t, h, players, entityRavager, 0.5, 70, 16.5), 1.95, 2.2)

	calf := putMob(t, h, players, entityCow, 0.5, 70, 24.5)
	calf.baby = true
	check("a calf", calf, 0.45, 0.7) // Cow.BABY_DIMENSIONS, exactly

	big := putMob(t, h, players, entitySlime, 0.5, 70, 32.5)
	big.size = 4
	check("a large slime", big, 2.08, 2.08) // 0.52 scaled by size
}

// maxEntityCramming: a mob sharing its box with the limit or more takes the
// crush. The roll is 1-in-4 per update, so this ticks until it lands.
func TestCrammingCrushesAPackedPen(t *testing.T) {
	h, players := pushWorld(t)
	victim := putMob(t, h, players, entityCow, 0.5, 70, 0.5)
	// Vanilla's threshold is "more than maxCramming-1 others", so pack the
	// victim's own box with exactly that many. They are given enough health to
	// survive the crush themselves — otherwise the pen thins itself out below
	// the threshold while the victim is still waiting for its 1-in-4 roll, and
	// the test measures the herd's mortality instead of the victim's.
	for i := 0; i < maxEntityCramming; i++ {
		m := putMob(t, h, players, entityCow, 0.5+float64(i)*1e-3, 70, 0.5)
		m.health = 1 << 20
	}
	full := victim.health
	for i := 0; i < 200 && victim.health == full; i++ {
		h.pushMobs(players)
	}
	if victim.health == full {
		t.Errorf("a cow packed with %d others took no cramming damage in 200 updates",
			maxEntityCramming)
	}
}

// …and an ordinary crowd is not crushed. A herd of a dozen is normal play.
func TestASmallCrowdIsNotCrushed(t *testing.T) {
	h, players := pushWorld(t)
	victim := putMob(t, h, players, entityCow, 0.5, 70, 0.5)
	for i := 0; i < 8; i++ {
		putMob(t, h, players, entityCow, 0.5+float64(i)*1e-3, 70, 0.5)
	}
	full := victim.health
	for i := 0; i < 200; i++ {
		h.pushMobs(players)
	}
	if victim.health != full {
		t.Errorf("a cow in a herd of 9 lost %d health to cramming", full-victim.health)
	}
}

// Mobs in different dimensions share coordinates constantly and must never
// interact — the same class of bug as the block-entity dimension keying.
func TestMobsInDifferentDimensionsDoNotPush(t *testing.T) {
	h, players := pushWorld(t)
	over := putMob(t, h, players, entityZombie, 0.5, 70, 0.5)
	nether := h.spawnMobIn(players, entityZombie, dimNether, 0.6, 70, 0.5)
	if nether == nil {
		t.Fatal("no nether zombie")
	}
	nether.rest = 1 << 20
	h.pushMobs(players)
	if over.pushX != 0 || nether.pushX != 0 {
		t.Errorf("a zombie shoved its counterpart in another dimension (%.4f, %.4f)",
			over.pushX, nether.pushX)
	}
}

// The shove decays once the crowd clears, rather than coasting forever.
func TestTheShoveDecaysWhenTheCrowdClears(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityCow, 0.5, 70, 0.5)
	b := putMob(t, h, players, entityCow, 0.7, 70, 0.5)
	h.pushMobs(players)
	if a.pushX == 0 {
		t.Fatal("no shove to decay")
	}
	h.removeMob(players, b)
	for i := 0; i < 200; i++ {
		h.pushMobs(players)
	}
	if a.pushX != 0 || a.pushZ != 0 {
		t.Errorf("the shove is still running at (%.6f, %.6f) long after the crowd left",
			a.pushX, a.pushZ)
	}
}

// A shoved mob keeps looking where it was looking. Vanilla's yaw comes from
// the move control — where the mob is walking — not from deltaMovement, so a
// jostled cow does not spin to face whatever bumped it. Ours would, because
// the yaw is derived from m.vx/m.vz, which is exactly why the shove rides its
// own accumulator instead.
func TestAShovedMobDoesNotTurnToFaceTheShove(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityCow, 100.5, 70, 100.5)
	putMob(t, h, players, entityCow, 100.9, 70, 100.5) // shoving from +x
	a.vx, a.vz, a.yaw = 0, 0, 0                        // standing still, facing +z

	startX := a.x
	h.updateMobs(players)
	if a.x == startX {
		t.Fatal("the cow was never shoved, so there is nothing to prove")
	}
	if a.yaw != 0 {
		t.Errorf("a shoved cow swung to yaw %.1f; the shove belongs in the position, "+
			"not in the steering velocity the facing is read from", a.yaw)
	}
}
