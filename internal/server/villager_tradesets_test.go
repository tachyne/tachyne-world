package server

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Each level of a villager's career is a trade_set: it draws `amount`
// offers from its listings without replacement, skipping a listing that
// yields none (a merchant_predicate for another villager type). Walk a
// villager of several professions through all five levels and check every
// level added exactly its share, and that every offer names real items in
// sane counts.
func TestTradeSetsDrawTheirAmount(t *testing.T) {
	w := world.New(41)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	for _, name := range []string{"farmer", "fisherman", "armorer", "cleric", "librarian", "leatherworker", "toolsmith"} {
		prof := -1
		for i, n := range professionNames {
			if n == name {
				prof = i
			}
		}
		if prof < 0 {
			t.Fatalf("no profession %q", name)
		}
		for trial := 0; trial < 8; trial++ {
			m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
			m.variant, m.variantSet = int32(trial%7), true
			for tier := 1; tier <= maxTradeTier; tier++ {
				before := len(m.offers)
				if tier == 1 {
					h.initVillagerTrades(m, prof)
				} else {
					m.tradeLevel = tier
					h.unlockTier(m, tier)
				}
				eligible := 0
				for _, tr := range villagerTrades[prof][tier] {
					if h.villagerTypeAllows(m, tr) {
						eligible++
					}
				}
				want := min(villagerTradeAmount[prof][tier], eligible)
				if got := len(m.offers) - before; got != want {
					t.Fatalf("%s level %d (type %d) drew %d offers, want %d", name, tier, m.variant, got, want)
				}
			}
			for _, o := range m.offers {
				if o.trade.inItem == 0 || o.trade.outItem == 0 || o.trade.inCount < 1 || o.trade.outCount < 1 ||
					o.trade.maxUses < 1 || o.trade.inCount > 64 {
					t.Fatalf("%s: malformed offer %+v", name, o.trade)
				}
				if o.trade.kind != vTradeFixed || o.trade.forTypes != 0 {
					t.Fatalf("%s: an offer must be resolved, got %+v", name, o.trade)
				}
				if (o.cost2Item == 0) != (o.cost2Count == 0) {
					t.Fatalf("%s: half a second cost: %+v", name, o)
				}
			}
		}
	}
}

// Every listing in the generated tables names a real item, and every rolled
// kind points into its side table.
func TestGeneratedTradeTablesAreWellFormed(t *testing.T) {
	check := func(where string, tr vTrade) {
		if tr.inItem == 0 || tr.outItem == 0 || tr.outCount < 1 || tr.maxUses < 1 || tr.mult100 < 1 {
			t.Errorf("%s: malformed listing %+v", where, tr)
		}
		side := map[int32]int{vTradeTreasureMap: len(villagerMapListings), vTradeStew: len(villagerStewListings),
			vTradePotion: len(villagerPotionPools)}
		if n, ok := side[tr.kind]; ok && (tr.aux < 1 || int(tr.aux) > n) {
			t.Errorf("%s: kind %d points at side row %d of %d", where, tr.kind, tr.aux, n)
		}
		if tr.forTypes>>7 != 0 {
			t.Errorf("%s: villager-type mask %b names no type", where, tr.forTypes)
		}
	}
	for prof, tiers := range villagerTrades {
		for tier, pool := range tiers {
			if villagerTradeAmount[prof][tier] < 1 {
				t.Errorf("%s level %d draws nothing", professionNames[prof], tier)
			}
			for _, tr := range pool {
				check(professionNames[prof], tr)
			}
		}
	}
	for i, set := range traderTradeSets {
		for _, tr := range set.trades {
			if tr.forTypes != 0 {
				t.Errorf("wandering trader set %d: a villager-type listing can never resolve", i)
			}
			check("wandering trader", tr)
		}
	}
	for i, pool := range villagerPotionPools {
		for _, n := range pool {
			if _, ok := potionByVanillaName[n]; !ok {
				t.Errorf("potion pool %d: %q has no engine potion", i, n)
			}
		}
	}
}

// A villager saved before trades came from the 26.3 data keeps its offers:
// the stored rows are full trades, so a 1.21.11-era explorer map (a
// filled_map carrying its name), a stew, and a legacy array row all load and
// sell exactly what they did, and survive another save.
func TestSavedOffersFromBeforeTheDataTables(t *testing.T) {
	raw := `[
		{"i":` + strconv.Itoa(int(itemByName["emerald"])) + `,"ic":13,"o":` + strconv.Itoa(int(itemFilledMap)) + `,"oc":1,"mu":12,"xp":10,"u":3,"c2":` + strconv.Itoa(int(itemByName["compass"])) + `,"c2n":1,"m":7,"n":"Ocean Explorer Map","pm":20},
		{"i":` + strconv.Itoa(int(itemByName["emerald"])) + `,"ic":1,"o":` + strconv.Itoa(int(itemSuspiciousStew)) + `,"oc":1,"mu":12,"xp":15,"st":17},
		[` + strconv.Itoa(int(itemByName["wheat"])) + `,20,` + strconv.Itoa(int(itemByName["emerald"])) + `,1,16,2,4,1]
	]`
	var saved []savedOffer
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	offers := make([]mobOffer, len(saved))
	for i, s := range saved {
		offers[i] = unpackOffer(s)
	}
	if st := offers[0].output(); st.item != itemFilledMap || st.mapID != 7 || st.name != "Ocean Explorer Map" ||
		offers[0].cost2Item != itemByName["compass"] || offers[0].uses != 3 || offers[0].priceMult() != 0.2 {
		t.Errorf("the old explorer map offer changed on load: %+v", offers[0])
	}
	if st := offers[1].output(); st.item != itemSuspiciousStew || st.stew != 17 {
		t.Errorf("the old stew offer changed on load: %+v", st)
	}
	if o := offers[2]; o.trade.inItem != itemByName["wheat"] || o.trade.inCount != 20 || o.uses != 4 ||
		o.demand != 1 || o.priceMult() != 0.05 {
		t.Errorf("the legacy array offer changed on load: %+v", o)
	}
	for i, o := range offers {
		if back := unpackOffer(packOffer(o)); back != o {
			t.Errorf("offer %d does not survive another save: %+v vs %+v", i, back, o)
		}
	}
}
