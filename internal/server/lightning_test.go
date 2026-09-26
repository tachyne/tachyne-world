package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/plugin"
)

// A bolt charges a creeper, turns a pig into a zombified piglin and a
// villager into a witch; a Channeling trident calls a bolt down on its
// target under open sky in a storm, and not otherwise.
func TestLightningTransformsAndChanneling(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	creeper := h.spawnMob(players, entityCreeper, x, y, z)
	pig := h.spawnMob(players, entityPig, x+1, y, z)
	villager := h.spawnMob(players, entityVillager, x-1, y, z)
	creeper.health, pig.health, villager.health = 20, 20, 20
	h.strikeLightning(players, dimOverworld, x, y, z, false)
	if !creeper.charged {
		t.Error("a struck creeper is charged")
	}
	if h.mobs[pig.eid] != nil || h.mobs[villager.eid] != nil {
		t.Error("the pig and the villager should have been replaced")
	}
	piglins, witches := 0, 0
	for _, m := range h.mobs {
		switch m.etype {
		case entityZombifiedPiglin:
			piglins++
		case entityWitch:
			witches++
		}
	}
	if piglins != 1 || witches != 1 {
		t.Errorf("want a zombified piglin and a witch, got %d and %d", piglins, witches)
	}
	// Channeling: only in a thunderstorm, only under open sky.
	target := h.spawnMob(players, entityCow, x, y, z)
	bolts := len(h.bolts)
	a := &arrowEntity{channeling: true, dim: dimOverworld}
	h.thundering = false
	h.channelingStrike(players, a, dimOverworld, target.x, target.y, target.z, nil)
	if len(h.bolts) != bolts {
		t.Error("no bolt without a storm")
	}
	h.thundering = true
	h.channelingStrike(players, a, dimOverworld, target.x, target.y, target.z, nil)
	if len(h.bolts) != bolts+1 {
		t.Error("a Channeling hit in a storm under open sky calls a bolt")
	}
	h.channelingStrike(players, a, dimOverworld, target.x, target.y-20, target.z, nil)
	if len(h.bolts) != bolts+1 {
		t.Error("no bolt underground")
	}
	a.channeling = false
	h.channelingStrike(players, a, dimOverworld, target.x, target.y, target.z, nil)
	if len(h.bolts) != bolts+1 {
		t.Error("no bolt without the enchantment")
	}
}

// /summon lightning_bolt works in any dimension: the bolt is an entity of
// the level it is summoned in (only weather keeps to the overworld). In the
// Nether it charges a creeper there, not one at the same spot overworld.
func TestSummonedLightningInTheNether(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	nw.ForceLoad(0, 0, 1)
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	h.playersRef = players
	nc := h.spawnMobIn(players, entityCreeper, dimNether, 0.5, 100, 0.5)
	oc := h.spawnMobIn(players, entityCreeper, dimOverworld, 0.5, 100, 0.5)
	h.withSpawnCause(plugin.SpawnCommand, func() {
		h.summonAt(players, evSummon{etype: entityLightning, dim: dimNether, x: 0.5, y: 100, z: 0.5})
	})
	if !nc.charged {
		t.Error("a bolt summoned in the Nether should charge the creeper under it")
	}
	if oc.charged {
		t.Error("the Nether bolt struck the overworld creeper at the same coordinates")
	}
	if len(h.bolts) != 1 || h.bolts[0].dim != dimNether {
		t.Fatalf("want one Nether bolt, got %+v", h.bolts)
	}
}
