package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A sniffer sent to a patch of dirt digs it: a seed pops out two seconds in,
// the dig ends with the spot remembered and an eight-minute cooldown.
func TestSnifferDigsForSeeds(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.rules.DoMobLoot = true
	w := h.worldFor(0)
	w.SetBlock(3, 179, 3, worldgen.BlockBase("dirt"))
	w.SetBlock(3, 180, 3, worldgen.Air)
	s := h.spawnSpecies(players, entitySniffer, 0, 3.5, 180, 3.5)
	if s == nil {
		t.Fatal("no sniffer")
	}
	s.x, s.y, s.z = 3.5, 180, 3.5
	s.sniffState, s.sniffTarget = 1, blockPos{3, 179, 3}
	h.tick.Store(1000)
	if !h.snifferStep(players, s) || s.sniffState != 2 {
		t.Fatalf("beside its scent the sniffer should start digging: state=%d", s.sniffState)
	}
	items := len(h.items)
	h.tick.Store(1000 + snifferSeedAt)
	h.snifferStep(players, s)
	if len(h.items) != items+1 {
		t.Fatal("two seconds in, a seed should drop")
	}
	h.tick.Store(s.sniffUntil)
	h.snifferStep(players, s)
	if s.sniffState != 0 || s.sniffCD != snifferSniffCD || !s.sniffExploredHas(blockPos{3, 179, 3}) {
		t.Fatalf("the dig should end with a cooldown and the spot remembered: state=%d cd=%d", s.sniffState, s.sniffCD)
	}
	if h.snifferStep(players, s) {
		t.Fatal("on cooldown the sniffer does nothing")
	}
}
