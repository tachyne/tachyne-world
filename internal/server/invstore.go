package server

import (
	"encoding/json"
	"log"
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
	Slots   [invSize]stackRow `json:"slots"`
	Armor   [4]stackRow       `json:"armor"`
	Offhand stackRow          `json:"offhand"`
	// The player's ender chest travels with them, not with any block.
	Ender    [27]stackRow `json:"ender,omitempty"`
	XPLevel  int32        `json:"xp_level,omitempty"`
	XPPoints int32        `json:"xp_points,omitempty"`

	// Last position (restored on login, vanilla-style: you log back in where
	// you logged out). HasPos distinguishes a real saved position from a legacy
	// entry or a brand-new player (both → world spawn).
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Z      float64 `json:"z,omitempty"`
	Yaw    float32 `json:"yaw,omitempty"`
	Pitch  float32 `json:"pitch,omitempty"`
	Dim    int32   `json:"dim,omitempty"`
	HasPos bool    `json:"has_pos,omitempty"`
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
		if err := loadStore(path, &s.m); err != nil {
			log.Fatal(err)
		}
	}
	return s
}

// stackRow is one persisted stack. NAMED so widening it stays a one-line
// change: every store (inventories, containers, mobs) shares this shape, and a
// shorter row in an existing file zero-fills on JSON decode, so growing it is
// backward-compatible by construction.
//
// Layout: [item, count, dmg, enchPack, mapID, 6 banner layers
// (patPlus1<<8|color), trimPack ((mat+1)<<8|(pat+1)), bookID, boxID, hiveID,
// bundleID, potion, repairCost, instrument, nameID].
// stackRow is the persisted form of a stack. It GROWS at the end: an older
// save decodes with the new trailing fields left zero, which is exactly what
// "no bundle", "no potion", "never repaired", "ponder" and "no name" mean.
//
// The last four columns closed a restart data-loss gap (2026-09-05): potion,
// name, repairCost and instrument existed on invStack but never reached the
// row, so every rollout turned potions into water bottles, stripped anvil
// names, reset the prior-work cost and made every goat horn play ponder.
type stackRow = [24]int32

func packStack(st invStack) stackRow {
	r := stackRow{st.item, int32(st.count), int32(st.dmg), packEnch(st.ench), st.mapID}
	for i, l := range st.pats {
		r[5+i] = int32(l.patPlus1)<<8 | int32(l.color)
	}
	if st.trimMat != 0 || st.trimPat != 0 {
		r[11] = int32(st.trimMat)<<8 | int32(st.trimPat)
	}
	r[12] = st.bookID
	r[13] = st.boxID
	r[14] = st.hiveID
	r[15] = st.bundleID
	r[16] = int32(st.potion)
	r[17] = int32(st.repairCost)
	r[18] = int32(st.instrument)
	r[19] = globalNames.Load().intern(st.name)
	lode := packLode(st.lode) // lodestone_tracker (columns 20-23)
	copy(r[20:24], lode[:])
	return r
}

func unpackStack(r stackRow) invStack {
	st := invStack{item: r[0], count: int(r[1]), dmg: int(r[2]), ench: unpackEnch(r[3]), mapID: r[4]}
	for i := 0; i < 6; i++ {
		st.pats[i] = bannerLayer{patPlus1: int16(r[5+i] >> 8), color: int8(r[5+i] & 0xff)}
	}
	st.trimMat, st.trimPat = int8(r[11]>>8), int8(r[11]&0xff)
	st.bookID = r[12]
	st.boxID = r[13]
	st.hiveID = r[14]
	st.bundleID = r[15]
	st.potion = int8(r[16])
	st.repairCost = int(r[17])
	st.instrument = int8(r[18])
	st.name = globalNames.Load().get(r[19])
	st.lode = unpackLode([4]int32{r[20], r[21], r[22], r[23]})
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
	for i, row := range saved.Ender {
		t.enderChest().slots[i] = unpackStack(row)
	}
	t.xpLevel, t.xpPoints = int(saved.XPLevel), int(saved.XPPoints)
}

// savedPos returns a player's last saved position (ok=false for a new player
// or a legacy entry without one). Safe to call off the hub goroutine.
func (s *invStore) savedPos(name string) (x, y, z float64, yaw, pitch float32, dim int32, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sv, has := s.m[name]; has && sv.HasPos {
		return sv.X, sv.Y, sv.Z, sv.Yaw, sv.Pitch, sv.Dim, true
	}
	return 0, 0, 0, 0, 0, 0, false
}

// record updates name's in-memory snapshot from the live loadout (no write).
func (s *invStore) record(name string, t *tracked) {
	if t.inv == nil {
		return
	}
	snap := &savedInv{Offhand: packStack(t.offhand),
		XPLevel: int32(t.xpLevel), XPPoints: int32(t.xpPoints),
		X: t.x, Y: t.y, Z: t.z, Yaw: t.yaw, Pitch: t.pitch, Dim: int32(t.dim), HasPos: true}
	for i, st := range t.inv.slots {
		snap.Slots[i] = packStack(st)
	}
	for i, st := range t.armor {
		snap.Armor[i] = packStack(st)
	}
	if t.ender != nil {
		for i, st := range t.ender.slots {
			snap.Ender[i] = packStack(st)
		}
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
	writeStore(path, data)
}

// save records and immediately flushes one player's loadout (on disconnect).
func (s *invStore) save(name string, t *tracked) {
	s.record(name, t)
	s.flush()
}
