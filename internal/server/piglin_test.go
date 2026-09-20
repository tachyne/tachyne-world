package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A gold ingot held out to a piglin is admired for six seconds and bought
// with a throw from the bartering table; a gold-clad player is not hunted;
// a blow ends the admiring and keeps the ingot.
func TestPiglinBartering(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.tick.Store(100)
	pg := h.spawnSpecies(players, entityPiglin, 0, 0.5, 70, 0.5)
	if pg == nil {
		t.Fatal("no piglin")
	}
	pg.y = 70
	pl.x, pl.y, pl.z = 2.5, 70, 0.5
	pl.inv.slots[0] = invStack{item: itemGoldIngot, count: 2}
	pl.p.setHotbarSlot(0, itemGoldIngot)
	pl.p.held = 0
	if !h.tryBarter(players, pl, pg) || pg.offhand.item != itemGoldIngot || pl.inv.slots[0].count != 1 || pg.admireUntil != 100+piglinAdmireTicks {
		t.Fatalf("the piglin should admire the ingot: offhand=%+v left=%d until=%d", pg.offhand, pl.inv.slots[0].count, pg.admireUntil)
	}
	if h.tryBarter(players, pl, pg) {
		t.Fatal("an admiring piglin takes no second ingot")
	}
	items := len(h.items)
	h.tick.Store(100 + piglinAdmireTicks)
	h.piglinAdmireTick(players, pg)
	if pg.offhand.item != 0 || pg.admireUntil != 0 || len(h.items) != items+1 {
		t.Fatalf("the admiring should end in a throw: offhand=%+v items %d→%d", pg.offhand, items, len(h.items))
	}
	// Gold armour keeps it peaceful.
	pl.armor[3] = invStack{item: int32(itemByName["golden_helmet"]), count: 1}
	if h.nearestPiglinPrey(players, pg, 16) != nil {
		t.Fatal("a player in gold is not prey")
	}
	pl.armor[3] = invStack{}
	if h.nearestPiglinPrey(players, pg, 16) != pl {
		t.Fatal("without gold the player is prey")
	}
	// A blow while admiring keeps the ingot and puts bartering off.
	h.tryBarter(players, pl, pg)
	h.piglinHurtByPlayer(players, pg)
	if pg.admireUntil != 0 || len(pg.hoard) != 1 || pg.hoard[0].item != itemGoldIngot || !(pg.admireOffUntil > h.tick.Load()) {
		t.Fatalf("a hit should end the admiring and hoard the ingot: hoard=%v off=%d", pg.hoard, pg.admireOffUntil)
	}
	// The table only ever pays out something.
	for i := 0; i < 200; i++ {
		if st := h.rollBarter(); st.item == 0 || st.count <= 0 {
			t.Fatal("an empty barter")
		}
	}
}

// A piglin keeps away from soul lights and from its own undead.
func TestPiglinAvoidsRepellentsAndZombified(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityPiglin, 0.5, 70, 0.5)
	m.setMoveSpeed(0.2)

	if h.piglinAvoidStep(players, m) {
		t.Fatal("nothing to avoid yet")
	}
	// A soul torch three blocks to the +x: it walks the other way.
	h.world.SetBlock(3, 70, 0, worldgen.BlockBase("soul_torch"))
	if !h.piglinAvoidStep(players, m) {
		t.Fatal("a soul torch should move a piglin")
	}
	if m.vx >= 0 {
		t.Errorf("it backs away from the torch at +x: vx=%v", m.vx)
	}
	h.world.SetBlock(3, 70, 0, worldgen.Air)

	// A zombified piglin two blocks to the -z: it retreats, and keeps
	// retreating for five to seven seconds.
	m.vx, m.vz = 0, 0
	h.spawnMob(players, entityZombifiedPiglin, 0.5, 70, -1.5)
	if !h.piglinAvoidStep(players, m) {
		t.Fatal("a zombified piglin should move a living one")
	}
	if m.vz <= 0 {
		t.Errorf("it backs away from the undead at -z: vz=%v", m.vz)
	}
	if m.piglinFlee < piglinAvoidMin {
		t.Errorf("the retreat lasts five to seven seconds, got %d", m.piglinFlee)
	}
	// A brute avoids them too; an ordinary zombie does not move either of them.
	if !isPiglinRepellent(worldgen.BlockBase("soul_lantern")) || isPiglinRepellent(worldgen.Stone) {
		t.Error("the repellent set is wrong")
	}
}
