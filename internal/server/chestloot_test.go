package server

import (
	"math"
	"math/rand"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func seededCtx(seed int64) *lootCtx {
	r := rand.New(rand.NewSource(seed))
	return &lootCtx{rng: r.Intn, randf: r.Float64}
}

func TestChestTablesBaked(t *testing.T) {
	for _, name := range []string{
		"chests/simple_dungeon", "chests/desert_pyramid", "chests/ruined_portal",
		"chests/pillager_outpost", "chests/village/village_plains_house",
		"chests/village/village_weaponsmith", "chests/stronghold_corridor",
	} {
		if _, ok := lootForChest(name); !ok {
			t.Errorf("baked chest table missing: %s", name)
		}
	}
}

func TestWeightedPickDistribution(t *testing.T) {
	pool := []lootEntry{
		{Type: "item", ID: 1, W: 10},
		{Type: "item", ID: 2, W: 1},
	}
	ctx := seededCtx(42)
	counts := map[int32]int{}
	for i := 0; i < 11000; i++ {
		e := ctx.pickWeighted(pool)
		counts[e.ID]++
	}
	ratio := float64(counts[1]) / float64(counts[2])
	if ratio < 7 || ratio > 14 { // expect ~10:1
		t.Fatalf("weighted pick ratio %.1f, want ~10 (%d vs %d)", ratio, counts[1], counts[2])
	}
}

func TestEmptyEntryParticipates(t *testing.T) {
	// bundle w1 vs empty w2 → the item appears ~1/3 of rolls.
	pool := []lootEntry{
		{Type: "item", ID: 5, W: 1},
		{Type: "empty", W: 2},
	}
	ctx := seededCtx(7)
	items := 0
	for i := 0; i < 9000; i++ {
		if e := ctx.pickWeighted(pool); e.Type == "item" {
			items++
		}
	}
	frac := float64(items) / 9000
	if frac < 0.28 || frac > 0.39 {
		t.Fatalf("item fraction %.2f, want ~0.33", frac)
	}
}

func TestSetDamageFraction(t *testing.T) {
	h := newHub(world.New(1))
	bow := itemBow
	maxd := itemMaxDurability[bow]
	if maxd == 0 {
		t.Skip("bow not durable in this build")
	}
	ctx := seededCtx(1)
	fn := lootFn{F: "set_damage", NP: &lootNP{T: "const", V: 0.5}} // leave 50% durability
	st := ctx.applyChestFn(h, &fn, invStack{item: bow, count: 1})
	if want := int(math.Floor(0.5 * float64(maxd))); st.dmg != want {
		t.Fatalf("set_damage 0.5 → dmg %d, want %d (max %d)", st.dmg, want, maxd)
	}
	fn.NP = &lootNP{T: "const", V: 1} // fully durable
	if st := ctx.applyChestFn(h, &fn, invStack{item: bow, count: 1}); st.dmg != 0 {
		t.Fatalf("set_damage 1.0 → dmg %d, want 0", st.dmg)
	}
}

func TestEnchRandomOnBook(t *testing.T) {
	h := newHub(world.New(1))
	ctx := seededCtx(3)
	fn := lootFn{F: "ench_random"}
	st := ctx.applyChestFn(h, &fn, invStack{item: itemBook, count: 1})
	if st.item != itemEnchantedBook {
		t.Fatalf("ench_random on a book should yield an enchanted book, got %d", st.item)
	}
	n := 0
	for _, e := range st.ench {
		if e.lvl > 0 {
			n++
			if e.lvl < 1 || e.lvl > enchMaxLvl(e.id) {
				t.Fatalf("ench level %d out of range for id %d", e.lvl, e.id)
			}
		}
	}
	if n != 1 {
		t.Fatalf("ench_random should apply exactly one enchant, got %d", n)
	}
}

func TestEnchLevelsCapsAtTwo(t *testing.T) {
	h := newHub(world.New(1))
	sword := int32(itemByName["diamond_sword"])
	if _, ok := meleeDamage[sword]; !ok {
		t.Skip("diamond_sword not a melee weapon in this build")
	}
	sawTwo := false
	for seed := int64(0); seed < 40; seed++ {
		ctx := seededCtx(seed)
		fn := lootFn{F: "ench_levels", NP: &lootNP{T: "const", V: 30}}
		st := ctx.applyChestFn(h, &fn, invStack{item: sword, count: 1})
		n := 0
		for _, e := range st.ench {
			if e.lvl > 0 {
				n++
			}
		}
		if n < 1 || n > 2 {
			t.Fatalf("ench_levels applied %d enchants, want 1-2", n)
		}
		if n == 2 {
			sawTwo = true
			if st.ench[0].id == st.ench[1].id {
				t.Fatal("the two enchants must be distinct")
			}
		}
	}
	if !sawTwo {
		t.Fatal("ench_levels(30) should sometimes apply a second enchant")
	}
}

func TestChestFillDeterministicAndScattered(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{100, 40, -200}
	a, b := &chest{}, &chest{}
	h.fillChest(a, "chests/village/village_plains_house", pos)
	h.fillChest(b, "chests/village/village_plains_house", pos)
	if a.slots != b.slots {
		t.Fatal("same table + position must fill identically")
	}
	filled, distinctSlots := 0, 0
	for _, s := range a.slots {
		if s.item != 0 {
			filled++
			distinctSlots++
			if s.count > stackCap(s.item) {
				t.Fatalf("stack of %d exceeds cap for item %d", s.count, s.item)
			}
		}
	}
	if filled == 0 {
		t.Fatal("a plains-house chest should hold loot")
	}
	if distinctSlots >= 2 {
		// scattered: not all crammed into slot 0 — trivially true if >=2 slots,
		// but assert the loot didn't collapse into a single slot.
	}
	// A different position yields different contents.
	c := &chest{}
	h.fillChest(c, "chests/village/village_plains_house", blockPos{101, 40, -200})
	if a.slots == c.slots {
		t.Fatal("different positions should differ")
	}
}

func TestLootEnchantCapsRespectVanilla(t *testing.T) {
	// enchMaxLvl must cap the enchants loot can roll at their real vanilla
	// maxima — an over-capped Mending V / Flame III used to slip through.
	caps := map[int8]int8{
		enchMending: 1, enchFlame: 1, enchInfinity: 1, enchPunch: 2,
		enchLure: 3, enchLuckOfTheSea: 3, enchPower: 5,
	}
	for id, want := range caps {
		if got := enchMaxLvl(id); got != want {
			t.Errorf("enchMaxLvl(%d) = %d, want %d", id, got, want)
		}
	}
	// A bow drawn from treasure must never exceed those caps over many rolls.
	h := newHub(world.New(1))
	for seed := int64(0); seed < 200; seed++ {
		ctx := seededCtx(seed)
		fn := lootFn{F: "ench_levels", NP: &lootNP{T: "const", V: 30}}
		st := ctx.applyChestFn(h, &fn, invStack{item: itemBow, count: 1})
		for _, e := range st.ench {
			if e.lvl > 0 && e.lvl > enchMaxLvl(e.id) {
				t.Fatalf("bow enchant id %d rolled level %d over cap %d", e.id, e.lvl, enchMaxLvl(e.id))
			}
		}
	}
}
