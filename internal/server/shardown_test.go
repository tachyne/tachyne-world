package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-common/shard"
	"github.com/tachyne/tachyne-world/internal/world"
)

func TestHubOwnership(t *testing.T) {
	// Unsharded hub (owned == nil) owns everything — the single-pod default.
	h := newHub(world.New(1))
	if !h.ownedChunk(1000, -1000) || !h.ownedBlock(5, 5) || !h.ownedAt(1e6, -1e6) {
		t.Fatal("unsharded hub must own everything")
	}

	// Sharded as SID 0 = the west tile, chunks [-16,0) x [-8,8).
	topo := shard.Map{Version: 1, Regions: []shard.Region{
		{SID: 0, MinCX: -16, MinCZ: -8, W: 16, H: 16},
		{SID: 1, MinCX: 0, MinCZ: -8, W: 16, H: 16},
	}}
	h.shardOf = func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }

	if !h.ownedChunk(-1, 0) {
		t.Error("SID 0 should own chunk (-1,0)")
	}
	if h.ownedChunk(0, 0) {
		t.Error("SID 0 should NOT own chunk (0,0) — that's SID 1")
	}
	if h.ownedChunk(100, 100) {
		t.Error("SID 0 should NOT own an unowned (void) chunk")
	}
	// block -1 -> chunk -1 (owned); block 0 -> chunk 0 (not owned)
	if !h.ownedBlock(-1, 0) || h.ownedBlock(0, 0) {
		t.Error("ownedBlock seam boundary wrong")
	}
	// block -8 -> chunk -1 (owned); block 8 -> chunk 0 (not owned)
	if !h.ownedAt(-8, 0) || h.ownedAt(8, 0) {
		t.Error("ownedAt seam boundary wrong")
	}

	// serveChunk (overlap): a shard STREAMS its own chunks AND the neighbour's
	// (deterministic terrain → continuous seam), but never true void.
	if !h.serveChunk(-1, 0) {
		t.Error("SID 0 must serve its own chunk (-1,0)")
	}
	if !h.serveChunk(0, 0) {
		t.Error("SID 0 must serve the neighbour's border chunk (0,0) — the overlap")
	}
	if h.serveChunk(100, 100) {
		t.Error("no shard serves a void chunk — that is where the world ends")
	}
	if !h.serveBlock(8, 0) || h.serveBlock(100*16, 0) {
		t.Error("serveBlock seam/void wrong")
	}
}

func TestEIDLanes(t *testing.T) {
	// Unsharded: plain sequential counters (legacy behavior for existing tests).
	h := newHub(world.New(1))
	if e := h.allocEID(); e != 1 {
		t.Errorf("unsharded allocEID=%d want 1", e)
	}
	if e := h.mintPlayerEID(); e != 2 {
		t.Errorf("unsharded mintPlayerEID=%d want 2", e)
	}

	// Sharded as SID 3: mob/entity eids in lane 3, player eids in the player lane,
	// and the shared counter guarantees they never collide.
	h2 := newHub(world.New(1))
	h2.sid = 3
	h2.shardOf = func(cx, cz int32) int32 { return 3 } // sharded (non-nil) as sid 3
	mob := h2.allocEID()
	if shard.Minter(mob) != 3 {
		t.Errorf("mob eid %d is in lane %d, want 3", mob, shard.Minter(mob))
	}
	pl := h2.mintPlayerEID()
	if shard.Minter(pl) != shard.PlayerSID {
		t.Errorf("player eid %d is in lane %d, want PlayerSID(%d)", pl, shard.Minter(pl), shard.PlayerSID)
	}
	if mob == pl {
		t.Error("mob and player eids collided")
	}
}

func TestLoadTopology(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"version":1,"regions":[`+
		`{"sid":0,"min_cx":-16,"min_cz":-8,"w":16,"h":16},`+
		`{"sid":1,"min_cx":0,"min_cz":-8,"w":16,"h":16}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadTopology(good)
	if err != nil {
		t.Fatalf("valid topology rejected: %v", err)
	}
	if len(m.Regions) != 2 || m.ShardOf(0, -1, 0) != 0 || m.ShardOf(0, 0, 0) != 1 {
		t.Errorf("loaded map wrong: %+v", m)
	}

	// Overlapping regions must be rejected by Validate.
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"version":1,"regions":[`+
		`{"sid":0,"min_cx":0,"min_cz":0,"w":16,"h":16},`+
		`{"sid":1,"min_cx":8,"min_cz":8,"w":16,"h":16}]}`), 0o644)
	if _, err := LoadTopology(bad); err == nil {
		t.Error("overlapping topology should be rejected")
	}
	if _, err := LoadTopology(filepath.Join(dir, "nope.json")); err == nil {
		t.Error("missing topology file should error")
	}
}
