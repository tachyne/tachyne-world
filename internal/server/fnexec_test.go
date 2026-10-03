package server

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// functionServer is feedbackServer (alice and bob operators, carol not)
// with a datapacks folder holding the pack "fp" (files), the function
// runner started and "fp" enabled above vanilla.
func functionServer(t *testing.T, files map[string]string) (*Server, *hub, map[string]*player, map[string]*chatLog) {
	t.Helper()
	t.Cleanup(func() { installPackContent(nil) }) // the load is process-wide
	s, h, ps, logs := feedbackServer(t)
	s.DataPackDir = t.TempDir()
	writePack(t, s.DataPackDir, "fp", "", files)
	onHub(t, h, func() { s.startFunctionRunner() })
	s.reloadDataPacks([]string{vanillaPackID, "file/fp"})
	return s, h, ps, logs
}

// ruleState reads the rules the tests watch off the hub.
func ruleState(t *testing.T, h *hub) worldRules {
	t.Helper()
	var r worldRules
	onHub(t, h, func() { r = h.rules })
	return r
}

// waitRules polls the hub until cond holds for its rules.
func waitRules(t *testing.T, h *hub, what string, cond func(worldRules) bool) {
	t.Helper()
	deadline := time.Now().Add(hubTestWait)
	for !cond(ruleState(t, h)) {
		if time.Now().After(deadline) {
			t.Fatalf("never saw %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// /function: "Running function …" for the caller (and the operators), the
// lines run in order through the dispatcher — comments skipped, a
// continued line joined — and the function's own commands tell nobody.
func TestFunctionCommandRunsSilently(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/a.mcfunction": "# set two rules\ngamerule keep_inventory true\n\ngamerule \\\n  fall_damage false\n",
	})
	alice := ps["alice"]
	settle(t, h, logs, "F0")
	s.handleCommand(alice, "function test:a")
	settle(t, h, logs, "F1")
	a := linesBetween(logs["alice"], "F0", "F1")
	if !hasLine(a, "Running function test:a") {
		t.Fatalf("alice should hear the function start, got %q", a)
	}
	if hasPrefixLine(a, "Gamerule ") {
		t.Errorf("the function's own commands spoke to its caller: %q", a)
	}
	b := linesBetween(logs["bob"], "F0", "F1")
	if !hasLine(b, adminLine("alice", "Running function test:a")) || hasPrefixLine(b, "§7§o[alice: Gamerule") {
		t.Errorf("bob (op) heard %q", b)
	}
	r := ruleState(t, h)
	if !r.KeepInventory || r.FallDamage {
		t.Errorf("keep_inventory %v fall_damage %v after the function", r.KeepInventory, r.FallDamage)
	}
	// After the function the caller hears its commands again.
	s.handleCommand(alice, "gamerule keep_inventory false")
	settle(t, h, logs, "F2")
	if !hasLine(linesBetween(logs["alice"], "F1", "F2"), "Gamerule keep_inventory = false") {
		t.Error("alice stayed silenced after the function")
	}

	// Unknown names and tags.
	s.handleCommand(alice, "function test:nope")
	s.handleCommand(alice, "function #test:nope")
	settle(t, h, logs, "F3")
	a = linesBetween(logs["alice"], "F2", "F3")
	if !hasLine(a, "Unknown function test:nope") || !hasLine(a, "Unknown function tag 'test:nope'") {
		t.Errorf("unknown ids: %q", a)
	}
	// carol is no operator: /function is not there for her.
	s.handleCommand(ps["carol"], "function test:a")
	settle(t, h, logs, "F4")
	if c := linesBetween(logs["carol"], "F3", "F4"); !hasLine(c, "You don't have permission.") {
		t.Errorf("carol ran /function: %q", c)
	}
}

// /return: a value or a failure ends the function and is reported;
// `return run function` hands back the callee's value; a plain nested
// call returns only from itself.
func TestFunctionReturn(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/r.mcfunction":      "return 7\ngamerule keep_inventory true",
		"data/test/function/f.mcfunction":      "return fail\ngamerule keep_inventory true",
		"data/test/function/inner.mcfunction":  "return 3",
		"data/test/function/outer.mcfunction":  "return run function test:inner\ngamerule keep_inventory true",
		"data/test/function/outer2.mcfunction": "function test:inner\ngamerule mob_griefing false",
		"data/test/function/plain.mcfunction":  "gamerule fall_damage false",
	})
	alice := ps["alice"]
	settle(t, h, logs, "R0")
	for _, fn := range []string{"test:r", "test:f", "test:outer", "test:outer2", "test:plain"} {
		s.handleCommand(alice, "function "+fn)
	}
	settle(t, h, logs, "R1")
	a := linesBetween(logs["alice"], "R0", "R1")
	for _, want := range []string{"Function test:r returned 7", "Function test:f returned 0", "Function test:outer returned 3"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if hasPrefixLine(a, "Function test:outer2 returned") || hasPrefixLine(a, "Function test:plain returned") || hasPrefixLine(a, "Function test:inner returned") {
		t.Errorf("a function without /return reported a value: %q", a)
	}
	r := ruleState(t, h)
	if r.KeepInventory {
		t.Error("a line after /return ran")
	}
	if r.MobGriefing || r.FallDamage {
		t.Errorf("mob_griefing %v fall_damage %v: the calls that return nothing should run to the end", r.MobGriefing, r.FallDamage)
	}
	// Outside a function /return does nothing and says nothing.
	s.handleCommand(alice, "return 5")
	settle(t, h, logs, "R2")
	if a := linesBetween(logs["alice"], "R1", "R2"); len(a) != 0 {
		t.Errorf("/return at the prompt said %q", a)
	}
}

// Macro functions take their variables from the compound /function is
// given, or from `with <source>`; missing ones fail the instantiation.
func TestFunctionMacroArguments(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/m.mcfunction": "$gamerule $(rule) $(value)",
	})
	alice := ps["alice"]
	settle(t, h, logs, "M0")
	s.handleCommand(alice, `function test:m {rule:"keep_inventory",value:"true"}`)
	s.handleCommand(alice, "function test:m")
	s.handleCommand(alice, `function test:m {rule:"keep_inventory"}`)
	s.handleCommand(alice, "function test:m with storage test:nothing")
	settle(t, h, logs, "M1")
	if !ruleState(t, h).KeepInventory {
		t.Error("the macro line did not run with its arguments")
	}
	a := linesBetween(logs["alice"], "M0", "M1")
	for _, want := range []string{
		"Failed to instantiate function test:m: Missing arguments to function test:m",
		"Failed to instantiate function test:m: Missing argument value to function test:m",
		"Failed to instantiate function test:m: Missing argument rule to function test:m",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	// /schedule refuses a macro.
	s.handleCommand(alice, "schedule function test:m 5t")
	settle(t, h, logs, "M2")
	if a := linesBetween(logs["alice"], "M1", "M2"); !hasLine(a, "Can't schedule a macro") {
		t.Errorf("scheduled a macro: %q", a)
	}
}

// The load tag runs after a (re)load and the tick tag every tick, as the
// server at the function permission level; a function using a level-3
// command is not loaded at all.
func TestFunctionTagsLoadAndTick(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/boot.mcfunction":      "gamerule keep_inventory true",
		"data/test/function/every.mcfunction":     "gamerule mob_griefing false",
		"data/test/function/bad.mcfunction":       "kick alice",
		"data/minecraft/tags/function/load.json":  `{"values":["test:boot"]}`,
		"data/minecraft/tags/function/tick.json":  `{"values":["test:every"]}`,
		"data/test/tags/function/needs_bad.json":  `{"values":["test:bad"]}`,
		"data/test/tags/function/optional.json":   `{"values":[{"id":"test:bad","required":false},"test:boot"]}`,
		"data/test/function/unrelated.mcfunction": "say hi",
	})
	waitRules(t, h, "the load and tick tags run", func(r worldRules) bool { return r.KeepInventory && !r.MobGriefing })
	alice := ps["alice"]
	settle(t, h, logs, "T0")
	s.handleCommand(alice, "function test:bad")
	s.handleCommand(alice, "function #test:needs_bad")
	settle(t, h, logs, "T1")
	a := linesBetween(logs["alice"], "T0", "T1")
	if !hasLine(a, "Unknown function test:bad") || !hasLine(a, "Unknown function tag 'test:needs_bad'") {
		t.Errorf("the level-3 function or its tag loaded: %q", a)
	}
	if lib := h.functions.Load(); len(lib.tag("test:optional")) != 1 {
		t.Errorf("optional entries: %d functions", len(lib.tag("test:optional")))
	}
	// /reload runs the load tag again.
	s.handleCommand(alice, "gamerule keep_inventory false")
	settle(t, h, logs, "T2")
	s.handleCommand(alice, "reload")
	settle(t, h, logs, "T3")
	if !hasLine(linesBetween(logs["alice"], "T2", "T3"), "Reloading!") {
		t.Error("no Reloading! line")
	}
	waitRules(t, h, "the load tag after /reload", func(r worldRules) bool { return r.KeepInventory })
}

// /schedule: a function runs after its delay; replace and append; clear.
func TestScheduleFunction(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/later.mcfunction": "gamerule keep_inventory true",
		"data/test/function/other.mcfunction": "gamerule fall_damage false",
	})
	alice := ps["alice"]
	settle(t, h, logs, "S0")
	s.handleCommand(alice, "schedule function test:later 3t")
	s.handleCommand(alice, "schedule function test:other 1d")
	s.handleCommand(alice, "schedule function test:other 2d append")
	s.handleCommand(alice, "schedule function test:later 0")
	s.handleCommand(alice, "schedule clear test:none")
	settle(t, h, logs, "S1")
	a := linesBetween(logs["alice"], "S0", "S1")
	if !hasPrefixLine(a, "Scheduled function 'test:later' in 3 tick(s) at gametime ") ||
		!hasPrefixLine(a, "Scheduled function 'test:other' in 24000 tick(s) at gametime ") ||
		!hasPrefixLine(a, "Scheduled function 'test:other' in 48000 tick(s) at gametime ") {
		t.Errorf("schedule lines %q", a)
	}
	if !hasLine(a, "Can't schedule for current tick") || !hasLine(a, "No schedules with ID test:none") {
		t.Errorf("failures %q", a)
	}
	waitRules(t, h, "the scheduled function", func(r worldRules) bool { return r.KeepInventory })
	s.handleCommand(alice, "schedule clear test:other")
	settle(t, h, logs, "S2")
	if a := linesBetween(logs["alice"], "S1", "S2"); !hasLine(a, "Removed 2 schedule(s) with ID test:other") {
		t.Errorf("clear %q", a)
	}
	var ids []string
	onHub(t, h, func() { ids = h.sched.ids() })
	if len(ids) != 0 {
		t.Errorf("left in the queue: %v", ids)
	}
	if !ruleState(t, h).FallDamage {
		t.Error("a cleared function ran")
	}
}

// max_command_sequence_length: the call and each command cost one; at
// nothing left the function stops.
func TestMaxCommandSequenceLength(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/long.mcfunction": "gamerule keep_inventory true\ngamerule mob_griefing false\ngamerule fall_damage false",
	})
	alice := ps["alice"]
	s.handleCommand(alice, "gamerule max_command_sequence_length 3")
	settle(t, h, logs, "L0")
	s.handleCommand(alice, "function test:long")
	settle(t, h, logs, "L1")
	r := ruleState(t, h)
	if !r.KeepInventory || r.MobGriefing || !r.FallDamage {
		t.Errorf("with a budget of 3: keep_inventory %v mob_griefing %v fall_damage %v (want the first two lines only)", r.KeepInventory, r.MobGriefing, r.FallDamage)
	}
	if r.MaxCmdSeq != 3 || r.MaxCmdForks != 65536 {
		t.Errorf("rules %d/%d", r.MaxCmdSeq, r.MaxCmdForks)
	}
}

// /datapack list, enable (with its positions) and disable, the selection
// saved in the rules, the data the engine does not apply named, and the
// packs that cannot be enabled.
func TestDatapackCommand(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/function/a.mcfunction":  "say a",
		"data/test/enchantment/thing.json": "{}",
	})
	writeZipPack(t, s.DataPackDir, "z.zip", "", nil)
	writePack(t, s.DataPackDir, "old", `{"pack":{"description":"old","pack_format":48}}`, nil)
	writePack(t, s.DataPackDir, "exp", `{"pack":{"description":"x","min_format":121,"max_format":121},"features":{"enabled":["minecraft:trade_rebalance"]}}`, nil)
	alice := ps["alice"]
	settle(t, h, logs, "D0")
	s.handleCommand(alice, "datapack list")
	settle(t, h, logs, "D1")
	a := linesBetween(logs["alice"], "D0", "D1")
	for _, want := range []string{
		"There are 2 data pack(s) enabled: [vanilla (built-in)], [file/fp (world)]",
		"[file/fp (world)] also carries enchantment: this server does not apply that data",
		"There are 2 data pack(s) available: [file/old (world)] (Made for an older version of Minecraft), [file/z.zip (world)]",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	selected := func() []string { return h.functions.Load().selectedIDs() }

	s.handleCommand(alice, `datapack enable "file/z.zip" first`)
	s.handleCommand(alice, `datapack enable "file/exp"`)
	s.handleCommand(alice, `datapack enable "file/fp"`)
	settle(t, h, logs, "D2")
	a = linesBetween(logs["alice"], "D1", "D2")
	for _, want := range []string{
		"Enabling data pack [file/z.zip (world)]",
		"Pack 'file/exp' cannot be enabled, since required flags are not enabled in this world: minecraft:trade_rebalance!",
		"Pack 'file/fp' is already enabled!",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if got := selected(); !reflect.DeepEqual(got, []string{"file/z.zip", "vanilla", "file/fp"}) {
		t.Fatalf("after enable first: %v", got)
	}

	s.handleCommand(alice, `datapack disable "file/fp"`)
	s.handleCommand(alice, `datapack enable "file/old" after vanilla`)
	s.handleCommand(alice, `datapack disable "file/nope"`)
	settle(t, h, logs, "D3")
	a = linesBetween(logs["alice"], "D2", "D3")
	if !hasLine(a, "Disabling data pack [file/fp (world)]") || !hasLine(a, "Unknown data pack 'file/nope'") {
		t.Errorf("disable lines %q", a)
	}
	if got := selected(); !reflect.DeepEqual(got, []string{"file/z.zip", "vanilla", "file/old"}) {
		t.Fatalf("after disable/enable after: %v", got)
	}
	if h.functions.Load().function("test:a") != nil {
		t.Error("a disabled pack's function is still loaded")
	}
	cfg := ruleState(t, h).DataPacks
	if cfg == nil || !reflect.DeepEqual(cfg.Enabled, []string{"file/z.zip", "vanilla", "file/old"}) || !hasPackID(cfg.Disabled, "file/fp") {
		t.Fatalf("saved selection %+v", cfg)
	}

	// /reload keeps a disabled pack disabled and picks up a new one.
	writePack(t, s.DataPackDir, "fresh", "", nil)
	s.handleCommand(alice, "reload")
	settle(t, h, logs, "D4")
	if got := selected(); !reflect.DeepEqual(got, []string{"file/z.zip", "vanilla", "file/old", "file/fresh"}) {
		t.Fatalf("after /reload: %v", got)
	}
	if !strings.HasPrefix(strings.Join(linesBetween(logs["alice"], "D3", "D4"), "|"), "Reloading!") {
		t.Errorf("reload lines %q", linesBetween(logs["alice"], "D3", "D4"))
	}
}

// The console's /function: it hears the start and the result.
func TestFunctionFromConsole(t *testing.T) {
	s, h, _, logs := functionServer(t, map[string]string{
		"data/test/function/r.mcfunction": "gamerule keep_inventory true\nreturn 2",
	})
	settle(t, h, logs, "C0")
	lines, err := s.runAsConsole("function test:r")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"Running function test:r", "Function test:r returned 2"}) {
		t.Errorf("console heard %q", lines)
	}
	if !ruleState(t, h).KeepInventory {
		t.Error("the console's function did not run")
	}
}
