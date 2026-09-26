package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Surge Protector's bystander is a villager inside LightningBolt's box (15
// blocks out, 15 below to 21 above the bolt) in the bolt's own dimension:
// one 25 blocks off, or at the same spot in the Nether, is no bystander.
func TestLightningBystanderBox(t *testing.T) {
	const adv, crit = "minecraft:adventure/lightning_rod_with_villager_no_fire", "lightning_rod_with_villager_no_fire"
	strike := func(vx float64, vdim int) bool {
		h := newHub(world.New(1))
		h.rules.Difficulty = diffEasy // no fire from the bolt
		pl := survPlayer(h)
		pl.adv = advState{}
		pl.x, pl.y, pl.z = 0, 200, 20
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		v := h.spawnMob(players, entityVillager, vx, 200, 0)
		v.dim = vdim
		h.strikeLightning(players, dimOverworld, 0, 200, 0, false)
		_, ok := pl.adv[adv][crit]
		return ok
	}
	if !strike(10, dimOverworld) {
		t.Error("a villager 10 blocks from the bolt is no bystander")
	}
	if strike(25, dimOverworld) {
		t.Error("a villager 25 blocks from the bolt counted as a bystander")
	}
	if strike(10, dimNether) {
		t.Error("a villager in the Nether counted as a bystander")
	}
}
