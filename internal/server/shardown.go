package server

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync/atomic"

	"github.com/tachyne/tachyne-common/shard"
)

// debugBorderRange is how far (in blocks, each way) around a player we scan for
// region seams to draw. Kept within the view radius so the wall is always in
// sight when near an edge.
const debugBorderRange = 20

// emitDebugBorders draws a wall of crit particles along this pod's region seams
// near each player — a build-time cue for where the world ends. A seam is a
// cell edge where ownership flips (owned↔unowned, or owned-by-us↔owned-by-them).
// Non-blocking and dev-only (-debug-borders). Payload-free crit particles are
// used because they are the only version-safe coloured-ish set today; a true red
// frame would need the glow-entity + Teams-frame route.
func (h *hub) emitDebugBorders(players map[int32]*tracked) {
	if !h.debugBorders || h.shardOf == nil {
		return
	}
	for _, t := range players {
		px, pz := int(math.Floor(t.x)), int(math.Floor(t.z))
		for x := px - debugBorderRange; x <= px+debugBorderRange; x++ {
			for z := pz - debugBorderRange; z <= pz+debugBorderRange; z++ {
				// Draw the wall only where SERVED terrain meets true VOID — the real
				// edge of the world. Shard-vs-shard seams are now seamless (overlap
				// streaming), so a wall there would just bisect continuous land.
				if h.serveBlock(x-1, z) != h.serveBlock(x, z) { // vertical (N-S) edge at world x
					for dy := 0; dy <= 4; dy++ {
						h.spawnParticles(players, particleCrit, float64(x), t.y+float64(dy), float64(z)+0.5, 0, 0, 1)
					}
				}
				if h.serveBlock(x, z-1) != h.serveBlock(x, z) { // horizontal (E-W) edge at world z
					for dy := 0; dy <= 4; dy++ {
						h.spawnParticles(players, particleCrit, float64(x)+0.5, t.y+float64(dy), float64(z), 0, 0, 1)
					}
				}
			}
		}
	}
}

// allocEID mints a unique entity id for a mob/item/arrow/etc. When sharded it is
// interleaved with this pod's shard lane so eids never collide across pods
// (shard.MintEID); unsharded it is a plain counter (unchanged legacy behavior,
// so existing single-pod tests keep their small sequential eids). Safe to call
// from any goroutine.
func (h *hub) allocEID() int32 {
	n := atomic.AddInt64(&h.eidCounter, 1)
	if h.shardOf == nil {
		return int32(n)
	}
	return shard.MintEID(n, h.sid)
}

// mintPlayerEID mints a player's entity id in the reserved PLAYER lane so it is
// session-stable across handovers and never collides with a shard's mob lane.
// Shares the counter with allocEID, so no two mints ever reuse a counter.
func (h *hub) mintPlayerEID() int32 {
	n := atomic.AddInt64(&h.eidCounter, 1)
	if h.shardOf == nil {
		return int32(n)
	}
	return shard.MintEID(n, shard.PlayerSID)
}

// Shard ownership: a pod owns a contiguous region of chunks; outside it the
// world does not exist for this pod. Ownership is dim-agnostic in v1 (regions
// apply to every dimension). A hub with shardOf == nil is unsharded and owns
// everything, so every existing single-pod path and test is unaffected.

// regionCenter returns the block coords of the centre of this shard's own region
// (the first region it owns) — a safe in-region point for a death respawn.
func (h *hub) regionCenter() (bx, bz int) {
	for _, r := range h.topo.Regions {
		if r.SID == h.sid {
			return int(r.MinCX+r.W/2)*16 + 8, int(r.MinCZ+r.H/2)*16 + 8
		}
	}
	return 0, 0
}

// ownedChunk reports whether this pod owns the chunk (true when unsharded).
func (h *hub) ownedChunk(cx, cz int32) bool {
	if h.shardOf == nil {
		return true
	}
	return h.shardOf(cx, cz) == h.sid
}

// shardAt returns the SID owning a float world position (this pod's sid when
// unsharded), for resolving which neighbour a crossing player is handed to.
func (h *hub) shardAt(x, z float64) int32 {
	if h.shardOf == nil {
		return h.sid
	}
	return h.shardOf(int32(chunkFloor(x)), int32(chunkFloor(z)))
}

// ownedBlock is ownedChunk for a block position (arithmetic shift = floor).
func (h *hub) ownedBlock(bx, bz int) bool {
	return h.ownedChunk(int32(bx>>4), int32(bz>>4))
}

// serveChunk reports whether this pod will STREAM a chunk to a client — a
// superset of ownedChunk: it serves any chunk SOME shard owns, not just its own.
// Because worldgen is deterministic (same seed+generator on every pod), a shard
// can generate a NEIGHBOUR's border chunks itself and stream them, so terrain is
// continuous across a seam and the client has the far side loaded BEFORE it
// crosses (the generated chunks also warm the shared cache the neighbour reads).
// Only true VOID (Unowned) stays unsent — that is where the world really ends.
func (h *hub) serveChunk(cx, cz int32) bool {
	if h.shardOf == nil {
		return true
	}
	return h.shardOf(cx, cz) != shard.Unowned
}

// serveBlock is serveChunk for a block position.
func (h *hub) serveBlock(bx, bz int) bool {
	return h.serveChunk(int32(bx>>4), int32(bz>>4))
}

// ownedAt is ownedChunk for a float world position.
func (h *hub) ownedAt(x, z float64) bool {
	return h.ownedChunk(int32(chunkFloor(x)), int32(chunkFloor(z)))
}

// LoadTopology reads and validates a shard region-map JSON file (mounted from
// the topology ConfigMap). Pods must refuse to start on an invalid partition.
func LoadTopology(path string) (shard.Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return shard.Map{}, fmt.Errorf("read topology %s: %w", path, err)
	}
	var m shard.Map
	if err := json.Unmarshal(data, &m); err != nil {
		return shard.Map{}, fmt.Errorf("parse topology %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return shard.Map{}, err
	}
	return m, nil
}
