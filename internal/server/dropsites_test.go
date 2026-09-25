package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A mob's picked-up gear drops whole: a plain but named, dyed helmet keeps
// its name and colour instead of going out bare through the id-only list.
func TestMobGearDropsWhole(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 1)
	z := h.spawnMob(players, entityZombie, 0.5, 200, 0.5)
	cap := invStack{item: itemByName["leather_helmet"], count: 1, name: "Lid", color: 0xabcdef}
	z.gear[0] = cap
	z.gearSure[0] = true
	h.killMob(players, z)
	h.despawnMob(players, z) // the death animation over: the drops
	for _, it := range h.items {
		if it.item == cap.item {
			if it.name != "Lid" || it.color != 0xabcdef {
				t.Fatalf("the helmet dropped as %q %x", it.name, it.color)
			}
			return
		}
	}
	t.Fatal("the helmet did not drop")
}

// Trade-slot leftovers that no longer fit the inventory land whole.
func TestTradeLeftoversDropWhole(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	for i := range pl.inv.slots {
		pl.inv.slots[i] = invStack{item: itemByName["dirt"], count: 64}
	}
	book := invStack{item: itemByName["enchanted_book"], count: 1, name: "Tome"}
	pl.trade[0] = book
	h.reclaimTrade(players, pl)
	for _, it := range h.items {
		if it.item == book.item && it.name == "Tome" {
			return
		}
	}
	t.Fatal("the leftover lost its name or never dropped")
}
