package server

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

const testPackMeta = `{"pack":{"description":"test pack","min_format":121,"max_format":121}}`

// writePack lays out a folder pack: pack.mcmeta (meta, or the 26.3 one
// when empty) and the given files by their path in the pack.
func writePack(t *testing.T, dir, name, meta string, files map[string]string) {
	t.Helper()
	if meta == "" {
		meta = testPackMeta
	}
	root := filepath.Join(dir, name)
	files = mergeFiles(files, map[string]string{"pack.mcmeta": meta})
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeZipPack is writePack as a .zip archive.
func writeZipPack(t *testing.T, dir, name, meta string, files map[string]string) {
	t.Helper()
	if meta == "" {
		meta = testPackMeta
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for rel, body := range mergeFiles(files, map[string]string{"pack.mcmeta": meta}) {
		w, err := zw.Create(rel)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func mergeFiles(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func packIDs(packs []*dataPack) []string {
	out := make([]string, len(packs))
	for i, p := range packs {
		out[i] = p.id
	}
	return out
}

// pack.mcmeta's format fields, as PackFormat's codec validates them and
// PackCompatibility grades them against 26.3's data format (121.0).
func TestPackMetaCompatibility(t *testing.T) {
	cases := []struct {
		name, meta string
		ok         bool
		want       packCompat
	}{
		{"exact", `{"pack":{"description":"x","min_format":121,"max_format":121}}`, true, packCompatible},
		{"minor range", `{"pack":{"description":"x","min_format":[121,0],"max_format":[121,5]}}`, true, packCompatible},
		{"bridging old and new", `{"pack":{"description":"x","pack_format":48,"supported_formats":[48,81],"min_format":48,"max_format":130}}`, true, packCompatible},
		{"old single format", `{"pack":{"description":"x","pack_format":48}}`, true, packTooOld},
		{"old range object", `{"pack":{"description":"x","pack_format":45,"supported_formats":{"min_inclusive":40,"max_inclusive":50}}}`, true, packTooOld},
		{"newer", `{"pack":{"description":"x","min_format":130,"max_format":131}}`, true, packTooNew},
		{"new format without range", `{"pack":{"description":"x","pack_format":121}}`, true, packUnknown},
		{"half a range", `{"pack":{"description":"x","min_format":121}}`, true, packUnknown},
		{"no format at all", `{"pack":{"description":"x"}}`, true, packUnknown},
		{"no description", `{"pack":{"pack_format":121}}`, false, 0},
		{"no pack section", `{"features":{}}`, false, 0},
		{"not json", `pack`, false, 0},
	}
	for _, c := range cases {
		_, got, _, ok := readPackMeta([]byte(c.meta))
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: got compat %d ok %v, want %d ok %v", c.name, got, ok, c.want, c.ok)
		}
	}
	desc, _, feats, _ := readPackMeta([]byte(`{"pack":{"description":[{"text":"Hi "},"there"],"min_format":121,"max_format":121},"features":{"enabled":["minecraft:trade_rebalance"]}}`))
	if desc != "Hi there" || !reflect.DeepEqual(feats, []string{"minecraft:trade_rebalance"}) {
		t.Errorf("description %q features %v", desc, feats)
	}
}

// The repository: the built-in pack, folders with a pack.mcmeta and .zip
// archives, sorted by id; anything else in datapacks/ is not a pack.
func TestDiscoverPacksFoldersAndZips(t *testing.T) {
	dir := t.TempDir()
	writePack(t, dir, "alpha", "", nil)
	writeZipPack(t, dir, "beta.zip", "", nil)
	if err := os.MkdirAll(filepath.Join(dir, "notapack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := packIDs(discoverPacks(dir))
	want := []string{"file/alpha", "file/beta.zip", "vanilla"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packs %v, want %v", got, want)
	}
	if got := packIDs(discoverPacks("")); !reflect.DeepEqual(got, []string{"vanilla"}) {
		t.Fatalf("no folder: %v", got)
	}
}

// configurePackRepository: the saved order first, missing packs dropped,
// new packs added unless disabled, a pack needing an experiment left out.
func TestConfigurePacks(t *testing.T) {
	dir := t.TempDir()
	writePack(t, dir, "a", "", nil)
	writePack(t, dir, "b", "", nil)
	writePack(t, dir, "exp", `{"pack":{"description":"x","min_format":121,"max_format":121},"features":{"enabled":["minecraft:trade_rebalance"]}}`, nil)
	avail := discoverPacks(dir)

	if got := configurePacks(avail, &dataPackConfig{Enabled: []string{vanillaPackID}}); !reflect.DeepEqual(got, []string{"vanilla", "file/a", "file/b"}) {
		t.Errorf("fresh world: %v", got)
	}
	got := configurePacks(avail, &dataPackConfig{Enabled: []string{"file/b", "file/gone", "vanilla"}, Disabled: []string{"file/a"}})
	if !reflect.DeepEqual(got, []string{"file/b", "vanilla"}) {
		t.Errorf("saved order: %v", got)
	}
	if got := configurePacks(avail, &dataPackConfig{Disabled: []string{"vanilla", "file/a", "file/b"}}); !reflect.DeepEqual(got, []string{"vanilla"}) {
		t.Errorf("nothing selected should force vanilla, got %v", got)
	}
	cfg := packConfigFor(avail, []string{"file/b", "vanilla"})
	if !reflect.DeepEqual(cfg.Disabled, []string{"file/a", "file/exp"}) {
		t.Errorf("disabled list %v", cfg.Disabled)
	}
}

// CommandFunction.fromLines: comments and blank lines skipped, lines
// trimmed and continued with a backslash, macro lines collecting their
// variables, and the lines that make a function fail to load.
func TestCompileFunction(t *testing.T) {
	fn, err := compileFunction("test:a", "# a comment\n  say one  \r\n\nsay \\\n   two \\\n three\n$say $(who) and $(what) $(who)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.macro || !reflect.DeepEqual(fn.params, []string{"who", "what"}) {
		t.Fatalf("macro %v params %v", fn.macro, fn.params)
	}
	lines, err := fn.instantiate(map[string]any{"who": "alice", "what": int64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"say one", "say two three", "say alice and 3 alice"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines %q, want %q", lines, want)
	}
	for _, bad := range []string{
		"/say hi",                // a leading slash
		"// comment",             // a C comment
		"say hi \\",              // a continuation with nothing after it
		"$say no variables here", // a macro line without a variable
		"$say $(unclosed",        // an unterminated variable
		"$say $(bad-name)",       // an invalid variable name
		"kick alice",             // a level-3 command: not there at the function level
	} {
		if _, err := compileFunction("test:bad", bad); err == nil {
			t.Errorf("%q loaded", bad)
		}
	}
	if _, err := compileFunction("test:ok", "#/not a command\n\n"); err != nil {
		t.Errorf("comment-only function: %v", err)
	}
}

// MacroFunction: every variable needs its argument; numbers print plainly,
// strings as they are, booleans as their byte.
func TestMacroInstantiate(t *testing.T) {
	fn, err := compileFunction("test:m", "$gamerule $(rule) $(value)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.instantiate(nil); err == nil || err.Error() != "Missing arguments to function test:m" {
		t.Errorf("no arguments: %v", err)
	}
	if _, err := fn.instantiate(map[string]any{"rule": "x"}); err == nil || err.Error() != "Missing argument value to function test:m" {
		t.Errorf("one missing: %v", err)
	}
	for v, want := range map[any]string{1.5: "1.5", 2.0: "2", true: "1", int64(-4): "-4", "on": "on", 0.1: "0.1"} {
		lines, err := fn.instantiate(map[string]any{"rule": "r", "value": v})
		if err != nil || lines[0] != "gamerule r "+want {
			t.Errorf("%v: %q %v", v, lines, err)
		}
	}
	if got := macroValue(map[string]any{"a": int64(1), "b c": "x"}); got != `{a:1,"b c":"x"}` {
		t.Errorf("compound as %s", got)
	}
}

// Function tags: files merge bottom to top (replace drops what came
// before), nested tags expand, optional entries may be missing, and a
// tag missing a required entry is not loaded.
func TestFunctionTagsResolve(t *testing.T) {
	dir := t.TempDir()
	writePack(t, dir, "low", "", map[string]string{
		"data/test/function/a.mcfunction":  "say a",
		"data/test/function/b.mcfunction":  "say b",
		"data/test/tags/function/all.json": `{"values":["test:a"]}`,
		"data/test/tags/function/rep.json": `{"values":["test:a"]}`,
	})
	writePack(t, dir, "high", "", map[string]string{
		"data/test/function/c.mcfunction":     "say c",
		"data/test/tags/function/all.json":    `{"values":["test:b","#test:inner",{"id":"test:missing","required":false},"test:a"]}`,
		"data/test/tags/function/inner.json":  `{"values":["test:c","test:a"]}`,
		"data/test/tags/function/rep.json":    `{"replace":true,"values":["test:c"]}`,
		"data/test/tags/function/broken.json": `{"values":["test:a","test:missing"]}`,
		"data/test/tags/function/uses.json":   `{"values":["#test:broken"]}`,
	})
	avail := discoverPacks(dir)
	lib := buildLibrary(selectedPacks(avail, []string{"vanilla", "file/low", "file/high"}))
	ids := func(tag string) []string {
		var out []string
		for _, f := range lib.tag(tag) {
			out = append(out, f.id)
		}
		return out
	}
	if got := ids("test:all"); !reflect.DeepEqual(got, []string{"test:a", "test:b", "test:c"}) {
		t.Errorf("test:all = %v", got)
	}
	if got := ids("test:rep"); !reflect.DeepEqual(got, []string{"test:c"}) {
		t.Errorf("test:rep = %v", got)
	}
	if lib.hasTag("test:broken") || lib.hasTag("test:uses") {
		t.Error("a tag with a missing required entry loaded")
	}
}

// The top pack's file wins; the data a pack carries that is not applied is
// listed.
func TestLibraryPackOrderAndUnappliedData(t *testing.T) {
	dir := t.TempDir()
	writePack(t, dir, "low", "", map[string]string{
		"data/test/function/f.mcfunction":        "say low",
		"data/test/recipe/thing.json":            "{}",
		"data/test/worldgen/biome/place.json":    "{}",
		"data/test/tags/block/stuff.json":        `{"values":[]}`,
		"data/Bad/function/upper.mcfunction":     "say invalid namespace",
		"data/test/function/broken.mcfunction":   "/say broken",
		"data/test/function/sub/dir.mcfunction":  "say nested",
		"data/test/loot_table/chests/box.json":   "{}",
		"data/test/advancement/story/first.json": "{}",
	})
	writeZipPack(t, dir, "high.zip", "", map[string]string{
		"data/test/function/f.mcfunction": "say high",
	})
	avail := discoverPacks(dir)
	lib := buildLibrary(selectedPacks(avail, []string{"vanilla", "file/low", "file/high.zip"}))
	if f := lib.function("test:f"); f == nil || f.entries[0].text != "say high" {
		t.Fatalf("test:f = %+v", f)
	}
	if lib.function("test:sub/dir") == nil {
		t.Error("a function in a subfolder did not load")
	}
	if lib.function("test:broken") != nil || lib.function("Bad:upper") != nil {
		t.Error("a bad function loaded")
	}
	// Recipes, loot tables, tags and advancements are applied now
	// (packcontent.go, packadv.go).
	want := []string{"worldgen/biome"}
	if got := lib.unapplied["file/low"]; !reflect.DeepEqual(got, want) {
		t.Errorf("unapplied %v, want %v", got, want)
	}
	if len(lib.unapplied["file/high.zip"]) != 0 {
		t.Errorf("the zip carries only a function: %v", lib.unapplied["file/high.zip"])
	}
}

// TimerQueue: one event per id and tick, in time then insertion order;
// clear by id; due events pop in order.
func TestTimerQueue(t *testing.T) {
	var q timerQueue
	q.schedule("test:b", 10, schedCallback{ID: "test:b"})
	q.schedule("test:a", 5, schedCallback{ID: "test:a"})
	q.schedule("#test:t", 10, schedCallback{Tag: true, ID: "test:t"})
	q.schedule("test:a", 5, schedCallback{ID: "test:a"}) // same id and tick: once
	q.schedule("test:a", 7, schedCallback{ID: "test:a"})
	if got := q.popDue(4); got != nil {
		t.Fatalf("popped early: %v", got)
	}
	if got := q.popDue(7); !reflect.DeepEqual(got, []schedCallback{{ID: "test:a"}, {ID: "test:a"}}) {
		t.Fatalf("due at 7: %v", got)
	}
	if got := q.ids(); !reflect.DeepEqual(got, []string{"#test:t", "test:b"}) {
		t.Fatalf("ids %v", got)
	}
	if n := q.remove("test:b"); n != 1 {
		t.Fatalf("removed %d", n)
	}
	if got := q.popDue(100); !reflect.DeepEqual(got, []schedCallback{{Tag: true, ID: "test:t"}}) {
		t.Fatalf("due at 100: %v", got)
	}
}

// The queue rides settings.json and keeps each event's distance from now
// across a restart, whose game time starts again.
func TestScheduleSurvivesRestart(t *testing.T) {
	h := newTestHub(world.New(1))
	h.tick.Store(100)
	h.sched.schedule("test:a", 140, schedCallback{ID: "test:a"})
	h.sched.schedule("#test:t", 90, schedCallback{Tag: true, ID: "test:t"}) // overdue
	h.packSchedule()
	raw, err := json.Marshal(h.rules)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"minecraft:function_tag"`) {
		t.Errorf("saved as %s", raw)
	}
	h2 := newTestHub(world.New(1))
	if err := json.Unmarshal(raw, &h2.rules); err != nil {
		t.Fatal(err)
	}
	h2.unpackSchedule()
	if len(h2.sched.events) != 2 {
		t.Fatalf("events %+v", h2.sched.events)
	}
	if e := h2.sched.events[0]; e.id != "#test:t" || e.at != 0 || !e.cb.Tag {
		t.Errorf("overdue tag event %+v", e)
	}
	if e := h2.sched.events[1]; e.id != "test:a" || e.at != 40 {
		t.Errorf("function event %+v, want due in 40", e)
	}
}
