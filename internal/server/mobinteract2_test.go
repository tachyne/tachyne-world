package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Shears turn a mooshroom into a cow with five mushrooms, strip a snow
// golem's pumpkin once, and take a bogged's two mushrooms once; an iron
// ingot mends a hurt golem; flint and steel lights a creeper; a cookie
// kills a parrot.
func TestOtherMobInteractions(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl
	h.playersRef = players
	hold := func(item int32, n int) { pl.inv.slots[pl.p.heldSlot()] = invStack{item: item, count: n} }
	items := func(item int32) int {
		n := 0
		for _, it := range h.items {
			if it.item == item {
				n += it.count
			}
		}
		return n
	}

	moo := h.spawnMobIn(players, entityMooshroom, 0, 0, 70, 0)
	hold(itemShears, 1)
	if !h.tryShearOther(players, pl, moo) {
		t.Fatal("shears on a mooshroom refused")
	}
	if items(itemRedMushroom) != 5 {
		t.Errorf("a red mooshroom sheds %d red mushrooms, want 5", items(itemRedMushroom))
	}
	cows := 0
	for _, m := range h.mobs {
		if m.etype == entityCow {
			cows++
		}
		if m.etype == entityMooshroom {
			t.Error("the mooshroom should be gone")
		}
	}
	if cows != 1 {
		t.Errorf("%d cows after shearing, want 1", cows)
	}
	if pl.inv.slots[pl.p.heldSlot()].dmg != 1 {
		t.Errorf("shears wear %d, want 1", pl.inv.slots[pl.p.heldSlot()].dmg)
	}

	golem := h.spawnMobIn(players, entitySnowGolem, 0, 0, 70, 0)
	if !h.tryShearOther(players, pl, golem) || !golem.sheared || items(itemCarvedPumpkin) != 1 {
		t.Errorf("snow golem shear: sheared=%v pumpkins=%d", golem.sheared, items(itemCarvedPumpkin))
	}
	if h.tryShearOther(players, pl, golem) {
		t.Error("a bare snow golem has nothing to shear")
	}

	bogged := h.spawnMobIn(players, entityBogged, 0, 0, 70, 0)
	before := items(itemRedMushroom) + items(itemBrownMushroom)
	if !h.tryShearOther(players, pl, bogged) || !bogged.sheared {
		t.Error("bogged shear refused")
	}
	if got := items(itemRedMushroom) + items(itemBrownMushroom) - before; got != 2 {
		t.Errorf("a bogged sheds %d mushrooms, want 2", got)
	}
	if h.tryShearOther(players, pl, bogged) {
		t.Error("a sheared bogged has no more")
	}

	iron := h.spawnMobIn(players, entityIronGolem, 0, 0, 70, 0)
	hold(itemIronIngot, 3)
	if h.tryRepairGolem(players, pl, iron) {
		t.Error("a whole golem takes no ingot")
	}
	iron.health = 10
	if !h.tryRepairGolem(players, pl, iron) || iron.health != 35 || pl.inv.slots[pl.p.heldSlot()].count != 2 {
		t.Errorf("repair: health %d held %+v", iron.health, pl.inv.slots[pl.p.heldSlot()])
	}

	creeper := h.spawnMobIn(players, entityCreeper, 0, 0, 70, 0)
	hold(itemFlintAndSteel, 1)
	if !h.tryIgniteCreeper(players, pl, creeper) || creeper.fuse != creeperFuseTicks || pl.inv.slots[pl.p.heldSlot()].dmg != 1 {
		t.Errorf("ignite: fuse %d held %+v", creeper.fuse, pl.inv.slots[pl.p.heldSlot()])
	}

	parrot := h.spawnMobIn(players, entityParrot, 0, 0, 70, 0)
	hold(itemCookie, 1)
	if !h.tryPoisonParrot(players, pl, parrot) || pl.inv.slots[pl.p.heldSlot()].count != 0 {
		t.Error("cookie refused")
	}
	if parrot.dying == 0 && parrot.health > 0 {
		t.Error("a cookie kills the parrot")
	}
}
