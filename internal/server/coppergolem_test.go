package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func copperGolemCount(h *hub) int {
	n := 0
	for _, m := range h.mobs {
		if m.etype == entityCopperGolem {
			n++
		}
	}
	return n
}

// TestCopperGolemConstruction: a carved pumpkin on a copper block builds a copper
// golem — pumpkin consumed, golem spawned where it was, copper block -> chest.
func TestCopperGolemConstruction(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 50, 70, 50
	h.world.SetBlock(x, y-1, z, worldgen.BlockID("weathered_copper")) // any copper block works
	pumpkin := worldgen.BlockID("carved_pumpkin")
	h.world.SetBlock(x, y, z, pumpkin)

	h.checkCopperGolemBuild(players, 0, x, y, z, pumpkin)

	if got := h.world.At(x, y, z); got != worldgen.Air {
		t.Errorf("pumpkin should be consumed, got state %d", got)
	}
	chestBase := worldgen.BlockBase("copper_chest")
	if got := h.world.At(x, y-1, z); got < chestBase || got > chestBase+23 {
		t.Errorf("copper block should become a copper chest, got state %d", got)
	}
	if n := copperGolemCount(h); n != 1 {
		t.Fatalf("exactly one copper golem should spawn, got %d", n)
	}
}

// TestCopperGolemNeedsCopperBase: a pumpkin on a non-copper block builds nothing.
func TestCopperGolemNeedsCopperBase(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 20, 70, 20
	h.world.SetBlock(x, y-1, z, worldgen.BlockID("iron_block"))
	pumpkin := worldgen.BlockID("carved_pumpkin")
	h.world.SetBlock(x, y, z, pumpkin)

	h.checkCopperGolemBuild(players, 0, x, y, z, pumpkin)

	if h.world.At(x, y, z) != pumpkin {
		t.Error("pumpkin on a non-copper block must remain")
	}
	if copperGolemCount(h) != 0 {
		t.Error("no golem should form without a copper base")
	}
}

func spawnGolem(h *hub, players map[int32]*tracked, x, y, z float64) *mob {
	m := h.spawnSpecies(players, entityCopperGolem, 0, x, y, z)
	return m
}

// TestCopperGolemOxidationAdvances: a due oxidation step advances one stage and
// reschedules; the last step (→ oxidized) leaves it ready to statue.
func TestCopperGolemOxidationAdvances(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := spawnGolem(h, players, 100.5, 70, 100.5)
	m.oxidation, m.oxidizeAt = 0, 1 // due immediately (now >= 1 after a tick)
	h.tick.Store(10)
	h.updateCopperGolems(players)
	if m.oxidation != 1 {
		t.Fatalf("oxidation should advance to 1, got %d", m.oxidation)
	}
	if m.oxidizeAt <= 10 {
		t.Errorf("next oxidation should be rescheduled into the future, got %d", m.oxidizeAt)
	}
	// waxed golems never advance.
	m.waxed, m.oxidizeAt = true, 1
	h.updateCopperGolems(players)
	if m.oxidation != 1 {
		t.Error("a waxed golem must not oxidize")
	}
}

// TestCopperGolemBecomesStatue: an oxidized golem freezes into a statue block and
// despawns.
func TestCopperGolemBecomesStatue(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := spawnGolem(h, players, 30.5, 70, 30.5)
	bx, by, bz := 30, 70, 30
	h.copperGolemToStatue(players, m, bx, by, bz)
	statueBase := worldgen.BlockBase("oxidized_copper_golem_statue")
	got := h.world.At(bx, by, bz)
	if got < statueBase || got > statueBase+31 {
		t.Errorf("expected a copper golem statue, got state %d", got)
	}
	if _, alive := h.mobs[m.eid]; alive {
		t.Error("the golem should despawn when it becomes a statue")
	}
}

// TestCopperGolemWaxAndScrape: honeycomb waxes; an axe un-waxes then scrapes a
// stage off.
func TestCopperGolemWaxAndScrape(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 30, 70, 30)
	players := map[int32]*tracked{1: pl}
	m := spawnGolem(h, players, 30.9, 70, 30.9)

	give(pl, itemHoneycomb)
	if !h.tryCopperGolem(players, pl, m) || !m.waxed {
		t.Fatal("honeycomb should wax the golem")
	}
	give(pl, itemByName["iron_axe"])
	if !h.tryCopperGolem(players, pl, m) || m.waxed {
		t.Fatal("an axe should un-wax the golem")
	}
	m.oxidation = 2
	if !h.tryCopperGolem(players, pl, m) || m.oxidation != 1 {
		t.Fatalf("an axe should scrape one oxidation stage, got %d", m.oxidation)
	}
}

// TestCopperGolemSortsItems: a golem beside a copper chest (with items) and a
// wooden chest moves the items from copper → wooden.
func TestCopperGolemSortsItems(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	src := blockPos{10, 70, 10}
	dst := blockPos{12, 70, 10}
	h.world.SetBlock(src.x, src.y, src.z, worldgen.BlockID("copper_chest"))
	h.world.SetBlock(dst.x, dst.y, dst.z, worldgen.BlockID("chest"))
	sc := &chest{}
	sc.slots[0] = invStack{item: itemByName["diamond"], count: 20}
	h.chests[src] = sc
	h.chests[dst] = &chest{}

	m := spawnGolem(h, players, 11.0, 70, 10.5) // adjacent to both chests
	for i := 0; i < 12; i++ {
		m.sortCD = 0 // skip the transport cooldown in the test
		h.copperGolemSort(players, m)
	}

	if left := h.chests[src].slots[0].count; left != 0 {
		t.Errorf("copper chest should be emptied by the golem, %d left", left)
	}
	moved := 0
	for _, st := range h.chests[dst].slots {
		if st.item == itemByName["diamond"] {
			moved += st.count
		}
	}
	if moved != 20 {
		t.Errorf("wooden chest should receive all 20 diamonds, got %d", moved)
	}
	if m.carrying.item != 0 {
		t.Error("golem should not be left holding items")
	}
}
