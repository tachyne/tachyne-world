package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// creditedKill reports whether pl was credited with killing a mob of the
// type: the trigger (Monster Hunter's criterion) and the killed statistic.
func creditedKill(pl *tracked, etype int) bool {
	return hasCrit(pl, "minecraft:adventure/kill_a_mob", "minecraft:"+advEntityName[etype]) &&
		pl.stats[statKey{attachproto.StatKilled, int32(etype)}] > 0
}

// TNT a player lit is the player's: what its blast kills counts as their
// kill. Unlit-by-anyone TNT credits no one.
func TestKillCreditPlayerTNT(t *testing.T) {
	for _, owned := range []bool{true, false} {
		h, pl, players := killRig(t)
		pl.z = -12 // well out of the blast
		z := h.spawnMob(players, entityZombie, 0.5, 180, 4.5)
		z.health = 1
		var owner int32
		if owned {
			owner = pl.p.eid
		}
		h.primeTNTBy(players, 0, 0, 180, 3, 1, owner)
		for i := 0; i < 3 && len(h.tnt) > 0; i++ {
			h.updateTNT(players)
		}
		if z.dying == 0 {
			t.Fatalf("owned=%v: the blast did not kill the zombie", owned)
		}
		if got := creditedKill(pl, entityZombie); got != owned {
			t.Errorf("owned=%v: kill credited=%v", owned, got)
		}
	}
}

// A TNT cart a player set off is theirs too.
func TestKillCreditTNTCart(t *testing.T) {
	h, pl, players := vehRig(t)
	pl.z = -8
	pl.adv = advState{}
	v := rigVehicle(t, h, players, "tnt_minecart")
	z := h.spawnMob(players, entityZombie, 2.5, 180, 0.5)
	z.health = 1
	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 180.3, -2, 0, 0, 1)
	a.shooter, a.playerShot, a.dmg, a.fire = pl.p.eid, true, 6, true
	for i := 0; i < 6 && h.vehicles[v.eid] != nil; i++ {
		h.updateArrows(players)
	}
	if z.dying == 0 {
		t.Fatal("the cart's blast did not kill the zombie")
	}
	if !creditedKill(pl, entityZombie) {
		t.Error("a zombie blown up by a cart the player's burning arrow set off is not their kill")
	}
}

// Thorns is the wearer's damage: a spider that dies biting a Thorns player
// is that player's kill.
func TestKillCreditThorns(t *testing.T) {
	h, pl, players := killRig(t)
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1, ench: enchList{{id: enchThorns, lvl: 3}}}
	}
	sp := h.spawnMob(players, entitySpider, 0, 180, 1)
	sp.health = 1
	for i := 0; i < 50 && sp.dying == 0; i++ {
		sp.attackCD = 0
		pl.health = 20
		h.tick.Add(20) // past the hurt cooldown
		h.mobMelee(players, sp)
	}
	if sp.dying == 0 {
		t.Fatal("Thorns never killed the spider")
	}
	if !creditedKill(pl, entitySpider) {
		t.Error("a spider killed by Thorns is not the wearer's kill")
	}
}

// A ghast's fireball batted back kills it outright (Ghast.hurtServer's
// reflected-fireball rule): the hit earns Return to Sender, and a ghast
// caught only in the returned fireball's blast is still the player's kill.
func TestKillCreditReturnedFireball(t *testing.T) {
	h, pl, players := killRig(t)
	ghast := h.spawnMob(players, entityGhast, 0, 180, 6)
	a := h.launchProjectileIn(players, entityLargeFireball, 0, 0, 181, 2, -0.3, 0, -0.95)
	a.shooter, a.dmg, a.explode, a.fire = ghast.eid, 6, 1, true
	h.deflectProjectile(players, pl, a)
	for i := 0; i < 12 && ghast.dying == 0; i++ {
		h.updateArrows(players)
	}
	if ghast.dying == 0 {
		t.Fatal("a full-health ghast survived its own fireball returned")
	}
	if !hasCrit(pl, advReturnToSender, "killed_ghast") || !creditedKill(pl, entityGhast) {
		t.Error("the returned fireball's kill is not credited as Return to Sender")
	}

	// Off to the side of the flight path, the fireball strikes a wall.
	h, pl, players = killRig(t)
	w := h.worldFor(0)
	for x := -1; x <= 1; x++ {
		for y := 180; y <= 183; y++ {
			w.SetBlock(x, y, 6, worldgen.BlockBase("stone"))
		}
	}
	ghast = h.spawnMob(players, entityGhast, 1.5, 180, 5.2)
	shooter := h.spawnMob(players, entityGhast, 0, 184, 12)
	a = h.launchProjectileIn(players, entityLargeFireball, 0, 0, 181, 2, -0.3, 0, -0.95)
	a.shooter, a.dmg, a.explode, a.fire = shooter.eid, 6, 1, true
	h.deflectProjectile(players, pl, a)
	for i := 0; i < 12 && h.arrows[a.eid] != nil; i++ {
		h.updateArrows(players)
	}
	if ghast.dying == 0 {
		t.Fatalf("the returned fireball's blast did not kill the ghast beside it (health %d)", ghast.health)
	}
	if !creditedKill(pl, entityGhast) {
		t.Error("a ghast killed by a returned fireball's blast is not the player's kill")
	}
	if hasCrit(pl, advReturnToSender, "killed_ghast") {
		t.Error("a blast kill earned Return to Sender (the blast is no projectile)")
	}
}

// An end crystal a player breaks is their blast: what it kills is theirs.
func TestKillCreditEndCrystal(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	pl.adv = advState{}
	var c *crystal
	for _, cc := range h.crystals {
		c = cc
		break
	}
	em := h.spawnMob(players, entityEnderman, c.x+1.5, c.y, c.z)
	em.dim, em.health = dimEnd, 1
	pl.x, pl.y, pl.z = c.x, c.y, c.z-3
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: c.eid})
	if h.crystals[c.eid] != nil {
		t.Fatal("the crystal did not go off")
	}
	if em.dying == 0 {
		t.Fatal("the crystal's blast did not kill the enderman beside it")
	}
	if !creditedKill(pl, entityEnderman) {
		t.Error("an enderman killed by a crystal the player broke is not their kill")
	}
}
