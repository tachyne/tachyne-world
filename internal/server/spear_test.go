package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// spearFixture arms a survival player with a spear, high in open air
// (y=180) facing +z (yaw 0, pitch 0), at a hub tick past zero.
func spearFixture(t *testing.T, spear string) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.tick.Store(1000)
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 0, 0
	id := itemByName[spear]
	pl.p.setHotbarSlot(0, id)
	pl.inv.slots[0] = invStack{item: id, count: 1}
	return h, pl, map[int32]*tracked{pl.p.eid: pl}
}

// cowAt stands a 100-hp cow d blocks in front of the fixture player (its
// eyes are inside the cow's box height).
func cowAt(h *hub, eid int32, x, d float64) *mob {
	m := &mob{eid: eid, etype: entityCow, health: 100, x: x, y: 180.5, z: 0.5 + d}
	m.setMaxHP(100)
	h.mobs[eid] = m
	return m
}

// The numbers come straight from Item.Properties.spear's arguments.
func TestSpearTables(t *testing.T) {
	cases := []struct {
		name          string
		dmg, period   int
		delay         int
		mult          float64
		dis, kb, dmgT int
	}{
		{"wooden_spear", 1, 13, 15, 0.7, 100, 200, 300},
		{"stone_spear", 2, 15, 14, 0.82, 90, 180, 275},
		{"copper_spear", 2, 17, 13, 0.82, 80, 165, 250},
		{"iron_spear", 3, 19, 12, 0.95, 50, 135, 225},
		{"golden_spear", 1, 19, 14, 0.7, 70, 170, 275},
		{"diamond_spear", 4, 21, 10, 1.075, 60, 130, 200},
		{"netherite_spear", 5, 23, 8, 1.2, 50, 110, 175},
	}
	for _, c := range cases {
		id := itemByName[c.name]
		sp := spearOf(id)
		if sp == nil {
			t.Fatalf("%s: no spear spec", c.name)
		}
		if got := meleeDamage[id]; got != c.dmg {
			t.Errorf("%s: jab damage %d, want %d", c.name, got, c.dmg)
		}
		if got := attackPeriod(id); got != c.period {
			t.Errorf("%s: attack period %d, want %d", c.name, got, c.period)
		}
		if sp.delay != c.delay || float32(sp.mult) != float32(c.mult) {
			t.Errorf("%s: delay %d mult %v, want %d %v", c.name, sp.delay, sp.mult, c.delay, c.mult)
		}
		if sp.dismount.maxTicks != c.dis || sp.knock.maxTicks != c.kb || sp.damage.maxTicks != c.dmgT {
			t.Errorf("%s: windows %d/%d/%d, want %d/%d/%d", c.name,
				sp.dismount.maxTicks, sp.knock.maxTicks, sp.damage.maxTicks, c.dis, c.kb, c.dmgT)
		}
	}
	if spearOf(itemByName["iron_sword"]) != nil {
		t.Error("a sword is not a spear")
	}
}

// A jab deals the spear's attack damage, material by material.
func TestSpearStabDamagePerMaterial(t *testing.T) {
	for name, want := range map[string]int{"wooden_spear": 1, "stone_spear": 2, "copper_spear": 2,
		"iron_spear": 3, "golden_spear": 1, "diamond_spear": 4, "netherite_spear": 5} {
		h, pl, players := spearFixture(t, name)
		m := cowAt(h, 9, 0.5, 3)
		h.spearStab(players, pl)
		if got := 100 - m.health; got != want {
			t.Errorf("%s jab dealt %d, want %d", name, got, want)
		}
	}
}

// The jab strikes everything on its line between 2 and 4.5 blocks — and
// nothing nearer, further, or beside it.
func TestSpearStabHitsEveryMobInLine(t *testing.T) {
	h, pl, players := spearFixture(t, "iron_spear")
	a := cowAt(h, 9, 0.5, 2.3)
	b := cowAt(h, 10, 0.5, 3.4)
	c := cowAt(h, 11, 0.6, 4.6)  // box starts at 4.15: on the line
	near := cowAt(h, 12, 0.5, 1) // box ends at 1.45: inside the minimum reach
	far := cowAt(h, 13, 0.5, 5.4)
	side := cowAt(h, 14, 3.5, 3)
	h.spearStab(players, pl)
	for _, m := range []*mob{a, b, c} {
		if m.health != 97 {
			t.Errorf("cow %d on the line: health %d, want 97", m.eid, m.health)
		}
	}
	for _, m := range []*mob{near, far, side} {
		if m.health != 100 {
			t.Errorf("cow %d off the line was hit: health %d", m.eid, m.health)
		}
	}
	if pl.inv.slots[0].dmg != 3 {
		t.Errorf("three enemies struck should wear the spear 3, wore %d", pl.inv.slots[0].dmg)
	}
}

// A wall between the eyes and the minimum reach stops the jab dead.
func TestSpearStabBlockedByWall(t *testing.T) {
	h, pl, players := spearFixture(t, "iron_spear")
	m := cowAt(h, 9, 0.5, 3)
	h.world.SetBlock(0, 181, 1, worldgen.Stone) // a block 0.5-1.5 out, at eye height
	h.spearStab(players, pl)
	if m.health != 100 {
		t.Fatalf("the jab went through a wall: health %d", m.health)
	}
}

// MINIMUM_ATTACK_CHARGE: a second jab straight after the first does nothing.
func TestSpearStabNeedsFullCharge(t *testing.T) {
	h, pl, players := spearFixture(t, "iron_spear")
	m := cowAt(h, 9, 0.5, 3)
	h.spearStab(players, pl)
	m.invulnTicks, m.lastHurt = 0, 0 // take the hurt cooldown out of it
	h.tick.Add(3)
	h.spearStab(players, pl)
	if m.health != 97 {
		t.Fatalf("an uncharged jab landed: health %d, want 97", m.health)
	}
	h.tick.Add(16) // 19 ticks after the first: iron's full period
	m.invulnTicks, m.lastHurt = 0, 0
	h.spearStab(players, pl)
	if m.health != 94 {
		t.Fatalf("a recharged jab should land: health %d, want 94", m.health)
	}
}

// chargeAt lowers the fixture player's spear far enough back that the delay
// has passed, moving forward at v blocks a tick.
func chargeAt(h *hub, pl *tracked, v float64) {
	sp := spearOf(pl.p.heldItem())
	pl.spearAt = h.tick.Load() - uint64(sp.delay) - 1
	pl.spearHits = map[int32]uint64{}
	h.noteKnownMove(pl, 0, 0, v)
}

// The charge's damage is 1 + floor(closing speed × multiplier).
func TestSpearChargeDamageScales(t *testing.T) {
	for _, c := range []struct {
		v    float64 // blocks per tick
		want int
	}{
		{0.3, 1 + 5}, // 6 b/s × 0.95 = 5.7
		{0.5, 1 + 9}, // 10 b/s × 0.95 = 9.5
		{0.8, 1 + 15},
	} {
		h, pl, players := spearFixture(t, "iron_spear")
		m := cowAt(h, 9, 0.5, 3)
		chargeAt(h, pl, c.v)
		h.spearChargeTick(players, pl)
		if got := 100 - m.health; got != c.want {
			t.Errorf("charge at %v b/t dealt %d, want %d", c.v, got, c.want)
		}
		if m.vz <= 0 {
			t.Errorf("charge at %v b/t (over 5.1 b/s) should knock the cow on", c.v)
		}
	}
}

// Below 4.6 blocks a second of closing speed, and past the damage window,
// the lowered spear does no damage.
func TestSpearChargeThresholdAndWindow(t *testing.T) {
	h, pl, players := spearFixture(t, "iron_spear")
	m := cowAt(h, 9, 0.5, 3)
	chargeAt(h, pl, 0.2) // 4 b/s
	h.spearChargeTick(players, pl)
	if m.health != 100 {
		t.Fatalf("a slow charge hurt: health %d", m.health)
	}

	h, pl, players = spearFixture(t, "iron_spear")
	m = cowAt(h, 9, 0.5, 3)
	chargeAt(h, pl, 0.5)
	pl.spearAt -= 226 // the damage window (225 ticks after the delay) is over
	h.spearChargeTick(players, pl)
	if m.health != 100 {
		t.Fatalf("a charge past its window hurt: health %d", m.health)
	}

	// A target running away as fast closes at nothing.
	h, pl, players = spearFixture(t, "iron_spear")
	m = cowAt(h, 9, 0.5, 3)
	m.vz = 0.5 * mobMoveInterval // 0.5 blocks a tick away
	chargeAt(h, pl, 0.5)
	h.spearChargeTick(players, pl)
	if m.health != 100 {
		t.Fatalf("no closing speed, yet the charge hurt: health %d", m.health)
	}
}

// Each target is struck once per contact cooldown, and a released spear
// strikes nothing.
func TestSpearChargeContactAndRelease(t *testing.T) {
	h, pl, players := spearFixture(t, "iron_spear")
	cowAt(h, 9, 0.5, 3)
	chargeAt(h, pl, 0.5)
	h.spearChargeTick(players, pl)
	at := pl.spearHits[9]
	h.tick.Add(4)
	h.noteKnownMove(pl, 0, 0, 0.5)
	h.spearChargeTick(players, pl)
	if pl.spearHits[9] != at {
		t.Fatal("the cow was struck again inside the ten-tick contact cooldown")
	}
	stopSpearCharge(pl)
	if pl.spearAt != 0 || pl.spearHits != nil {
		t.Fatal("releasing use should raise the spear")
	}
}

// A charging spear hits players too — PvP rules apply.
func TestSpearChargeHitsPlayer(t *testing.T) {
	h, pl, players := spearFixture(t, "iron_spear")
	v := survPlayer(h)
	v.p = newPlayer(2, "victim", [16]byte{2})
	v.x, v.y, v.z = 0.5, 180, 3.5
	players[v.p.eid] = v
	h.rules.PvP = true
	chargeAt(h, pl, 0.5)
	h.spearChargeTick(players, pl)
	if v.health >= 20 {
		t.Fatalf("the charge missed the player: health %v", v.health)
	}
}

// Zombies arm themselves as 26.3's do: one armed zombie in six has a spear.
func TestZombieSpawnsWithSpear(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffHard
	counts := map[int32]int{}
	for i := 0; i < 6000; i++ {
		m := &mob{eid: int32(100 + i), etype: entityZombie, health: 20, x: 0.5, y: 180, z: 0.5}
		h.spawnGear(nil, m)
		counts[m.held]++
	}
	spears, swords, shovels := counts[itemIronSpear], counts[itemIronSword], counts[itemIronShovel]
	if spears == 0 || swords == 0 || shovels == 0 {
		t.Fatalf("armed zombies: %d spears, %d swords, %d shovels — each should appear", spears, swords, shovels)
	}
	if shovels < 2*spears {
		t.Errorf("shovels (4 in 6) should far outnumber spears (1 in 6): %d vs %d", shovels, spears)
	}
}

// ZombifiedPiglin.populateDefaultEquipmentSlots: a golden sword, one in twenty a golden spear.
func TestZombifiedPiglinWeapon(t *testing.T) {
	h := newHub(world.New(1))
	spears := 0
	for i := 0; i < 2000; i++ {
		m := &mob{eid: int32(100 + i), etype: entityZombifiedPiglin}
		h.zombifiedPiglinWeapon(nil, m)
		switch m.held {
		case itemGoldSpear:
			spears++
		case itemGoldSword:
		default:
			t.Fatalf("zombified piglin spawned holding %d", m.held)
		}
	}
	if spears < 50 || spears > 160 {
		t.Errorf("%d golden spears in 2000, want about 100", spears)
	}
	// A reloaded or already-armed one keeps what it has.
	m := &mob{etype: entityZombifiedPiglin, held: itemIronSword}
	h.zombifiedPiglinWeapon(nil, m)
	if m.held != itemIronSword {
		t.Error("an armed zombified piglin was re-armed")
	}
}

// A zombie with a spear closes, lowers it within ten blocks, and its charge
// hurts the player it runs into.
func TestZombieSpearCharge(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(1000)
	h.rules.Difficulty = diffNormal
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 20.5
	players := map[int32]*tracked{pl.p.eid: pl}
	z := &mob{eid: 20, etype: entityZombie, health: 20, x: 0.5, y: 180, z: 0.5,
		hostile: true, hasTarget: true, held: itemIronSpear}
	z.mobAttrs().SetBase(attr.AttackDamage, 3)
	z.setFollowRange(35) // the zombie family's FOLLOW_RANGE
	h.mobs[z.eid] = z

	if !h.spearGoalStep(players, z) || z.spearUseAt != 0 || z.vz <= 0 {
		t.Fatalf("20 blocks off the zombie should walk in, spear up (using=%d vz=%v)", z.spearUseAt, z.vz)
	}
	pl.z = 8.5
	h.spearGoalStep(players, z)
	if z.spearUseAt == 0 || !z.handActive {
		t.Fatal("within ten blocks the zombie should lower its spear")
	}

	// The charge connects: 2 b/s of closing speed, a fifth of a player's thresholds.
	pl.z = 2.3
	z.spearUseAt = h.tick.Load() - uint64(spearOf(itemIronSpear).delay) - 1
	z.vx, z.vz = 0, 0.2 // per update: 0.1 blocks a tick
	h.mobSpearTick(players, z)
	if got := 20 - pl.health; got != 4 { // 3 + floor(2 × 0.95)
		t.Fatalf("zombie charge dealt %v, want 4", got)
	}
}
