package server

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Placed banners: a loom-patterned banner keeps its layers when placed — the
// client renders them from the block entity's update tag. The mutex'd store
// (banners.json) follows the sign/campfire pattern: hub writes, attach chunk
// builders read. v1: a broken patterned banner drops plain (follow-up).

var bannerRanges = func() [][2]uint32 {
	var out [][2]uint32
	colors := []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}
	for _, c := range colors {
		if b := worldgen.BlockBase(c + "_banner"); b != 0 {
			out = append(out, [2]uint32{b, b + 15}) // rotation 0-15
		}
		if b := worldgen.BlockBase(c + "_wall_banner"); b != 0 {
			out = append(out, [2]uint32{b, b + 3}) // facing 4
		}
	}
	return out
}()

func isBannerState(s uint32) bool {
	for _, r := range bannerRanges {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}

type bannerStore struct {
	mu    sync.Mutex
	path  string
	m     map[string][]attachproto.BannerLayer
	dirty bool
}

func newBannerStore(path string) *bannerStore {
	s := &bannerStore{path: path, m: map[string][]attachproto.BannerLayer{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

func (s *bannerStore) get(x, y, z int) []attachproto.BannerLayer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[posKey(blockPos{x, y, z})]
}

func (s *bannerStore) set(pos blockPos, layers []attachproto.BannerLayer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[posKey(pos)] = layers
	s.dirty = true
}

func (s *bannerStore) remove(pos blockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := posKey(pos)
	if _, ok := s.m[k]; ok {
		delete(s.m, k)
		s.dirty = true
	}
}

func (s *bannerStore) flushIfDirty() {
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
	if os.WriteFile(tmp, data, 0o644) == nil && os.Rename(tmp, s.path) == nil {
		s.dirty = false
	}
}

// dyeName reverses the DyeColor enum for NBT.
var dyeName = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// bannersOnBlockChange records a patterned banner's layers at placement (the
// placer's held stack still carries them — consumption lands after evBlock)
// and clears the entry when the banner goes away. Overworld-only, like the
// other block sims.
func (h *hub) bannersOnBlockChange(players map[int32]*tracked, x, y, z int, state uint32, by int32) {
	pos := blockPos{x, y, z}
	if !isBannerState(state) {
		h.banners.remove(pos)
		return
	}
	t := players[by]
	if t == nil || t.inv == nil {
		return
	}
	held := t.inv.slots[t.p.heldSlot()]
	n := held.patCount()
	if !bannerItems[held.item] || n == 0 {
		return
	}
	layers := make([]attachproto.BannerLayer, 0, n)
	for _, l := range held.pats[:n] {
		name := protocol.BannerPatternName(int32(l.patPlus1) - 1)
		if name == "" || int(l.color) >= len(dyeName) {
			continue
		}
		layers = append(layers, attachproto.BannerLayer{Pattern: name, Color: dyeName[l.color]})
	}
	if len(layers) == 0 {
		return
	}
	h.banners.set(pos, layers)
	h.toNearbyEv(players, 0, float64(x), float64(z), attachproto.BannerPatterns{
		X: int32(x), Y: int32(y), Z: int32(z), Layers: layers})
}

// bannerColorName strips "minecraft:" for pattern names already qualified.
func bannerPatternQualified(name string) string {
	if strings.HasPrefix(name, "minecraft:") {
		return name
	}
	return "minecraft:" + name
}
