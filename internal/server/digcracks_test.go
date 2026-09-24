package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Break times from getDestroyProgress, in ticks, standing on the ground.
func TestDestroyProgressMatchesVanilla(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.onGround = 0.5, 200, 0.5, true
	ticks := func(item string, block string) int {
		pl.inv.slots[pl.p.held] = invStack{}
		if item != "" {
			pl.inv.slots[pl.p.held] = invStack{item: itemByName[item], count: 1}
		}
		return int(math.Ceil(1 / h.destroyProgress(pl, worldgen.BlockBase(block))))
	}
	for _, tc := range []struct {
		item, block string
		want        int
	}{
		{"wooden_pickaxe", "stone", 23}, // 2 / 1.5 / 30
		{"", "stone", 150},              // hand, no harvest: 1 / 1.5 / 100
		{"iron_shovel", "dirt", 3},      // 6 / 0.5 / 30
		{"diamond_sword", "cobweb", 8},  // 15 / 4 / 30
		{"iron_pickaxe", "oak_log", 60}, // a pickaxe is no faster on wood: 1 / 2 / 30
	} {
		if got := ticks(tc.item, tc.block); got != tc.want {
			t.Errorf("%s on %s: %d ticks, want %d", tc.item, tc.block, got, tc.want)
		}
	}
	pl.onGround = false // airborne: a fifth of the speed
	if got := ticks("wooden_pickaxe", "stone"); got != 113 {
		t.Errorf("airborne wooden pickaxe on stone: %d ticks, want 113", got)
	}
}

// Another player sees the cracks advance, and they clear on abort.
func TestOthersSeeTheCracks(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	digger := survPlayer(h)
	watcher := &tracked{p: newPlayer(2, "watcher", [16]byte{}), gamemode: gmSurvival} // its own eid
	initSurvival(watcher)
	players := map[int32]*tracked{digger.p.eid: digger, watcher.p.eid: watcher}
	digger.x, digger.y, digger.z, digger.onGround = 0.5, 200, 0.5, true
	watcher.x, watcher.y, watcher.z = 3.5, 200, 0.5
	watcher.tracked = map[int32]bool{digger.p.eid: true}
	h.world.SetBlock(1, 200, 0, worldgen.BlockBase("stone"))
	digger.inv.slots[digger.p.held] = invStack{item: itemByName["wooden_pickaxe"], count: 1}
	drainEvs(watcher.p)
	h.startDig(players, evDigStart{eid: digger.p.eid, x: 1, y: 200, z: 0})
	var stages []int8
	collect := func() {
		for _, ev := range drainEvs(watcher.p) {
			if b, ok := ev.(attachproto.BlockBreakProgress); ok && b.EID == digger.p.eid {
				stages = append(stages, b.Progress)
			}
		}
	}
	for i := 0; i < 12; i++ {
		h.tickDigCracks(players)
	}
	collect()
	if len(stages) < 3 || stages[0] != 0 || stages[len(stages)-1] < 4 {
		t.Fatalf("the watcher saw stages %v over 12 ticks, want 0 rising to about 5", stages)
	}
	h.stopDig(players, digger.p.eid)
	collect()
	if stages[len(stages)-1] != -1 {
		t.Fatalf("an abort did not clear the cracks: %v", stages)
	}
}
