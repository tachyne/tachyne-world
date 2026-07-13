package server

import (
	"encoding/json"
	"os"
	"sync"
)

// invStore persists survival inventories by player name so picked-up items
// survive a relog/restart — like modeStore, plain JSON an admin can inspect.
// Worn armor and the offhand persist too (they used to be folded back into
// the main inventory on logout). Accessed from the hub goroutine (load on
// join, record on leave) plus a periodic flush; the mutex guards the
// in-memory map and the atomic file write.
type invStore struct {
	mu   sync.Mutex
	path string
	m    map[string]*savedInv
}

// savedInv is one player's persisted loadout, each slot an
// (item,count,dmg,ench,mapID) row. Older files stored 4-column rows (or a
// bare 36-slot array) — shorter JSON arrays zero-fill the new column, so
// they migrate on load.
type savedInv struct {
	Slots    [invSize][12]int32 `json:"slots"`
	Armor    [4][12]int32       `json:"armor"`
	Offhand  [12]int32          `json:"offhand"`
	XPLevel  int32              `json:"xp_level,omitempty"`
	XPPoints int32              `json:"xp_points,omitempty"`
}

func (s *savedInv) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' { // legacy: bare slot array
		return json.Unmarshal(b, &s.Slots)
	}
	type plain savedInv // drop the method to avoid recursion
	return json.Unmarshal(b, (*plain)(s))
}

func newInvStore(path string) *invStore {
	s := &invStore{path: path, m: map[string]*savedInv{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

// Stack rows: [item, count, dmg, enchPack, mapID, 6 banner layers
// (patPlus1<<8|color), trimPack ((mat+1)<<8|(pat+1))]. Old shorter rows
// zero-fill on JSON decode.
func packStack(st invStack) [12]int32 {
	r := [12]int32{st.item, int32(st.count), int32(st.dmg), packEnch(st.ench), st.mapID}
	for i, l := range st.pats {
		r[5+i] = int32(l.patPlus1)<<8 | int32(l.color)
	}
	if st.trimMat != 0 || st.trimPat != 0 {
		r[11] = int32(st.trimMat)<<8 | int32(st.trimPat)
	}
	return r
}

func unpackStack(r [12]int32) invStack {
	st := invStack{item: r[0], count: int(r[1]), dmg: int(r[2]), ench: unpackEnch(r[3]), mapID: r[4]}
	for i := 0; i < 6; i++ {
		st.pats[i] = bannerLayer{patPlus1: int16(r[5+i] >> 8), color: int8(r[5+i] & 0xff)}
	}
	st.trimMat, st.trimPat = int8(r[11]>>8), int8(r[11]&0xff)
	return st
}

// loadInto fills the player's inventory, armor and offhand from their saved
// loadout (no-op if none saved).
func (s *invStore) loadInto(t *tracked, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, ok := s.m[name]
	if !ok {
		return
	}
	for i, row := range saved.Slots {
		t.inv.slots[i] = unpackStack(row)
	}
	for i, row := range saved.Armor {
		t.armor[i] = unpackStack(row)
	}
	t.offhand = unpackStack(saved.Offhand)
	t.xpLevel, t.xpPoints = int(saved.XPLevel), int(saved.XPPoints)
}

// record updates name's in-memory snapshot from the live loadout (no write).
func (s *invStore) record(name string, t *tracked) {
	if t.inv == nil {
		return
	}
	snap := &savedInv{Offhand: packStack(t.offhand),
		XPLevel: int32(t.xpLevel), XPPoints: int32(t.xpPoints)}
	for i, st := range t.inv.slots {
		snap.Slots[i] = packStack(st)
	}
	for i, st := range t.armor {
		snap.Armor[i] = packStack(st)
	}
	s.mu.Lock()
	s.m[name] = snap
	s.mu.Unlock()
}

// migrateItemIDs rewrites every saved item id (main slots, armor, offhand)
// through remap — for a one-time id-space migration after a canonical version
// bump. Returns the count changed. Item 0 (empty) is left alone.
func (s *invStore) migrateItemIDs(remap func(int32) int32) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	mig := func(id *int32) {
		if *id != 0 {
			if ns := remap(*id); ns != *id {
				*id = ns
				n++
			}
		}
	}
	for _, inv := range s.m {
		for i := range inv.Slots {
			mig(&inv.Slots[i][0])
		}
		for i := range inv.Armor {
			mig(&inv.Armor[i][0])
		}
		mig(&inv.Offhand[0])
	}
	return n
}

// flush writes the table to disk atomically.
func (s *invStore) flush() {
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

// save records and immediately flushes one player's loadout (on disconnect).
func (s *invStore) save(name string, t *tracked) {
	s.record(name, t)
	s.flush()
}
