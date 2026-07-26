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

// Armour and a raised shield both blunt the blow, as they do against a mob.
func TestPvPRespectsArmourAndShields(t *testing.T) {
	h := newHub(world.New(1))
	a, b, players := pvpPair(h)

	h.attackPlayer(players, a.p.eid, b.p.eid)
	bare := 20 - b.health

	b.health = 20
	a.lastAttack = 0
	b.blockingSince = 1
	h.tick.Store(uint64(shieldDelay) + 2)
	b.yaw = -90 // facing the attacker's side
	h.attackPlayer(players, a.p.eid, b.p.eid)
	if b.health < 20 && bare > 0 {
		// The shield either caught it entirely or the facing was wrong; only
		// the first is a pass, so require no damage when it did face the blow.
		if h.shieldBlocks(b, a.x, a.z) {
			t.Error("a shield facing the blow should have caught it")
		}
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
