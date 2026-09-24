package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A stronghold's chests fill from the table of the piece that holds them:
// corridor, crossing, library.
func TestStrongholdChestTables(t *testing.T) {
	seen := map[string]bool{}
	for seed := int64(1); seed <= 4; seed++ {
		w := world.New(seed)
		h := newHub(w)
		st, ok := findStronghold(w)
		if !ok {
			t.Fatalf("seed %d: no stronghold", seed)
		}
		for _, c := range st.Chests() {
			name, ok := h.structureChestTable(blockPos{c.X, c.Y, c.Z})
			if !ok || name != c.Table {
				t.Errorf("seed %d: chest at %d,%d,%d routes to %q, want %q", seed, c.X, c.Y, c.Z, name, c.Table)
			}
			seen[name] = true
		}
	}
	for _, k := range []string{"chests/stronghold_corridor", "chests/stronghold_crossing", "chests/stronghold_library"} {
		if !seen[k] {
			t.Errorf("no chest routed to %s", k)
		}
	}
}

// The portal room's spawner is a silverfish spawner, and it stands in the
// world where the hub looks for it.
func TestStrongholdSpawnerIsSilverfish(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	st, ok := findStronghold(w)
	if !ok {
		t.Fatal("no stronghold")
	}
	sp, _ := st.Spawner()
	for _, s := range h.structureSpawnersNear(st.X, st.Z) {
		if s.pos == (blockPos{sp[0], sp[1], sp[2]}) {
			if s.etype != entitySilverfish {
				t.Fatalf("portal room spawner runs %d, want silverfish", s.etype)
			}
			if w.At(sp[0], sp[1], sp[2]) != worldgen.Spawner {
				t.Fatal("no spawner block in the portal room")
			}
			return
		}
	}
	t.Fatal("the hub does not know the portal room's spawner")
}

// findMineshaftWith scans outward for a mineshaft passing a test.
func findMineshaftWith(g *worldgen.Generator, ok func(worldgen.Mineshaft) bool) (worldgen.Mineshaft, bool) {
	for r := 0; r <= 120; r++ {
		for cx := -r; cx <= r; cx++ {
			for cz := -r; cz <= r; cz++ {
				if max(abs(cx), abs(cz)) != r {
					continue
				}
				if m := g.MineshaftAt(cx, cz); m.Exists && ok(m) {
					return m, true
				}
			}
		}
	}
	return worldgen.Mineshaft{}, false
}

// A mineshaft nest's spawner is a cave spider spawner.
func TestMineshaftSpawnerIsCaveSpider(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	g := w.Gen()
	var sp [3]int
	_, ok := findMineshaftWith(g, func(m worldgen.Mineshaft) bool {
		for _, s := range g.MineshaftSpawners(m) {
			if w.At(s[0], s[1], s[2]) == worldgen.Spawner {
				sp = s
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("no stamped cave spider spawner")
	}
	for _, s := range h.structureSpawnersNear(sp[0], sp[2]) {
		if s.pos == (blockPos{sp[0], sp[1], sp[2]}) {
			if s.etype != entityCaveSpider {
				t.Fatalf("nest spawner runs %d, want cave spider", s.etype)
			}
			return
		}
	}
	t.Fatal("the hub does not know the nest's spawner")
}

// A mineshaft's chest minecart appears when its chunk is first seeded, holds
// chests/abandoned_mineshaft unrolled until opened, and keeps it across a
// save.
func TestMineshaftChestMinecart(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	g := w.Gen()
	var cart [3]int
	_, ok := findMineshaftWith(g, func(m worldgen.Mineshaft) bool {
		for _, c := range g.MineshaftCarts(m) {
			if isAnyRail(w.At(c[0], c[1], c[2])) {
				cart = c
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("no chest-minecart rail stamped")
	}
	players := map[int32]*tracked{}
	h.seedChunkCarts(players, [2]int32{int32(cart[0] >> 4), int32(cart[2] >> 4)})
	var v *vehicle
	for _, c := range h.vehicles {
		if c.etype == entityChestMinecart && floorInt(c.x) == cart[0] && floorInt(c.y) == cart[1] && floorInt(c.z) == cart[2] {
			v = c
		}
	}
	if v == nil {
		t.Fatalf("no chest minecart seeded at %v", cart)
	}
	if v.loot != worldgen.MineshaftCartTable {
		t.Fatalf("cart carries %q, want %q", v.loot, worldgen.MineshaftCartTable)
	}
	saved := h.snapshotVehicles()
	h2 := newHub(world.New(7))
	h2.restoreVehicles(saved)
	for _, c := range h2.vehicles {
		if c.loot != worldgen.MineshaftCartTable || c.lootPos != (blockPos{cart[0], cart[1], cart[2]}) {
			t.Errorf("unrolled loot lost in the store: %q at %v", c.loot, c.lootPos)
		}
	}
	pl := testTracked()
	h.openVehicleChest(map[int32]*tracked{1: pl}, pl, v)
	items := 0
	for _, s := range v.chest.slots {
		if s.count > 0 {
			items++
		}
	}
	if v.loot != "" || items == 0 {
		t.Fatalf("opening should roll the table: loot %q, %d stacks", v.loot, items)
	}
}
