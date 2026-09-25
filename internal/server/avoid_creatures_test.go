package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// avoidPad is a stone strip at y=179 wide enough for a sixteen-block
// getPosAway, with the avoider at the origin.
func avoidPad(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -18; x <= 18; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	return h, map[int32]*tracked{}
}

// WanderingTrader's AvoidEntityGoals: a husk (Zombie class) or a pillager
// in range sends it off at 0.5; a skeleton does not.
func TestWanderingTraderAvoidsIllagersAndZombies(t *testing.T) {
	for _, tc := range []struct {
		threat int
		at     float64
		avoids bool
	}{{entityHusk, 5.5, true}, {entityPillager, 12.5, true}, {entityPillager, 16.5, false}, {entitySkeleton, 4.5, false}} {
		h, players := avoidPad(t)
		m := h.spawnMob(players, entityWanderingTrader, 0.5, 180, 0.5)
		o := h.spawnMob(players, tc.threat, tc.at, 180, 0.5)
		h.gridDirty()
		h.updateMobs(players)
		if got := m.avoidLeft > 0 && m.avoidEID == o.eid; got != tc.avoids {
			t.Errorf("trader avoiding %s at %.1f: %v, want %v", advEntityName[tc.threat], tc.at, got, tc.avoids)
		}
		if tc.avoids && (m.avoidWalk != 0.5 || m.avoidSprint != 0.5) {
			t.Errorf("trader avoid speeds %v/%v, want 0.5/0.5", m.avoidWalk, m.avoidSprint)
		}
	}
}

// PandaAvoidGoal<Monster>(4): a worried panda backs off from a zombie
// within four blocks; an ordinary one does not.
func TestWorriedPandaAvoidsMonsters(t *testing.T) {
	for _, trait := range []int32{pandaWorried, pandaNormal} {
		h, players := avoidPad(t)
		m := h.spawnMob(players, entityPanda, 0.5, 180, 0.5)
		m.variant = packPandaGenes(trait, trait)
		z := h.spawnHostileY(players, entityZombie, 3.5, 180, 0.5)
		h.gridDirty()
		h.updateMobs(players)
		if got := m.avoidLeft > 0 && m.avoidEID == z.eid; got != (trait == pandaWorried) {
			t.Errorf("panda trait %d avoiding a zombie: %v", trait, got)
		}
	}
}
