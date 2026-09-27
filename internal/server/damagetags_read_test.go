package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The damage-type tags the hurt paths ask about, each at the place vanilla
// asks it.
func TestDamageTagsAreConsulted(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	owner := survPlayer(h)
	owner.x, owner.y, owner.z = 0.5, 180, 0.5
	players := map[int32]*tracked{owner.p.eid: owner}
	h.playersRef = players

	// #always_triggers_silverfish: a fall or a burn does not call the
	// others out; a blow from something, or magic, does.
	sf := h.spawnMob(players, entitySilverfish, 3.5, 180, 3.5)
	sf.health = 100
	for _, tc := range []struct {
		dt   dmgType
		want bool
	}{{dtFall, false}, {dtOnFire, false}, {dtMagic, true}, {dtMobAttack, true}} {
		sf.silverHurt, sf.invulnTicks = false, 0
		sf.hurtKind(1, tc.dt)
		if sf.silverHurt != tc.want {
			t.Errorf("silverfish hurt by %s: notifyHurt %v, want %v", tc.dt.name(), sf.silverHurt, tc.want)
		}
	}

	// #avoids_guardian_thorns and thorns itself spare the attacker.
	g := h.spawnMob(players, entityGuardian, 1.5, 180, 0.5)
	g.hasTarget, g.rest = false, 100 // resting: the spikes are out
	for _, tc := range []struct {
		dt   dmgType
		want bool
	}{{dtPlayerAttack, true}, {dtMagic, false}, {dtThorns, false}} {
		owner.health, owner.hurtAt, owner.lastHurt, owner.loadUntil = 20, 0, 0, 0
		h.guardianThorns(players, g, owner.p.eid, tc.dt)
		if hurt := owner.health < 20; hurt != tc.want {
			t.Errorf("guardian struck by %s: thorns %v, want %v", tc.dt.name(), hurt, tc.want)
		}
	}

	// #no_anger: a goat's ram leaves nobody for the wolves to answer.
	goat := h.spawnMob(players, entityGoat, 5.5, 180, 0.5)
	owner.lastHurtByMob = 0
	h.hurtByMob(owner, goat, dtMobAttackNoAggro)
	if owner.lastHurtByMob != 0 {
		t.Error("a #no_anger blow named its mob as the attacker")
	}

	// #no_wolf_retaliation: burnt by a hot sulfur cube, the owner's wolf
	// stays put; bitten by a zombie next, it goes for the zombie.
	wolf := h.spawnMob(players, entityWolf, 0.5, 180, 3.5)
	wolf.tamed, wolf.owner = true, owner.p.eid
	cube := h.spawnMob(players, entitySulfurCube, 2.5, 180, 2.5)
	owner.health, owner.hurtAt, owner.lastHurt = 20, 0, 0
	h.cubeBurn(players, cube, owner)
	if owner.lastHurtByMob != cube.eid {
		t.Fatalf("the cube's burn is remembered: last hurt by %d, want %d", owner.lastHurtByMob, cube.eid)
	}
	if o := h.wolfPickTarget(players, wolf); o == cube {
		t.Error("a wolf answered a #no_wolf_retaliation burn")
	}
	z := h.spawnMob(players, entityZombie, -2.5, 180, 0.5)
	owner.hurtAt, owner.lastHurt = 0, 0
	if !h.hurtFrom(players, owner, 2, dtMobAttack, deathCause{}, fromMob(z.x, z.z)) {
		t.Fatal("the zombie's bite did not land")
	}
	h.hurtByMob(owner, z, dtMobAttack)
	if o := h.wolfPickTarget(players, wolf); o != z {
		t.Errorf("the wolf should go for the zombie that bit its owner, picked %v", o)
	}
}
