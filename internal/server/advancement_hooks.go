package server

// Hub wiring for the advancement tracker (advtracker.go): the advance() entry
// point gameplay code calls at trigger sites, the join/leave/persistence glue,
// and the advStore (advancements.json, mirroring invStore). All hub-goroutine
// only, except the store's own mutex.

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// advEntityName reverses entityByName for the advancement entity strings
// ("blaze", "zombie" — registry names without namespace).
var advEntityName = func() map[int]string {
	m := make(map[int]string, len(entityByName))
	for name, id := range entityByName {
		m[id] = name
	}
	return m
}()

// advance evaluates every criterion behind trigger against the match payload,
// grants what fires, reveals newly visible tree nodes, streams progress
// deltas, and on a completed advancement announces + pays the XP reward.
// Cheap when nothing matches — trigger sites call it unconditionally.
func (h *hub) advance(players map[int32]*tracked, t *tracked, trigger string, m advMatch) {
	if t == nil || t.adv == nil {
		return
	}
	var granted []*advNode
	var completed []*advNode
	for _, ref := range advByTrigger[trigger] {
		if !m.criterion(ref.crit) {
			continue
		}
		fresh, nowDone := t.adv.grant(ref.node, ref.crit.name)
		if !fresh {
			continue
		}
		granted = append(granted, ref.node)
		if nowDone {
			completed = append(completed, ref.node)
		}
	}
	if len(granted) == 0 {
		return
	}
	// Reveal what the grants made visible (frontier growth, earned hidden
	// nodes) BEFORE any progress for them — the client needs the node first.
	vis := t.adv.visible()
	if added := visibleTree(vis, t.advVisible); len(added.Nodes) > 0 {
		t.p.trySendEv(added)
		var p attachproto.AdvProgress
		for _, an := range added.Nodes {
			if st := t.adv[an.ID]; len(st) > 0 && !containsNode(granted, an.ID) {
				p.Entries = append(p.Entries, attachproto.AdvProgressEntry{
					ID: an.ID, Done: copyDone(st)})
			}
		}
		if len(p.Entries) > 0 {
			t.p.trySendEv(p)
		}
	}
	t.advVisible = vis
	var p attachproto.AdvProgress
	for _, n := range granted {
		if !vis[n.id] {
			continue // progress on an invisible node stays server-side (vanilla)
		}
		p.Entries = append(p.Entries, attachproto.AdvProgressEntry{
			ID: n.id, Done: copyDone(t.adv[n.id])})
	}
	if len(p.Entries) > 0 {
		t.p.trySendEv(p)
	}
	for _, n := range completed {
		if d := n.display; d != nil && d.announceChat && h.rules.AnnounceAdv {
			verb := "has made the advancement"
			switch d.frame {
			case 1:
				verb = "has completed the challenge"
			case 2:
				verb = "has reached the goal"
			}
			h.broadcastChat(players, fmt.Sprintf("%s %s [%s]", t.p.name, verb, d.titleEN))
		}
		if n.xp > 0 {
			h.addXP(t, int(n.xp))
		}
	}
}

// copyDone snapshots a criteria map for a frame: the session goroutine
// marshals it later, while the hub keeps mutating the live state.
func copyDone(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func containsNode(ns []*advNode, id string) bool {
	for _, n := range ns {
		if n.id == id {
			return true
		}
	}
	return false
}

// advTick runs at 1 Hz beside survivalTick: the two polled trigger families —
// inventory_changed (no single mutation choke point exists; vanilla effectively
// re-tests on every inventory change, we re-test on a clock) and location
// (biome visits, vanilla ticks these every second too).
func (h *hub) advTick(players map[int32]*tracked) {
	for _, t := range players {
		if t.adv == nil || t.dead {
			continue
		}
		if t.inv != nil {
			ids := make([]int32, 0, invSize+5)
			for i := range t.inv.slots {
				if s := &t.inv.slots[i]; s.count > 0 {
					ids = append(ids, s.item)
				}
			}
			for i := range t.armor {
				if t.armor[i].count > 0 {
					ids = append(ids, t.armor[i].item)
				}
			}
			if t.offhand.count > 0 {
				ids = append(ids, t.offhand.item)
			}
			h.advance(players, t, "inventory_changed", advMatch{inv: ids})
			h.recipeUnlocks(t, ids)
		}
		// health objectives are a gauge (vanilla read-only criteria): poll at
		// 1 Hz; sbSetScore suppresses unchanged values.
		h.sbCriteria(players, "health", t.p.name, int32(t.health+t.absorption+0.5), true)
		if t.dim == 0 { // the visit-every-biome list is overworld-only
			if biome := h.worldFor(t.dim).BiomeAt(int(t.x), int(t.z)); biome != "" {
				h.advance(players, t, "location", advMatch{biome: biome})
			}
		}
	}
}

// advSendAll ships the player's VISIBLE tree + their progress snapshot (join,
// and again after a shard crossing so the new pod's gateway can re-init).
// Vanilla visibility: an empty state means an empty advancement screen.
func (h *hub) advSendAll(t *tracked) {
	t.advVisible = t.adv.visible()
	t.p.sendEv(visibleTree(t.advVisible, nil))
	snap := t.adv.snapshot()
	entries := snap.Entries[:0]
	for _, e := range snap.Entries {
		if t.advVisible[e.ID] {
			e.Done = copyDone(e.Done) // session-marshal race, as in advance()
			entries = append(entries, e)
		}
	}
	snap.Entries = entries
	t.p.sendEv(snap)
}

// advStore persists advancement grant state by player name — the same
// shape/cadence as invStore (load on join, record every 30 s + on leave,
// atomic JSON writes).
type advStore struct {
	mu   sync.Mutex
	path string
	m    map[string]advState
}

func newAdvStore(path string) *advStore {
	s := &advStore{path: path, m: map[string]advState{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

// load returns a player's state as a fresh mutable copy for the hub to own.
func (s *advStore) load(name string) advState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := advState{}
	for id, crits := range s.m[name] {
		m := make(map[string]int64, len(crits))
		for k, v := range crits {
			m[k] = v
		}
		st[id] = m
	}
	return st
}

// record snapshots the live state back into the store (no write).
func (s *advStore) record(name string, st advState) {
	if st == nil {
		return
	}
	snap := advState{}
	for id, crits := range st {
		m := make(map[string]int64, len(crits))
		for k, v := range crits {
			m[k] = v
		}
		snap[id] = m
	}
	s.mu.Lock()
	s.m[name] = snap
	s.mu.Unlock()
}

// flush writes the table to disk atomically.
func (s *advStore) flush() {
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

// save records and immediately flushes one player's state (on disconnect).
func (s *advStore) save(name string, st advState) {
	s.record(name, st)
	s.flush()
}
