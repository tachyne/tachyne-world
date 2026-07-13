package server

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Campfires cook without a menu (vanilla): right-click a LIT campfire with a
// cookable item to lay it on one of four slots; each cooks for the recipe's
// 600 ticks and pops into the world. Unlit fires decay progress instead
// (vanilla -2/tick). The visible food rides the block entity's Items tag:
// live changes go out as CampfireItems frames, chunk loads read the mutex'd
// campfireStore (the sign-store pattern — chunk builders read off-hub).
// Overworld-only, like the other block sims.

var (
	campfireMin     = worldgen.BlockBase("campfire") // facing(4) x lit x signal_fire x waterlogged
	campfireMax     = worldgen.BlockBase("campfire") + 31
	soulCampfireMin = worldgen.BlockBase("soul_campfire")
	soulCampfireMax = worldgen.BlockBase("soul_campfire") + 31
)

func isCampfireBlock(s uint32) bool {
	return (s >= campfireMin && s <= campfireMax) || (s >= soulCampfireMin && s <= soulCampfireMax)
}

func isSoulCampfire(s uint32) bool { return s >= soulCampfireMin && s <= soulCampfireMax }

// itemNameOf reverse-maps item ids to registry names once (the campfire
// frame and block-entity NBT carry names).
var itemNameOf = func() map[int32]string {
	m := make(map[int32]string, len(itemByName))
	for name, id := range itemByName {
		m[int32(id)] = name
	}
	return m
}()

// campfire is one campfire's hub-owned cook state.
type campfire struct {
	items [4]int32
	prog  [4]int
	total [4]int
}

func (cf *campfire) empty() bool {
	for _, it := range cf.items {
		if it != 0 {
			return false
		}
	}
	return true
}

// campfireItems is the client-visible view (item registry names, "" = empty).
type campfireItems struct {
	Items [4]string `json:"items"`
}

// campfireStore holds the render view + persistence (campfires.json). Like
// the sign store, it is mutex-owned: attach chunk builders read it while the
// hub writes.
type campfireStore struct {
	mu    sync.Mutex
	path  string
	m     map[string]campfireItems
	dirty bool
}

func newCampfireStore(path string) *campfireStore {
	s := &campfireStore{path: path, m: map[string]campfireItems{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

func (s *campfireStore) get(x, y, z int) (campfireItems, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[posKey(blockPos{x, y, z})]
	return d, ok
}

func (s *campfireStore) set(pos blockPos, d campfireItems) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[posKey(pos)] = d
	s.dirty = true
}

func (s *campfireStore) remove(pos blockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := posKey(pos)
	if _, ok := s.m[k]; ok {
		delete(s.m, k)
		s.dirty = true
	}
}

// snapshot copies the stored entries (boot-time hub map rebuild).
func (s *campfireStore) snapshot() map[string]campfireItems {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]campfireItems, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

func (s *campfireStore) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s.m, "", " ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if os.Rename(tmp, s.path) == nil {
		s.dirty = false
	}
}

// loadCampfires rebuilds the hub cook map from the persisted item view
// (progress restarts, like furnace viewers and jukebox clocks).
func (h *hub) loadCampfires() {
	for key, d := range h.cfStore.snapshot() {
		pos, ok := parsePosKey(key)
		if !ok {
			continue
		}
		cf := &campfire{}
		for i, name := range d.Items {
			id := int32(itemByName[strings.TrimPrefix(name, "minecraft:")])
			if name == "" || id == 0 {
				continue
			}
			if rec, ok := campfireResult[id]; ok {
				cf.items[i], cf.total[i] = id, rec.Cook
			}
		}
		if !cf.empty() {
			h.campfires[pos] = cf
		}
	}
}

type evCampfireAdd struct {
	eid     int32
	x, y, z int
}

func (evCampfireAdd) isHubEvent() {}

// onCampfireAdd lays the held cookable on the first free slot.
func (h *hub) onCampfireAdd(players map[int32]*tracked, e evCampfireAdd) {
	t := players[e.eid]
	if t == nil || t.inv == nil || t.dim != 0 {
		return
	}
	state := h.world.At(e.x, e.y, e.z)
	if !isCampfireBlock(state) || !boolProp(state, "lit") {
		return
	}
	held := t.inv.slots[t.p.heldSlot()]
	rec, ok := campfireResult[held.item]
	if !ok || held.count <= 0 {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	cf := h.campfires[pos]
	if cf == nil {
		cf = &campfire{}
		h.campfires[pos] = cf
	}
	slot := -1
	for i, it := range cf.items {
		if it == 0 {
			slot = i
			break
		}
	}
	if slot < 0 {
		return // all four slots busy
	}
	cf.items[slot], cf.prog[slot], cf.total[slot] = held.item, 0, rec.Cook
	if t.gamemode != gmCreative {
		s := &t.inv.slots[t.p.heldSlot()]
		if s.count--; s.count <= 0 {
			*s = invStack{}
		}
		h.sendSlot(t, t.p.heldSlot())
	}
	h.campfireSync(players, pos, cf)
	h.incCustom(t, "interact_with_campfire", 1)
}

// campfireSync refreshes the store (chunk builders + persistence) and pushes
// the live block-entity update to nearby viewers.
func (h *hub) campfireSync(players map[int32]*tracked, pos blockPos, cf *campfire) {
	var names [4]string
	for i, it := range cf.items {
		if it != 0 {
			names[i] = "minecraft:" + itemNameOf[it]
		}
	}
	h.cfStore.set(pos, campfireItems{Items: names})
	h.toNearbyEv(players, 0, float64(pos.x), float64(pos.z), attachproto.CampfireItems{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Items: names})
}

// campfireTick advances every laid item one tick while its fire is lit
// (vanilla cookTick); unlit fires decay progress at -2 (cooldownTick). Done
// items pop into the world — there is no output slot.
func (h *hub) campfireTick(players map[int32]*tracked) {
	for pos, cf := range h.campfires {
		state := h.world.At(pos.x, pos.y, pos.z)
		if !isCampfireBlock(state) {
			delete(h.campfires, pos) // spillCampfire handles drops; this is the fallback
			h.cfStore.remove(pos)
			continue
		}
		lit := boolProp(state, "lit")
		changed := false
		for i := range cf.items {
			if cf.items[i] == 0 {
				continue
			}
			if !lit {
				if cf.prog[i] > 0 {
					cf.prog[i] -= 2
					if cf.prog[i] < 0 {
						cf.prog[i] = 0
					}
				}
				continue
			}
			if cf.prog[i]++; cf.prog[i] >= cf.total[i] {
				if rec, ok := campfireResult[cf.items[i]]; ok {
					h.spawnItem(players, rec.Out, 1,
						float64(pos.x)+0.5, float64(pos.y)+1, float64(pos.z)+0.5)
				}
				cf.items[i], cf.prog[i] = 0, 0
				changed = true
			}
		}
		if changed {
			h.campfireSync(players, pos, cf)
		}
		if cf.empty() {
			delete(h.campfires, pos)
		}
	}
}

// spillCampfire drops the raw food when the campfire block is broken.
func (h *hub) spillCampfire(players map[int32]*tracked, x, y, z int, newState uint32) {
	if isCampfireBlock(newState) {
		return
	}
	pos := blockPos{x, y, z}
	h.cfStore.remove(pos)
	cf := h.campfires[pos]
	if cf == nil {
		return
	}
	delete(h.campfires, pos)
	for _, it := range cf.items {
		if it != 0 {
			h.spawnItem(players, it, 1, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
		}
	}
}
