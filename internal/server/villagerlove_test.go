package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func bedHeadAt(w *world.World, x, y, z int) blockPos {
	s := worldgen.BlockBase("red_bed")
	info, _ := worldgen.InfoForState(s)
	w.SetBlock(x, y, z, worldgen.SetProperty(info, s, "part", "head"))
	w.SetBlock(x, y, z+1, worldgen.SetProperty(info, s, "part", "foot"))
	return blockPos{x, y, z}
}

func villagerPair(t *testing.T) (*hub, *mob, *mob, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	for x := -8; x <= 12; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 40.5, 180, 0.5
	h.dayTime.Store(1000) // dawn: the idle segment
	h.tick.Store(1000)
	a := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	b := h.spawnMob(players, entityVillager, 4.5, 180, 0.5)
	for _, v := range []*mob{a, b} {
		v.meet = blockPos{0, 180, 0}
		v.hoard = []invStack{{item: int32(itemByName["bread"]), count: 3}} // twelve points
	}
	return h, a, b, players
}

// Two fed villagers take up, walk together, and with a vacant bed about
// bear a child that gets the bed; each parent eats and cools down.
func TestVillagersBreedWithAVacantBed(t *testing.T) {
	skipHeavy(t)
	h, a, b, players := villagerPair(t)
	bed := bedHeadAt(h.world, 6, 180, 3)
	for i := 0; i < 200 && a.breedMate == 0 && b.breedMate == 0; i++ {
		h.tick.Store(1000 + uint64(i)*survivalTickN)
		h.villagerBreedStep(players, a)
		h.villagerBreedStep(players, b)
	}
	if a.breedMate == 0 || b.breedMate == 0 {
		t.Fatal("the villagers never took up")
	}
	lead, other := a, b
	if !a.breedLead {
		lead, other = b, a
	}
	if !h.villagerBreedStep(players, lead) || lead.vx == 0 {
		t.Error("the lead is not walking to its mate")
	}
	other.x, other.z = lead.x+1, lead.z
	h.tick.Store(lead.breedAt)
	before := len(h.mobs)
	h.villagerBreedStep(players, lead)
	if len(h.mobs) != before+1 {
		t.Fatal("no child was born")
	}
	var child *mob
	for _, m := range h.mobs {
		if m.etype == entityVillager && m.baby {
			child = m
		}
	}
	if child == nil || child.bed != bed || child.growLeft != growUpTicks || child.profession != profUnemployed {
		t.Fatalf("child %+v", child)
	}
	for _, v := range []*mob{a, b} {
		if v.breedCD != breedCooldown || villagerFoodInPockets(v) != 0 || v.vFood != 0 || v.breedMate != 0 {
			t.Errorf("parent after birth: cd %d food %d belly %d mate %d", v.breedCD, villagerFoodInPockets(v), v.vFood, v.breedMate)
		}
	}
}

// No vacant bed: no child, the pair shows anger and parts.
func TestVillagersNeedABed(t *testing.T) {
	skipHeavy(t)
	h, a, b, players := villagerPair(t)
	bed := bedHeadAt(h.world, 6, 180, 3)
	a.bed = bed // claimed
	a.breedMate, b.breedMate, a.breedLead = b.eid, a.eid, true
	a.breedAt = h.tick.Load()
	b.x, b.z = a.x+1, a.z
	before := len(h.mobs)
	h.villagerBreedStep(players, a)
	if len(h.mobs) != before || a.breedMate != 0 || b.breedMate != 0 {
		t.Error("a child was born with no vacant bed, or the pair stayed together")
	}
	if a.breedCD != 0 {
		t.Error("a parent cooled down without a birth")
	}
	// Hungry villagers never start.
	a.hoard, b.hoard = nil, nil
	for i := 0; i < 100; i++ {
		h.tick.Store(2000 + uint64(i)*survivalTickN)
		h.villagerBreedStep(players, a)
	}
	if a.breedMate != 0 {
		t.Error("hungry villagers took up")
	}
}

// A farmer with a surplus of bread throws half a stack to a neighbour it
// chats with; a hungry neighbour with nothing gets nothing thrown back.
func TestVillagersShareFood(t *testing.T) {
	h, a, b, players := villagerPair(t)
	a.profession = profFarmer
	b.profession = professionIndex("librarian") // not a farmer (a zero profession is one)
	a.hoard = []invStack{{item: int32(itemByName["bread"]), count: 40}}
	b.hoard = nil
	b.x, b.z = a.x+1, a.z
	h.villagerShareFood(players, a, b)
	if villagerCount(a, int32(itemByName["bread"])) != 20 {
		t.Errorf("farmer kept %d bread, want 20", villagerCount(a, int32(itemByName["bread"])))
	}
	thrown := 0
	for _, it := range h.items {
		if it.item == int32(itemByName["bread"]) {
			thrown += it.count
		}
	}
	if thrown != 20 {
		t.Errorf("bread thrown %d, want 20", thrown)
	}
	h.villagerShareFood(players, b, a)
	if len(h.items) != 1 {
		t.Error("the empty-handed neighbour threw something")
	}
	// A non-farmer holding what a farmer asks for hands it over.
	b.hoard = []invStack{{item: int32(itemByName["wheat_seeds"]), count: 30}}
	h.villagerShareFood(players, b, a)
	if villagerCount(b, int32(itemByName["wheat_seeds"])) != 24 {
		t.Errorf("seeds kept %d, want 24", villagerCount(b, int32(itemByName["wheat_seeds"])))
	}
}
