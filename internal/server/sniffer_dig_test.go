package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// snifferField lays a dirt floor high above the terrain for a sniffer to
// scent over.
func snifferField(h *hub) {
	h.world.ForceLoad(0, 0, 2)
	for x := -20; x <= 20; x++ {
		for z := -20; z <= 20; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("dirt"))
		}
	}
}

// A sniffer sent to a patch of dirt digs it: a seed pops out two seconds in;
// when the dig runs its course the cooldown starts and it gets up (RISING,
// forty ticks), then remembers the spot and is happy for 40-100 ticks.
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
	h.tick.Store(1000)
	s.sniffState, s.sniffTarget, s.sniffStart = sniffSearching, blockPos{3, 179, 3}, 1000
	if !h.snifferStep(players, s) || s.sniffState != sniffDigging {
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
	if s.sniffState != sniffRising || s.sniffCD != snifferSniffCD || s.sniffExploredHas(blockPos{3, 179, 3}) {
		t.Fatalf("the dig should end in RISING with the cooldown, the spot not yet stored: state=%d cd=%d", s.sniffState, s.sniffCD)
	}
	if !h.snifferStep(players, s) || s.vx != 0 {
		t.Fatal("getting up, the sniffer holds still")
	}
	h.tick.Store(h.tick.Load() + snifferRiseTicks)
	h.snifferStep(players, s)
	if s.sniffState != sniffHappy || !s.sniffExploredHas(blockPos{3, 179, 3}) {
		t.Fatalf("up again, it remembers the spot and is happy: state=%d", s.sniffState)
	}
	if d := s.sniffUntil - h.tick.Load(); d < 40 || d > 100 {
		t.Fatalf("FeelingHappy runs 40-100 ticks: %d", d)
	}
	h.tick.Store(s.sniffUntil)
	h.snifferStep(players, s)
	if s.sniffState != sniffIdling {
		t.Fatal("the happiness should wear off")
	}
}

// Idle, a sniffer scents and sniffs now and then; a sniff that runs its
// course sets it searching toward diggable ground. On its cooldown, or as a
// baby, it only scents.
func TestSnifferIdleScentingAndSniffing(t *testing.T) {
	h := newHub(world.New(1))
	snifferField(h)
	players := map[int32]*tracked{}
	s := h.spawnSpecies(players, entitySniffer, 0, 0.5, 180, 0.5)
	seen := map[int8]bool{}
	for i := 0; i < 5000 && !(seen[sniffScenting] && seen[sniffSniffing]); i++ {
		s.sniffState = sniffIdling
		if h.snifferStep(players, s) {
			seen[s.sniffState] = true
			if d := s.sniffUntil - h.tick.Load(); d < 40 || d > 80 {
				t.Fatalf("scenting and sniffing run 40-80 ticks: %d", d)
			}
		}
	}
	if !seen[sniffScenting] || !seen[sniffSniffing] {
		t.Fatalf("an idle sniffer should both scent and sniff: %v", seen)
	}
	s.sniffState, s.sniffUntil = sniffSniffing, h.tick.Load()
	if !h.snifferStep(players, s) || s.sniffState != sniffSearching {
		t.Fatalf("a finished sniff finds a scent: state=%d", s.sniffState)
	}
	if st := h.world.At(s.sniffTarget.x, s.sniffTarget.y, s.sniffTarget.z); !inRanges(snifferDiggable, st) {
		t.Fatal("the scent is not diggable ground")
	}
	for _, c := range []struct {
		baby bool
		cd   int
	}{{false, snifferSniffCD}, {true, 0}} {
		s.baby, s.sniffCD = c.baby, c.cd
		for i := 0; i < 3000; i++ {
			s.sniffState = sniffIdling
			if h.snifferStep(players, s) && s.sniffState == sniffSniffing {
				t.Fatalf("baby=%v cd=%d: sniffed", c.baby, c.cd)
			}
		}
	}
}

// resetSniffing through the real update path: a panic mid-dig stands the
// sniffer up and ends the dig, with no cooldown and nothing remembered.
func TestSnifferPanicResetsDig(t *testing.T) {
	h := newHub(world.New(1))
	snifferField(h)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 15.5, 180, 15.5
	s := h.spawnSpecies(players, entitySniffer, 0, 0.5, 180, 0.5)
	h.tick.Store(1000)
	s.sniffState, s.sniffTarget, s.sniffStart, s.sniffUntil = sniffDigging, blockPos{0, 179, 0}, 1000, 1170
	h.updateMobs(players)
	if s.sniffState != sniffDigging {
		t.Fatalf("an undisturbed dig carries on: state=%d", s.sniffState)
	}
	h.mobStruck(players, s, pl, dtPlayerAttack)
	if s.panic == 0 {
		t.Fatal("a struck sniffer panics")
	}
	h.updateMobs(players)
	if s.sniffState != sniffIdling || s.sniffCD != 0 || s.sniffExploredHas(blockPos{0, 179, 0}) {
		t.Fatalf("the panic should break off the dig: state=%d cd=%d", s.sniffState, s.sniffCD)
	}
}

// Sniffers breed into an egg, not a snifflet, and never while one of the
// pair is busy with a dig (Sniffer.canMate).
func TestSnifferBreedsAnEgg(t *testing.T) {
	h := newHub(world.New(1))
	snifferField(h)
	players := map[int32]*tracked{}
	a := h.spawnSpecies(players, entitySniffer, 0, 0.5, 180, 0.5)
	b := h.spawnSpecies(players, entitySniffer, 0, 2.5, 180, 0.5)
	a.loveTicks, b.loveTicks = loveTicks, loveTicks
	b.sniffState = sniffDigging
	for i := 0; i < 5; i++ {
		h.updateBreeding(players)
	}
	if a.breedCD != 0 {
		t.Fatal("bred with a sniffer that was digging")
	}
	b.sniffState = sniffHappy
	mobs := len(h.mobs)
	for i := 0; i < 5 && a.breedCD == 0; i++ {
		h.updateBreeding(players)
	}
	if a.breedCD == 0 {
		t.Fatal("a happy sniffer should mate")
	}
	egg := false
	for _, it := range h.items {
		if it.item == itemByName["sniffer_egg"] {
			egg = true
		}
	}
	if !egg || len(h.mobs) != mobs {
		t.Fatalf("the pair should lay an egg, not a snifflet: egg=%v mobs %d -> %d", egg, mobs, len(h.mobs))
	}
}
