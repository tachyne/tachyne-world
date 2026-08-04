package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The 32 states are facing(4) x ominous(true,false) x vault_state(4), pinned
// against the vanilla state report. A vault must keep its facing when its pose
// changes, or opening one would spin it round.
func TestVaultBlockStates(t *testing.T) {
	north := vaultMin // facing=north, ominous=true, inactive
	if got := vaultBlock(north, true, vaultActive); got != north+1 {
		t.Errorf("ominous active -> %d, want %d", got, north+1)
	}
	if got := vaultBlock(north, false, vaultInactive); got != north+4 {
		t.Errorf("plain inactive -> %d, want %d", got, north+4)
	}
	if got := vaultBlock(north, false, vaultEjecting); got != north+7 {
		t.Errorf("plain ejecting -> %d, want %d", got, north+7)
	}
	// A vault facing south (the second facing) keeps facing south.
	south := vaultMin + 8
	if got := vaultBlock(south, false, vaultActive); got != south+5 {
		t.Errorf("south plain active -> %d, want %d (facing must be kept)", got, south+5)
	}
	if vaultMax != vaultMin+31 {
		t.Errorf("vault range %d..%d, want 32 states", vaultMin, vaultMax)
	}
}

// Which kind of vault a block is decides which key it takes.
func TestVaultOminousDetection(t *testing.T) {
	if !vaultOminousAt(vaultMin) {
		t.Error("the first vault state should be the ominous one")
	}
	if vaultOminousAt(vaultMin + 4) {
		t.Error("state +4 is the plain vault")
	}
	if vaultKeyFor(true) == vaultKeyFor(false) {
		t.Error("both vault kinds take the same key")
	}
}

// A vault pays each player exactly once, ever — that is the whole design, and
// it is keyed on the player's UUID so it survives a relog.
func TestVaultPaysEachPlayerOnce(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pos := blockPos{0, 180, 0}
	pl.x, pl.y, pl.z = float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5
	players[pl.p.eid] = pl

	w := h.worldFor(0)
	w.SetBlock(pos.x, pos.y, pos.z, vaultBlock(vaultMin, false, vaultInactive))

	v := h.vaultAt(pos, false)
	v.state = vaultActive
	key := vaultKeyFor(false)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: key, count: 2}

	h.useVault(players, pl, pos)
	if v.state != vaultUnlocking {
		t.Fatalf("vault state %d after a valid key, want unlocking", v.state)
	}
	if got := pl.inv.slots[pl.p.heldSlot()].count; got != 1 {
		t.Errorf("%d keys left, want 1 spent", got)
	}
	if !v.hasRewarded(pl.p.uuid) {
		t.Error("the player was not recorded as rewarded")
	}

	// Let it pay out.
	before := len(h.items)
	for i := 0; i < 200 && v.state != vaultInactive; i++ {
		h.tick.Add(1)
		h.tickVault(players, v, w.At(pos.x, pos.y, pos.z), h.tick.Load())
	}
	if len(h.items) <= before {
		t.Error("the vault ejected nothing")
	}

	// Second attempt with the remaining key: refused.
	v.state = vaultActive
	keysBefore := pl.inv.slots[pl.p.heldSlot()].count
	h.useVault(players, pl, pos)
	if v.state != vaultActive {
		t.Errorf("vault state %d on a repeat claim, want it to stay active", v.state)
	}
	if got := pl.inv.slots[pl.p.heldSlot()].count; got != keysBefore {
		t.Errorf("a repeat claim spent a key: %d -> %d", keysBefore, got)
	}
}

// The wrong key (or no key) does not open it.
func TestVaultRefusesTheWrongKey(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pos := blockPos{0, 180, 0}
	players[pl.p.eid] = pl
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, vaultBlock(vaultMin, false, vaultInactive))

	v := h.vaultAt(pos, false)
	v.state = vaultActive
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: vaultKeyFor(true), count: 1} // ominous key
	h.useVault(players, pl, pos)
	if v.state != vaultActive {
		t.Error("an ominous key opened a plain vault")
	}
	if v.hasRewarded(pl.p.uuid) {
		t.Error("a refused claim still recorded the player")
	}
}
