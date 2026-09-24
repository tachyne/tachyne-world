package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// killRig is a stone slab at y=179 with a survival player standing on it,
// tracking advancements, for driving kills through the real combat paths.
func killRig(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.adv = advState{}
	pl.x, pl.y, pl.z, pl.yaw, pl.pitch = 0, 180, 0, 0, 0 // looking +z
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	return h, pl, players
}

func hasCrit(pl *tracked, adv, crit string) bool {
	_, ok := pl.adv[adv][crit]
	return ok
}

const (
	advReturnToSender = "minecraft:nether/return_to_sender"
	advUneasyAlliance = "minecraft:nether/uneasy_alliance"
	advSniperDuel     = "minecraft:adventure/sniper_duel"
	advVoluntaryExile = "minecraft:adventure/voluntary_exile"
	advBlowback       = "minecraft:adventure/blowback"
)

// Return to Sender asks for a ghast killed by a fireball; Uneasy Alliance
// for one killed in the Overworld. A ghast punched to death in the Overworld
// earns only the second; its own fireball batted back earns both.
func TestKillCriteriaGhast(t *testing.T) {
	h, pl, players := killRig(t)
	ghast := h.spawnMob(players, entityGhast, 0, 180, 2)
	ghast.health = 1
	h.attackMob(players, pl.p.eid, ghast.eid)
	if ghast.dying == 0 {
		t.Fatal("the punch did not kill the ghast")
	}
	if !hasCrit(pl, advUneasyAlliance, "killed_ghast") {
		t.Error("a ghast killed in the Overworld does not earn Uneasy Alliance")
	}
	if hasCrit(pl, advReturnToSender, "killed_ghast") {
		t.Error("a punched ghast earned Return to Sender")
	}

	h, pl, players = killRig(t)
	ghast = h.spawnMob(players, entityGhast, 0, 180, 6)
	ghast.health = 1
	a := h.launchProjectileIn(players, entityLargeFireball, 0, 0, 181, 2, -0.3, 0, -0.95)
	a.shooter, a.dmg, a.explode, a.fire = ghast.eid, 6, 1, true
	if !h.deflectProjectile(players, pl, a) {
		t.Fatal("the fireball was not deflected")
	}
	for i := 0; i < 12 && ghast.dying == 0; i++ {
		h.updateArrows(players)
	}
	if ghast.dying == 0 {
		t.Fatal("the returned fireball did not kill the ghast")
	}
	if !hasCrit(pl, advReturnToSender, "killed_ghast") {
		t.Error("a ghast killed by its returned fireball does not earn Return to Sender")
	}

	// A ghast killed in the Nether is no alliance.
	h, pl, players = killRig(t)
	ghast = h.spawnMob(players, entityGhast, 0, 180, 2)
	ghast.health, ghast.dim, pl.dim = 1, dimNether, dimNether
	h.attackMob(players, pl.p.eid, ghast.eid)
	if ghast.dying == 0 {
		t.Fatal("the punch did not kill the Nether ghast")
	}
	if hasCrit(pl, advUneasyAlliance, "killed_ghast") {
		t.Error("a ghast killed in the Nether earned Uneasy Alliance")
	}
}

// Sniper Duel is a skeleton killed by a projectile from at least 50 blocks
// away (horizontally): the same arrow from close range, or a sword, is not.
func TestKillCriteriaSniperDuel(t *testing.T) {
	shoot := func(standZ float64) *tracked {
		h, pl, players := killRig(t)
		sk := h.spawnMob(players, entitySkeleton, 0, 180, 6)
		sk.health = 1
		pl.z = standZ
		a := h.launchProjectileIn(players, entityArrow, 0, 0, 181, 4, 0, 0, 1)
		a.shooter, a.playerShot, a.dmg = pl.p.eid, true, 6
		for i := 0; i < 12 && sk.dying == 0; i++ {
			h.updateArrows(players)
		}
		if sk.dying == 0 {
			t.Fatalf("the arrow did not kill the skeleton (shooter at z=%.0f)", standZ)
		}
		return pl
	}
	if pl := shoot(-50); !hasCrit(pl, advSniperDuel, "killed_skeleton") {
		t.Error("a skeleton shot dead from 56 blocks does not earn Sniper Duel")
	}
	if pl := shoot(-30); hasCrit(pl, advSniperDuel, "killed_skeleton") {
		t.Error("a skeleton shot dead from 36 blocks earned Sniper Duel")
	}
}

// Voluntary Exile is a raider wearing the ominous banner: a patrol captain,
// not any pillager.
func TestKillCriteriaVoluntaryExile(t *testing.T) {
	kill := func(captain bool) *tracked {
		h, pl, players := killRig(t)
		p := h.spawnMob(players, entityPillager, 0, 180, 2)
		p.health, p.patrolCaptain = 1, captain
		h.attackMob(players, pl.p.eid, p.eid)
		if p.dying == 0 {
			t.Fatal("the punch did not kill the pillager")
		}
		return pl
	}
	if pl := kill(false); hasCrit(pl, advVoluntaryExile, "voluntary_exile") {
		t.Error("an ordinary pillager earned Voluntary Exile")
	}
	if pl := kill(true); !hasCrit(pl, advVoluntaryExile, "voluntary_exile") {
		t.Error("a patrol captain does not earn Voluntary Exile")
	}
}

// Blowback is a breeze killed by a breeze's wind charge the player batted
// back; a player's own wind charge does not count.
func TestKillCriteriaBlowback(t *testing.T) {
	kill := func(breezeBorn bool) *tracked {
		h, pl, players := killRig(t)
		br := h.spawnMob(players, entityBreeze, 0, 180, 6)
		br.health = 1
		et := entityWindCharge
		if breezeBorn {
			et = entityBreezeWindCharge
		}
		a := h.launchProjectileIn(players, et, 0, 0, 181, 2, 0, 0, -0.9)
		if breezeBorn {
			a.shooter, a.mobShot = br.eid, true
			if !h.deflectProjectile(players, pl, a) {
				t.Fatal("the wind charge was not deflected")
			}
		} else {
			a.shooter, a.playerShot, a.vz = pl.p.eid, true, 0.9
			a.noHitUntil = h.tick.Load() + 5
		}
		for i := 0; i < 12 && br.dying == 0; i++ {
			h.updateArrows(players)
		}
		if br.dying == 0 {
			t.Fatalf("the wind charge did not kill the breeze (breezeBorn %v)", breezeBorn)
		}
		return pl
	}
	if pl := kill(true); !hasCrit(pl, advBlowback, "blowback") {
		t.Error("a breeze killed by its own batted-back charge does not earn Blowback")
	}
	if pl := kill(false); hasCrit(pl, advBlowback, "blowback") {
		t.Error("a breeze killed by the player's own wind charge earned Blowback")
	}
}
