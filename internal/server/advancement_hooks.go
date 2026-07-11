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
// grants what fires, streams the progress delta to the player, and on a
// completed advancement announces + pays the XP reward. Cheap when nothing
// matches — trigger sites call it unconditionally.
func (h *hub) advance(players map[int32]*tracked, t *tracked, trigger string, m advMatch) {
	if t == nil || t.adv == nil {
		return
	}
	for _, ref := range advByTrigger[trigger] {
		if !m.criterion(ref.crit) {
			continue
		}
		fresh, completed := t.adv.grant(ref.node, ref.crit.name)
		if !fresh {
			continue
		}
		// Copy the criteria map into the frame: the session goroutine marshals
		// it later, while the hub keeps mutating the live state.
		done := make(map[string]int64, len(t.adv[ref.node.id]))
		for k, v := range t.adv[ref.node.id] {
			done[k] = v
		}
		t.p.trySendEv(attachproto.AdvProgress{Entries: []attachproto.AdvProgressEntry{
			{ID: ref.node.id, Done: done}}})
		if !completed {
			continue
		}
		if d := ref.node.display; d != nil && d.announceChat {
			verb := "has made the advancement"
			switch d.frame {
			case 1:
				verb = "has completed the challenge"
			case 2:
				verb = "has reached the goal"
			}
			h.broadcastChat(players, fmt.Sprintf("%s %s [%s]", t.p.name, verb, d.titleEN))
		}
		if ref.node.xp > 0 {
			h.addXP(t, int(ref.node.xp))
		}
	}
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
		}
		if t.dim == 0 { // the visit-every-biome list is overworld-only
			if biome := h.worldFor(t.dim).BiomeAt(int(t.x), int(t.z)); biome != "" {
				h.advance(players, t, "location", advMatch{biome: biome})
			}
		}
	}
}

// advSendAll ships the static tree + the player's full snapshot (join, and
// again after a shard crossing so the new pod's gateway session can re-init).
func (h *hub) advSendAll(t *tracked) {
	t.p.sendEv(advTreeFrame)
	snap := t.adv.snapshot()
	// deep-copy the maps for the same session-marshal race reason as advance()
	for i, e := range snap.Entries {
		done := make(map[string]int64, len(e.Done))
		for k, v := range e.Done {
			done[k] = v
		}
		snap.Entries[i].Done = done
	}
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
