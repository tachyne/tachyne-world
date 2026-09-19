package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A nautilus is tamed with a pufferfish, takes a saddle only once tamed,
// carries its rider, dashes on the jump key with a forty-tick cooldown,
// and keeps its rider breathing on Breath of the Nautilus.
func TestNautilusMount(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 60, 0.5
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			for y := 58; y <= 62; y++ {
				h.world.SetBlock(x, y, z, worldgen.WaterBase)
			}
		}
	}
	m := h.spawnMob(players, entityNautilus, 1.5, 60, 0.5)
	if !tameable(entityNautilus) || !isTameFood(entityNautilus, itemPufferfishItem) || isTameFood(entityNautilus, int32(itemByName["cod"])) {
		t.Fatal("a nautilus is tamed with a pufferfish and nothing else")
	}
	pl.p.setHotbarSlot(0, itemSaddle)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemSaddle, count: 1}
	if h.tryMount(players, pl, m) || m.saddled {
		t.Fatal("an untamed nautilus takes no saddle")
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemPufferfishItem, count: 64}
	pl.p.setHotbarSlot(0, itemPufferfishItem)
	for i := 0; i < 200 && !m.tamed; i++ {
		h.tryTame(players, pl, m)
	}
	if !m.tamed || m.owner != pl.p.eid || m.behavior.name() == "hostile" {
		t.Fatalf("taming: tamed=%v owner=%d behaviour=%s", m.tamed, m.owner, m.behavior.name())
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemSaddle, count: 1}
	pl.p.setHotbarSlot(0, itemSaddle)
	if !h.tryMount(players, pl, m) || !m.saddled {
		t.Fatal("a tamed nautilus takes the saddle")
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{}
	pl.p.setHotbarSlot(0, 0)
	if !h.tryMount(players, pl, m) || m.rider != pl.p.eid || pl.ridingEID != m.eid {
		t.Fatalf("a saddled nautilus is ridden: rider=%d riding=%d", m.rider, pl.ridingEID)
	}
	h.nautilusDashStart(players, pl)
	if m.dashCD != nautilusDashCooldown || !m.dashing {
		t.Fatalf("the dash should start its cooldown: %d", m.dashCD)
	}
	h.nautilusDashStart(players, pl)
	for i := 0; i < 30 && m.dashCD > 0; i++ {
		h.nautilusDashTick(players, m)
	}
	if m.dashCD != 0 || m.dashing {
		t.Fatal("the cooldown should run out")
	}
	h.nautilusBreath(players, m)
	if pl.hasEffect(effBreathOfTheNautilus) == 0 || !pl.breathesUnderwater() {
		t.Fatal("the rider should breathe on Breath of the Nautilus")
	}
	if !bodyArmorFor(entityNautilus, int32(itemByName["diamond_nautilus_armor"])) || bodyArmorFor(entityNautilus, int32(itemByName["diamond_horse_armor"])) {
		t.Fatal("a nautilus wears nautilus armour, not horse armour")
	}
	_, _, ambient := h.mobSoundsFor(m)
	if ambient != "minecraft:entity.nautilus.ambient" {
		t.Fatalf("under water it speaks under water: %s", ambient)
	}
}
