package server

import (
	"testing"

	"tachyne/internal/world"
)

func TestSlimeSplitsOnDeath(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnHostile(players, entitySlime, 5, 5)
	if m.size != 4 || m.health != 16 {
		t.Fatalf("big slime should be size 4 / 16 HP: %+v", m)
	}
	h.despawnMob(players, m)
	halves := 0
	for _, o := range h.mobs {
		if o.etype == entitySlime {
			halves++
			if o.size != 2 || o.health != 4 {
				t.Fatalf("split slime should be size 2 / 4 HP: size=%d hp=%d", o.size, o.health)
			}
		}
	}
	if halves < 2 || halves > 4 {
		t.Fatalf("a big slime splits into 2-4, got %d", halves)
	}
	// The smallest drops slimeballs instead of splitting.
	small := h.spawnHostile(players, entitySlime, 8, 8)
	small.size, small.health = 1, 1
	before := len(h.mobs)
	h.despawnMob(players, small)
	if len(h.mobs) != before-1 {
		t.Fatal("a size-1 slime must not split")
	}
}

func TestEndermanNeutralUntilHitThenBlinks(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players := map[int32]*tracked{1: pl}
	m := h.spawnHostile(players, entityEnderman, 2, 0)
	m.y = pl.y
	h.acquireTarget(players, m)
	if m.hasTarget {
		t.Fatal("an unprovoked enderman must stay neutral")
	}
	ox, oz := m.x, m.z
	h.attackMob(players, 1, m.eid)
	if m.anger == 0 {
		t.Fatal("a hit enderman must anger")
	}
	if m.x == ox && m.z == oz {
		t.Fatal("a hit enderman should have teleported away")
	}
	h.acquireTarget(players, m)
	if !m.hasTarget && dist3(m.x, 0, m.z, pl.x, 0, pl.z) < m.aggro+deaggroSlack {
		t.Fatal("an angry enderman in range must hunt")
	}
}

func TestBiomeVariantsConfigured(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	husk := h.spawnHostile(players, entityHusk, 1, 1)
	if husk.burns {
		t.Fatal("husks must not burn in daylight")
	}
	stray := h.spawnHostile(players, entityStray, 3, 3)
	if !stray.burns {
		t.Fatal("strays burn like skeletons")
	}
	if _, ok := stray.behavior.(rangedBehavior); !ok {
		t.Fatal("strays kite like skeletons")
	}
}

func TestPearlTeleportsThrower(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	pl.inv.slots[0] = invStack{item: itemEnderPearl, count: 2}
	players := map[int32]*tracked{1: pl}
	pl.yaw, pl.pitch = 0, 0 // facing +z
	h.throwPearl(players, pl)
	if pl.inv.slots[0].count != 1 || len(h.arrows) != 1 {
		t.Fatalf("one pearl consumed + in flight: count=%d arrows=%d", pl.inv.slots[0].count, len(h.arrows))
	}
	hp := pl.health
	for i := 0; i < 200 && len(h.arrows) > 0; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if pl.z <= 2 {
		t.Fatalf("the pearl should have carried the player forward, z=%v", pl.z)
	}
	if pl.health >= hp {
		t.Fatal("pearl landings cost health")
	}
}
