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
	count := func() (villagers, golems int) {
		for _, m := range h.mobs {
			switch m.etype {
			case entityVillager:
				villagers++
			case entityIronGolem:
				golems++
			}
		}
		return
	}

	// A village that has never slept grows no golem: vanilla's quorum asks for
	// five villagers who lay down within the last day.
	h.updateVillageGolems(players)
	villagers, golems := count()
	wantVillagers := len(w.Gen().VillageVillagers(v)) // the jigsaw's villager pieces
	if villagers != wantVillagers {
		t.Fatalf("want %d villagers, got %d", wantVillagers, villagers)
	}
	if golems != 0 {
		t.Fatalf("a village whose villagers have never slept grew %d golems", golems)
	}

	// Let them sleep, and gather them at the bell the way the midday segment
	// does — vanilla's trigger is two villagers gossiping at the meeting
	// point, so the five that agree are standing together by construction.
	gathered := 0
	for _, m := range h.mobs {
		if m.etype != entityVillager {
			continue
		}
		m.lastSlept = h.tick.Load() + 1
		if !m.baby && gathered < golemVillagersToAgree {
			m.x, m.y, m.z = float64(v.X)+float64(gathered), float64(v.Y), float64(v.Z)
			gathered++
		}
	}
	h.updateVillageGolems(players)
	_, golems = count()
	if gathered >= golemVillagersToAgree && golems == 0 {
		t.Fatalf("%d rested villagers gathered at the bell grew no golem", gathered)
	}
	if gathered < golemVillagersToAgree && golems != 0 {
		t.Fatalf("only %d villagers gathered, under the quorum, but %d golems grew", gathered, golems)
	}

	// Having one, they do not grow another.
	before := golems
	h.updateVillageGolems(players)
	if _, now := count(); now != before {
		t.Fatalf("a village with a golem grew %d more", now-before)
	}
	// Second pass: no duplicates.
	mobsBefore := len(h.mobs)
	h.updateVillages(players)
	if len(h.mobs) != mobsBefore {
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
		{trade: vTrade{itemByName["wheat"], 20, itemByName["emerald"], 1, 16, 2, vTradeFixed, 0, 0, 0, defaultPriceMult100}},
		{trade: vTrade{itemByName["emerald"], 1, itemByName["bread"], 6, 16, 1, vTradeFixed, 0, 0, 0, defaultPriceMult100}},
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
	// The award is paid in ladder denominations, so check the total rather
	// than each orb: a 4-point trade comes out as a 3 and a 1.
	total := 0
	for _, o := range h.orbs {
		total += o.value * o.count
	}
	if total < 3 || total > 11 {
		t.Fatalf("trade experience out of vanilla's range: %d", total)
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
	// Vanilla does not promote on the trade itself: rewardTradeXp arms
	// updateMerchantTimer and the tier lands forty ticks later, along with the
	// Regeneration that draws the sparkle. That pause is visible in game.
	if !h.awardTradeXP(m, 10) { // vanilla threshold to apprentice
		t.Fatal("10 XP should arm a promotion")
	}
	if m.tradeLevel != 1 || !m.levelUpPending {
		t.Fatalf("the tier must wait for the timer: level %d pending %v", m.tradeLevel, m.levelUpPending)
	}
	for i := 0; i < merchantUpdateDelay/mobMoveInterval-1; i++ {
		h.villagerMerchantTick(players, m)
	}
	if m.tradeLevel != 1 {
		t.Fatalf("promoted %d ticks early", merchantUpdateDelay)
	}
	h.villagerMerchantTick(players, m)
	if m.tradeLevel != 2 {
		t.Fatalf("10 XP should promote to tier 2 after the delay, got %d", m.tradeLevel)
	}
	if len(m.offers) <= base {
		t.Fatal("leveling up should unlock more offers")
	}
	if m.hasEffect(effRegen) == 0 {
		t.Error("a promotion grants Regeneration — the level-up sparkle")
	}

	// And only ONE tier per trade, however much experience it paid.
	if !h.awardTradeXP(m, 500) {
		t.Fatal("500 XP should arm another promotion")
	}
	runMerchantTimer := func() {
		for i := 0; i < merchantUpdateDelay/mobMoveInterval; i++ {
			h.villagerMerchantTick(players, m)
		}
	}
	runMerchantTimer()
	if m.tradeLevel != 3 {
		t.Fatalf("one trade promotes one tier; got %d", m.tradeLevel)
	}
	for m.tradeLevel < maxTradeTier {
		if !h.awardTradeXP(m, 0) {
			t.Fatalf("stuck at tier %d with %d XP", m.tradeLevel, m.tradeXP)
		}
		runMerchantTimer()
	}
	if h.awardTradeXP(m, 500) {
		t.Fatalf("master is the cap, got %d", m.tradeLevel)
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
	h.initVillagerTrades(m, 0) // a villager with nothing to sell opens nothing
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
			if tr.kind == vTradeTreasureMap && villagerMapListings[tr.aux-1].dest == "monument" {
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
	// 26.3: the ocean explorer map is its own item, named by the item.
	if st.item != int32(itemByName["ocean_monument_map"]) || st.mapID == 0 || st.name != "" {
		t.Fatalf("offer hands over %+v, want an ocean monument map", st)
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
		if _, ok := h.rollMapOffer(m, vTrade{kind: vTradeTreasureMap, aux: int32(i + 1)}); ok {
			t.Errorf("%s is not offered to villager type %d", l.label, typ)
		}
		break
	}
}

// The four remaining vanilla listing types, which between them finish the
// trade map: a leatherworker's dyed armour, a farmer's suspicious stew, a
// fletcher's tipped arrows and a fisherman's biome boat — plus
// ItemsAndEmeraldsToItems, whose second item cost rides the plain path.
func TestRemainingVillagerListingTypes(t *testing.T) {
	w := world.New(13)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	prof := func(name string) int {
		for i, n := range professionNames {
			if n == name {
				return i
			}
		}
		t.Fatalf("no profession %q", name)
		return -1
	}
	find := func(p int, kind int32) (vTrade, bool) {
		for tier := 1; tier <= maxTradeTier; tier++ {
			for _, tr := range villagerTrades[p][tier] {
				if tr.kind == kind {
					return tr, true
				}
			}
		}
		return vTrade{}, false
	}

	// Dyed armour: a leather piece with a blended colour, every roll.
	dyed, ok := find(prof("leatherworker"), vTradeDyedArmor)
	if !ok {
		t.Fatal("the leatherworker has no dyed-armour listing")
	}
	colors := map[int32]bool{}
	for i := 0; i < 60; i++ {
		o := h.rollDyedOffer(dyed)
		st := o.output()
		if !isDyeable(st.item) || st.color == 0 {
			t.Fatalf("roll %d gave %+v, want a dyed leather piece", i, st)
		}
		colors[st.color] = true
	}
	if len(colors) < 5 {
		t.Errorf("only %d distinct colours in 60 rolls — the dye is not random", len(colors))
	}

	// Suspicious stew: every listing must resolve to a stew table row, and
	// the stack must carry the effect vanilla's listing names.
	stews := 0
	for i, l := range villagerStewListings {
		code, ok := stewCodeFor(l.effect, l.ticks)
		if !ok {
			t.Errorf("listing %d (%s %d ticks) has no suspicious-stew row; add one to stewEffects",
				i, l.effect, l.ticks)
			continue
		}
		stews++
		o, ok := h.rollStewOffer(vTrade{inItem: itemByName["emerald"], inCount: 1,
			outItem: itemSuspiciousStew, outCount: 1, maxUses: 12, xp: 15,
			kind: vTradeStew, aux: int32(i + 1)})
		if !ok {
			t.Fatalf("listing %d did not roll", i)
		}
		st := o.output()
		eff, found := stewEffectOf(st.stew)
		if st.item != itemSuspiciousStew || !found || eff.effect != effectNames[l.effect] {
			t.Errorf("listing %d sold %+v, want a stew of %s", i, st, l.effect)
		}
		if code != st.stew {
			t.Errorf("listing %d resolved to %d but sold %d", i, code, st.stew)
		}
	}
	if stews != len(villagerStewListings) {
		t.Errorf("%d of %d stew listings resolved", stews, len(villagerStewListings))
	}

	// Tipped arrows: a brewable potion with effects, never a plain bottle.
	arrow, ok := find(prof("fletcher"), vTradeTippedArrow)
	if !ok {
		t.Fatal("the fletcher has no tipped-arrow listing")
	}
	brewable := map[int8]bool{}
	for _, p := range brewablePotions() {
		brewable[p] = true
	}
	if brewable[potLuck] {
		t.Error("Luck cannot be brewed, so a villager cannot tip an arrow with it")
	}
	kinds := map[int8]bool{}
	for i := 0; i < 60; i++ {
		o, ok := h.rollArrowOffer(arrow)
		if !ok {
			t.Fatal("the tipped-arrow listing must always roll")
		}
		st := o.output()
		if st.item != itemTippedArrow || !brewable[st.potion] ||
			len(potionDefs[st.potion].effects) == 0 {
			t.Fatalf("roll %d gave %+v, want a tipped arrow of a brewable potion", i, st)
		}
		if o.cost2Item != itemByName["arrow"] || o.cost2Count != 5 {
			t.Fatalf("second cost %d×%d, want five arrows", o.cost2Item, o.cost2Count)
		}
		kinds[st.potion] = true
	}
	if len(kinds) < 5 {
		t.Errorf("only %d distinct potions in 60 rolls", len(kinds))
	}

	// The biome boat: the villager buys the boat its own birth biome names.
	boat, ok := find(prof("fisherman"), vTradeTypeItem)
	if !ok {
		t.Fatal("the fisherman has no villager-type listing")
	}
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	o, ok := h.typeItemOffer(m, boat)
	if !ok {
		t.Fatal("the boat listing must roll for any villager type")
	}
	want := villagerTypeItems[boat.aux-1][h.villagerType(m)]
	if o.trade.inItem != want || o.trade.outItem != itemByName["emerald"] {
		t.Errorf("buys %d for %d, want %d for an emerald", o.trade.inItem, o.trade.outItem, want)
	}

	// ItemsAndEmeraldsToItems: a plain offer that carries a second item cost
	// out of the table, and keeps it through the store.
	combo := vTrade{}
	for tier := 1; tier <= maxTradeTier; tier++ {
		for _, tr := range villagerTrades[prof("fisherman")][tier] {
			if tr.kind == vTradeFixed && tr.c2Item != 0 {
				combo = tr
			}
		}
	}
	if combo.c2Item == 0 {
		t.Fatal("the fisherman's cooked-fish trades need a second item cost")
	}
	co := offerFrom(combo)
	if co.cost2Item != combo.c2Item || co.cost2Count != combo.c2Count {
		t.Errorf("second cost %d×%d did not reach the offer", co.cost2Item, co.cost2Count)
	}
	if back := unpackOffer(packOffer(co)); back != co {
		t.Errorf("round trip lost data: %+v vs %+v", back, co)
	}
	// And a rolled offer keeps its roll through the store.
	for _, o := range []mobOffer{h.rollDyedOffer(dyed)} {
		if back := unpackOffer(packOffer(o)); back.outColor != o.outColor || back.trade != o.trade {
			t.Errorf("dyed offer round trip lost data: %+v vs %+v", back, o)
		}
	}
}

// Vanilla's priceMultiplier is per listing, not per world: 0.2 on armour,
// bells, shields, saddles, explorer maps, dyed armour and most enchanted gear,
// 0.05 on everything else. It scales both the demand markup and the reputation
// discount, so a single global 0.05 made those offers react four times too
// weakly to both.
func TestPerListingPriceMultiplier(t *testing.T) {
	seen := map[int32]int{}
	for _, tiers := range villagerTrades {
		for _, pool := range tiers {
			for _, tr := range pool {
				seen[tr.mult100]++
			}
		}
	}
	for m := range seen {
		if m != 5 && m != 20 {
			t.Errorf("multiplier %d/100 is neither of vanilla's two values", m)
		}
	}
	if seen[20] == 0 || seen[5] == 0 {
		t.Fatalf("expected both multipliers in the table, got %v", seen)
	}

	// The armorer's iron helmet is one of vanilla's 0.2 listings; the wheat
	// the farmer buys is 0.05. Demand bites four times harder on the former.
	steep := mobOffer{trade: vTrade{inItem: itemByName["emerald"], inCount: 20, mult100: 20}, demand: 10}
	shallow := mobOffer{trade: vTrade{inItem: itemByName["emerald"], inCount: 20, mult100: 5}, demand: 10}
	if got, want := steep.costCount(), 20+int(20*10*0.2); got != want {
		t.Errorf("0.2 offer charges %d, want %d", got, want)
	}
	if got, want := shallow.costCount(), 20+int(20*10*0.05); got != want {
		t.Errorf("0.05 offer charges %d, want %d", got, want)
	}
	if steep.priceMult() <= shallow.priceMult() {
		t.Error("the 0.2 listing must react harder than the 0.05 one")
	}

	// An offer stored before the multiplier was per-listing reads back as the
	// old global 0.05 rather than as zero (which would kill demand entirely).
	var legacy savedOffer
	if err := json.Unmarshal([]byte(`[1,2,3,4,5,6,7,8]`), &legacy); err != nil {
		t.Fatal(err)
	}
	if o := unpackOffer(legacy); o.priceMult() != 0.05 {
		t.Errorf("a legacy offer's multiplier is %v, want 0.05", o.priceMult())
	}
}

// The librarian's enchanted book is one of the tier's listings, not an extra
// on top: vanilla's EnchantBookForEmeralds sits in the pool and competes for
// the two slots. Appending it gave librarians three offers a tier where every
// other profession gets two.
func TestLibrarianBookIsOneOfTheTierListings(t *testing.T) {
	books := 0
	for tier := 1; tier <= 4; tier++ {
		found := false
		for _, tr := range villagerTrades[librarianProfession][tier] {
			if tr.kind == vTradeEnchantedBook {
				found = true
				books++
				if tr.c2Item != itemByName["book"] || tr.c2Count != 1 || tr.mult100 != 20 {
					t.Errorf("tier %d's book listing is %+v; want one book beside the emeralds at 0.2", tier, tr)
				}
			}
		}
		if !found {
			t.Errorf("tier %d has no enchanted-book listing", tier)
		}
	}
	if books != 4 {
		t.Errorf("%d book listings, want one each for tiers 1-4", books)
	}
	if len(villagerTrades[librarianProfession][5]) != 1 {
		t.Error("a master librarian's only listing is the name tag")
	}

	w := world.New(29)
	h := newHub(w)
	players := map[int32]*tracked{}
	sawBook := false
	for i := 0; i < 40; i++ {
		m := h.spawnMob(players, entityVillager, float64(i), 70, 0)
		h.initVillagerTrades(m, librarianProfession)
		if len(m.offers) != offersPerTier {
			t.Fatalf("a novice librarian has %d offers, want %d", len(m.offers), offersPerTier)
		}
		for _, o := range m.offers {
			if o.trade.outItem == itemEnchantedBook {
				sawBook = true
				if o.outEnchs[0].lvl == 0 || o.cost2Item != itemByName["book"] {
					t.Fatalf("the book offer is unrolled: %+v", o)
				}
			}
		}
	}
	if !sawBook {
		t.Error("the book never came up in 40 novice librarians")
	}
}

// Villager.mobInteract refuses a screen it has nothing to put in: a sleeping
// villager is not interactable at all, and one with no offers left shakes its
// head instead of opening an empty window.
func TestTradeScreenRefusals(t *testing.T) {
	w := world.New(31)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	h.initVillagerTrades(m, librarianProfession)

	m.sleeping = true
	h.openTrades(pl, m)
	if pl.winKind == winTrade {
		t.Error("a sleeping villager must not open its trades")
	}
	m.sleeping = false

	m.offers = nil
	h.openTrades(pl, m)
	if pl.winKind == winTrade {
		t.Error("a villager with no offers must not open an empty screen")
	}

	h.initVillagerTrades(m, librarianProfession)
	h.openTrades(pl, m)
	if pl.winKind != winTrade || pl.tradeWith != m.eid {
		t.Errorf("a stocked villager must open its trades, got window kind %v", pl.winKind)
	}
}

// Villager.shouldRestock rolls the trading day over off the DAY COUNT, not off
// waking up, so a villager with no bed — or one whose bed was broken — still
// gets its two restocks a day. The counters persist, too: a reload used to
// hand every villager a fresh budget.
func TestRestockDayRollsOverWithoutABed(t *testing.T) {
	w := world.New(37)
	h := newHub(w)
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityVillager, 0, 70, 0)
	h.initVillagerTrades(m, 0) // farmer
	use := func() {
		for i := range m.offers {
			m.offers[i].uses = m.offers[i].trade.maxUses
		}
	}

	// Two restocks, then the cap bites.
	h.tick.Store(1000)
	use()
	if !h.shouldRestock(m) {
		t.Fatal("the first restock of the day must be allowed")
	}
	h.restockOffers(m)
	h.tick.Store(h.tick.Load() + restockInterval + 1)
	use()
	if !h.shouldRestock(m) {
		t.Fatal("a second restock 2400 ticks later must be allowed")
	}
	h.restockOffers(m)
	h.tick.Store(h.tick.Load() + restockInterval + 1)
	use()
	if h.shouldRestock(m) {
		t.Fatalf("a third restock in one day must be refused (today=%d)", m.restocksToday)
	}

	// The day turns over — no bed, no sleeping, just time — and the budget
	// comes back, with the missed restocks caught up.
	h.dayTime.Store(h.dayTime.Load() + dayLengthTicks*2)
	h.tick.Store(h.tick.Load() + restockDayInterval + 1)
	use()
	if !h.shouldRestock(m) {
		t.Fatal("a new day must restore the restock budget")
	}
	if m.restocksToday != 0 {
		t.Errorf("the daily counter is %d after the roll-over, want 0", m.restocksToday)
	}
	// catchUpDemand only makes up restocks the villager did NOT take, so a
	// villager that used both of yesterday's gets nothing extra here — the
	// restock the fresh budget allows is what unlocks its offers.
	h.restockOffers(m)
	for _, o := range m.offers {
		if o.uses != 0 {
			t.Error("the day's first restock must unlock every offer")
			break
		}
	}

	// A villager that took NEITHER restock has both made up for it.
	idle := h.spawnMob(players, entityVillager, 4, 70, 0)
	h.initVillagerTrades(idle, 0)
	for i := range idle.offers {
		idle.offers[i].uses = idle.offers[i].trade.maxUses
	}
	idle.lastRestockDay = 1
	h.dayTime.Store(h.dayTime.Load() + dayLengthTicks)
	h.shouldRestock(idle)
	for _, o := range idle.offers {
		if o.uses != 0 {
			t.Error("catchUpDemand unlocks the offers an idle villager never restocked")
			break
		}
	}

	// And the budget survives the store.
	sm := toSavedMob(m)
	if sm.Restocks != m.restocksToday || sm.LastStock != m.lastRestockTick {
		t.Errorf("the restock budget did not reach the store: %+v", sm)
	}
}

// The iron-golem quorum is vanilla's, not a bell census: five ADULT villagers
// within one villager's ten-block box, each of whom lay down within the last
// day, and none of whom has seen a golem in the last thirty seconds.
func TestIronGolemQuorum(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	now := h.tick.Load()

	make5 := func(x, y, z float64) []*mob {
		var out []*mob
		for i := 0; i < golemVillagersToAgree; i++ {
			m := h.spawnMob(players, entityVillager, x+float64(i), y, z)
			m.lastSlept = now + 1
			out = append(out, m)
		}
		return out
	}
	golems := func() (n int) {
		for _, m := range h.mobs {
			if m.etype == entityIronGolem {
				n++
			}
		}
		return
	}

	vs := make5(0, 70, 0)
	// One of them has never slept: four is under the quorum.
	vs[0].lastSlept = 0
	h.updateVillageGolems(players)
	if n := golems(); n != 0 {
		t.Fatalf("four rested villagers grew %d golems", n)
	}

	vs[0].lastSlept = now + 1
	h.updateVillageGolems(players)
	if n := golems(); n != 1 {
		t.Fatalf("five rested villagers grew %d golems, want 1", n)
	}

	// Having seen it, they do not agree again.
	h.updateVillageGolems(players)
	if n := golems(); n != 1 {
		t.Fatalf("the quorum fired twice: %d golems", n)
	}

	// A villager who last slept more than a day ago no longer counts, so a
	// second village of five that stopped going home grows nothing.
	far := make5(400, 70, 400)
	for _, m := range far {
		m.lastSlept = 1 // a day and more ago once the clock is wound on
	}
	h.tick.Store(now + golemSleptWithin + 10)
	h.updateVillageGolems(players)
	if n := golems(); n != 1 {
		t.Fatalf("villagers who stopped sleeping still grew a golem: %d", n)
	}
}
