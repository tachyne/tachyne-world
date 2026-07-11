package server

// Statistics: per-player vanilla stat counters (the F3-menu-free half of
// progression). Counters accumulate at gameplay sites via incStat/incCustom;
// the client's Statistics screen is request-driven — client_command action 1
// arrives as attach.StatsReq and the hub replies with the full snapshot
// (attach.Stats, canonical 774 key ids; the gateways translate per version).
// Persistence mirrors the advancement store: stats.json, keyed by player
// name, entries "type:key" → value.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// blockStatRange maps a block-state range to its block-REGISTRY id (the
// mined stat's key space). Table generated in stats_gen.go.
type blockStatRange struct {
	lo, hi uint32
	reg    int32
}

// statBlockReg resolves a block state to its block registry id.
func statBlockReg(state uint32) (int32, bool) {
	lo, hi := 0, len(blockStatRanges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		r := blockStatRanges[mid]
		switch {
		case state < r.lo:
			hi = mid - 1
		case state > r.hi:
			lo = mid + 1
		default:
			return r.reg, true
		}
	}
	return 0, false
}

type statKey struct{ T, K int32 }

// incStat bumps one counter. Safe on players without stats (probes, resume
// edge) — the map is created lazily.
func (h *hub) incStat(t *tracked, typ, key, n int32) {
	if t == nil || n == 0 {
		return
	}
	if t.stats == nil {
		t.stats = map[statKey]int32{}
	}
	t.stats[statKey{typ, key}] += n
}

// incCustom bumps a custom stat by name ("deaths", "play_time", …).
func (h *hub) incCustom(t *tracked, name string, n int32) {
	if id, ok := customStatID[name]; ok {
		h.incStat(t, attachproto.StatCustom, id, n)
	}
}

// statsSnapshot renders the player's counters as the attach frame, sorted
// for deterministic output.
func statsSnapshot(t *tracked) attachproto.Stats {
	s := attachproto.Stats{Entries: make([]attachproto.StatEntry, 0, len(t.stats))}
	for k, v := range t.stats {
		s.Entries = append(s.Entries, attachproto.StatEntry{T: k.T, K: k.K, V: v})
	}
	sort.Slice(s.Entries, func(i, j int) bool {
		a, b := s.Entries[i], s.Entries[j]
		if a.T != b.T {
			return a.T < b.T
		}
		return a.K < b.K
	})
	return s
}

// statsStore persists counters by player name (stats.json) — same
// shape/cadence as advStore.
type statsStore struct {
	mu   sync.Mutex
	path string
	m    map[string]map[string]int32 // name → "type:key" → value
}

func newStatsStore(path string) *statsStore {
	s := &statsStore{path: path, m: map[string]map[string]int32{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

func (s *statsStore) load(name string) map[statKey]int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[statKey]int32{}
	for k, v := range s.m[name] {
		t, key, ok := strings.Cut(k, ":")
		if !ok {
			continue
		}
		ti, e1 := strconv.Atoi(t)
		ki, e2 := strconv.Atoi(key)
		if e1 != nil || e2 != nil {
			continue
		}
		out[statKey{int32(ti), int32(ki)}] = v
	}
	return out
}

func (s *statsStore) record(name string, st map[statKey]int32) {
	if st == nil {
		return
	}
	snap := make(map[string]int32, len(st))
	for k, v := range st {
		snap[fmt.Sprintf("%d:%d", k.T, k.K)] = v
	}
	s.mu.Lock()
	s.m[name] = snap
	s.mu.Unlock()
}

func (s *statsStore) flush() {
	s.mu.Lock()
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, path)
	}
}

func (s *statsStore) save(name string, st map[statKey]int32) {
	s.record(name, st)
	s.flush()
}

// evStatsReq: the client opened its Statistics screen.
type evStatsReq struct{ eid int32 }

func (evStatsReq) isHubEvent() {}
