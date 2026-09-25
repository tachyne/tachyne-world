package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// untaggedInts collects the path of every integer the migrator does NOT
// rewrite: fields that are neither tagged nor inside a type it knows.
func untaggedInts(t reflect.Type, path string, seen map[reflect.Type]bool, out *[]string) {
	switch t {
	case stackRowType, containerRowType, potSherdsType:
		return
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		*out = append(*out, path)
	case reflect.Pointer, reflect.Array, reflect.Slice:
		untaggedInts(t.Elem(), path+"[]", seen, out)
	case reflect.Map:
		untaggedInts(t.Elem(), path+"{}", seen, out)
	case reflect.Struct:
		if seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if _, ok := f.Tag.Lookup("mig"); ok {
				continue
			}
			untaggedInts(f.Type, path+"."+f.Name, seen, out)
		}
	}
}

// notIDs is every integer in the migrated stores that is not a built-in
// registry id, reviewed field by field. A new integer field fails this test
// until it is either tagged `mig:"…"` (a built-in registry id — item, entity
// type, block state) or added here with the reason it is not one.
const (
	whyPos    = "coordinates, dimension or facing"
	whyCount  = "a count, timer, level or other quantity"
	whyRef    = "an instance id into another store, or a counter for one"
	whyEnch   = "enchantments: the engine's declared registry, stable by rule (append-only)"
	whyDecl   = "a registry the engine declares (banner pattern, trim, instrument, variant, wolf sound): stable by rule"
	whyEnum   = "an engine enum or table index (potion, stew, dye, firework shape, trial state)"
	whyEffect = "mob_effect: built-in but outside the translation tables; unmoved 1.21.5→26.3 — add an IDSpace and tag it if that changes"
	whyProf   = "villager_profession: built-in but outside the translation tables; unmoved 1.21.5→26.3"
	whyRgb    = "an RGB colour"
)

var notIDs = map[string]string{
	"map[string]*server.savedInv{}[].DeathDim":          whyPos,
	"map[string]*server.savedInv{}[].DeathPos[]":        whyPos,
	"map[string]*server.savedInv{}[].Dim":               whyPos,
	"map[string]*server.savedInv{}[].Effects[].Amp":     whyCount,
	"map[string]*server.savedInv{}[].Effects[].ID":      whyEffect,
	"map[string]*server.savedInv{}[].Effects[].Left":    whyCount,
	"map[string]*server.savedInv{}[].EnchSeed":          whyCount,
	"map[string]*server.savedInv{}[].WardenCool":        whyCount,
	"map[string]*server.savedInv{}[].WardenSince":       whyCount,
	"map[string]*server.savedInv{}[].WardenWarn":        whyCount,
	"map[string]*server.savedInv{}[].XPLevel":           whyCount,
	"map[string]*server.savedInv{}[].XPPoints":          whyCount,
	"server.containerFile.Beacons{}[]":                  whyEffect,
	"server.containerFile.Bins{}.Disabled":              whyCount,
	"server.containerFile.Bins{}.Size":                  whyCount,
	"server.containerFile.Brews{}.BrewTime":             whyCount,
	"server.containerFile.Brews{}.Fuel":                 whyCount,
	"server.containerFile.Falling[].Dim":                whyPos,
	"server.containerFile.Falling[].HurtMax":            whyCount,
	"server.containerFile.Falling[].Time":               whyCount,
	"server.containerFile.Frames[].Dim":                 whyPos,
	"server.containerFile.Frames[].Dir":                 whyPos,
	"server.containerFile.Frames[].Rot":                 whyPos,
	"server.containerFile.Frames[].X":                   whyPos,
	"server.containerFile.Frames[].Y":                   whyPos,
	"server.containerFile.Frames[].Z":                   whyPos,
	"server.containerFile.Furnaces{}.BurnLeft":          whyCount,
	"server.containerFile.Furnaces{}.BurnMax":           whyCount,
	"server.containerFile.Furnaces{}.Cook":              whyCount,
	"server.containerFile.Furnaces{}.CookMax":           whyCount,
	"server.containerFile.HiveItems{}.Honey":            whyCount,
	"server.containerFile.HiveItems{}.Occ[].Flower[][]": whyPos,
	"server.containerFile.HiveItems{}.Occ[].SecsLeft":   whyCount,
	"server.containerFile.Items[].Age":                  whyCount,
	"server.containerFile.Items[].Book":                 whyRef,
	"server.containerFile.Items[].Box":                  whyRef,
	"server.containerFile.Items[].Bundle":               whyRef,
	"server.containerFile.Items[].Count":                whyCount,
	"server.containerFile.Items[].Dim":                  whyPos,
	"server.containerFile.Items[].Dmg":                  whyCount,
	"server.containerFile.Items[].Ench":                 whyEnch,
	"server.containerFile.Items[].Ench2":                whyEnch,
	"server.containerFile.Items[].Ench3":                whyEnch,
	"server.containerFile.Items[].Ench4":                whyEnch,
	"server.containerFile.Items[].Hive":                 whyRef,
	"server.containerFile.Items[].Instr":                whyDecl,
	"server.containerFile.Items[].Lode[]":               whyPos,
	"server.containerFile.Items[].MapID":                whyRef,
	"server.containerFile.Items[].Pats[]":               whyDecl,
	"server.containerFile.Items[].Potion":               whyEnum,
	"server.containerFile.Items[].Repair":               whyCount,
	"server.containerFile.Items[].Shield":               whyEnum,
	"server.containerFile.Items[].Stew":                 whyEnum,
	"server.containerFile.Items[].Trim":                 whyDecl,
	"server.containerFile.Lecterns{}.Page":              whyCount,
	"server.containerFile.NextBoxID":                    whyRef,
	"server.containerFile.NextBundleID":                 whyRef,
	"server.containerFile.NextHiveID":                   whyRef,
	"server.containerFile.NextNameID":                   whyRef,
	"server.containerFile.NextStarID":                   whyRef,
	"server.containerFile.Paintings[].Dim":              whyPos,
	"server.containerFile.Paintings[].Dir":              whyPos,
	"server.containerFile.Paintings[].X":                whyPos,
	"server.containerFile.Paintings[].Y":                whyPos,
	"server.containerFile.Paintings[].Z":                whyPos,
	"server.containerFile.ShelfLast{}":                  whyCount,
	"server.containerFile.Stands[].Dim":                 whyPos,
	"server.containerFile.Stars{}[].Colors[]":           whyRgb,
	"server.containerFile.Stars{}[].Fade[]":             whyRgb,
	"server.containerFile.Stars{}[].Shape":              whyEnum,
	"server.containerFile.Trials{}.CooldownLeft":        whyCount,
	"server.containerFile.Trials{}.Spawned":             whyCount,
	"server.containerFile.Trials{}.State":               whyEnum,
	"server.containerFile.Vehicles[].Dim":               whyPos,
	"server.containerFile.Vehicles[].Fuel":              whyCount,
	"server.containerFile.Vehicles[].Fuse":              whyCount,
	"server.containerFile.Vehicles[].LootPos[]":         whyPos,
	"server.mobFile.Bastions[][]":                       whyPos,
	"server.mobFile.Chunks{}[].Anger":                   whyCount,
	"server.mobFile.Chunks{}[].Bed[]":                   whyPos,
	"server.mobFile.Chunks{}[].BeeFlower[][]":           whyPos,
	"server.mobFile.Chunks{}[].BeeHive[][]":             whyPos,
	"server.mobFile.Chunks{}[].BeeNoNectar":             whyCount,
	"server.mobFile.Chunks{}[].BreedCD":                 whyCount,
	"server.mobFile.Chunks{}[].Collar":                  whyEnum,
	"server.mobFile.Chunks{}[].Color":                   whyEnum,
	"server.mobFile.Chunks{}[].Converting":              whyCount,
	"server.mobFile.Chunks{}[].CubeFuse":                whyCount,
	"server.mobFile.Chunks{}[].CubeMaxFuse":             whyCount,
	"server.mobFile.Chunks{}[].CubePickup":              whyCount,
	"server.mobFile.Chunks{}[].Dim":                     whyPos,
	"server.mobFile.Chunks{}[].DupCD":                   whyCount,
	"server.mobFile.Chunks{}[].EID":                     whyRef,
	"server.mobFile.Chunks{}[].EggIn":                   whyCount,
	"server.mobFile.Chunks{}[].Food":                    whyCount,
	"server.mobFile.Chunks{}[].Gossip{}[]":              whyCount,
	"server.mobFile.Chunks{}[].GrowLeft":                whyCount,
	"server.mobFile.Chunks{}[].Health":                  whyCount,
	"server.mobFile.Chunks{}[].Home[]":                  whyPos,
	"server.mobFile.Chunks{}[].HornsGone":               whyCount,
	"server.mobFile.Chunks{}[].LastSlept":               whyCount,
	"server.mobFile.Chunks{}[].LastStock":               whyCount,
	"server.mobFile.Chunks{}[].LeashPos[][]":            whyPos,
	"server.mobFile.Chunks{}[].Lifetime":                whyCount,
	"server.mobFile.Chunks{}[].LoveTicks":               whyCount,
	"server.mobFile.Chunks{}[].Max":                     whyCount,
	"server.mobFile.Chunks{}[].Meet[]":                  whyPos,
	"server.mobFile.Chunks{}[].Mount":                   whyRef,
	"server.mobFile.Chunks{}[].Offers[].C2N":            whyCount,
	"server.mobFile.Chunks{}[].Offers[].Color":          whyRgb,
	"server.mobFile.Chunks{}[].Offers[].Demand":         whyCount,
	"server.mobFile.Chunks{}[].Offers[].Ench[]":         whyEnch,
	"server.mobFile.Chunks{}[].Offers[].InN":            whyCount,
	"server.mobFile.Chunks{}[].Offers[].MapID":          whyRef,
	"server.mobFile.Chunks{}[].Offers[].MaxUses":        whyCount,
	"server.mobFile.Chunks{}[].Offers[].Mult":           whyCount,
	"server.mobFile.Chunks{}[].Offers[].OutN":           whyCount,
	"server.mobFile.Chunks{}[].Offers[].Potion":         whyEnum,
	"server.mobFile.Chunks{}[].Offers[].Stew":           whyEnum,
	"server.mobFile.Chunks{}[].Offers[].Uses":           whyCount,
	"server.mobFile.Chunks{}[].Offers[].XP":             whyCount,
	"server.mobFile.Chunks{}[].Overworld":               whyCount,
	"server.mobFile.Chunks{}[].Oxidation":               whyCount,
	"server.mobFile.Chunks{}[].PoseTick":                whyCount,
	"server.mobFile.Chunks{}[].Profession":              whyProf,
	"server.mobFile.Chunks{}[].Raid[]":                  whyPos,
	"server.mobFile.Chunks{}[].RaidWave":                whyCount,
	"server.mobFile.Chunks{}[].RavRoar":                 whyCount,
	"server.mobFile.Chunks{}[].Restrict[]":              whyPos,
	"server.mobFile.Chunks{}[].RestrictR":               whyCount,
	"server.mobFile.Chunks{}[].RavStun":                 whyCount,
	"server.mobFile.Chunks{}[].Restocks":                whyCount,
	"server.mobFile.Chunks{}[].Size":                    whyCount,
	"server.mobFile.Chunks{}[].SniffCD":                 whyCount,
	"server.mobFile.Chunks{}[].SoundSet":                whyDecl,
	"server.mobFile.Chunks{}[].Stew":                    whyEnum,
	"server.mobFile.Chunks{}[].Strength":                whyCount,
	"server.mobFile.Chunks{}[].TadpoleAge":              whyCount,
	"server.mobFile.Chunks{}[].TradeLevel":              whyCount,
	"server.mobFile.Chunks{}[].TradeXP":                 whyCount,
	"server.mobFile.Chunks{}[].TraderDespawn":           whyCount,
	"server.mobFile.Chunks{}[].Variant":                 whyDecl,
	"server.mobFile.Chunks{}[].WanderTarget[][]":        whyPos,
	"server.mobFile.Chunks{}[].Work[]":                  whyPos,
	"server.mobFile.EndCities[][]":                      whyPos,
	"server.mobFile.Huts[][]":                           whyPos,
	"server.mobFile.Mansions[][]":                       whyPos,
	"server.mobFile.OceanRuins[][]":                     whyPos,
	"server.mobFile.Raids[].Center[]":                   whyPos,
	"server.mobFile.Raids[].NumGroups":                  whyCount,
	"server.mobFile.Raids[].Omen":                       whyCount,
	"server.mobFile.Raids[].Spawned":                    whyCount,
	"server.mobFile.Raids[].Wave":                       whyCount,
	"server.mobFile.Seeded[][]":                         whyPos,
	"server.mobFile.Villages[][]":                       whyPos,
}

// A parrot riding a player's shoulder is a saved mob inside the player's
// row: every mob field is classified there exactly as it is in mobs.json.
func init() {
	const mobRow, shoulderRow = "server.mobFile.Chunks{}[].", "map[string]*server.savedInv{}[].Shoulders[][]."
	for p, why := range notIDs {
		if strings.HasPrefix(p, mobRow) {
			notIDs[shoulderRow+strings.TrimPrefix(p, mobRow)] = why
		}
	}
}

func TestContentMigratorClassifiesEveryInt(t *testing.T) {
	var got []string
	for _, root := range []any{map[string]*savedInv{}, containerFile{}, mobFile{}} {
		ty := reflect.TypeOf(root)
		untaggedInts(ty, ty.String(), map[reflect.Type]bool{}, &got)
	}
	sort.Strings(got)
	var want []string
	for p := range notIDs {
		want = append(want, p)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unclassified integer fields in the stores; tag each built-in registry id with mig, list the rest in notIDs:\n%s",
			strings.Join(got, "\n"))
	}
}

// movers picks n distinct canonical ids of reg that exist in 1.21.5 under a
// different id, so a fixture built from them proves the migration moved it.
func movers(t *testing.T, reg protocol.IDSpace, n int, candidates []int32) []int32 {
	t.Helper()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	var out []int32
	for _, c := range candidates {
		if c != 0 && protocol.IDPresent(reg, 770, c) && protocol.RemapID(reg, 770, c) != c {
			out = append(out, c)
			if len(out) == n {
				return out
			}
		}
	}
	t.Fatalf("registry %d: only %d of %d ids move between 1.21.5 and canonical", reg, len(out), n)
	return nil
}

func old(reg protocol.IDSpace, id int32) int32 { return protocol.RemapID(reg, 770, id) }

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateContentFrom1215 saves ids in 1.21.5's numbering everywhere the
// stores keep them — including every place the old per-field migration
// missed (ender chest, pot sherds, bundles, shelves, jukeboxes, brews, mobs,
// statistics) — and expects every one back in canonical numbering, with the
// counts, damage and other columns untouched.
func TestMigrateContentFrom1215(t *testing.T) {
	var items, ents, customs, blocks []int32
	for _, id := range itemByName {
		items = append(items, id)
	}
	for _, id := range entityByName {
		ents = append(ents, int32(id))
	}
	for _, id := range customStatID {
		customs = append(customs, id)
	}
	for id := int32(1); id < 1100; id++ {
		blocks = append(blocks, id)
	}
	it := movers(t, protocol.RegItem, 12, items)
	en := movers(t, protocol.RegEntity, 1, ents)[0]
	cu := movers(t, protocol.RegCustomStat, 1, customs)
	bl := movers(t, protocol.RegBlock, 1, blocks)[0]
	var states []int32
	for s := int32(1); s < 20000; s += 97 {
		states = append(states, s)
	}
	st := movers(t, protocol.RegBlockState, 1, states)[0]

	row := func(item int32) stackRow {
		var r stackRow
		r[0], r[1], r[2], r[3] = old(protocol.RegItem, item), 7, 11, 0x05030000
		return r
	}
	dir := t.TempDir()
	files := contentFiles{
		Inventories: filepath.Join(dir, "inventories.json"),
		Containers:  filepath.Join(dir, "containers.json"),
		Mobs:        filepath.Join(dir, "mobs.json"),
		Stats:       filepath.Join(dir, "stats.json"),
	}

	inv := &savedInv{}
	inv.Slots[0], inv.Ender[26] = row(it[0]), row(it[1])
	inv.Slots[1][32] = old(protocol.RegItem, it[2]) // a pot's back sherd
	writeJSON(t, files.Inventories, map[string]*savedInv{"Legion": inv})

	var cr containerRow
	cr[0] = 4
	chest := row(it[3])
	copy(cr[1:], chest[:])
	writeJSON(t, files.Containers, containerFile{
		Chests:    map[string][]containerRow{"1,2,3": {cr}},
		Bundles:   map[string][]stackRow{"9": {row(it[4])}},
		Shelves:   map[string][6]stackRow{"1,2,3": {5: row(it[5])}},
		Jukeboxes: map[string]stackRow{"1,2,3": row(it[6])},
		PotSherds: map[string]potSherds{"1,2,3": {3: old(protocol.RegItem, it[7])}},
		Brews:     map[string]savedBrew{"1,2,3": {Ing: old(protocol.RegItem, it[8])}},
		Furnaces:  map[string]savedFurnace{"1,2,3": {Slots: [3][3]int32{2: {old(protocol.RegItem, it[9]), 5, 0}}}},
		Items:     []savedItem{{Item: old(protocol.RegItem, it[10]), Count: 3}},
	})

	writeJSON(t, files.Mobs, mobFile{Chunks: map[string][]savedMob{"0,0": {{
		Etype: int(old(protocol.RegEntity, en)), CarriedBlk: uint32(old(protocol.RegBlockState, st)),
		Held: old(protocol.RegItem, it[11]), Gear: [4]stackRow{2: row(it[0])},
		Offers: []savedOffer{{In: old(protocol.RegItem, it[1]), InN: 6, Out: old(protocol.RegItem, it[2])}},
	}}}})

	key := func(t, k int32) string { return strconv.Itoa(int(t)) + ":" + strconv.Itoa(int(k)) }
	writeJSON(t, files.Stats, map[string]map[string]int32{"Legion": {
		key(0, old(protocol.RegBlock, bl)):         40,
		key(1, old(protocol.RegItem, it[3])):       2,
		key(6, old(protocol.RegEntity, en)):        9,
		key(8, old(protocol.RegCustomStat, cu[0])): 1234,
	}})

	n, err := migrateContent(files, "1.21.5", "1.21.11")
	if err != nil {
		t.Fatal(err)
	}
	if n < 20 {
		t.Errorf("migrated %d ids, want at least 20", n)
	}

	var gotInv map[string]*savedInv
	readJSON(t, files.Inventories, &gotInv)
	g := gotInv["Legion"]
	if g.Slots[0][0] != it[0] || g.Ender[26][0] != it[1] || g.Slots[1][32] != it[2] {
		t.Errorf("inventory ids = %d %d %d, want %d %d %d", g.Slots[0][0], g.Ender[26][0], g.Slots[1][32], it[0], it[1], it[2])
	}
	if g.Slots[0][1] != 7 || g.Slots[0][2] != 11 || g.Slots[0][3] != 0x05030000 {
		t.Errorf("non-id columns changed: %v", g.Slots[0][:4])
	}

	var c containerFile
	readJSON(t, files.Containers, &c)
	for name, pair := range map[string][2]int32{
		"chest":      {c.Chests["1,2,3"][0][1], it[3]},
		"chest slot": {c.Chests["1,2,3"][0][0], 4},
		"bundle":     {c.Bundles["9"][0][0], it[4]},
		"shelf":      {c.Shelves["1,2,3"][5][0], it[5]},
		"jukebox":    {c.Jukeboxes["1,2,3"][0], it[6]},
		"sherd":      {c.PotSherds["1,2,3"][3], it[7]},
		"brew":       {c.Brews["1,2,3"].Ing, it[8]},
		"furnace":    {c.Furnaces["1,2,3"].Slots[2][0], it[9]},
		"dropped":    {c.Items[0].Item, it[10]},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d, want %d", name, pair[0], pair[1])
		}
	}

	var m mobFile
	readJSON(t, files.Mobs, &m)
	sm := m.Chunks["0,0"][0]
	if int32(sm.Etype) != en || sm.CarriedBlk != uint32(st) || sm.Held != it[11] || sm.Gear[2][0] != it[0] {
		t.Errorf("mob = type %d blk %d held %d gear %d, want %d %d %d %d", sm.Etype, sm.CarriedBlk, sm.Held, sm.Gear[2][0], en, st, it[11], it[0])
	}
	if o := sm.Offers[0]; o.In != it[1] || o.Out != it[2] || o.InN != 6 {
		t.Errorf("offer = %+v, want in %d out %d", o, it[1], it[2])
	}

	var s map[string]map[string]int32
	readJSON(t, files.Stats, &s)
	want := map[string]int32{key(0, bl): 40, key(1, it[3]): 2, key(6, en): 9, key(8, cu[0]): 1234}
	if !reflect.DeepEqual(s["Legion"], want) {
		t.Errorf("stats = %v, want %v", s["Legion"], want)
	}

	for _, p := range []string{files.Inventories, files.Containers, files.Mobs, files.Stats} {
		if _, err := os.Stat(p + ".pre-1.21.11"); err != nil {
			t.Errorf("no backup of %s: %v", filepath.Base(p), err)
		}
	}
}

// TestMigrateContentWritesNothingOnFailure: a store that does not decode
// stops the migration before any file is touched — no rewrite, no backup.
func TestMigrateContentWritesNothingOnFailure(t *testing.T) {
	dir := t.TempDir()
	files := contentFiles{
		Inventories: filepath.Join(dir, "inventories.json"),
		Stats:       filepath.Join(dir, "stats.json"),
	}
	inv := &savedInv{}
	inv.Slots[0] = stackRow{old(protocol.RegItem, itemByName["apple"]), 1}
	writeJSON(t, files.Inventories, map[string]*savedInv{"Legion": inv})
	before, _ := os.ReadFile(files.Inventories)
	if err := os.WriteFile(files.Stats, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateContent(files, "1.21.5", "1.21.11"); err == nil {
		t.Fatal("migration over an undecodable store succeeded")
	}
	after, _ := os.ReadFile(files.Inventories)
	if string(before) != string(after) {
		t.Error("inventories rewritten although the migration failed")
	}
	if _, err := os.Stat(files.Inventories + ".pre-1.21.11"); err == nil {
		t.Error("backup written although the migration failed")
	}
	if _, err := migrateContent(contentFiles{}, "9.9", "1.21.11"); err == nil {
		t.Error("an unknown source version was accepted")
	}
}
