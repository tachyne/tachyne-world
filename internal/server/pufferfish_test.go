package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestPufferfishPuffsAndStings: a player within two blocks inflates it in
// two stages, leaving deflates it in two, and a puffed fish stings on touch.
func TestPufferfishPuffsAndStings(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	f := h.spawnMob(players, entityPufferfish, 0.5, 180, 0.5)
	pl.x, pl.y, pl.z = 4.5, 180, 0.5
	h.pufferStep(players, f)
	if f.puff != 0 {
		t.Fatal("four blocks off is not scary")
	}
	pl.x = 2.0
	h.pufferStep(players, f)
	if f.puff != 1 {
		t.Fatalf("a player within two blocks: puff %d, want 1", f.puff)
	}
	for i := 0; i < 21 && f.puff < 2; i++ {
		h.pufferStep(players, f)
	}
	if f.puff != 2 {
		t.Fatalf("after forty ticks: puff %d, want 2", f.puff)
	}
	// Touching it stings: 1 + state damage and poison for 3 s per state.
	pl.x, pl.health = 0.6, 20
	h.pufferStep(players, f)
	if pl.health != 17 || pl.hasEffect(effPoison) == 0 {
		t.Fatalf("sting: health %v poison %d", pl.health, pl.hasEffect(effPoison))
	}
	pl.health = 20
	h.pufferStep(players, f)
	if pl.health != 20 {
		t.Fatal("a second sting waits for the cooldown")
	}
	// Left alone it deflates: 1 after sixty ticks, 0 after a hundred more.
	pl.x = 20
	for i := 0; i < 35 && f.puff == 2; i++ {
		h.pufferStep(players, f)
	}
	if f.puff != 1 {
		t.Fatalf("after sixty ticks alone: puff %d, want 1", f.puff)
	}
	for i := 0; i < 25 && f.puff == 1; i++ {
		h.pufferStep(players, f)
	}
	if f.puff != 0 {
		t.Fatalf("after a hundred ticks alone: puff %d, want 0", f.puff)
	}
	// A creative player, or a cod, does not scare it.
	pl.x, pl.gamemode = 1.5, gmCreative
	h.pufferStep(players, f)
	cod := h.spawnMob(players, entityCod, 1.5, 180, 0.5)
	h.pufferStep(players, f)
	if f.puff != 0 {
		t.Fatalf("creative players and cod are not scary: puff %d", f.puff)
	}
	cod.dying = 1
}

// TestMobSpeedFactor: soul sand and honey under a walker slow it to 0.4×.
func TestMobSpeedFactor(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	w.SetBlock(0, 179, 0, worldgen.Stone)
	if f := h.mobSpeedFactor(z); f != 1 {
		t.Fatalf("stone: %v", f)
	}
	w.SetBlock(0, 179, 0, worldgen.SoulSand)
	if f := h.mobSpeedFactor(z); f != 0.4 {
		t.Fatalf("soul sand: %v", f)
	}
	w.SetBlock(0, 179, 0, worldgen.BlockBase("honey_block"))
	if f := h.mobSpeedFactor(z); f != 0.4 {
		t.Fatalf("honey: %v", f)
	}
}
