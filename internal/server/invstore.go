package server

import (
	"encoding/json"
	"log"
	"sort"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
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
// savedEffect is one active effect, with its remaining time in TICKS.
type savedEffect struct {
	ID      int32 `json:"id"`
	Amp     int   `json:"amp,omitempty"`
	Left    int   `json:"left"`
	Ambient bool  `json:"ambient,omitempty"`
}

type savedInv struct {
	Slots   [invSize]stackRow `json:"slots"`
	Armor   [4]stackRow       `json:"armor"`
	Offhand stackRow          `json:"offhand"`
	// The player's ender chest travels with them, not with any block.
	Ender    [27]stackRow `json:"ender,omitempty"`
	XPLevel  int32        `json:"xp_level,omitempty"`
	XPPoints int32        `json:"xp_points,omitempty"`
	// Player.enchantmentSeed (vanilla's XpSeed): the enchanting table's three
	// offers are a function of it, so it has to outlive the session or a
	// relog would reshuffle a roll the player was saving up for.
	EnchSeed int32 `json:"ench_seed,omitempty"`
	// WardenSpawnTracker (warning level, cooldown, quiet time): vanilla keeps
	// it in the player's data, so a relog does not wipe the deep dark's tally.
	// Active status effects: vanilla keeps them in the player's data, so a
	// relog does not strip a brewed potion or a beacon's gift.
	Effects []savedEffect `json:"effects,omitempty"`

	WardenWarn  int `json:"warden_warn,omitempty"`
	WardenCool  int `json:"warden_cool,omitempty"`
	WardenSince int `json:"warden_since,omitempty"`

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

	// Last death location (ServerPlayer.lastDeathLocation): what the
	// recovery compass points at. HasDeath false = never died.
	DeathDim int32    `json:"death_dim,omitempty"`
	DeathPos [3]int32 `json:"death_pos,omitempty"`
	HasDeath bool     `json:"has_death,omitempty"`

	// Scoreboard tags (/tag): vanilla keeps them in the player's data.
	Tags []string `json:"tags,omitempty"`
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
type stackRow [39]int32 // 28 → 30 on 2026-09-19 for enchantments 5-8, 32 on 2026-09-20 for a firework's flight and bursts, 36 for a pot's four sherds, 38 on 2026-09-24 for a sulfur cube bucket's block and age, 39 for a bucketed mob's variant; older rows load with the tail zero

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
	r[24] = packEnchHi(st.ench)  // enchantments 3-4 (column 24)
	r[25] = st.color             // dyed_color (column 25)
	r[26] = int32(st.stew)       // suspicious_stew_effects (column 26)
	r[27] = int32(st.shieldBase) // base_color, a decorated shield (column 27)
	r[28] = packEnch3(st.ench)   // enchantments 5-6 (column 28)
	r[29] = packEnch4(st.ench)   // enchantments 7-8 (column 29)
	r[30] = int32(st.flight)     // a rocket's flight duration (column 30)
	r[31] = st.starID            // a firework's bursts (column 31)
	copy(r[32:36], st.sherds[:]) // a decorated pot's four faces (columns 32-35)
	r[36] = st.cube.item         // a sulfur cube bucket's swallowed block (column 36)
	r[37] = st.cube.age          // …and the cube's age (column 37)
	r[38] = st.cube.variant      // a bucketed mob's variant + 1 (column 38)
	return r
}

func unpackStack(r stackRow) invStack {
	st := invStack{item: r[0], count: int(r[1]), dmg: int(r[2]), ench: unpackEnch4(r[3], r[24], r[28], r[29]), mapID: r[4]}
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
	st.color = r[25]
	st.stew = int8(r[26])
	st.shieldBase = int8(r[27])
	st.flight = int8(r[30])
	st.starID = r[31]
	copy(st.sherds[:], r[32:36])
	st.cube = cubeContent{item: r[36], age: r[37], variant: r[38]}
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
	t.enchSeed = saved.EnchSeed
	t.wardenWarn, t.wardenCool, t.wardenSince = saved.WardenWarn, saved.WardenCool, saved.WardenSince
	t.tags = tagSet(saved.Tags)
	if len(saved.Effects) > 0 {
		t.effects = map[int32]*activeEffect{}
		for _, e := range saved.Effects {
			if e.Left <= 0 {
				continue
			}
			t.effects[e.ID] = &activeEffect{amp: e.Amp, left: e.Left, ambient: e.Ambient}
			t.applyEffectModifiers(e.ID, e.Amp) // the attribute side comes back too
		}
	}
}

// savedPos returns a player's last saved position (ok=false for a new player
// or a legacy entry without one). Safe to call off the hub goroutine.
// setDeath records where name last died (the block they were standing in).
func (s *invStore) setDeath(name string, d attachproto.DeathPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv := s.m[name]
	if sv == nil {
		sv = &savedInv{}
		s.m[name] = sv
	}
	sv.DeathDim, sv.DeathPos, sv.HasDeath = d.Dim, [3]int32{d.X, d.Y, d.Z}, true
}

// death returns name's last death location, nil if they never died.
func (s *invStore) death(name string) *attachproto.DeathPos {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sv, has := s.m[name]; has && sv.HasDeath {
		return &attachproto.DeathPos{Dim: sv.DeathDim, X: sv.DeathPos[0], Y: sv.DeathPos[1], Z: sv.DeathPos[2]}
	}
	return nil
}

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
		XPLevel: int32(t.xpLevel), XPPoints: int32(t.xpPoints), EnchSeed: t.enchSeed,
		WardenWarn: t.wardenWarn, WardenCool: t.wardenCool, WardenSince: t.wardenSince,
		Effects: savedEffectsOf(t), Tags: sortedTags(t.tags),
		X: t.x, Y: t.y, Z: t.z, Yaw: t.yaw, Pitch: t.pitch, Dim: int32(t.dim), HasPos: true}
	if old := s.m[name]; old != nil && old.HasDeath { // the death location outlives the loadout
		snap.DeathDim, snap.DeathPos, snap.HasDeath = old.DeathDim, old.DeathPos, true
	}
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

// savedEffectsOf snapshots a player's active effects in a stable order.
func savedEffectsOf(t *tracked) []savedEffect {
	if len(t.effects) == 0 {
		return nil
	}
	ids := make([]int32, 0, len(t.effects))
	for id := range t.effects {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]savedEffect, 0, len(ids))
	for _, id := range ids {
		e := t.effects[id]
		out = append(out, savedEffect{ID: id, Amp: e.amp, Left: e.left, Ambient: e.ambient})
	}
	return out
}
