package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func findTestVillage(w *world.World) (worldgen.Village, bool) {
	for x := -5000; x <= 5000; x += 384 {
		for z := -5000; z <= 5000; z += 384 {
			if v := w.Gen().VillageIn(x, z); v.Exists {
				return v, true
			}
		}
	}
	return worldgen.Village{}, false
}

func TestVillagePopulatesOnApproach(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	v, ok := findTestVillage(w)
	if !ok {
		t.Skip("no village near origin")
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(v.X), float64(v.Y), float64(v.Z)
	players := map[int32]*tracked{1: pl}
	h.updateVillages(players)
	h.updateVillageGolems(players) // golems now spawn via the ≥5-villager census
	villagers, golems := 0, 0
	for _, m := range h.mobs {
		switch m.etype {
		case entityVillager:
			villagers++
		case entityIronGolem:
			golems++
		}
	}
	wantVillagers := len(w.Gen().VillageVillagers(v)) // the jigsaw's villager pieces
	// The golem appears only if the village met the vanilla 5-villager quorum.
	wantGolems := 0
	if wantVillagers >= golemVillagersToAgree {
		wantGolems = 1
	}
	if villagers != wantVillagers || golems != wantGolems {
		t.Fatalf("want %d villagers + %d golem, got %d + %d", wantVillagers, wantGolems, villagers, golems)
	}
	// Second pass: no duplicates.
	before := len(h.mobs)
	h.updateVillages(players)
	if len(h.mobs) != before {
		t.Fatal("village must populate once per session")
	}
}

func TestTradeAuthority(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	h.initVillagerTrades(m, 0) // farmer
	// Pin two known offers so the exchange math is deterministic (the tier
	// rotation is a separate concern).
	m.offers = []mobOffer{
		{trade: vTrade{itemByName["wheat"], 20, itemByName["emerald"], 1, 16, 2, vTradeFixed, 0}},
		{trade: vTrade{itemByName["emerald"], 1, itemByName["bread"], 6, 16, 1, vTradeFixed, 0}},
	}
	h.openTrades(pl, m)
	if pl.winKind != winTrade {
		t.Fatal("trade window should open")
	}
	// Not enough wheat: result empty, click gives nothing.
	pl.trade[0] = invStack{item: itemByName["wheat"], count: 10}
	h.takeTradeResult(players, pl, 0)
	if pl.cursor.item != 0 {
		t.Fatal("AUTHORITY: trade must not pay without the full cost")
	}
	// Full cost: pays out, consumes exactly 20.
	pl.trade[0] = invStack{item: itemByName["wheat"], count: 25}
	h.takeTradeResult(players, pl, 0)
	if pl.cursor.item != itemByName["emerald"] || pl.cursor.count != 1 {
		t.Fatalf("trade should pay 1 emerald, cursor %+v", pl.cursor)
	}
	if pl.trade[0].count != 5 {
		t.Fatalf("trade should consume 20 wheat, left %d", pl.trade[0].count)
	}
	// Selecting another offer changes the result.
	pl.cursor = invStack{}
	pl.tradeSel = 1 // 1 emerald → 6 bread
	pl.trade[0] = invStack{item: itemByName["emerald"], count: 1}
	h.takeTradeResult(players, pl, 0)
	if pl.cursor.item != itemByName["bread"] || pl.cursor.count != 6 {
		t.Fatalf("bread trade broken: %+v", pl.cursor)
	}
	// Every completed trade pays the player experience (rewardTradeXp).
	if len(h.orbs) == 0 {
		t.Fatal("a trade must drop experience for the player")
	}
	for _, o := range h.orbs {
		if o.value < 3 || o.value > 11 {
			t.Fatalf("trade experience out of vanilla's range: %d", o.value)
		}
	}
}

func TestGolemPunchesHostiles(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	g := h.spawnMob(players, entityIronGolem, 0.5, 70, 0.5)
	g.behavior = golemBehavior{}
	z := h.spawnMob(players, entityZombie, 1.5, 70, 0.5)
	z.hostile = true
	hp := z.health
	h.golemMelee(players, g)
	if z.health >= hp {
		t.Fatal("golem should damage the zombie")
	}
	if z.kb == 0 {
		t.Fatal("golem hits should launch the target")
	}
}

func TestVillagerLevelsUp(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityVillager, 0, 70, 0)
	h.initVillagerTrades(m, 0) // farmer
	if m.tradeLevel != 1 {
		t.Fatalf("fresh villager should be tier 1, got %d", m.tradeLevel)
	}
	base := len(m.offers)
	if base == 0 {
		t.Fatal("a novice should already have tier-1 offers")
	}
	h.awardTradeXP(m, 10) // vanilla threshold to apprentice
	if m.tradeLevel != 2 {
		t.Fatalf("10 XP should promote to tier 2, got %d", m.tradeLevel)
	}
	if len(m.offers) <= base {
		t.Fatal("leveling up should unlock more offers")
	}
	h.awardTradeXP(m, 500) // overshoot straight to master, capped
	if m.tradeLevel != 5 {
		t.Fatalf("should cap at master (5), got %d", m.tradeLevel)
	}
}

func TestAllProfessionsHaveTrades(t *testing.T) {
	for i := range professionNames {
		if len(villagerTrades[i]) == 0 {
			t.Errorf("profession %d (%s) has no generated trades", i, professionNames[i])
		}
	}
}

// A librarian sells an enchanted book at every tier from one to four: a
// tradeable enchantment, priced by vanilla's formula (doubled for treasure,
// capped at 64) in emeralds plus one plain book, both of which the trade
// consumes; and the offer survives the store round trip.
func TestLibrarianSellsEnchantedBooks(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	h.initVillagerTrades(m, librarianProfession)
	var book *mobOffer
	for i := range m.offers {
		if m.offers[i].trade.outItem == itemEnchantedBook {
			book = &m.offers[i]
		}
	}
	if book == nil {
		t.Fatalf("a tier-1 librarian must offer an enchanted book: %+v", m.offers)
	}
	if book.outEnchs[0].lvl < 1 || !enchTradeAllowed(book.outEnchs[0].id) {
		t.Fatalf("book enchantment %+v must be tradeable", book.outEnchs[0])
	}
	if book.cost2Item != itemByName["book"] || book.cost2Count != 1 {
		t.Errorf("second cost %d×%d, want one book", book.cost2Item, book.cost2Count)
	}
	price := int(book.trade.inCount)
	lvl := int(book.outEnchs[0].lvl)
	lo, hi := 2+3*lvl, 2+(5+lvl*10-1)+3*lvl
	if enchDefs[book.outEnchs[0].id].flags&enchDoubleTradePrice != 0 {
		lo, hi = lo*2, hi*2
	}
	if hi > 64 {
		hi = 64
	}
	if price < lo || price > hi {
		t.Errorf("price %d emeralds outside vanilla's [%d, %d] for %s %d", price, lo, hi, enchName(book.outEnchs[0].id), lvl)
	}

	// Emeralds alone do not buy it; emeralds + a book do, and both are taken.
	h.openTrades(pl, m) // resets the selection to the first offer
	for i := range m.offers {
		if &m.offers[i] == book {
			pl.tradeSel = i
		}
	}
	pl.trade[0] = invStack{item: itemByName["emerald"], count: 64}
	h.takeTradeResult(players, pl, 0)
	if pl.cursor.item != 0 {
		t.Fatal("the book cost must be enforced")
	}
	pl.trade[1] = invStack{item: itemByName["book"], count: 2}
	h.takeTradeResult(players, pl, 0)
	if pl.cursor.item != itemEnchantedBook || pl.cursor.ench[0] != book.outEnchs[0] {
		t.Fatalf("expected the enchanted book on the cursor, got %+v", pl.cursor)
	}
	if pl.trade[0].count != 64-price || pl.trade[1].count != 1 {
		t.Errorf("costs not consumed: emeralds %d (want %d), books %d (want 1)", pl.trade[0].count, 64-price, pl.trade[1].count)
	}
	if back := unpackOffer(packOffer(*book)); back.cost2Item != book.cost2Item || back.outEnchs[0] != book.outEnchs[0] || back.trade != book.trade {
		t.Errorf("offer store round trip lost data: %+v vs %+v", back, *book)
	}
}

// A villager with its trade screen open stands still and faces the customer
// (LookAndFollowTradingPlayerSink).
func TestTradingVillagerStandsAndFaces(t *testing.T) {
	h := newHub(world.New(7))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 10, 70, 0
	m := h.spawnSpecies(players, entityVillager, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("no villager")
	}
	if h.tradingPartner(players, m) != nil {
		t.Fatal("nobody is trading with it yet")
	}
	h.openTrades(pl, m)
	if got := h.tradingPartner(players, m); got != pl {
		t.Fatalf("the customer should be the trading partner, got %v", got)
	}
	m.vx, m.vz = 1, 1
	h.updateMobs(players)
	if m.vx != 0 || m.vz != 0 {
		t.Errorf("a trading villager stands still, got %v %v", m.vx, m.vz)
	}
	if m.headYaw < -95 || m.headYaw > -85 { // the customer is due +x: yaw -90
		t.Errorf("it should face the customer, headYaw=%v", m.headYaw)
	}
}

// AcquirePoi(HOME): a villager with no bed claims a free one nearby, gives
// it up when it is broken, and never takes one another villager sleeps in.
func TestVillagerClaimsAPlacedBed(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	m := h.spawnSpecies(players, entityVillager, 0, 0.5, 70, 0.5)
	if m == nil {
		t.Fatal("no villager")
	}
	m.bed, m.home = blockPos{}, blockPos{}
	bed := blockPos{3, 70, 0}
	h.world.SetBlock(bed.x, bed.y, bed.z, worldgen.BlockBase("red_bed"))
	if !isBedBlock(h.world.At(bed.x, bed.y, bed.z)) {
		t.Fatal("test fixture: that is not a bed")
	}
	h.villagerBedTick(m)
	if m.bed != bed {
		t.Fatalf("a bedless villager should claim the bed, got %+v", m.bed)
	}
	// Another villager cannot take the same bed.
	o := h.spawnSpecies(players, entityVillager, 0, 1.5, 70, 0.5)
	o.bed, o.home, o.bedSearchAt = blockPos{}, blockPos{}, 0
	h.villagerBedTick(o)
	if o.bed == bed {
		t.Error("two villagers must not share a bed")
	}
	// Break it and the claim goes.
	h.world.SetBlock(bed.x, bed.y, bed.z, worldgen.Air)
	h.villagerBedTick(m)
	if m.bed != (blockPos{}) {
		t.Errorf("a broken bed should be forgotten, still %+v", m.bed)
	}
}

// The mason sells stone, not armour: its table used to be the trade-rebalance
// armorer's (the generator ran the last profession's region to the end of the
// file and swallowed the experimental trades).
func TestMasonSellsStone(t *testing.T) {
	prof := -1
	for i, n := range professionNames {
		if n == "mason" {
			prof = i
		}
	}
	if prof < 0 {
		t.Fatal("no mason profession")
	}
	banned := map[int32]string{
		itemByName["iron_helmet"]: "iron helmet", itemByName["chainmail_chestplate"]: "chainmail",
		itemByName["name_tag"]: "name tag", itemByName["bell"]: "bell", itemByName["shield"]: "shield",
	}
	seen := map[int32]bool{}
	for _, offers := range villagerTrades[prof] {
		for _, o := range offers {
			if what, bad := banned[o.outItem]; bad {
				t.Errorf("a mason should not sell a %s", what)
			}
			seen[o.outItem] = true
		}
	}
	for _, want := range []string{"brick", "chiseled_stone_bricks", "quartz_block"} {
		if !seen[itemByName[want]] {
			t.Errorf("a mason should sell %s", want)
		}
	}
}

// Vanilla's EnchantedItemForEmeralds: a weaponsmith/toolsmith/armorer/fletcher/
// fisherman sells one piece of gear, enchanted at roll time. The enchantments
// must all come from #on_traded_equipment, must land on the traded stack, and
// the price must be the listing's base cost topped up by the roll level (5..19,
// capped at 64 emeralds).
func TestVillagerSellsEnchantedGear(t *testing.T) {
	w := world.New(11)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	// Every generated enchanted-gear listing, rolled many times over.
	rolled := 0
	for prof, tiers := range villagerTrades {
		for _, pool := range tiers {
			for _, tr := range pool {
				if tr.kind != vTradeEnchantedGear {
					continue
				}
				if tr.inItem != itemByName["emerald"] || tr.outCount != 1 {
					t.Errorf("%s: an enchanted-gear listing costs emeralds and sells one item, got %+v",
						professionNames[prof], tr)
				}
				for i := 0; i < 40; i++ {
					o := h.rollGearOffer(tr)
					rolled++
					if o.trade.kind != vTradeFixed {
						t.Fatal("a rolled offer must not roll again")
					}
					lvl := int(o.trade.inCount) - int(tr.inCount)
					if o.trade.inCount != 64 && (lvl < 5 || lvl > 19) {
						t.Fatalf("price %d is base %d + %d, outside vanilla's 5..19 top-up",
							o.trade.inCount, tr.inCount, lvl)
					}
					if o.trade.inCount > 64 {
						t.Fatalf("price %d exceeds vanilla's 64-emerald cap", o.trade.inCount)
					}
					st := o.output()
					if st.item != tr.outItem {
						t.Fatalf("sold %d, want %d", st.item, tr.outItem)
					}
					for _, e := range st.ench {
						if e.lvl == 0 {
							continue
						}
						if !enchGearAllowed(e.id) {
							t.Fatalf("%s is not in #on_traded_equipment", enchName(e.id))
						}
						if e.lvl > enchMaxLvl(e.id) {
							t.Fatalf("%s %d exceeds its cap %d", enchName(e.id), e.lvl, enchMaxLvl(e.id))
						}
					}
				}
			}
		}
	}
	if rolled == 0 {
		t.Fatal("no enchanted-gear listings in the generated table")
	}

	// A weaponsmith at master tier actually has one to sell, enchanted.
	weaponsmith := -1
	for i, n := range professionNames {
		if n == "weaponsmith" {
			weaponsmith = i
		}
	}
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	h.initVillagerTrades(m, weaponsmith)
	for tier := 2; tier <= maxTradeTier; tier++ {
		m.tradeLevel = tier
		h.unlockTier(m, tier)
	}
	found := false
	for i := range m.offers {
		if o := &m.offers[i]; o.outEnchs[0].lvl > 0 {
			found = true
			// The offer survives the store, enchantments and all.
			back := unpackOffer(packOffer(*o))
			if back.outEnchs != o.outEnchs || back.trade != o.trade {
				t.Errorf("offer round trip lost data: %+v vs %+v", back, *o)
			}
		}
	}
	if !found {
		t.Errorf("a master weaponsmith must offer enchanted gear: %+v", m.offers)
	}
}

// Worlds saved before offers became a named object still load: the historical
// flat array decodes into the same offer.
func TestSavedOfferLegacyArray(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want savedOffer
	}{
		{`[1,2,3,4,5,6,7,8]`, savedOffer{In: 1, InN: 2, Out: 3, OutN: 4, MaxUses: 5, XP: 6, Uses: 7, Demand: 8}},
		{`[1,2,3,4,5,6,7]`, savedOffer{In: 1, InN: 2, Out: 3, OutN: 4, MaxUses: 5, XP: 6, Uses: 7}},
		{`[1,2,3,4,5,6,7,8,9,1,2306]`, savedOffer{In: 1, InN: 2, Out: 3, OutN: 4, MaxUses: 5, XP: 6,
			Uses: 7, Demand: 8, C2Item: 9, C2N: 1, Ench: []int32{2306}}},
	} {
		var got savedOffer
		if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s decoded to %+v, want %+v", tc.raw, got, tc.want)
		}
		// And the enchantment survives into the live offer.
		if o := unpackOffer(got); len(tc.want.Ench) > 0 && o.outEnchs[0] != (enchApply{id: 9, lvl: 2}) {
			t.Errorf("%s: enchantment lost, got %+v", tc.raw, o.outEnchs[0])
		}
	}
}

// Vanilla's TreasureMapForEmeralds — the cartographer's explorer maps, and the
// only route to a monument, mansion or trial chamber that does not mean walking
// the ocean. Every generated listing must name a structure the generator can
// actually locate and a real decoration; the rolled offer must hand over a
// marked, named map for emeralds plus a compass; and a listing restricted to
// other villager types must yield nothing.
func TestCartographerSellsExplorerMaps(t *testing.T) {
	known := map[string]bool{}
	for _, n := range worldgen.StructureNames() {
		known[n] = true
	}
	for i, l := range villagerMapListings {
		if !known[l.dest] {
			t.Errorf("listing %d points at %q, which the generator cannot locate", i, l.dest)
		}
		if l.decor < 4 || l.decor > 34 {
			t.Errorf("listing %d has decoration %d, outside the registry", i, l.decor)
		}
		if l.label == "" {
			t.Errorf("listing %d has no name", i)
		}
	}
	// Every map listing is reachable from the cartographer's table.
	seen := 0
	cartographer := -1
	for i, n := range professionNames {
		if n == "cartographer" {
			cartographer = i
		}
	}
	for _, pool := range villagerTrades[cartographer] {
		for _, tr := range pool {
			if tr.kind == vTradeTreasureMap {
				seen++
				if tr.inItem != itemByName["emerald"] || tr.outItem != itemFilledMap {
					t.Errorf("a treasure map listing sells a filled map for emeralds, got %+v", tr)
				}
			}
		}
	}
	if seen != len(villagerMapListings) {
		t.Errorf("%d map listings in the cartographer's table, %d defined", seen, len(villagerMapListings))
	}

	// Roll the ocean-monument listing beside a real monument.
	w := world.New(5)
	g := w.Gen()
	var mx, mz int
	found := false
	for r := 0; r < 40 && !found; r++ {
		mx, mz, found = g.LocateStructure("monument", r*2000, 0, 2000)
	}
	if !found {
		t.Skip("no ocean monument in the scanned range for this seed")
	}
	h := newHub(w)
	h.maps = newMapStore("")
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityVillager, float64(mx)+8, 64, float64(mz)+8)
	var monument vTrade
	for _, pool := range villagerTrades[cartographer] {
		for _, tr := range pool {
			if tr.kind == vTradeTreasureMap && villagerMapListings[tr.mapIdx-1].dest == "monument" {
				monument = tr
			}
		}
	}
	o, ok := h.rollMapOffer(m, monument)
	if !ok {
		t.Fatalf("the monument at %d,%d is right here and must be locatable", mx, mz)
	}
	if o.cost2Item != itemByName["compass"] || o.cost2Count != 1 {
		t.Errorf("second cost %d×%d, want one compass", o.cost2Item, o.cost2Count)
	}
	if o.trade.kind != vTradeFixed {
		t.Error("a rolled offer must not roll again")
	}
	st := o.output()
	if st.item != itemFilledMap || st.mapID == 0 || st.name != "Ocean Explorer Map" {
		t.Fatalf("offer hands over %+v, want a named filled map", st)
	}
	md := h.maps.get(st.mapID)
	if md == nil || len(md.Marks) != 1 || md.Marks[0].Type != 9 ||
		int(md.Marks[0].X) != mx || int(md.Marks[0].Z) != mz {
		t.Fatalf("map %d does not mark the monument at %d,%d: %+v", st.mapID, mx, mz, md)
	}
	if back := unpackOffer(packOffer(o)); back.outMapID != o.outMapID || back.outName != o.outName ||
		back.cost2Item != o.cost2Item || back.trade != o.trade {
		t.Errorf("offer round trip lost data: %+v vs %+v", back, o)
	}

	// A listing meant for other villager types yields nothing at all.
	typ := h.villagerType(m)
	for i, l := range villagerMapListings {
		if l.forTypes == 0 || l.forTypes&(1<<uint(typ)) != 0 {
			continue
		}
		if _, ok := h.rollMapOffer(m, vTrade{kind: vTradeTreasureMap, mapIdx: int32(i + 1)}); ok {
			t.Errorf("%s is not offered to villager type %d", l.label, typ)
		}
		break
	}
}
