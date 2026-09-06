package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// containerStore persists world-container contents (furnace input/fuel/output +
// burn/cook progress; chest slots later) keyed by block position — like
// invStore, plain JSON an admin can inspect, written atomically. The hub
// records a snapshot every 30s and on graceful shutdown; the store is loaded
// once at boot before the hub starts ticking.
type containerStore struct {
	mu   sync.Mutex
	path string
	m    containerFile
}

type savedItem struct {
	Dim   int      `json:"dim,omitempty"`
	X     float64  `json:"x"`
	Y     float64  `json:"y"`
	Z     float64  `json:"z"`
	Item  int32    `json:"item"`
	Count int      `json:"count"`
	Dmg   int      `json:"dmg,omitempty"`
	Ench  int32    `json:"ench,omitempty"`
	Ench2 int32    `json:"ench2,omitempty"` // enchantments 3-4 (2026-09-06)
	MapID int32    `json:"map_id,omitempty"`
	Pats  [6]int32 `json:"pats,omitempty"` // banner layers, patPlus1<<8|color
	Trim  int32    `json:"trim,omitempty"` // (mat+1)<<8|(pat+1)
	Book  int32    `json:"book,omitempty"` // book id
	Box   int32    `json:"box,omitempty"`  // shulker-box contents id
	Hive  int32    `json:"hive,omitempty"` // carried-hive contents id
	// The four that were lost on every restart until 2026-09-05.
	Bundle int32    `json:"bundle,omitempty"` // bundle contents id — was dropped on the floor, literally
	Potion int8     `json:"potion,omitempty"`
	Repair int      `json:"repair,omitempty"` // anvil prior-work cost
	Instr  int8     `json:"instr,omitempty"`  // goat horn instrument
	Name   string   `json:"name,omitempty"`   // anvil rename
	Lode   [4]int32 `json:"lode,omitempty"`   // lodestone compass target (packLode)
}

type containerFile struct {
	Furnaces  map[string]savedFurnace   `json:"furnaces,omitempty"`
	Chests    map[string][]containerRow `json:"chests,omitempty"`   // (slot + the stack pack) — sparse; old shorter rows zero-fill
	Bins      map[string]savedBin       `json:"bins,omitempty"`     // dispenser/dropper/hopper
	Items     []savedItem               `json:"items,omitempty"`    // dropped item entities
	Vehicles  []savedVehicle            `json:"vehicles,omitempty"` // boats and minecarts (2026-09-06)
	Paintings []savedPainting           `json:"paintings,omitempty"`
	Frames    []savedFrame              `json:"frames,omitempty"`
	Jukeboxes map[string]stackRow       `json:"jukeboxes,omitempty"`
	Beacons   map[string][2]int32       `json:"beacons,omitempty"` // chosen powers (mob_effect id+1; 0 = none)
	Stands    []savedStand              `json:"stands,omitempty"`  // placed armor stands
	Lecterns  map[string]savedLectern   `json:"lecterns,omitempty"`
	Shelves   map[string][6]stackRow    `json:"shelves,omitempty"` // chiseled bookshelves
	// Shulker-box contents riding a dropped or stored item, keyed by boxID.
	Boxes map[string][]containerRow `json:"boxes,omitempty"`
	// The next boxID to mint, so ids stay unique across restarts.
	NextBoxID int32 `json:"next_box_id,omitempty"`
	// Bundle contents riding a bundle item, keyed by bundleID. Ordered, not
	// slot-indexed: a pouch has no slots, just a list with the most recently
	// touched stack first.
	Bundles      map[string][]stackRow `json:"bundles,omitempty"`
	NextBundleID int32                 `json:"next_bundle_id,omitempty"`
	// Custom item names by id (names.go). Loaded before any other store
	// decodes a stack, because unpackStack resolves nameIDs through it.
	Names      map[string]string `json:"names,omitempty"`
	NextNameID int32             `json:"next_name_id,omitempty"`
	// Bees + honey riding a Silk-Touched hive item, keyed by hiveID.
	HiveItems  map[string]hiveStow `json:"hive_items,omitempty"`
	NextHiveID int32               `json:"next_hive_id,omitempty"`
	// Placed conduits as "dim,x,y,z" — they are player-built, so nothing else
	// can rediscover them after a restart.
	Conduits []string `json:"conduits,omitempty"`
	// Trial-chamber progress. Both are overworld-only, so a plain position key
	// is enough. Without these a restart re-arms every spent spawner and lets
	// everyone claim every vault again.
	Vaults map[string]savedVault `json:"vaults,omitempty"`
	Trials map[string]savedTrial `json:"trials,omitempty"`
}

// savedVault is who has already been paid by one vault (VaultServerData's
// rewarded_players), oldest claim first.
type savedVault struct {
	Rewarded []string `json:"rewarded,omitempty"` // player UUIDs, hex
}

// savedTrial is one trial spawner's progress. Its POSE is not here — that
// rides the block state in world.gob, exactly as vanilla reads it back off the
// block — only the bookkeeping the block cannot express.
//
// The cooldown is stored as ticks REMAINING, not as an absolute deadline: the
// hub's world age restarts at zero every boot, so an absolute tick would read
// as long expired.
type savedTrial struct {
	State        uint8    `json:"state"`
	CooldownLeft uint64   `json:"cooldown_left,omitempty"`
	Spawned      int      `json:"spawned,omitempty"`
	Detected     []string `json:"detected,omitempty"` // player UUIDs, hex
	Ominous      bool     `json:"ominous,omitempty"`  // turned ominous by an omen
}

// savedLectern is one lectern's book + open page.
type savedLectern struct {
	Item stackRow `json:"item"`
	Page int      `json:"page,omitempty"`
}

type savedPainting struct {
	Dim     int    `json:"dim,omitempty"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Z       int    `json:"z"`
	Dir     int32  `json:"dir"`
	Variant string `json:"variant"`
}

// savedFrame is one placed item frame; Item is the packed stack row
// (item,count,dmg,ench,mapID — same shape as inventory slots).
type savedFrame struct {
	Dim  int      `json:"dim,omitempty"`
	X    int      `json:"x"`
	Y    int      `json:"y"`
	Z    int      `json:"z"`
	Dir  int32    `json:"dir"`
	Glow bool     `json:"glow,omitempty"`
	Rot  int      `json:"rot,omitempty"`
	Item stackRow `json:"item"`
}

type savedBin struct {
	Size     int            `json:"size"`
	Slots    []containerRow `json:"slots,omitempty"`    // (slot + the stack pack)
	Disabled uint16         `json:"disabled,omitempty"` // crafter: bitmask of disabled grid slots
}

type savedFurnace struct {
	Slots    [3][3]int32 `json:"slots"` // (item,count,dmg) — input, fuel, output
	BurnLeft int         `json:"burnLeft,omitempty"`
	BurnMax  int         `json:"burnMax,omitempty"`
	Cook     int         `json:"cook,omitempty"`
	CookMax  int         `json:"cookMax,omitempty"`
}

// savedStand is one placed armor stand (equipment rows = the stack pack).
type savedStand struct {
	Dim   int         `json:"dim,omitempty"`
	X     float64     `json:"x"`
	Y     float64     `json:"y"`
	Z     float64     `json:"z"`
	Yaw   float32     `json:"yaw"`
	Equip [6]stackRow `json:"equip"`
}

// recordLecterns / loadLecterns persist lectern books + pages.
func (s *containerStore) recordLecterns(ls map[simPos]*lectern) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Lecterns = map[string]savedLectern{}
	for pos, l := range ls {
		s.m.Lecterns[simKey(pos)] = savedLectern{Item: packStack(l.book), Page: l.page}
	}
}

func (s *containerStore) loadLecterns() map[simPos]*lectern {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]*lectern{}
	for k, sv := range s.m.Lecterns {
		if pos, ok := parseSimKey(k); ok {
			out[pos] = &lectern{book: unpackStack(sv.Item), page: sv.Page}
		}
	}
	return out
}

// recordShelves / loadShelves persist chiseled bookshelves.
func (s *containerStore) recordShelves(m map[simPos]*[6]invStack) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Shelves = map[string][6]stackRow{}
	for pos, shelf := range m {
		var rows [6]stackRow
		for i, st := range shelf {
			rows[i] = packStack(st)
		}
		s.m.Shelves[simKey(pos)] = rows
	}
}

func (s *containerStore) loadShelves() map[simPos]*[6]invStack {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]*[6]invStack{}
	for k, rows := range s.m.Shelves {
		if pos, ok := parseSimKey(k); ok {
			var shelf [6]invStack
			for i, r := range rows {
				shelf[i] = unpackStack(r)
			}
			out[pos] = &shelf
		}
	}
	return out
}

// recordStands snapshots placed armor stands for the next flush.
func (s *containerStore) recordStands(stands map[int32]*armorStand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Stands = s.m.Stands[:0]
	for _, st := range stands {
		sv := savedStand{Dim: st.dim, X: st.x, Y: st.y, Z: st.z, Yaw: st.yaw}
		for i, e := range st.equip {
			sv.Equip[i] = packStack(e)
		}
		s.m.Stands = append(s.m.Stands, sv)
	}
}

// loadStands rebuilds placed armor stands (fresh eids).
func (s *containerStore) loadStands(alloc func() int32) map[int32]*armorStand {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int32]*armorStand{}
	for _, sv := range s.m.Stands {
		st := &armorStand{eid: alloc(), dim: sv.Dim, x: sv.X, y: sv.Y, z: sv.Z, yaw: sv.Yaw}
		for i, r := range sv.Equip {
			st.equip[i] = unpackStack(r)
		}
		out[st.eid] = st
	}
	return out
}

func posKey(p blockPos) string { return fmt.Sprintf("%d,%d,%d", p.x, p.y, p.z) }

// simKey is posKey with the dimension in front — the key every block-entity
// store now writes, in the same "dim:x,y,z" shape signs have always used. A key
// without the prefix is a record from before the stores knew about dimensions,
// and every one of those was written in the overworld.
func simKey(p simPos) string { return fmt.Sprintf("%d:%d,%d,%d", p.dim, p.x, p.y, p.z) }

// migrateSimKeys rewrites a store's legacy "x,y,z" keys into "0:x,y,z" in
// place. Stores that only ever rewrite the entries they touch — the banner and
// campfire render views — need this at load, or a build placed before the
// dimension existed would go blank the moment its key stopped matching.
func migrateSimKeys[T any](m map[string]T) {
	for k, v := range m {
		if strings.Contains(k, ":") {
			continue
		}
		if pos, ok := parsePosKey(k); ok {
			delete(m, k)
			m[simKey(simPos{blockPos: pos})] = v
		}
	}
}

// parseSimKey reads either shape back.
func parseSimKey(k string) (simPos, bool) {
	dim := 0
	if d, rest, ok := strings.Cut(k, ":"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return simPos{}, false
		}
		dim, k = n, rest
	}
	pos, ok := parsePosKey(k)
	return simPos{dim: dim, blockPos: pos}, ok
}

// containerRow is one sparse container slot: the slot index + the full
// stackRow pack. NAMED for the same reason as stackRow — widening it is a
// one-line change, where a [N]int32 literal silently TRUNCATES the copy in
// slotRow when the stack pack outgrows it (which is how hiveID nearly got
// dropped from chest rows).
type containerRow = [1 + len(stackRow{})]int32

// slotRow packs a slot index + stack into a sparse container row.
func slotRow(i int, st invStack) containerRow {
	var r containerRow
	r[0] = int32(i)
	p := packStack(st)
	copy(r[1:], p[:])
	return r
}

// rowStack unpacks a sparse container row (index, stack).
func rowStack(r containerRow) (int, invStack) {
	var p stackRow
	copy(p[:], r[1:])
	return int(r[0]), unpackStack(p)
}

func parsePosKey(k string) (blockPos, bool) {
	var p blockPos
	if _, err := fmt.Sscanf(k, "%d,%d,%d", &p.x, &p.y, &p.z); err != nil {
		return blockPos{}, false
	}
	return p, true
}

func newContainerStore(path string) *containerStore {
	s := &containerStore{path: path}
	if path != "" {
		if err := loadStore(path, &s.m); err != nil {
			log.Fatal(err)
		}
	}
	return s
}

// loadFurnaces reconstructs live furnace state from the last snapshot. Viewer
// and bar-sync fields are transient and start zeroed.
func (s *containerStore) loadFurnaces() map[simPos]*furnace {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]*furnace{}
	for k, sf := range s.m.Furnaces {
		pos, ok := parseSimKey(k)
		if !ok {
			continue
		}
		f := &furnace{burnLeft: sf.BurnLeft, burnMax: sf.BurnMax, cook: sf.Cook, cookMax: sf.CookMax}
		if f.cookMax == 0 {
			f.cookMax = 200
		}
		for i, row := range sf.Slots {
			f.slots[i] = invStack{item: row[0], count: int(row[1]), dmg: int(row[2])}
		}
		out[pos] = f
	}
	return out
}

// loadChests reconstructs chest storage from the last snapshot.
func (s *containerStore) loadChests() map[simPos]*chest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]*chest{}
	for k, saved := range s.m.Chests {
		pos, ok := parseSimKey(k)
		if !ok {
			continue
		}
		c := &chest{}
		for _, e := range saved {
			if i, st := rowStack(e); i >= 0 && i < 27 {
				c.slots[i] = st
			}
		}
		out[pos] = c
	}
	return out
}

// recordItems / loadItems persist dropped item entities across restarts.
func (s *containerStore) recordItems(items []savedItem) {
	s.mu.Lock()
	s.m.Items = items
	s.mu.Unlock()
}

// savedVehicle is one parked boat or minecart: the entity type by NAME (ids
// shift with the canonical version) and where it sits. A rider is never
// saved — the player is gone by the time the store is written.
type savedVehicle struct {
	Dim   int        `json:"dim,omitempty"`
	Etype string     `json:"type"`
	X     float64    `json:"x"`
	Y     float64    `json:"y"`
	Z     float64    `json:"z"`
	Yaw   float32    `json:"yaw,omitempty"`
	Chest []stackRow `json:"chest,omitempty"` // a chest boat's or container cart's cargo
	// The special carts' state.
	Fuel     int     `json:"fuel,omitempty"`
	PushX    float64 `json:"push_x,omitempty"`
	PushZ    float64 `json:"push_z,omitempty"`
	Fuse     int     `json:"fuse,omitempty"` // fuse+1 (0 = not primed)
	Disabled bool    `json:"disabled,omitempty"`
}

func (s *containerStore) recordVehicles(v []savedVehicle) {
	s.mu.Lock()
	s.m.Vehicles = v
	s.mu.Unlock()
}

func (s *containerStore) loadVehicles() []savedVehicle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Vehicles
}

func (s *containerStore) loadItems() []savedItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Items
}

// loadBins reconstructs dispenser/dropper/hopper storage from the snapshot.
func (s *containerStore) loadBins() map[simPos]*bin {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]*bin{}
	for k, saved := range s.m.Bins {
		pos, ok := parseSimKey(k)
		if !ok || saved.Size <= 0 || saved.Size > 27 {
			continue
		}
		b := &bin{slots: make([]invStack, saved.Size)}
		for _, e := range saved.Slots {
			if i, st := rowStack(e); i >= 0 && i < saved.Size {
				b.slots[i] = st
			}
		}
		for i := 0; i < 9; i++ { // crafter disabled-slot mask
			b.disabled[i] = saved.Disabled&(1<<uint(i)) != 0
		}
		out[pos] = b
	}
	return out
}

// recordBins replaces the in-memory bin snapshot (no write).
func (s *containerStore) recordBins(bins map[simPos]*bin) {
	snap := map[string]savedBin{}
	for pos, b := range bins {
		sb := savedBin{Size: len(b.slots)}
		for i, st := range b.slots {
			if st.item != 0 && st.count > 0 {
				sb.Slots = append(sb.Slots, slotRow(i, st))
			}
		}
		for i := 0; i < 9; i++ { // crafter disabled-slot mask
			if b.disabled[i] {
				sb.Disabled |= 1 << uint(i)
			}
		}
		snap[simKey(pos)] = sb
	}
	s.mu.Lock()
	s.m.Bins = snap
	s.mu.Unlock()
}

// recordChests replaces the in-memory chest snapshot (no write).
func (s *containerStore) recordChests(chests map[simPos]*chest) {
	snap := map[string][]containerRow{}
	for pos, c := range chests {
		var rows []containerRow
		for i, st := range c.slots {
			if st.item != 0 && st.count > 0 {
				rows = append(rows, slotRow(i, st))
			}
		}
		snap[simKey(pos)] = rows
	}
	s.mu.Lock()
	s.m.Chests = snap
	s.mu.Unlock()
}

// recordFurnaces replaces the in-memory furnace snapshot (no write).
func (s *containerStore) recordFurnaces(furnaces map[simPos]*furnace) {
	snap := map[string]savedFurnace{}
	for pos, f := range furnaces {
		sf := savedFurnace{BurnLeft: f.burnLeft, BurnMax: f.burnMax, Cook: f.cook, CookMax: f.cookMax}
		for i, st := range f.slots {
			sf.Slots[i] = [3]int32{st.item, int32(st.count), int32(st.dmg)}
		}
		snap[simKey(pos)] = sf
	}
	s.mu.Lock()
	s.m.Furnaces = snap
	s.mu.Unlock()
}

// migrateItemIDs rewrites every saved item id (furnaces, chests, bins, dropped
// items) through remap — one-time id-space migration after a canonical bump.
// Returns the count changed; item 0 (empty) is skipped.
func (s *containerStore) migrateItemIDs(remap func(int32) int32) int {
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
	for k, f := range s.m.Furnaces { // savedFurnace is a value copy → write back
		for i := range f.Slots {
			mig(&f.Slots[i][0])
		}
		s.m.Furnaces[k] = f
	}
	for _, rows := range s.m.Chests { // slice shares backing → in place
		for i := range rows {
			mig(&rows[i][1])
		}
	}
	for _, b := range s.m.Bins { // b.Slots slice shares backing → in place
		for i := range b.Slots {
			mig(&b.Slots[i][1])
		}
	}
	for i := range s.m.Items {
		mig(&s.m.Items[i].Item)
	}
	return n
}

// flush writes the table to disk atomically.
func (s *containerStore) flush() {
	s.mu.Lock()
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return
	}
	writeStore(path, data)
}

// recordPaintings snapshots the live paintings for the next flush.
func (s *containerStore) recordPaintings(paintings map[int32]*painting) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Paintings = s.m.Paintings[:0]
	for _, pt := range paintings {
		s.m.Paintings = append(s.m.Paintings, savedPainting{
			Dim: pt.dim, X: pt.x, Y: pt.y, Z: pt.z, Dir: pt.dir, Variant: pt.variant})
	}
}

// recordJukeboxes snapshots jukebox discs (playback clocks reset on boot).
func (s *containerStore) recordJukeboxes(jbs map[simPos]*jukebox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Jukeboxes = map[string]stackRow{}
	for pos, jb := range jbs {
		s.m.Jukeboxes[simKey(pos)] = packStack(jb.disc)
	}
}

// loadJukeboxes restores held discs (not playing until re-inserted).
func (s *containerStore) loadJukeboxes() map[simPos]*jukebox {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]*jukebox{}
	for key, row := range s.m.Jukeboxes {
		if pos, ok := parseSimKey(key); ok {
			out[pos] = &jukebox{disc: unpackStack(row)}
		}
	}
	return out
}

// recordBeacons snapshots each beacon's chosen powers (menu encoding:
// mob_effect id + 1, 0 = none). The pyramid tier is recomputed live.
func (s *containerStore) recordBeacons(bs map[simPos]*beacon) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Beacons = map[string][2]int32{}
	for pos, b := range bs {
		s.m.Beacons[simKey(pos)] = [2]int32{b.primary, b.secondary}
	}
}

// loadBeacons re-attaches saved powers to the beacons already rebuilt from
// the world edits (a saved row without a live beacon is a stale ghost).
func (s *containerStore) loadBeacons(bs map[simPos]*beacon) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, row := range s.m.Beacons {
		if pos, ok := parseSimKey(key); ok {
			if b := bs[pos]; b != nil {
				b.primary, b.secondary = row[0], row[1]
			}
		}
	}
}

// recordFrames snapshots the live item frames for the next flush.
func (s *containerStore) recordFrames(frames map[int32]*itemFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Frames = s.m.Frames[:0]
	for _, f := range frames {
		s.m.Frames = append(s.m.Frames, savedFrame{
			Dim: f.dim, X: f.x, Y: f.y, Z: f.z, Dir: f.dir, Glow: f.glow,
			Rot: f.rot, Item: packStack(f.held)})
	}
}

// loadFrames reconstructs placed item frames; entity ids are re-allocated
// by the caller.
func (s *containerStore) loadFrames(alloc func() int32) map[int32]*itemFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int32]*itemFrame{}
	for _, sf := range s.m.Frames {
		eid := alloc()
		out[eid] = &itemFrame{eid: eid, x: sf.X, y: sf.Y, z: sf.Z, dim: sf.Dim,
			dir: sf.Dir, glow: sf.Glow, rot: sf.Rot, held: unpackStack(sf.Item)}
	}
	return out
}

// loadPaintings reconstructs placed paintings; sizes come from the variant
// table and entity ids are re-allocated by the caller.
func (s *containerStore) loadPaintings(alloc func() int32) map[int32]*painting {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int32]*painting{}
	for _, sp := range s.m.Paintings {
		for _, v := range paintingVariants {
			if v.Name == sp.Variant {
				eid := alloc()
				out[eid] = &painting{eid: eid, x: sp.X, y: sp.Y, z: sp.Z,
					dim: sp.Dim, dir: sp.Dir, variant: v.Name, w: v.W, h: v.H}
				break
			}
		}
	}
	return out
}

// recordBoxes replaces the in-memory shulker-box snapshot (no write). The
// contents of a box that is currently an ITEM have nowhere else to live —
// a placed box stores under its position like any chest.
func (s *containerStore) recordBoxes(boxes map[int32]*chest, next int32) {
	snap := map[string][]containerRow{}
	for id, c := range boxes {
		var rows []containerRow
		for i, st := range c.slots {
			if st.item != 0 && st.count > 0 {
				rows = append(rows, slotRow(i, st))
			}
		}
		if len(rows) > 0 {
			snap[strconv.Itoa(int(id))] = rows
		}
	}
	s.mu.Lock()
	s.m.Boxes, s.m.NextBoxID = snap, next
	s.mu.Unlock()
}

// loadBoxes reconstructs shulker-box contents and the id counter.
func (s *containerStore) loadBoxes() (map[int32]*chest, int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int32]*chest{}
	for k, saved := range s.m.Boxes {
		id, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		c := &chest{}
		for _, e := range saved {
			if i, st := rowStack(e); i >= 0 && i < 27 {
				c.slots[i] = st
			}
		}
		out[int32(id)] = c
	}
	return out, s.m.NextBoxID
}

// recordHiveItems replaces the in-memory snapshot of Silk-Touched hive items —
// the bees and honey a broken hive carries until it is placed again.
func (s *containerStore) recordHiveItems(stows map[int32]hiveStow, next int32) {
	snap := map[string]hiveStow{}
	for id, st := range stows {
		snap[strconv.Itoa(int(id))] = st
	}
	s.mu.Lock()
	s.m.HiveItems, s.m.NextHiveID = snap, next
	s.mu.Unlock()
}

// loadHiveItems reconstructs carried-hive contents and the id counter.
func (s *containerStore) loadHiveItems() (map[int32]hiveStow, int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int32]hiveStow{}
	for k, saved := range s.m.HiveItems {
		if id, err := strconv.Atoi(k); err == nil {
			out[int32(id)] = saved
		}
	}
	return out, s.m.NextHiveID
}

// recordVaults snapshots who each vault has already paid.
func (s *containerStore) recordVaults(vaults map[blockPos]*vaultRecord) {
	snap := map[string]savedVault{}
	for pos, v := range vaults {
		if len(v.rewarded) == 0 {
			continue // nothing to remember; the pose comes off the block
		}
		snap[posKey(pos)] = savedVault{Rewarded: hexUUIDs(v.rewarded)}
	}
	s.mu.Lock()
	s.m.Vaults = snap
	s.mu.Unlock()
}

// loadVaults reconstructs the claim ledgers. The vault's kind and pose are
// filled in by vaultAt from the block itself.
func (s *containerStore) loadVaults() map[blockPos]*vaultRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]*vaultRecord{}
	for k, sv := range s.m.Vaults {
		pos, ok := parsePosKey(k)
		if !ok {
			continue
		}
		out[pos] = &vaultRecord{pos: pos, rewarded: parseHexUUIDs(sv.Rewarded)}
	}
	return out
}

// recordTrials snapshots trial-spawner progress, converting the cooldown
// deadline to ticks remaining (see savedTrial).
func (s *containerStore) recordTrials(trials map[blockPos]*trialSpawner, now uint64) {
	snap := map[string]savedTrial{}
	for pos, ts := range trials {
		left := uint64(0)
		if ts.cooldown > now {
			left = ts.cooldown - now
		}
		if ts.state == trialWaitingPlayers && left == 0 && ts.spawned == 0 && len(ts.detected) == 0 && !ts.ominous {
			continue // a sleeping spawner is indistinguishable from an unvisited one
		}
		snap[posKey(pos)] = savedTrial{
			State:        uint8(ts.state),
			CooldownLeft: left,
			Spawned:      ts.spawned,
			Detected:     hexUUIDSet(ts.detected),
			Ominous:      ts.ominous,
		}
	}
	s.mu.Lock()
	s.m.Trials = snap
	s.mu.Unlock()
}

// loadTrials reconstructs spawner progress, rebasing the cooldown onto the
// current world age. kind/ominous come from worldgen when trialAt first sees
// the spawner again.
func (s *containerStore) loadTrials(now uint64) map[blockPos]*trialSpawner {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]*trialSpawner{}
	for k, st := range s.m.Trials {
		pos, ok := parsePosKey(k)
		if !ok {
			continue
		}
		ts := &trialSpawner{
			pos: pos, state: trialState(st.State), spawned: st.Spawned, ominous: st.Ominous,
			detected: map[[16]byte]bool{}, current: map[int32]bool{},
		}
		if st.CooldownLeft > 0 {
			ts.cooldown = now + st.CooldownLeft
		}
		for _, u := range parseHexUUIDs(st.Detected) {
			ts.detected[u] = true
		}
		// A fight cannot resume: its mobs were saved as ordinary mobs and no
		// longer answer to this spawner. Anything mid-round falls back to the
		// waiting pose, which is where vanilla's own reload lands once its
		// current_mobs fail to resolve.
		if ts.state == trialActive || ts.state == trialWaitingEjection || ts.state == trialEjecting {
			ts.state, ts.spawned = trialWaitingPlayers, 0
		}
		out[pos] = ts
	}
	return out
}

// hexUUIDs / parseHexUUIDs move ordered UUID lists to and from JSON.
func hexUUIDs(us [][16]byte) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, hex.EncodeToString(u[:]))
	}
	return out
}

// hexUUIDSet is the same for an unordered set, sorted so the file is stable.
func hexUUIDSet(us map[[16]byte]bool) []string {
	out := make([]string, 0, len(us))
	for u := range us {
		out = append(out, hex.EncodeToString(u[:]))
	}
	sort.Strings(out)
	return out
}

func parseHexUUIDs(ss []string) [][16]byte {
	out := make([][16]byte, 0, len(ss))
	for _, s := range ss {
		b, err := hex.DecodeString(s)
		if err != nil || len(b) != 16 {
			continue
		}
		var u [16]byte
		copy(u[:], b)
		out = append(out, u)
	}
	return out
}

// recordConduits replaces the in-memory conduit snapshot (no write).
func (s *containerStore) recordConduits(conduits map[simPos]bool) {
	out := make([]string, 0, len(conduits))
	for k := range conduits {
		out = append(out, strconv.Itoa(k.dim)+","+posKey(k.blockPos))
	}
	sort.Strings(out) // stable on disk, so an unchanged world writes an identical file
	s.mu.Lock()
	s.m.Conduits = out
	s.mu.Unlock()
}

// loadConduits reconstructs the placed-conduit registry.
func (s *containerStore) loadConduits() map[simPos]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]bool{}
	for _, k := range s.m.Conduits {
		dim, rest, ok := strings.Cut(k, ",")
		if !ok {
			continue
		}
		d, err := strconv.Atoi(dim)
		if err != nil {
			continue
		}
		if pos, ok := parsePosKey(rest); ok {
			out[simPos{dim: d, blockPos: pos}] = true
		}
	}
	return out
}

// recordBundles snapshots every bundle's contents for the save file.
func (s *containerStore) recordBundles(bs *bundleStore) {
	if bs == nil {
		return
	}
	bs.mu.Lock()
	snap := make(map[string][]stackRow, len(bs.items))
	for id, items := range bs.items {
		rows := make([]stackRow, 0, len(items))
		for _, st := range items {
			if st.item != 0 && st.count > 0 {
				rows = append(rows, packStack(st))
			}
		}
		if len(rows) > 0 {
			snap[strconv.Itoa(int(id))] = rows
		}
	}
	last := bs.lastID
	bs.mu.Unlock()

	s.mu.Lock()
	s.m.Bundles, s.m.NextBundleID = snap, last
	s.mu.Unlock()
}

// loadBundles rebuilds the bundle store from the save file.
func (s *containerStore) loadBundles() *bundleStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := newBundleStore()
	for k, rows := range s.m.Bundles {
		id, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		items := make([]invStack, 0, len(rows))
		for _, r := range rows {
			items = append(items, unpackStack(r))
		}
		out.items[int32(id)] = items
	}
	out.lastID = s.m.NextBundleID
	return out
}
