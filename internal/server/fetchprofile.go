package server

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /fetchprofile (FetchProfileCommand): resolve a game profile by name, by
// id, or from an entity, and answer with it as the clickable list vanilla
// sends — copy the profile component, give a head, summon a mannequin, copy
// the player sprite.
//
//	/fetchprofile name <name> | id <uuid> | entity <entity>
//
// Vanilla resolves names and ids through the session service (a Mojang
// lookup, cached). The engine makes no outbound lookups: identity comes in
// with the gateway, which verified it at login. So a profile resolves from
// what this world holds — an online player's full profile, skin textures
// and all (Identity.Props), or the name and UUID of anyone who has joined
// (usercache.json), without textures. Anyone else fails to resolve, as an
// unknown name does in vanilla.

// gameProfile is the profile a lookup found.
type gameProfile struct {
	id    [16]byte
	name  string
	props []skinProperty
}

func (s *Server) cmdFetchProfile(p *player, args []string) {
	if !s.isOp(p.name) { // FetchProfileCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /fetchprofile name <name> | id <uuid> | entity <entity>"
	if len(args) < 2 || (args[0] != "name" && len(args) != 2) {
		p.tell(usage)
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		hb := s.hub
		switch args[0] {
		case "name":
			name := strings.Join(args[1:], " ") // a greedy string
			prof, ok := hb.profileByName(players, name)
			arg := map[string]any{"text": name}
			if !ok {
				cmdFail(p, "Failed to resolve profile for name "+name)
				return
			}
			hb.reportProfile(p, "commands.fetchprofile.name.success", "Resolved profile for name %s: %s", name, arg, prof)
		case "id":
			u, ok := parseUUIDString(args[1])
			if !ok {
				p.tell("Invalid UUID")
				return
			}
			id := uuidString(u)
			prof, ok := hb.profileByID(players, u)
			if !ok {
				cmdFail(p, "Failed to resolve profile for ID "+id)
				return
			}
			hb.reportProfile(p, "commands.fetchprofile.id.success", "Resolved profile for ID %s: %s", id, map[string]any{"text": id}, prof)
		case "entity":
			es := hb.commandEntities(players, p.eid, args[1])
			switch {
			case len(es) == 0:
				cmdFail(p, "No entity was found")
				return
			case len(es) > 1:
				cmdFail(p, "Only one entity is allowed, but the provided selector allows more than one")
				return
			}
			e := es[0]
			if e.t == nil { // Avatar: players (and mannequins, which the engine does not run)
				cmdFail(p, fmt.Sprintf("Entity %s has no profile", e.name()))
				return
			}
			prof := gameProfile{id: e.t.p.uuid, name: e.t.p.name, props: e.t.p.props}
			hb.reportProfile(p, "commands.fetchprofile.entity.success", "Resolved profile for entity %s: %s", e.name(), map[string]any{"text": e.name()}, prof)
		default:
			p.tell(usage)
		}
	})
}

// profileByName is ProfileResolver.fetchByName over what this world knows:
// an online player first, then the name cache. StringUtil.isValidPlayerName
// gates it: at most 16 characters, none of them a space or control.
func (h *hub) profileByName(players map[int32]*tracked, name string) (gameProfile, bool) {
	if len(name) == 0 || len(name) > 16 || strings.IndexFunc(name, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
		return gameProfile{}, false
	}
	for _, t := range players {
		if strings.EqualFold(t.p.name, name) {
			return gameProfile{id: t.p.uuid, name: t.p.name, props: t.p.props}, true
		}
	}
	u, ok := ids.cached(name)
	if !ok {
		return gameProfile{}, false
	}
	id, ok := parseUUIDString(u.UUID)
	if !ok {
		return gameProfile{}, false
	}
	if prof, ok := h.profileByID(players, id); ok {
		return prof, true
	}
	return gameProfile{id: id, name: u.Name}, true
}

// profileByID is ProfileResolver.fetchById: an online player, or the name
// the cache last saw with that UUID.
func (h *hub) profileByID(players map[int32]*tracked, id [16]byte) (gameProfile, bool) {
	for _, t := range players {
		if t.p.uuid == id {
			return gameProfile{id: t.p.uuid, name: t.p.name, props: t.p.props}, true
		}
	}
	want := uuidString(id)
	ids.mu.Lock()
	defer ids.mu.Unlock()
	for _, u := range ids.byName {
		if u.UUID == want {
			return gameProfile{id: id, name: u.Name}, true
		}
	}
	return gameProfile{}, false
}

// profileSNBT is ResolvableProfile.CODEC's NBT, printed: the id as an int
// array, the name, and the properties.
func profileSNBT(p gameProfile) string {
	var b strings.Builder
	b.WriteString("{id:[I;")
	for i := 0; i < 4; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(int(int32(binary.BigEndian.Uint32(p.id[i*4:])))))
	}
	b.WriteString("],name:")
	b.WriteString(strconv.Quote(p.name))
	if len(p.props) > 0 {
		b.WriteString(",properties:[")
		for i, pr := range p.props {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "{name:%s,value:%s", strconv.Quote(pr.Name), strconv.Quote(pr.Value))
			if pr.Signature != "" {
				fmt.Fprintf(&b, ",signature:%s", strconv.Quote(pr.Signature))
			}
			b.WriteByte('}')
		}
		b.WriteByte(']')
	}
	b.WriteByte('}')
	return b.String()
}

// reportProfile is FetchProfileCommand.reportResolvedProfile: the message
// with the four green bracketed actions. The player sprite the last one
// shows (an object component) stands as the profile's name.
func (h *hub) reportProfile(p *player, key, english, plain string, arg map[string]any, prof gameProfile) {
	snbt := profileSNBT(prof)
	head := map[string]any{"text": prof.name, "color": "white"}
	action := func(c map[string]any, click map[string]any) any {
		c["color"] = "green"
		c["click_event"] = click
		return map[string]any{"translate": "chat.square_brackets", "with": []any{c}}
	}
	list := []any{
		action(map[string]any{"translate": "commands.fetchprofile.copy_component"},
			map[string]any{"action": "copy_to_clipboard", "value": snbt}),
		" ",
		action(map[string]any{"translate": "commands.fetchprofile.give_item"},
			map[string]any{"action": "run_command", "command": "give @s minecraft:player_head[profile=" + snbt + "]"}),
		" ",
		action(map[string]any{"translate": "commands.fetchprofile.summon_mannequin"},
			map[string]any{"action": "run_command", "command": "summon minecraft:mannequin ~ ~ ~ {profile:" + snbt + "}"}),
		" ",
		action(map[string]any{"translate": "commands.fetchprofile.copy_text", "with": []any{head}},
			map[string]any{"action": "copy_to_clipboard", "value": prof.name}),
	}
	comp := map[string]any{"translate": key, "with": []any{arg, map[string]any{"text": "", "extra": list}}}
	raw, err := json.Marshal(comp)
	if err != nil {
		cmdFail(p, "Failed to serialize profile: "+err.Error())
		return
	}
	text := fmt.Sprintf(english, plain, "[Copy Component] [Give Item] [Summon Mannequin] [Copy "+prof.name+"]")
	p.trySendEv(attachproto.Chat{Text: text, Component: raw})
}
