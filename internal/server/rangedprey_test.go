package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// preyFixture is a hub with a stone floor at y=179 and no players.
func preyFixture(t *testing.T) (*hub, map[int32]*tracked) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	return h, players
}

// A pillager with no player about shoots the villager it hunts.
func TestPillagerShootsItsVillager(t *testing.T) {
	h, players := preyFixture(t)
	p := h.spawnMob(players, entityPillager, 0.5, 180, 0.5)
	v := h.spawnMob(players, entityVillager, 6.5, 180, 0.5)
	p.preyTarget = v.eid
	for i := 0; i < 400 && len(h.arrows) == 0; i++ {
		h.pillagerTick(players, p)
	}
	if len(h.arrows) == 0 {
		t.Fatal("a pillager never shot at its villager")
	}
	for _, a := range h.arrows {
		if a.vx <= 0 || !a.mobShot {
			t.Errorf("the bolt flies vx=%v mobShot=%v, want toward the villager and able to hit it", a.vx, a.mobShot)
		}
	}
}

// A skeleton's arrow strikes a mob in its path: the creeper of the music
// disc. It used to pass through every mob.
func TestSkeletonArrowHitsAMob(t *testing.T) {
	h, players := preyFixture(t)
	sk := h.spawnMob(players, entitySkeleton, 0.5, 180, 0.5)
	cr := h.spawnMob(players, entityCreeper, 4.5, 180, 0.5)
	before := cr.health
	h.spawnArrowAt(players, sk, cr.x, cr.y+0.6, cr.z)
	for i := 0; i < 40; i++ {
		h.updateArrows(players)
	}
	if cr.health >= before || cr.lastAttacker != sk.eid {
		t.Fatalf("creeper health %v (was %v), last attacker %d, want hurt by the skeleton %d",
			cr.health, before, cr.lastAttacker, sk.eid)
	}
}

// A guardian beams the squid it hunts.
func TestGuardianBeamsItsSquid(t *testing.T) {
	h, players := preyFixture(t)
	g := h.spawnMob(players, entityGuardian, 0.5, 180, 0.5)
	sq := h.spawnMob(players, entitySquid, 6.5, 180, 0.5)
	g.preyTarget = sq.eid
	sq.health = 1000
	for i := 0; i < 200 && sq.health == 1000; i++ {
		h.guardianTick(players, g)
	}
	if sq.health == 1000 {
		t.Fatalf("a guardian never beamed its squid (beam target %d)", g.beamTarget)
	}
}
