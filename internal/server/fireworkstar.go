package server

import (
	"sync"
	"sync/atomic"

	"github.com/tachyne/tachyne-common/protocol"
)

// Firework stars. A star is a crafted burst: the shape it opens in, the
// colours it opens with, the colours it fades to, and whether it trails or
// twinkles (FireworkExplosion). A rocket carries up to seven of them, which is
// what the sky actually shows; the paper-and-gunpowder rocket is the one with
// none.
//
// The burst list is variable-length, so — like bundles, books and shulker
// boxes — it cannot live on invStack (which stays comparable). The stack
// carries an id into this store instead, and the same id serves a star (one
// burst) and a rocket (one to seven).

var (
	itemFireworkStar = int32(itemByName["firework_star"])
	itemDiamond      = int32(itemByName["diamond"])
)

// Shape ids, FireworkExplosion.Shape's own numbering.
const (
	burstSmallBall = 0
	burstLargeBall = 1
	burstStar      = 2
	burstCreeper   = 3
	burstBurst     = 4

	maxRocketBursts = 7 // FireworkRocketRecipe: paper, gunpowder, up to 7 stars
	maxBurstColors  = 8 // a 3x3 grid cannot hold more dyes than this
)

// fireworkBurst is one FireworkExplosion. Exported fields: it is persisted.
type fireworkBurst struct {
	Shape   int8    `json:"shape,omitempty"`
	Colors  []int32 `json:"colors,omitempty"`
	Fade    []int32 `json:"fade,omitempty"`
	Trail   bool    `json:"trail,omitempty"`
	Twinkle bool    `json:"twinkle,omitempty"`
}

// starStore holds the bursts a star or rocket stack points at. Guarded and
// reachable through a global, because stackComponents composes the wire form
// from a free function.
type starStore struct {
	mu     sync.Mutex
	items  map[int32][]fireworkBurst
	byKey  map[string]int32 // identical bursts share an id (see intern)
	lastID int32
}

func newStarStore() *starStore { return &starStore{items: map[int32][]fireworkBurst{}} }

func (s *starStore) get(id int32) []fireworkBurst {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[id]
}

// intern returns the id for a burst list, minting one only the first time it
// is seen. Crafting asks for the id on every RESULT PREVIEW, not just on the
// craft, so minting blindly would hand out a fresh id each time the player
// nudged the grid and fill the save with orphans. Bursts are immutable once
// made, so sharing an id between identical stars is free.
func (s *starStore) intern(bursts []fireworkBurst) int32 {
	if len(bursts) == 0 {
		return 0
	}
	k := burstKey(bursts)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byKey == nil {
		s.byKey = map[string]int32{}
		for id, b := range s.items {
			s.byKey[burstKey(b)] = id
		}
	}
	if id, ok := s.byKey[k]; ok {
		return id
	}
	s.lastID++
	s.items[s.lastID] = bursts
	s.byKey[k] = s.lastID
	return s.lastID
}

// burstKey is a burst list's identity: the same shape, colours, fades and
// flags in the same order are the same star.
func burstKey(bursts []fireworkBurst) string {
	var b []byte
	for _, e := range bursts {
		b = append(b, byte(e.Shape), boolByte(e.Trail), boolByte(e.Twinkle), '|')
		for _, list := range [][]int32{e.Colors, e.Fade} {
			for _, c := range list {
				b = append(b, byte(c>>16), byte(c>>8), byte(c), ',')
			}
			b = append(b, ';')
		}
	}
	return string(b)
}

func (s *starStore) snapshot() (map[int32][]fireworkBurst, int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int32][]fireworkBurst, len(s.items))
	for id, b := range s.items {
		out[id] = b
	}
	return out, s.lastID
}

func (s *starStore) restore(items map[int32][]fireworkBurst, last int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = items
	if s.items == nil {
		s.items = map[int32][]fireworkBurst{}
	}
	s.byKey = nil // rebuilt on the next intern
	s.lastID = last
}

// globalStars lets stackComponents reach a stack's bursts.
var globalStars atomic.Pointer[starStore]

func (h *hub) initStars(ss *starStore) {
	h.stars = ss
	globalStars.Store(ss)
}

// burstsOf is the bursts a stack carries (nil for a plain rocket).
func burstsOf(st invStack) []fireworkBurst {
	if st.starID == 0 {
		return nil
	}
	ss := globalStars.Load()
	if ss == nil {
		return nil
	}
	return ss.get(st.starID)
}

// appendBurst writes one FireworkExplosion: shape, the colours it opens with,
// the colours it fades to, then the trail and twinkle flags. Colours are
// fixed-width ints, not varints.
func appendBurst(b []byte, e fireworkBurst) []byte {
	b = protocol.AppendVarInt(b, int32(e.Shape))
	for _, list := range [][]int32{e.Colors, e.Fade} {
		b = protocol.AppendVarInt(b, int32(len(list)))
		for _, c := range list {
			b = protocol.AppendI32(b, c)
		}
	}
	return append(b, boolByte(e.Trail), boolByte(e.Twinkle))
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
