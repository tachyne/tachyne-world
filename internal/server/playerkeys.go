package server

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
)

// Player identity for persistence. Every per-player store (inventories,
// game modes, advancements, stats, the recipe book, spawn points) is keyed by
// the player's UUID, as vanilla keys playerdata, advancements and stats — so
// a player who renames their account keeps everything. The stores still
// accept a name wherever a caller only has one (a command naming an offline
// player): it resolves the way vanilla's GameProfileCache does, through the
// names this world has seen players join with (usercache.json).
//
// Files written before the switch are keyed by name; they convert on load.
// An operator moving the world to online mode drops a uuidmap.json beside
// it first:
//
//	{"names": {"EdgeZA": "430803e4-3068-442f-9f47-e0fd6ee57e3c"},
//	 "uuids": {"<old uuid>": "<new uuid>"}}
//
// Each name there moves to the account's real UUID, and so does the offline
// UUID that name used to play under (so pets and anything else already keyed
// by UUID follow); "uuids" moves any other identity (a Bedrock player whose
// UUID scheme changed). A name nobody can resolve stays as it is until a
// player joins with it: the join claims it for the UUID that player actually
// has, whatever the gateway's identity scheme (claimName).

// playerIDs resolves store keys. One per process: the stores are reached from
// the hub and from session goroutines alike, hence the lock.
type playerIDs struct {
	mu     sync.Mutex
	path   string                // usercache.json ("" = remember in memory only)
	byName map[string]cachedUser // lower-case name → the last profile seen with it
	names  map[string]string     // uuidmap "names": lower-case name → UUID
	remap  map[string]string     // uuidmap "uuids" (+ each name's offline UUID) → UUID
}

// cachedUser is one usercache.json entry.
type cachedUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// uuidMap is uuidmap.json.
type uuidMap struct {
	Names map[string]string `json:"names"`
	UUIDs map[string]string `json:"uuids"`
}

var ids = newPlayerIDs()

func newPlayerIDs() *playerIDs {
	return &playerIDs{byName: map[string]cachedUser{}, names: map[string]string{}, remap: map[string]string{}}
}

// loadPlayerIDs reads dir's usercache.json and uuidmap.json (both optional).
func loadPlayerIDs(dir string) *playerIDs {
	p := newPlayerIDs()
	if dir == "" {
		return p
	}
	p.path = dir + "/usercache.json"
	var cache []cachedUser
	if err := loadStore(p.path, &cache); err != nil {
		log.Fatal(err)
	}
	for _, u := range cache {
		if id, ok := normUUID(u.UUID); ok && u.Name != "" {
			p.byName[strings.ToLower(u.Name)] = cachedUser{Name: u.Name, UUID: id}
		}
	}
	var m uuidMap
	if err := loadStore(dir+"/uuidmap.json", &m); err != nil {
		log.Fatal(err)
	}
	for name, u := range m.Names {
		id, ok := normUUID(u)
		if !ok {
			log.Fatalf("uuidmap.json: %q is not a UUID (for %s)", u, name)
		}
		p.names[strings.ToLower(name)] = id
		if off := offlineUUIDString(name); off != id {
			p.remap[off] = id
		}
	}
	for from, to := range m.UUIDs {
		f, ok1 := normUUID(from)
		t, ok2 := normUUID(to)
		if !ok1 || !ok2 {
			log.Fatalf("uuidmap.json: bad uuid pair %q → %q", from, to)
		}
		p.remap[f] = t
	}
	if len(p.names)+len(p.remap) > 0 {
		log.Printf("uuidmap.json: %d name(s) and %d uuid(s) to move", len(p.names), len(p.remap))
	}
	return p
}

// key is the store key for a UUID or a name.
func (p *playerIDs) key(k string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := normUUID(k); ok {
		return p.moved(id)
	}
	if u, ok := p.byName[strings.ToLower(k)]; ok {
		return p.moved(u.UUID)
	}
	if id, ok := p.names[strings.ToLower(k)]; ok {
		return id
	}
	// Nobody this world knows: the name stays the key until a player joins
	// with it and claims the entry (claimName) for the UUID they really have.
	// Guessing here — the offline UUID — strands a Bedrock player, whose
	// UUID is not derived from the name.
	return k
}

// moved follows the uuid map (once: a map is a list of moves, not a chain).
func (p *playerIDs) moved(id string) string {
	if to, ok := p.remap[id]; ok {
		return to
	}
	return id
}

// remapUUID moves a raw UUID (a pet's owner) as the stores' keys move.
func (p *playerIDs) remapUUID(u [16]byte) [16]byte {
	if u == ([16]byte{}) {
		return u
	}
	p.mu.Lock()
	to, ok := p.remap[uuidString(u)]
	p.mu.Unlock()
	if !ok {
		return u
	}
	out, _ := parseUUIDString(to)
	return out
}

// learn records that name joined as uuid (usercache.json), so a command
// naming that player finds their data, whatever mode the world is in.
func (p *playerIDs) learn(name string, u [16]byte) {
	id := uuidString(u)
	p.mu.Lock()
	lower := strings.ToLower(name)
	if cur, ok := p.byName[lower]; ok && cur.UUID == id && cur.Name == name {
		p.mu.Unlock()
		return
	}
	p.byName[lower] = cachedUser{Name: name, UUID: id}
	var out []cachedUser
	for _, u := range p.byName {
		out = append(out, u)
	}
	path := p.path
	p.mu.Unlock()
	if path == "" {
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	data, _ := json.MarshalIndent(out, "", "  ")
	writeStore(path, data)
}

// rekeyPlayers converts a freshly loaded store's keys to UUIDs. Anything it
// changes, it changes once: the file as it was is kept beside it as
// <file>.pre-uuid (unless one is already there) before the store next writes.
// Two keys landing on one UUID keep the entry that was already keyed by it.
func rekeyPlayers[T any](path string, m map[string]T) map[string]T {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]T, len(m))
	from := map[string]string{}
	changed := 0
	for _, k := range keys {
		to := ids.key(k)
		if to != k {
			changed++
		}
		if prev, dup := from[to]; dup {
			if prev == to { // the UUID-keyed entry wins
				log.Printf("%s: %q and %q are one player (%s); keeping the entry already keyed by UUID", path, prev, k, to)
				continue
			}
			log.Printf("%s: %q and %q are one player (%s); keeping %q", path, prev, k, to, k)
		}
		out[to], from[to] = m[k], k
	}
	if changed > 0 {
		log.Printf("%s: %d player key(s) moved to UUIDs", path, changed)
		keepPreUUID(path)
	}
	return out
}

// keepPreUUID copies a store file to <file>.pre-uuid before the switch to
// UUID keys first changes it (once: an existing copy is the original).
func keepPreUUID(path string) {
	if path == "" {
		return
	}
	backup := path + ".pre-uuid"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if data, err := os.ReadFile(path); err == nil {
			writeStore(backup, data)
		}
	}
}

// uuidString is the dashed lower-case form stores are keyed by.
func uuidString(u [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// normUUID accepts a UUID with or without dashes, any case, and returns the
// store form.
func normUUID(s string) (string, bool) {
	u, ok := parseUUIDString(s)
	if !ok {
		return "", false
	}
	return uuidString(u), true
}

func parseUUIDString(s string) ([16]byte, bool) {
	var u [16]byte
	hex := strings.ReplaceAll(s, "-", "")
	if len(hex) != 32 || (len(s) != 32 && (len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-')) {
		return u, false
	}
	for i := 0; i < 16; i++ {
		hi, ok1 := hexNibble(hex[2*i])
		lo, ok2 := hexNibble(hex[2*i+1])
		if !ok1 || !ok2 {
			return u, false
		}
		u[i] = hi<<4 | lo
	}
	return u, true
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// offlineUUIDString is vanilla's offline-mode UUID for a name (UUIDv3 of
// "OfflinePlayer:<name>"), the one an offline gateway gives that player.
func offlineUUIDString(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return uuidString(sum)
}

// claimName is the join-time half of the switch: an entry still keyed by the
// name this player joined with (nobody could resolve it at load) moves to
// their UUID, unless they already have one. Case-insensitive, as vanilla
// matches names; a UUID-shaped key is never a name. The store's file is
// rewritten when anything moves. Reports whether it did.
func claimName[T any](mu *sync.Mutex, path string, m map[string]T, name, key string) bool {
	mu.Lock()
	if _, ok := m[key]; ok || name == key {
		mu.Unlock()
		return false
	}
	from := ""
	if _, ok := m[name]; ok {
		from = name
	} else {
		var cands []string
		for k := range m {
			if _, isID := normUUID(k); !isID && strings.EqualFold(k, name) {
				cands = append(cands, k)
			}
		}
		sort.Strings(cands)
		if len(cands) > 0 {
			from = cands[0]
		}
	}
	if from == "" {
		mu.Unlock()
		return false
	}
	m[key] = m[from]
	delete(m, from)
	data, _ := json.MarshalIndent(m, "", "  ")
	mu.Unlock()
	if path != "" {
		keepPreUUID(path)
		writeStore(path, data)
	}
	return true
}
