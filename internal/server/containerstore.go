package server

import (
	"encoding/json"
	"fmt"
	"os"
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
	MapID int32    `json:"map_id,omitempty"`
	Pats  [6]int32 `json:"pats,omitempty"` // banner layers, patPlus1<<8|color
	Trim  int32    `json:"trim,omitempty"` // (mat+1)<<8|(pat+1)
	Book  int32    `json:"book,omitempty"` // book id
}

type containerFile struct {
	Furnaces  map[string]savedFurnace `json:"furnaces,omitempty"`
	Chests    map[string][][14]int32  `json:"chests,omitempty"` // (slot + the 12-col stack pack) — sparse; old shorter rows zero-fill
	Bins      map[string]savedBin     `json:"bins,omitempty"`   // dispenser/dropper/hopper
	Items     []savedItem             `json:"items,omitempty"`  // dropped item entities
	Paintings []savedPainting         `json:"paintings,omitempty"`
	Frames    []savedFrame            `json:"frames,omitempty"`
	Jukeboxes map[string][13]int32    `json:"jukeboxes,omitempty"`
	Beacons   map[string][2]int32     `json:"beacons,omitempty"` // chosen powers (mob_effect id+1; 0 = none)
	Stands    []savedStand            `json:"stands,omitempty"`  // placed armor stands
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
	Dim  int       `json:"dim,omitempty"`
	X    int       `json:"x"`
	Y    int       `json:"y"`
	Z    int       `json:"z"`
	Dir  int32     `json:"dir"`
	Glow bool      `json:"glow,omitempty"`
	Rot  int       `json:"rot,omitempty"`
	Item [13]int32 `json:"item"`
}

type savedBin struct {
	Size  int         `json:"size"`
	Slots [][14]int32 `json:"slots,omitempty"` // (slot + the 12-col stack pack)
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
	Dim   int          `json:"dim,omitempty"`
	X     float64      `json:"x"`
	Y     float64      `json:"y"`
	Z     float64      `json:"z"`
	Yaw   float32      `json:"yaw"`
	Equip [6][13]int32 `json:"equip"`
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

// slotRow packs a slot index + stack into a sparse container row.
func slotRow(i int, st invStack) [14]int32 {
	var r [14]int32
	r[0] = int32(i)
	p := packStack(st)
	copy(r[1:], p[:])
	return r
}

// rowStack unpacks a sparse container row (index, stack).
func rowStack(r [14]int32) (int, invStack) {
	var p [13]int32
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
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

// loadFurnaces reconstructs live furnace state from the last snapshot. Viewer
// and bar-sync fields are transient and start zeroed.
func (s *containerStore) loadFurnaces() map[blockPos]*furnace {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]*furnace{}
	for k, sf := range s.m.Furnaces {
		pos, ok := parsePosKey(k)
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
func (s *containerStore) loadChests() map[blockPos]*chest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]*chest{}
	for k, saved := range s.m.Chests {
		pos, ok := parsePosKey(k)
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

func (s *containerStore) loadItems() []savedItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Items
}

// loadBins reconstructs dispenser/dropper/hopper storage from the snapshot.
func (s *containerStore) loadBins() map[blockPos]*bin {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]*bin{}
	for k, saved := range s.m.Bins {
		pos, ok := parsePosKey(k)
		if !ok || saved.Size <= 0 || saved.Size > 27 {
			continue
		}
		b := &bin{slots: make([]invStack, saved.Size)}
		for _, e := range saved.Slots {
			if i, st := rowStack(e); i >= 0 && i < saved.Size {
				b.slots[i] = st
			}
		}
		out[pos] = b
	}
	return out
}

// recordBins replaces the in-memory bin snapshot (no write).
func (s *containerStore) recordBins(bins map[blockPos]*bin) {
	snap := map[string]savedBin{}
	for pos, b := range bins {
		sb := savedBin{Size: len(b.slots)}
		for i, st := range b.slots {
			if st.item != 0 && st.count > 0 {
				sb.Slots = append(sb.Slots, slotRow(i, st))
			}
		}
		snap[posKey(pos)] = sb
	}
	s.mu.Lock()
	s.m.Bins = snap
	s.mu.Unlock()
}

// recordChests replaces the in-memory chest snapshot (no write).
func (s *containerStore) recordChests(chests map[blockPos]*chest) {
	snap := map[string][][14]int32{}
	for pos, c := range chests {
		var rows [][14]int32
		for i, st := range c.slots {
			if st.item != 0 && st.count > 0 {
				rows = append(rows, slotRow(i, st))
			}
		}
		snap[posKey(pos)] = rows
	}
	s.mu.Lock()
	s.m.Chests = snap
	s.mu.Unlock()
}

// recordFurnaces replaces the in-memory furnace snapshot (no write).
func (s *containerStore) recordFurnaces(furnaces map[blockPos]*furnace) {
	snap := map[string]savedFurnace{}
	for pos, f := range furnaces {
		sf := savedFurnace{BurnLeft: f.burnLeft, BurnMax: f.burnMax, Cook: f.cook, CookMax: f.cookMax}
		for i, st := range f.slots {
			sf.Slots[i] = [3]int32{st.item, int32(st.count), int32(st.dmg)}
		}
		snap[posKey(pos)] = sf
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
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, path)
	}
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
func (s *containerStore) recordJukeboxes(jbs map[blockPos]*jukebox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Jukeboxes = map[string][13]int32{}
	for pos, jb := range jbs {
		s.m.Jukeboxes[posKey(pos)] = packStack(jb.disc)
	}
}

// loadJukeboxes restores held discs (not playing until re-inserted).
func (s *containerStore) loadJukeboxes() map[blockPos]*jukebox {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[blockPos]*jukebox{}
	for key, row := range s.m.Jukeboxes {
		if pos, ok := parsePosKey(key); ok {
			out[pos] = &jukebox{disc: unpackStack(row)}
		}
	}
	return out
}

// recordBeacons snapshots each beacon's chosen powers (menu encoding:
// mob_effect id + 1, 0 = none). The pyramid tier is recomputed live.
func (s *containerStore) recordBeacons(bs map[blockPos]*beacon) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Beacons = map[string][2]int32{}
	for pos, b := range bs {
		s.m.Beacons[posKey(pos)] = [2]int32{b.primary, b.secondary}
	}
}

// loadBeacons re-attaches saved powers to the beacons already rebuilt from
// the world edits (a saved row without a live beacon is a stale ghost).
func (s *containerStore) loadBeacons(bs map[blockPos]*beacon) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, row := range s.m.Beacons {
		if pos, ok := parsePosKey(key); ok {
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
