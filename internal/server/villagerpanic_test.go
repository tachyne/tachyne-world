package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func villagerPanicRig(t *testing.T) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 50, 180, 50
	h.world.ForceLoad(0, 0, 2)
	for x := -18; x <= 18; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	return h, players, pl, h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
}

// TestVillagerFleesZombie: a zombie inside six blocks sends a villager off
// to a spot away from it at one and a half times its pace; one twenty
// blocks off is nothing to it; a pillager fifteen away still sets it
// scurrying.
func TestVillagerFleesZombie(t *testing.T) {
	h, players, _, v := villagerPanicRig(t)
	z := h.spawnMob(players, entityZombie, 4.5, 180, 0.5)
	h.gridDirty()
	if !h.villagerPanicStep(players, v) || v.vx >= 0 {
		t.Fatalf("a zombie four blocks east sends it west: vx %.3f", v.vx)
	}
	if v.panicHasT && v.panicTX >= v.x {
		t.Fatalf("its walk target %.1f is not away from the zombie", v.panicTX)
	}
	if got, want := math.Hypot(v.vx, v.vz), v.moveSpeed()*villagerFleeSpeed; math.Abs(got-want) > 1e-9 {
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

// Struck, a villager runs from its attacker (HURT_BY_ENTITY) rather than
// bolting on a generic PanicGoal, and calms down once the blow is two
// seconds old and the attacker is more than six blocks off.
func TestVillagerRunsFromItsAttackerThenCalms(t *testing.T) {
	h, players, pl, v := villagerPanicRig(t)
	pl.x, pl.y, pl.z = 3.5, 180, 0.5
	v.health = 1000
	h.attackMob(players, pl.p.eid, v.eid)
	if v.panic != 0 {
		t.Fatal("a villager has no PanicGoal")
	}
	if !h.villagerPanicStep(players, v) || !v.panicHasT || v.panicTX >= v.x {
		t.Fatalf("a struck villager walks away from its attacker: target %v (%.1f)", v.panicHasT, v.panicTX)
	}
	pl.x = 15.5
	for i := 0; i < villagerHurtTicks/mobMoveInterval; i++ {
		h.villagerPanicStep(players, v)
	}
	if h.villagerPanicStep(players, v) {
		t.Fatal("the blow is old and the attacker far: it calms down")
	}
	pl.x = v.x + 3
	if h.villagerPanicStep(players, v) {
		t.Fatal("once calm, it has forgotten who hit it")
	}
}
