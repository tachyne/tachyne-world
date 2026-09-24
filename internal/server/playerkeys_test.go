package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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

// With nothing to go on, a name stays the key (a join claims it later); a
// UUID in any spelling keys as itself.
func TestPlayerKeyUnresolved(t *testing.T) {
	useIDs(t, newPlayerIDs())
	if got := ids.key("EdgeZA"); got != "EdgeZA" {
		t.Errorf("key(EdgeZA) = %s, want the name kept until a join claims it", got)
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
		"asananica81":                    "asananica81", // not in the map: left for the join to claim
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
	if _, ok := back["probe"]; !ok {
		t.Error("a name not in the map should stay as it is, for its player's join to claim")
	}
	if _, err := os.Stat(path + ".pre-uuid"); err != nil {
		t.Error("no .pre-uuid copy of the file as it was")
	}
}

// The join claims what was saved under the joining name, for whatever UUID
// the gateway gives the player: the offline UUID of a Java player in offline
// mode, or a Bedrock identity that has nothing to do with the name.
func TestJoinClaimsNameKeyedData(t *testing.T) {
	dir := t.TempDir()
	useIDs(t, loadPlayerIDs(dir))
	modes := filepath.Join(dir, "players.json")
	writeJSONFile(t, modes, map[string]int{"LegionZA": gmCreative, "EdgeZA1951": gmCreative})
	s := &Server{modes: newModeStore(modes, gmSurvival), hub: newHub(world.New(1))}
	invPath := filepath.Join(dir, "inventories.json")
	writeJSONFile(t, invPath, map[string]*savedInv{"EdgeZA1951": {XPLevel: 12}})
	s.hub.invs = newInvStore(invPath)

	offline, _ := parseUUIDString(offlineUUIDString("LegionZA"))
	java := newPlayer(1, "LegionZA", offline)
	ids.learn(java.name, java.uuid)
	s.claimLegacyData(java.name, java.key())
	if got := s.modes.get(java.key()); got != gmCreative {
		t.Errorf("LegionZA's saved game mode is %d after joining, want creative", got)
	}
	if got := s.modes.get("LegionZA"); got != gmCreative {
		t.Error("a name (an offline /gamemode target) no longer finds the player")
	}

	bedrock, _ := parseUUIDString(bedrockOld)
	br := newPlayer(2, "EdgeZA1951", bedrock)
	ids.learn(br.name, br.uuid)
	s.claimLegacyData(br.name, br.key())
	pl := testTracked()
	s.hub.invs.loadInto(pl, br.key())
	if pl.xpLevel != 12 || s.modes.get(br.key()) != gmCreative {
		t.Errorf("the Bedrock player's data did not follow them to their UUID (xp %d)", pl.xpLevel)
	}
	if _, err := os.Stat(invPath + ".pre-uuid"); err != nil {
		t.Error("claiming rewrote inventories.json without keeping the original")
	}
	// A second join claims nothing and keeps what the UUID already has.
	if s.hub.invs.claim("EdgeZA1951", br.key()) {
		t.Error("a claim ran twice")
	}
}

// Two keys for one player: the entry already keyed by UUID is kept.
func TestRekeyCollisionKeepsUUIDEntry(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "uuidmap.json"), uuidMap{Names: map[string]string{"EdgeZA": edgeReal}})
	useIDs(t, loadPlayerIDs(dir))
	got := rekeyPlayers("", map[string]int{"EdgeZA": 1, edgeReal: 2})
	if len(got) != 1 || got[edgeReal] != 2 {
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

// bedrockRig is a world whose stores hold EdgeZA1951's data the way the
// switch found it.
func bedrockRig(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	useIDs(t, loadPlayerIDs(dir))
	modes := filepath.Join(dir, "players.json")
	writeJSONFile(t, modes, map[string]int{"EdgeZA1951": gmCreative})
	s := &Server{modes: newModeStore(modes, gmSurvival), hub: newHub(world.New(1))}
	inv := filepath.Join(dir, "inventories.json")
	writeJSONFile(t, inv, map[string]*savedInv{"EdgeZA1951": {XPLevel: 7}})
	s.hub.invs = newInvStore(inv)
	return s, dir
}

// Floodgate's identity renames a Bedrock player (".EdgeZA1951", the XUID
// UUID): what was saved under the bare gamertag follows them.
func TestBedrockRenameClaimsNameKeyedData(t *testing.T) {
	s, _ := bedrockRig(t)
	u, _ := parseUUIDString(bedrockXUID)
	p := newPlayer(1, ".EdgeZA1951", u)
	ids.learn(p.name, p.uuid, "bedrock")
	s.claimLegacyData(p.name, p.key())
	s.claimBedrockRename(p.name, p.key())
	pl := testTracked()
	s.hub.invs.loadInto(pl, p.key())
	if pl.xpLevel != 7 || s.modes.get(p.key()) != gmCreative {
		t.Errorf("the renamed Bedrock player lost their data (xp %d, mode %d)", pl.xpLevel, s.modes.get(p.key()))
	}
}

// One who joined under the old identity since the switch (data and a pet
// under that UUID) keeps both after the rename.
func TestBedrockRenameFollowsTheOldUUID(t *testing.T) {
	s, dir := bedrockRig(t)
	oldU, _ := parseUUIDString(bedrockOld)
	before := newPlayer(1, "EdgeZA1951", oldU)
	ids.learn(before.name, before.uuid, "bedrock")
	s.claimLegacyData(before.name, before.key()) // their join under the old scheme
	h := s.hub
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	cat := h.spawnSpecies(players, entityCat, dimOverworld, 0.5, 180, 0.5)
	cat.tamed, cat.ownerUUID = true, oldU

	newU, _ := parseUUIDString(bedrockXUID)
	after := newPlayer(2, ".EdgeZA1951", newU)
	ids.learn(after.name, after.uuid, "bedrock")
	s.claimLegacyData(after.name, after.key())
	s.claimBedrockRename(after.name, after.key())
	pl := testTracked()
	h.invs.loadInto(pl, after.key())
	if pl.xpLevel != 7 || s.modes.get(after.key()) != gmCreative {
		t.Errorf("data under the old identity did not follow (xp %d)", pl.xpLevel)
	}
	tr := &tracked{p: after}
	h.resolvePetOwners(tr)
	if cat.owner != after.eid || cat.ownerUUID != newU {
		t.Errorf("the cat still belongs to %x", cat.ownerUUID)
	}
	if again := loadPlayerIDs(dir); uuidString(again.remapUUID(oldU)) != bedrockXUID {
		t.Error("the move was not recorded for pets still in unloaded chunks")
	}
}

// A Java player who joined with the bare name keeps it: a Bedrock ".Steve"
// takes nothing of Steve's.
func TestBedrockRenameLeavesJavaNamesAlone(t *testing.T) {
	dir := t.TempDir()
	useIDs(t, loadPlayerIDs(dir))
	modes := filepath.Join(dir, "players.json")
	writeJSONFile(t, modes, map[string]int{"Steve": gmCreative})
	s := &Server{modes: newModeStore(modes, gmSurvival), hub: newHub(world.New(1))}
	javaU, _ := parseUUIDString(offlineUUIDString("Steve"))
	java := newPlayer(1, "Steve", javaU)
	ids.learn(java.name, java.uuid, "java")
	s.claimLegacyData(java.name, java.key())

	bu, _ := parseUUIDString(bedrockXUID)
	br := newPlayer(2, ".Steve", bu)
	ids.learn(br.name, br.uuid, "bedrock")
	s.claimLegacyData(br.name, br.key())
	s.claimBedrockRename(br.name, br.key())
	if s.modes.get(java.key()) != gmCreative || s.modes.get(br.key()) == gmCreative {
		t.Error("the Bedrock .Steve took the Java Steve's game mode")
	}
}
