package server

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// respawnHub is a hub with a real Nether and a spawn store to claim into.
func respawnHub(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	h.spawns = newSpawnStore(t.TempDir() + "/spawns.json")
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.dim = dimNether
	players[pl.p.eid] = pl
	h.playersRef = players
	return h, players, pl
}

// A bed in the Nether is a bomb, not a respawn point.
func TestBedExplodesOutsideTheOverworld(t *testing.T) {
	h, players, pl := respawnHub(t)
	nw := h.nether
	head := blockPos{4, 70, 3} // the foot faces north, so the head is one north
	nw.SetBlock(4, 70, 4, tWhiteBed)
	nw.SetBlock(head.x, head.y, head.z, worldgen.SetProperty(
		mustInfo(t, tWhiteBed), tWhiteBed, "part", "head"))
	pl.x, pl.y, pl.z = 4.5, 70, 4.5
	pl.health = 20

	h.handleUseBed(players, pl, blockPos{4, 70, 4})

	if _, _, ok := h.spawns.get(pl.p.name); ok {
		t.Error("a Nether bed silently claimed a respawn point")
	}
	if nw.At(4, 70, 4) != worldgen.Air || nw.At(head.x, head.y, head.z) != worldgen.Air {
		t.Errorf("both halves of the bed should be gone: foot=%d head=%d",
			nw.At(4, 70, 4), nw.At(head.x, head.y, head.z))
	}
	if pl.health >= 20 {
		t.Errorf("health %v — standing on an exploding bed should hurt", pl.health)
	}
	if pl.lastCause.key != causeBadRespawn {
		t.Errorf("death cause %q, want %q", pl.lastCause.key, causeBadRespawn)
	}
}

// mustInfo is the block-info lookup with the test's error handling.
func mustInfo(t *testing.T, state uint32) worldgen.BlockInfo {
	t.Helper()
	info, ok := worldgen.InfoForState(state)
	if !ok {
		t.Fatalf("no block info for state %d", state)
	}
	return info
}

// A respawn point recorded in one dimension must not be honoured from another:
// before the spawn store carried a dimension, a Nether bed's coordinates were
// validated against whatever stood at the same spot in the overworld.
func TestRespawnPointIsDimensionScoped(t *testing.T) {
	h, players, pl := respawnHub(t)
	// A bed stands in the Nether at these coordinates…
	h.nether.SetBlock(9, 70, 9, tWhiteBed)
	h.spawns.set(pl.p.name, blockPos{9, 70, 9}, dimNether)

	_, _, _, dim := h.respawnPoint(players, pl)
	if dim != dimOverworld {
		t.Errorf("respawned into dim %d — a Nether bed is not a respawn point", dim)
	}
}

func TestRespawnAnchorChargesAndClaims(t *testing.T) {
	h, players, pl := respawnHub(t)
	pos := blockPos{2, 70, 2}
	h.nether.SetBlock(pos.x, pos.y, pos.z, anchorWithCharge(anchorMin, 0))
	pl.x, pl.y, pl.z = 2.5, 70, 2.5
	pl.gamemode = gmSurvival
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemGlowstoneBlock, count: 2}

	// An empty anchor does nothing without fuel.
	h.handleUseAnchor(players, pl, pos)
	if got := anchorCharge(h.nether.At(pos.x, pos.y, pos.z)); got != 1 {
		t.Fatalf("charge %d after one glowstone, want 1", got)
	}
	if got := pl.inv.slots[pl.p.heldSlot()].count; got != 1 {
		t.Errorf("%d glowstone left, want 1 spent", got)
	}

	// Empty-handed on a charged anchor: claim it.
	pl.inv.slots[pl.p.heldSlot()] = invStack{}
	h.handleUseAnchor(players, pl, pos)
	gotPos, gotDim, ok := h.spawns.get(pl.p.name)
	if !ok || gotPos != pos || gotDim != dimNether {
		t.Fatalf("respawn point %v dim %d ok %v, want the anchor in the Nether", gotPos, gotDim, ok)
	}

	// Respawning there spends a charge and keeps the player in the Nether.
	_, _, _, dim := h.respawnPoint(players, pl)
	if dim != dimNether {
		t.Errorf("respawned into dim %d, want the Nether", dim)
	}
	if got := anchorCharge(h.nether.At(pos.x, pos.y, pos.z)); got != 0 {
		t.Errorf("charge %d after a respawn, want the charge spent", got)
	}
}

// The same anchor in the overworld detonates instead of claiming.
func TestRespawnAnchorExplodesInTheOverworld(t *testing.T) {
	h, players, pl := respawnHub(t)
	pl.dim = dimOverworld
	pos := blockPos{2, 70, 2}
	h.world.SetBlock(pos.x, pos.y, pos.z, anchorWithCharge(anchorMin, 3))
	pl.x, pl.y, pl.z = 2.5, 70, 2.5
	pl.health = 20

	h.handleUseAnchor(players, pl, pos)

	if _, _, ok := h.spawns.get(pl.p.name); ok {
		t.Error("an overworld anchor claimed a respawn point")
	}
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Error("the anchor should have been consumed by its own blast")
	}
	if pl.health >= 20 {
		t.Error("standing on an exploding anchor should hurt")
	}
}

// Spawn files written before the dimension column read back as the overworld,
// which is where every bed they recorded actually stood.
func TestSpawnStoreReadsPreDimensionFiles(t *testing.T) {
	path := t.TempDir() + "/spawns.json"
	old, _ := json.Marshal(map[string][3]int{"tester": {10, 64, -20}})
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}
	pos, dim, ok := newSpawnStore(path).get("tester")
	if !ok || pos != (blockPos{10, 64, -20}) || dim != dimOverworld {
		t.Fatalf("got %v dim %d ok %v, want the old point in the overworld", pos, dim, ok)
	}
}
