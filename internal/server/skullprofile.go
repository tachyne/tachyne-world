package server

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/render770"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Player heads and their owners. A player_head stack can carry a profile
// component (ResolvableProfile: a name, a UUID, and the profile's
// properties — the "textures" one is the skin). Placing the head copies it
// into the skull's block entity (SkullBlockEntity.applyImplicitComponents),
// whose update tag — the profile, as its map codec writes it — is what the
// client draws the face from; breaking the head puts it back on the drop
// (the player_head loot table's copy_components).
//
// Vanilla's server never resolves a skull's profile: a name-only or id-only
// profile is a dynamic one, and the client looks the skin up itself. The
// engine does the same and keeps the profile exactly as it was given.

// componentProfile is the profile component's canonical id.
const componentProfile = 61

var itemPlayerHead = int32(itemByName["player_head"])

// isPlayerHeadState reports whether a block is a player head, standing or
// on a wall.
func isPlayerHeadState(s uint32) bool {
	name, ok := worldgen.StateName(s)
	return ok && (name == "player_head" || name == "player_wall_head")
}

// A profile rides invStack and the skull store as its canonical stream form
// (1.21.5's ResolvableProfile.STREAM_CODEC: optional name, optional UUID,
// the property map) in a string, so invStack stays comparable.

// appendProfile writes a profile in that form.
func appendProfile(b []byte, p gameProfile) []byte {
	b = protocol.AppendBool(b, p.name != "")
	if p.name != "" {
		b = protocol.AppendString(b, p.name)
	}
	b = protocol.AppendBool(b, p.id != [16]byte{})
	if p.id != [16]byte{} {
		b = append(b, p.id[:]...)
	}
	b = protocol.AppendVarInt(b, int32(len(p.props)))
	for _, pr := range p.props {
		b = protocol.AppendString(b, pr.Name)
		b = protocol.AppendString(b, pr.Value)
		b = protocol.AppendBool(b, pr.Signature != "")
		if pr.Signature != "" {
			b = protocol.AppendString(b, pr.Signature)
		}
	}
	return b
}

// readProfile reads one profile in that form. The limits are the codec's:
// a name of at most 16 characters, at most 16 properties.
func readProfile(r *bytes.Reader) (gameProfile, bool) {
	var p gameProfile
	has, err := r.ReadByte()
	if err != nil {
		return p, false
	}
	if has != 0 {
		if p.name, err = protocol.ReadString(r); err != nil || len(p.name) > 16 {
			return p, false
		}
	}
	if has, err = r.ReadByte(); err != nil {
		return p, false
	}
	if has != 0 {
		if _, err := io.ReadFull(r, p.id[:]); err != nil {
			return p, false
		}
	}
	n, err := protocol.ReadVarInt(r)
	if err != nil || n < 0 || n > 16 {
		return p, false
	}
	for i := int32(0); i < n; i++ {
		var pr skinProperty
		if pr.Name, err = protocol.ReadString(r); err != nil {
			return p, false
		}
		if pr.Value, err = protocol.ReadString(r); err != nil {
			return p, false
		}
		if has, err = r.ReadByte(); err != nil {
			return p, false
		}
		if has != 0 {
			if pr.Signature, err = protocol.ReadString(r); err != nil {
				return p, false
			}
		}
		p.props = append(p.props, pr)
	}
	return p, true
}

// profileString is a profile as the string a stack or a skull keeps.
func profileString(p gameProfile) string { return string(appendProfile(nil, p)) }

// profileOf reads a kept profile back; false for none.
func profileOf(s string) (gameProfile, bool) {
	if s == "" {
		return gameProfile{}, false
	}
	r := bytes.NewReader([]byte(s))
	p, ok := readProfile(r)
	return p, ok && r.Len() == 0
}

// profileKey / profileFromKey are a kept profile's persisted form: the
// stream bytes are not text, and the save files are JSON.
func profileKey(s string) string {
	if s == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func profileFromKey(k string) string {
	if k == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		return ""
	}
	if _, ok := profileOf(string(b)); !ok {
		return ""
	}
	return string(b)
}

// profileEv is a kept profile as the attach frames carry it.
func profileEv(s string) *attachproto.GameProfile {
	p, ok := profileOf(s)
	if !ok {
		return nil
	}
	out := &attachproto.GameProfile{Name: p.name, UUID: p.id}
	for _, pr := range p.props {
		out.Properties = append(out.Properties, attachproto.Property{Name: pr.Name, Value: pr.Value, Signature: pr.Signature})
	}
	return out
}

// profileFromSNBT reads the profile component as a command writes it: a
// bare player name, or a compound with name, id (a UUID as four ints, or
// its string form) and properties. The skin-patch fields a 26.x profile may
// carry (texture, cape, elytra, model) the engine does not keep.
func profileFromSNBT(v any) (gameProfile, bool) {
	validName := func(s string) bool {
		return len(s) <= 16 && strings.IndexFunc(s, func(r rune) bool { return r <= ' ' || r == 127 }) < 0
	}
	switch x := v.(type) {
	case string:
		if x == "" || !validName(x) {
			return gameProfile{}, false
		}
		return gameProfile{name: x}, true
	case map[string]any:
		var p gameProfile
		if n, ok := x["name"]; ok {
			s, isStr := n.(string)
			if !isStr || !validName(s) {
				return gameProfile{}, false
			}
			p.name = s
		}
		switch id := x["id"].(type) {
		case nil:
		case string:
			u, ok := parseUUIDString(id)
			if !ok {
				return gameProfile{}, false
			}
			p.id = u
		case []any:
			if len(id) != 4 {
				return gameProfile{}, false
			}
			for i, e := range id {
				n, ok := snbtInt(e)
				if !ok {
					return gameProfile{}, false
				}
				u := uint32(int32(n))
				p.id[i*4], p.id[i*4+1], p.id[i*4+2], p.id[i*4+3] = byte(u>>24), byte(u>>16), byte(u>>8), byte(u)
			}
		default:
			return gameProfile{}, false
		}
		if props, ok := x["properties"]; ok {
			list, isList := props.([]any)
			if !isList || len(list) > 16 {
				return gameProfile{}, false
			}
			for _, e := range list {
				m, isMap := e.(map[string]any)
				if !isMap {
					return gameProfile{}, false
				}
				name, ok1 := m["name"].(string)
				value, ok2 := m["value"].(string)
				if !ok1 || !ok2 {
					return gameProfile{}, false
				}
				sig, _ := m["signature"].(string)
				p.props = append(p.props, skinProperty{Name: name, Value: value, Signature: sig})
			}
		}
		return p, true
	}
	return gameProfile{}, false
}

// skullStore holds the owners of placed player heads, keyed by simKey. It
// is guarded because the chunk block-entity section is composed on the
// parallel chunk workers (appendBlockEntities).
type skullStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newSkullStore() *skullStore { return &skullStore{m: map[string]string{}} }

func (s *skullStore) get(pos simPos) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[simKey(pos)]
}

// set records a head's owner; "" forgets it.
func (s *skullStore) set(pos simPos, profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if profile == "" {
		delete(s.m, simKey(pos))
		return
	}
	s.m[simKey(pos)] = profile
}

// remove forgets an owner.
func (s *skullStore) remove(pos simPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, simKey(pos))
}

// take removes and returns an owner.
func (s *skullStore) take(pos simPos) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := simKey(pos)
	p := s.m[k]
	delete(s.m, k)
	return p
}

// snapshot is what the save file records: each owner in its persisted form.
func (s *skullStore) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.m))
	for k, v := range s.m {
		out[k] = profileKey(v)
	}
	return out
}

func (s *skullStore) restore(m map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]string, len(m))
	for k, v := range m {
		if p := profileFromKey(v); p != "" {
			s.m[k] = p
		}
	}
}

// skullDisplayEv is a head's block-entity update: its owner.
func skullDisplayEv(pos blockPos, profile string) attachproto.BlockDisplay {
	ev := blockDisplayEv(pos, attachproto.DisplaySkull, "", 0)
	ev.Profile = profileEv(profile)
	return ev
}

// ownSkullFromStack is a placed head taking the stack's profile: the block
// entity keeps it, and every viewer is sent the update that draws the face.
func (h *hub) ownSkullFromStack(players map[int32]*tracked, pos simPos, state uint32, st invStack) {
	if h.skulls == nil || !isPlayerHeadState(state) {
		return
	}
	h.skulls.set(pos, st.profile)
	if st.profile != "" {
		h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), skullDisplayEv(pos.blockPos, st.profile))
	}
}

// holdSkullProfile runs as a head is removed: its owner leaves the store
// and is held for the drop that follows the removal (spawnBlockDrop).
func (h *hub) holdSkullProfile(pos simPos, old, now uint32) {
	if h.skulls == nil || !isPlayerHeadState(old) || sameBlockKind(old, now) {
		return
	}
	if p := h.skulls.take(pos); p != "" {
		h.lastSkullPos, h.lastSkullProfile = pos, p
	}
}

// takeHeldSkullProfile is the held owner for a drop of item at pos, when the
// drop is the head itself.
func (h *hub) takeHeldSkullProfile(pos simPos, item int32) string {
	if item != itemPlayerHead || h.lastSkullProfile == "" || h.lastSkullPos != pos {
		return ""
	}
	p := h.lastSkullProfile
	h.lastSkullPos, h.lastSkullProfile = simPos{}, ""
	return p
}

// skullUpdateTagNBT is SkullBlockEntity's update tag for a chunk's
// block-entity section: the profile, in the codec's map form.
func skullUpdateTagNBT(b []byte, profile string) []byte {
	ev := profileEv(profile)
	if ev == nil {
		return append(b, 0x00) // TAG_End: a plain head
	}
	b = append(b, protocol.NBTRoot()...)
	b = render770.AppendProfileNBT(protocol.NBTCompound(b, "profile"), *ev)
	return protocol.NBTEnd(b)
}
