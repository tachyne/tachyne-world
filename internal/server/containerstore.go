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
	Dim   int     `json:"dim,omitempty"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Item  int32   `json:"item"`
	Count int     `json:"count"`
	Dmg   int     `json:"dmg,omitempty"`
	Ench  int32   `json:"ench,omitempty"`
}

type containerFile struct {
	Furnaces  map[string]savedFurnace `json:"furnaces,omitempty"`
	Chests    map[string][][5]int32   `json:"chests,omitempty"` // (slot,item,count,dmg,ench) — sparse; old 4-column rows load with ench 0
	Bins      map[string]savedBin     `json:"bins,omitempty"`   // dispenser/dropper/hopper
	Items     []savedItem             `json:"items,omitempty"`  // dropped item entities
	Paintings []savedPainting         `json:"paintings,omitempty"`
}

type savedPainting struct {
	Dim     int    `json:"dim,omitempty"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Z       int    `json:"z"`
	Dir     int32  `json:"dir"`
	Variant string `json:"variant"`
}

type savedBin struct {
	Size  int        `json:"size"`
	Slots [][5]int32 `json:"slots,omitempty"` // (slot,item,count,dmg,ench)
}

type savedFurnace struct {
	Slots    [3][3]int32 `json:"slots"` // (item,count,dmg) — input, fuel, output
	BurnLeft int         `json:"burnLeft,omitempty"`
	BurnMax  int         `json:"burnMax,omitempty"`
	Cook     int         `json:"cook,omitempty"`
	CookMax  int         `json:"cookMax,omitempty"`
}

func posKey(p blockPos) string { return fmt.Sprintf("%d,%d,%d", p.x, p.y, p.z) }

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
			if e[0] >= 0 && e[0] < 27 {
				c.slots[e[0]] = invStack{item: e[1], count: int(e[2]), dmg: int(e[3]), ench: unpackEnch(e[4])}
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
			if int(e[0]) >= 0 && int(e[0]) < saved.Size {
				b.slots[e[0]] = invStack{item: e[1], count: int(e[2]), dmg: int(e[3]), ench: unpackEnch(e[4])}
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
				sb.Slots = append(sb.Slots, [5]int32{int32(i), st.item, int32(st.count), int32(st.dmg), packEnch(st.ench)})
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
	snap := map[string][][5]int32{}
	for pos, c := range chests {
		var rows [][5]int32
		for i, st := range c.slots {
			if st.item != 0 && st.count > 0 {
				rows = append(rows, [5]int32{int32(i), st.item, int32(st.count), int32(st.dmg), packEnch(st.ench)})
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
