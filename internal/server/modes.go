package server

import (
	"encoding/json"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"log"
	"sync"
)

// Game modes (the values the client and protocol use).
const (
	gmSurvival  = 0
	gmCreative  = 1
	gmAdventure = 2
	gmSpectator = 3
)

// mayBuild is Abilities.mayBuild, which GameType sets to
// !isBlockPlacingRestricted(): adventure and spectator may not change the
// world. Vanilla softens that for an item carrying a can_place_on or can_break
// list — a map-maker's tool that works on exactly the blocks it names — and
// the engine has no such component, so for us the restriction is absolute.
func mayBuild(mode int) bool { return mode == gmSurvival || mode == gmCreative }

// modeStore remembers each player's game mode by name so a mixed survival/
// creative server keeps who-is-who across restarts. Plain JSON so an admin can
// hand-edit who's creative. Unknown players get the server default.
type modeStore struct {
	mu   sync.Mutex
	path string
	def  int
	m    map[string]int
}

func newModeStore(path string, def int) *modeStore {
	s := &modeStore{path: path, def: def, m: map[string]int{}}
	if path != "" {
		if err := loadStore(path, &s.m); err != nil {
			log.Fatal(err)
		}
	}
	return s
}

// get returns name's stored mode, or the server default.
func (s *modeStore) get(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode, ok := s.m[name]; ok {
		return mode
	}
	return s.def
}

// set records name's mode and persists the table atomically.
func (s *modeStore) set(name string, mode int) {
	s.mu.Lock()
	s.m[name] = mode
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()

	if path != "" {
		writeStore(path, data)
	}
}

// abilitiesFor returns the Player Abilities flag byte that matches a game mode:
// 0x01 invulnerable, 0x02 flying, 0x04 may-fly, 0x08 instant-build.
func abilitiesFor(mode int) attachproto.Abilities {
	switch mode {
	case gmCreative:
		return attachproto.Abilities{Invulnerable: true, MayFly: true, Creative: true}
	case gmSpectator:
		return attachproto.Abilities{Invulnerable: true, Flying: true, MayFly: true}
	default: // survival, adventure
		return attachproto.Abilities{}
	}
}
