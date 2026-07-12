package server

import (
	"path/filepath"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestStatsCountersAndSnapshot: counters accumulate at gameplay sites and the
// snapshot renders them deterministically in canonical ids.
func TestStatsCountersAndSnapshot(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.adv = advState{}
	players[1] = pl

	// eat an apple: used(apple) + husbandry progress ride the same hook
	pl.food = 10
	pl.inv.slots[0] = invStack{item: itemByName["apple"], count: 1}
	h.eat(players, pl, 0)
	if pl.stats[statKey{attachproto.StatUsed, itemByName["apple"]}] != 1 {
		t.Fatalf("used(apple) not counted: %+v", pl.stats)
	}

	h.incCustom(pl, "deaths", 1)
	h.incCustom(pl, "play_time", 40)
	h.incCustom(pl, "no_such_stat", 1) // unknown names are ignored
	if len(pl.stats) != 3 {
		t.Fatalf("unexpected counter set: %+v", pl.stats)
	}

	snap := statsSnapshot(pl)
	if len(snap.Entries) != 3 {
		t.Fatalf("snapshot size %d", len(snap.Entries))
	}
	for i := 1; i < len(snap.Entries); i++ {
		a, b := snap.Entries[i-1], snap.Entries[i]
		if a.T > b.T || (a.T == b.T && a.K >= b.K) {
			t.Fatal("snapshot not sorted")
		}
	}
}

// TestStatBlockReg: broken block states resolve to block-registry ids.
func TestStatBlockReg(t *testing.T) {
	if reg, ok := statBlockReg(1); !ok || reg != 1 { // stone: state 1 = block id 1
		t.Fatalf("stone: reg=%d ok=%v", reg, ok)
	}
	if _, ok := statBlockReg(1 << 30); ok {
		t.Fatal("bogus state resolved")
	}
}

// TestStatsStoreRoundTrip: persistence keyed by name, "type:key" entries.
func TestStatsStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	st := newStatsStore(path)
	st.save("wesley", map[statKey]int32{{8, 1}: 12345, {0, 5}: 7})
	got := newStatsStore(path).load("wesley")
	if got[statKey{8, 1}] != 12345 || got[statKey{0, 5}] != 7 {
		t.Fatalf("round trip: %+v", got)
	}
}
