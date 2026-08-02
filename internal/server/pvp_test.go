package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func pvpPair(h *hub) (*tracked, *tracked, map[int32]*tracked) {
	a := survPlayer(h)
	b := &tracked{p: newPlayer(2, "victim", [16]byte{2}), gamemode: gmSurvival}
	initSurvival(b)
	a.x, a.y, a.z = 0, 180, 0
	b.x, b.y, b.z = 1, 180, 0
	return a, b, map[int32]*tracked{a.p.eid: a, b.p.eid: b}
}

// The whole point: one player can now hurt another.
func TestPlayersCanHurtEachOther(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)

	if !h.attackPlayer(players, a.p.eid, b.p.eid) {
		t.Fatal("a player target should be claimed by the player path")
	}
	if b.health >= 20 {
		t.Fatalf("the victim took no damage: health=%v", b.health)
	}
	// …and a swing at a target that is not a player falls through to mobs.
	if h.attackPlayer(players, a.p.eid, 9999) {
		t.Error("a non-player target should fall through")
	}
}

// The guards: reach, self-harm, dimension, gamemode and the pvp rule.
func TestPvPGuards(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)

	check := func(name string, setup func(), wantHurt bool) {
		b.health = 20
		a.lastAttack = 0
		setup()
		h.attackPlayer(players, a.p.eid, b.p.eid)
		if hurt := b.health < 20; hurt != wantHurt {
			t.Errorf("%s: hurt=%v want %v (health %v)", name, hurt, wantHurt, b.health)
		}
	}

	check("baseline", func() {}, true)
	check("pvp off", func() { h.rules.PvP = false }, false)
	h.rules.PvP = true
	check("out of reach", func() { b.x = 40 }, false)
	b.x = 1
	check("other dimension", func() { b.dim = 1 }, false)
	b.dim = 0
	check("creative victim", func() { b.gamemode = gmCreative }, false)
	b.gamemode = gmSurvival
	check("spectator victim", func() { b.gamemode = gmSpectator }, false)
	b.gamemode = gmSurvival
	check("dead victim", func() { b.dead = true }, false)
	b.dead = false
	check("back to normal", func() {}, true)

	// Hitting yourself does nothing.
	a.health = 20
	h.attackPlayer(players, a.p.eid, a.p.eid)
	if a.health < 20 {
		t.Error("a player hurt themselves")
	}
}

// A raised shield blunts the blow, but only from the front.
//
// The attacker sits at x=0 and the victim at x=1, so the blow comes from -x.
// The look vector is (-sin yaw, cos yaw): yaw 90 points at -x, into the blow,
// and yaw -90 points away from it. The previous version of this test used -90
// and then accepted damage either way, so it could not fail.
func TestPvPRespectsArmourAndShields(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)

	h.attackPlayer(players, a.p.eid, b.p.eid)
	bare := 20 - b.health
	if bare <= 0 {
		t.Fatal("the unshielded blow did no damage — this test would prove nothing")
	}

	h.tick.Store(uint64(shieldDelay) + 2)
	b.inv.slots[0] = invStack{item: itemShield, count: 1}
	b.p.held = 0

	b.health, a.lastAttack, b.blockingSince = 20, 0, 1
	b.yaw = 90 // facing the attacker
	h.attackPlayer(players, a.p.eid, b.p.eid)
	if b.health != 20 {
		t.Errorf("a shield facing the blow should have caught it: took %v", 20-b.health)
	}

	b.health, a.lastAttack, b.blockingSince = 20, 0, 1
	b.yaw = -90 // turned away from it
	h.attackPlayer(players, a.p.eid, b.p.eid)
	if b.health == 20 {
		t.Error("a shield turned away from the blow should not have caught it")
	}
}

// A player's arrow is PvP too: it hits, and it stops hitting when the rule is
// off — the melee path and the bow must not disagree.
func TestPlayerArrowsObeyThePvPRule(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)
	b.y = 180

	shoot := func() {
		arrow := &arrowEntity{eid: h.allocEID(), dim: 0, shooter: a.p.eid,
			dmg: 5, playerShot: true, x: b.x, y: b.y, z: b.z}
		h.arrowHitsPlayer(players, arrow, b.x, b.y+0.5, b.z)
	}

	b.health = 20
	shoot()
	if b.health >= 20 {
		t.Fatal("a player's arrow should hit another player")
	}

	h.rules.PvP = false
	b.health = 20
	shoot()
	if b.health < 20 {
		t.Error("a player's arrow ignored the pvp gamerule")
	}
}

// Thorns is the victim's armour biting the ATTACKER, and vanilla hangs it on
// the victim's equipment without caring whether a mob or a player landed the
// blow — so it has to fire in PvP exactly as it does against a mob's bite.
func TestThornsBitesAPlayerAttacker(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)

	swing := func() {
		a.health, b.health = 20, 20
		a.lastAttack = 0
		h.attackPlayer(players, a.p.eid, b.p.eid)
	}

	// An unarmoured victim never hurts the attacker, however many blows land.
	for i := 0; i < 200; i++ {
		swing()
		if a.health < 20 {
			t.Fatalf("a bare victim retaliated: attacker health=%v", a.health)
		}
	}

	for i := range b.armor {
		b.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1,
			ench: [2]enchApply{{id: enchThorns, lvl: 3}}}
	}
	bit := 0
	for i := 0; i < 200; i++ {
		swing()
		if a.health < 20 {
			bit++
		}
	}
	if bit == 0 {
		t.Error("a full Thorns III set never bit a player attacker over 200 blows")
	}

	// And the death it causes says so, rather than falling back to "died".
	if got, want := deathMessage("attacker", deathCause{key: causeThorns, by: "victim"}),
		"attacker was killed whilst trying to hurt victim"; got != want {
		t.Errorf("thorns death message = %q, want %q", got, want)
	}
}

// A blocked blow deals no damage, so vanilla runs no post-attack effects —
// raising a shield must not hand the attacker a free Thorns hit.
func TestShieldBlockSuppressesThorns(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)
	for i := range b.armor {
		b.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1,
			ench: [2]enchApply{{id: enchThorns, lvl: 3}}}
	}
	b.inv.slots[b.p.heldSlot()] = invStack{item: itemByName["shield"], count: 1}
	b.blockingSince = 1
	h.tick.Store(uint64(shieldDelay) + 2) // the shield has to be UP long enough
	// …and facing the attacker: look is (-sin yaw, cos yaw), the attacker lies
	// at -x from the victim, so the shield needs yaw 90 to point that way.
	b.yaw = 90

	for i := 0; i < 200; i++ {
		a.health, b.health = 20, 20
		a.lastAttack = 0
		h.attackPlayer(players, a.p.eid, b.p.eid)
		if b.health < 20 {
			t.Fatal("the shield did not block — this test would prove nothing")
		}
		if a.health < 20 {
			t.Fatal("a shielded victim's Thorns bit the attacker")
		}
	}
}

// Vanilla runs post-attack effects for an arrow exactly as for a fist, and
// resolves the attacker to the SHOOTER — so Thorns reaches back down the
// arrow's flight path to the archer who loosed it.
func TestThornsReachesTheArcher(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)
	for i := range b.armor {
		b.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1,
			ench: [2]enchApply{{id: enchThorns, lvl: 3}}}
	}
	a.x, a.z = 30, 30 // right across the field, well out of melee reach

	bit := 0
	for i := 0; i < 200; i++ {
		a.health, b.health = 20, 20
		arrow := &arrowEntity{eid: h.allocEID(), dim: 0, shooter: a.p.eid,
			dmg: 1, playerShot: true, x: b.x, y: b.y, z: b.z}
		h.arrowHitsPlayer(players, arrow, b.x, b.y+0.5, b.z)
		if b.health >= 20 {
			t.Fatal("the arrow never landed — this test would prove nothing")
		}
		if a.health < 20 {
			bit++
		}
	}
	if bit == 0 {
		t.Error("Thorns never reached the archer over 200 arrows")
	}
}
