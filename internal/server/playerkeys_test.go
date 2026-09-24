package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// useIDs installs a resolver for one test and puts the old one back.
func useIDs(t *testing.T, p *playerIDs) {
	t.Helper()
	old := ids
	ids = p
	t.Cleanup(func() { ids = old })
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	edgeReal    = "430803e4-3068-442f-9f47-e0fd6ee57e3c"
	legionReal  = "a9671729-90dc-45e7-9126-d5076a793a1d"
	bedrockOld  = "11111111-2222-3333-4444-555555555555"
	bedrockXUID = "00000000-0000-0000-0009-01f2a3b4c5d6"
)

// Offline (no map): a name is its offline UUID, the one an offline gateway
// gives that player — the same player, before and after the switch.
func TestPlayerKeyOffline(t *testing.T) {
	useIDs(t, newPlayerIDs())
	if got, want := ids.key("EdgeZA"), offlineUUIDString("EdgeZA"); got != want {
		t.Errorf("key(EdgeZA) = %s, want the offline UUID %s", got, want)
	}
	if ids.key("EdgeZA") == ids.key("edgeza") {
		t.Error("offline UUIDs are case-sensitive, as vanilla's are")
	}
	if got := ids.key("430803E4306844 2F9F47E0FD6EE57E3C"); got == edgeReal {
		t.Error("a malformed UUID was taken as one")
	}
	if got := ids.key("430803E43068442F9F47E0FD6EE57E3C"); got != edgeReal {
		t.Errorf("an undashed upper-case UUID keys as %s", got)
	}
}

// With uuidmap.json: a name, and the offline UUID it played under, both move
// to the real account; any listed UUID moves too; a learned name resolves.
func TestPlayerKeyUUIDMap(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "uuidmap.json"), uuidMap{
		Names: map[string]string{"EdgeZA": edgeReal, "LegionZA": "a967172990dc45e79126d5076a793a1d"},
		UUIDs: map[string]string{bedrockOld: bedrockXUID},
	})
	useIDs(t, loadPlayerIDs(dir))
	for in, want := range map[string]string{
		"EdgeZA":                         edgeReal,
		"edgeza":                         edgeReal, // names in the map are matched as vanilla matches names
		offlineUUIDString("EdgeZA"):      edgeReal,
		"LegionZA":                       legionReal,
		bedrockOld:                       bedrockXUID,
		edgeReal:                         edgeReal,
		offlineUUIDString("asananica81"): offlineUUIDString("asananica81"),
		"asananica81":                    offlineUUIDString("asananica81"),
	} {
		if got := ids.key(in); got != want {
			t.Errorf("key(%s) = %s, want %s", in, got, want)
		}
	}
	u, _ := parseUUIDString("b5615a53-244f-4801-b70d-7660174a723a")
	ids.learn("asananica81", u)
	if got := ids.key("asananica81"); got != "b5615a53-244f-4801-b70d-7660174a723a" {
		t.Errorf("a learned name keys as %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "usercache.json")); err != nil {
		t.Error("learning a name did not persist usercache.json")
	}
	again := loadPlayerIDs(dir)
	if got := again.key("asananica81"); got != "b5615a53-244f-4801-b70d-7660174a723a" {
		t.Errorf("after a restart the learned name keys as %s", got)
	}
	var off [16]byte
	off, _ = parseUUIDString(offlineUUIDString("EdgeZA"))
	if got := uuidString(ids.remapUUID(off)); got != edgeReal {
		t.Errorf("a pet owned by EdgeZA's offline UUID now belongs to %s", got)
	}
}

// A name-keyed inventories.json loads for the player's real UUID once the
// map says who they are, is written back keyed by UUID, and the file as it
// was is kept as .pre-uuid.
func TestInvStoreMovesToUUIDs(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "uuidmap.json"), uuidMap{Names: map[string]string{"EdgeZA": edgeReal}})
	useIDs(t, loadPlayerIDs(dir))
	path := filepath.Join(dir, "inventories.json")
	writeJSONFile(t, path, map[string]*savedInv{
		"EdgeZA": {XPLevel: 30, Tags: []string{"builder"}},
		"probe":  {XPLevel: 1},
	})
	s := newInvStore(path)
	pl := testTracked()
	pl.p.uuid, _ = parseUUIDString(edgeReal)
	s.loadInto(pl, pl.p.key())
	if pl.xpLevel != 30 {
		t.Fatalf("EdgeZA's saved inventory did not load for their real UUID (xp %d)", pl.xpLevel)
	}
	s.record(pl.p.key(), pl)
	s.flush()
	var back map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &back)
	if _, ok := back[edgeReal]; !ok {
		t.Errorf("inventories.json keys after the move: %v, want %s", keysOf(back), edgeReal)
	}
	if _, ok := back["EdgeZA"]; ok {
		t.Error("the name key survived the move")
	}
	if _, ok := back[offlineUUIDString("probe")]; !ok {
		t.Error("a name not in the map should keep its offline UUID")
	}
	if _, err := os.Stat(path + ".pre-uuid"); err != nil {
		t.Error("no .pre-uuid copy of the file as it was")
	}
}

// Offline mode keeps working across the switch: a name-keyed file loads for
// a player an offline gateway identifies by its name-derived UUID.
func TestOfflinePlayerFindsNameKeyedData(t *testing.T) {
	dir := t.TempDir()
	useIDs(t, loadPlayerIDs(dir))
	path := filepath.Join(dir, "players.json")
	writeJSONFile(t, path, map[string]int{"LegionZA": gmCreative})
	s := newModeStore(path, gmSurvival)
	p := newPlayer(1, "LegionZA", [16]byte{})
	p.uuid, _ = parseUUIDString(offlineUUIDString("LegionZA"))
	if got := s.get(p.key()); got != gmCreative {
		t.Errorf("LegionZA's saved game mode is %d after the switch, want creative", got)
	}
	if got := s.get("LegionZA"); got != gmCreative {
		t.Error("a name (an offline /gamemode target) no longer finds the player")
	}
}

// Two keys for one player: the entry already keyed by UUID is kept.
func TestRekeyCollisionKeepsUUIDEntry(t *testing.T) {
	useIDs(t, newPlayerIDs())
	off := offlineUUIDString("EdgeZA")
	got := rekeyPlayers("", map[string]int{"EdgeZA": 1, off: 2})
	if len(got) != 1 || got[off] != 2 {
		t.Errorf("rekeyed %v, want only the UUID-keyed entry", got)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
