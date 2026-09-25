package server

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /posteffect (26.3): shader passes on a player's screen. They belong to the
// player (ServerPlayer.postEffects, saved with them) and are sent whole
// whenever they change and when the player joins.

// postEffectStore persists each player's list, keyed like the other player
// stores.
type postEffectStore struct {
	mu   sync.Mutex
	path string
	m    map[string][]string
}

func postEffectsPathFor(spawnPath string) string {
	if spawnPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(spawnPath), "posteffects.json")
}

func newPostEffectStore(path string) *postEffectStore {
	s := &postEffectStore{path: path, m: map[string][]string{}}
	if path != "" {
		if err := loadStore(path, &s.m); err != nil {
			log.Fatal(err)
		}
	}
	return s
}

func (s *postEffectStore) get(key string) []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.m[key]...)
}

func (s *postEffectStore) set(key string, list []string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if len(list) == 0 {
		delete(s.m, key)
	} else {
		s.m[key] = list
	}
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path != "" {
		writeStore(path, data)
	}
}

func (h *hub) sendPostEffects(t *tracked) {
	t.p.trySendEv(attachproto.PostEffects{Effects: h.postFX.get(t.p.key())})
}

func (s *Server) cmdPostEffect(p *player, args []string) {
	if !s.isOp(p.name) { // PostEffectCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		if msg := s.hub.postEffectCommand(players, p, args); msg != "" {
			cmdFail(p, msg)
		}
	})
}

const postEffectUsage = "Usage: /posteffect add|remove <targets> <posteffect> | clear <targets> | list <target>"

func (h *hub) postEffectCommand(players map[int32]*tracked, p *player, args []string) string {
	if len(args) < 2 {
		return postEffectUsage
	}
	verb := strings.ToLower(args[0])
	targets := h.commandTargets(players, p.eid, args[1])
	if len(targets) == 0 {
		return "No player was found"
	}
	id := ""
	if verb == "add" || verb == "remove" {
		if len(args) != 3 {
			return postEffectUsage
		}
		id = args[2]
		if !strings.Contains(id, ":") {
			id = "minecraft:" + id
		}
	}
	changed := []*tracked{}
	for _, t := range targets {
		cur := h.postFX.get(t.p.key())
		has := -1
		for i, e := range cur {
			if e == id {
				has = i
			}
		}
		switch verb {
		case "add":
			if has >= 0 {
				continue
			}
			cur = append(cur, id)
		case "remove":
			if has < 0 {
				continue
			}
			cur = append(cur[:has], cur[has+1:]...)
		case "clear":
			if len(cur) == 0 {
				continue
			}
			cur = nil
		case "list":
			if len(cur) == 0 {
				h.cmdSuccess(players, p, fmt.Sprintf("Player %s does not have any post effects", t.p.name), false)
			} else {
				h.cmdSuccess(players, p, fmt.Sprintf("Player %s has %d post effects: %s", t.p.name, len(cur), strings.Join(cur, ", ")), false)
			}
			return ""
		default:
			return postEffectUsage
		}
		h.postFX.set(t.p.key(), cur)
		h.sendPostEffects(t)
		changed = append(changed, t)
	}
	if len(changed) == 0 {
		switch verb {
		case "add":
			return "Player already has the specified post effect"
		case "remove":
			return "Player does not have the specified post effect"
		}
		return "Player does not have any post effects to remove"
	}
	single := len(changed) == 1
	var msg string
	switch verb {
	case "add":
		if single {
			msg = fmt.Sprintf("Added post effect %s to %s", id, changed[0].p.name)
		} else {
			msg = fmt.Sprintf("Added post effect %s to %d players", id, len(changed))
		}
	case "remove":
		if single {
			msg = fmt.Sprintf("Removed post effect %s from %s", id, changed[0].p.name)
		} else {
			msg = fmt.Sprintf("Removed post effect %s from %d players", id, len(changed))
		}
	default:
		if single {
			msg = "Removed all post effects from " + changed[0].p.name
		} else {
			msg = fmt.Sprintf("Removed all post effects from %d players", len(changed))
		}
	}
	h.cmdSuccess(players, p, msg, true)
	return ""
}
