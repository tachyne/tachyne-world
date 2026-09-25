package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// pickupCases is one stack per stored field: each must come back from the
// ground exactly as it went down. The first four fields stood in for all of
// them until 2026-09-24, and everything else was dropped on pickup.
func pickupCases() map[string]invStack {
	id := func(name string) int32 { return itemByName[name] }
	return map[string]invStack{
		"potion":     {item: id("potion"), count: 1, potion: 5},
		"shulker":    {item: id("shulker_box"), count: 1, boxID: 7},
		"bundle":     {item: id("bundle"), count: 1, bundleID: 3},
		"hive":       {item: id("beehive"), count: 1, hiveID: 2},
		"dyed":       {item: id("leather_helmet"), count: 1, color: 0x3355aa},
		"stew":       {item: id("suspicious_stew"), count: 1, stew: 4},
		"horn":       {item: id("goat_horn"), count: 1, instrument: 3},
		"rocket":     {item: id("firework_rocket"), count: 16, flight: 3, starID: 9},
		"pot":        {item: id("decorated_pot"), count: 1, sherds: potSherds{id("angler_pottery_sherd"), 0, 0, 0}},
		"shield":     {item: id("shield"), count: 1, shieldBase: 4},
		"repairCost": {item: id("stone"), count: 12, repairCost: 3},
	}
}

func TestPickupKeepsEveryStoredField(t *testing.T) {
	for name, st := range pickupCases() {
		t.Run(name, func(t *testing.T) {
			if st.item == 0 {
				t.Fatalf("unknown item in case %s", name)
			}
			h := newHub(world.New(1))
			pl := testTracked()
			players := map[int32]*tracked{1: pl}
			pl.x, pl.y, pl.z = 0.5, h.world.SurfaceY(0, 0), 0.5
			it := h.spawnItemAt(players, 0, st.item, st.count, pl.x, pl.y, pl.z, 0, 0, 0)
			it.setFrom(st)
			h.tick.Add(pickupDelay + 1)
			h.pickupItems(players)
			if got := pl.inv.slots[0]; got != st {
				t.Fatalf("picked up %+v, want %+v", got, st)
			}
		})
	}
}

func TestStacksMergeOnlyWithTheSameData(t *testing.T) {
	for name, st := range pickupCases() {
		t.Run(name, func(t *testing.T) {
			plain := invStack{item: st.item, count: 1}
			var inv inventory
			inv.slots[0] = plain
			one := st
			one.count = 1
			inv.addStack(one)
			if inv.slots[0] != plain {
				t.Fatalf("a stack with data merged into a plain one: %+v", inv.slots[0])
			}
			if inv.slots[1] != one {
				t.Fatalf("second slot %+v, want %+v", inv.slots[1], one)
			}
			if stackCap(st.item) > 1 { // the same data merges
				inv.addStack(one)
				if inv.slots[1].count != 2 || inv.slots[2].count != 0 {
					t.Fatalf("identical stacks did not merge: %+v %+v", inv.slots[1], inv.slots[2])
				}
			}
		})
	}
}

func TestGroundItemsMergeOnlyWithTheSameData(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	rocket := itemByName["firework_rocket"]
	y := h.world.SurfaceY(0, 0)
	a := h.spawnItemAt(players, 0, rocket, 4, 0.5, y, 0.5, 0, 0, 0)
	a.flight = 1
	b := h.spawnItemAt(players, 0, rocket, 4, 0.6, y, 0.5, 0, 0, 0)
	b.flight = 3
	h.updateItems(players)
	if len(h.items) != 2 {
		t.Fatalf("rockets of different flight merged on the ground: %d left", len(h.items))
	}
	b.flight = 1
	h.updateItems(players)
	if len(h.items) != 1 {
		t.Fatalf("identical rockets did not merge on the ground: %d left", len(h.items))
	}
}

// A creative player picks items up too (ItemEntity.playerTouch); a
// spectator touches nothing.
func TestCreativePicksUpSpectatorDoesNot(t *testing.T) {
	for _, tc := range []struct {
		mode int
		want bool
	}{{gmCreative, true}, {gmSpectator, false}} {
		h := newHub(world.New(1))
		pl := testTracked()
		pl.gamemode = tc.mode
		players := map[int32]*tracked{1: pl}
		pl.x, pl.y, pl.z = 0.5, h.world.SurfaceY(0, 0), 0.5
		h.spawnItemAt(players, 0, itemByName["stone"], 3, pl.x, pl.y, pl.z, 0, 0, 0)
		h.tick.Add(pickupDelay + 1)
		h.pickupItems(players)
		if got := pl.inv.slots[0].count == 3; got != tc.want {
			t.Fatalf("mode %d picked up %v, want %v", tc.mode, got, tc.want)
		}
	}
}
