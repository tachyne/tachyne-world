package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A lingering potion flies as its own entity type (ThrownLingeringPotion),
// whoever throws it; a splash potion stays a splash potion.
func TestLingeringPotionFliesAsItsOwnEntity(t *testing.T) {
	onlyType := func(t *testing.T, h *hub) int {
		t.Helper()
		if len(h.arrows) != 1 {
			t.Fatalf("want one projectile, have %d", len(h.arrows))
		}
		for _, a := range h.arrows {
			if !a.splash {
				t.Fatalf("the projectile does not shatter as a potion: %+v", a)
			}
			return a.etype
		}
		return 0
	}

	t.Run("player throw", func(t *testing.T) {
		for _, c := range []struct {
			item int32
			want int
		}{{itemLingerPotion, entityLingerProj}, {itemSplashPotion, entitySplashProj}} {
			h, players, pl := flightHub(t)
			pl.inv.slots[0] = invStack{item: c.item, count: 1, potion: potPoison}
			h.throwSplashPotion(players, pl, 0)
			if got := onlyType(t, h); got != c.want {
				t.Errorf("item %d threw entity %d, want %d", c.item, got, c.want)
			}
		}
	})

	t.Run("dispenser", func(t *testing.T) {
		for _, c := range []struct {
			item int32
			want int
		}{{itemLingerPotion, entityLingerProj}, {itemSplashPotion, entitySplashProj}} {
			h := newHub(world.New(1))
			h.world.ForceLoad(0, 0, 1)
			h.arrows = map[int32]*arrowEntity{}
			state := eastDispenser(t)
			pos := blockPos{5, 180, 5}
			h.world.SetBlock(pos.x, pos.y, pos.z, state)
			b := &bin{slots: make([]invStack, 9)}
			b.slots[0] = invStack{item: c.item, count: 1, potion: potPoison}
			h.bins[simPos{blockPos: pos}] = b
			h.ejectFromBin(map[int32]*tracked{}, simPos{blockPos: pos}, state)
			if got := onlyType(t, h); got != c.want {
				t.Errorf("dispensed item %d as entity %d, want %d", c.item, got, c.want)
			}
		}
	})

	t.Run("ominous item spawner", func(t *testing.T) {
		h := newHub(world.New(1))
		h.arrows = map[int32]*arrowEntity{}
		h.itemSpawners = map[int32]*itemSpawnerEnt{}
		h.tick.Store(500)
		h.itemSpawners[77] = &itemSpawnerEnt{eid: 77, x: 0.5, y: 190, z: 0.5,
			drop: ominousDrops[0], dueAt: 500, warned: true}
		if ominousDrops[0].item != "lingering_potion" {
			t.Fatalf("drop table reordered: %+v", ominousDrops[0])
		}
		h.updateItemSpawners(map[int32]*tracked{})
		if got := onlyType(t, h); got != entityLingerProj {
			t.Errorf("the spawner dropped entity %d, want lingering_potion %d", got, entityLingerProj)
		}
	})
}
