package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestVillagerFleesZombie: a villager with a zombie six blocks off runs the
// other way at one and a half times its pace; one twenty blocks off is
// nothing to it; a pillager fifteen away still is.
func TestVillagerFleesZombie(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 50, 180, 50
	v := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	z := h.spawnMob(players, entityZombie, 6.5, 180, 0.5)
	h.gridDirty()
	if !h.villagerPanicStep(players, v) || v.vx >= 0 {
		t.Fatalf("a zombie six blocks off sends it west: vx %.3f", v.vx)
	}
	if got, want := -v.vx, v.moveSpeed()*villagerFleeSpeed; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("at one and a half times its pace: %.4f vs %.4f", got, want)
	}
	z.x = 20.5
	h.gridDirty()
	if h.villagerPanicStep(players, v) {
		t.Fatal("twenty blocks off is nothing to it")
	}
	h.spawnMob(players, entityPillager, 14.5, 180, 0.5)
	h.gridDirty()
	if !h.villagerPanicStep(players, v) {
		t.Fatal("a pillager is feared from fifteen")
	}
}
