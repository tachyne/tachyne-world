package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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
	h.strikeLightning(players, x, y, z, false)
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
	h.channelingStrike(players, a, dimOverworld, target.x, target.y, target.z)
	if len(h.bolts) != bolts {
		t.Error("no bolt without a storm")
	}
	h.thundering = true
	h.channelingStrike(players, a, dimOverworld, target.x, target.y, target.z)
	if len(h.bolts) != bolts+1 {
		t.Error("a Channeling hit in a storm under open sky calls a bolt")
	}
	h.channelingStrike(players, a, dimOverworld, target.x, target.y-20, target.z)
	if len(h.bolts) != bolts+1 {
		t.Error("no bolt underground")
	}
	a.channeling = false
	h.channelingStrike(players, a, dimOverworld, target.x, target.y, target.z)
	if len(h.bolts) != bolts+1 {
		t.Error("no bolt without the enchantment")
	}
}
