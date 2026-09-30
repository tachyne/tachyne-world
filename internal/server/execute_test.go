package server

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// exTracked finds a player's hub entry by name (hub goroutine only).
func exTracked(h *hub, name string) *tracked {
	for _, t := range h.playersRef {
		if t.p.name == name {
			return t
		}
	}
	return nil
}

// exCount counts a line's occurrences.
func exCount(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if l == want {
			n++
		}
	}
	return n
}

// exRunner runs one command as a player between two markers and returns
// what that player was told in between.
func exRunner(t *testing.T, s *Server, h *hub, logs map[string]*chatLog, p *player) func(cmd string) []string {
	n := 0
	return func(cmd string) []string {
		t.Helper()
		n++
		a, b := fmt.Sprintf("X%da", n), fmt.Sprintf("X%db", n)
		settle(t, h, logs, a)
		s.handleCommand(p, cmd)
		settle(t, h, logs, b)
		return linesBetween(logs[p.name], a, b)
	}
}

func exNear(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// /execute as @a runs the command once per player, each as that player —
// /say names them — and every line reaches the room exactly once: the
// stand-ins the commands ran as hear nothing of the broadcast and are gone
// afterwards.
func TestExecuteAsRunsForEachSource(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	settle(t, h, logs, "E0")
	s.handleCommand(ps["alice"], "execute as @a run say hi")
	settle(t, h, logs, "E1")
	for _, who := range []string{"alice", "carol"} {
		got := linesBetween(logs[who], "E0", "E1")
		for _, want := range []string{"[alice] hi", "[bob] hi", "[carol] hi"} {
			if n := exCount(got, want); n != 1 {
				t.Errorf("%s heard %q %d times: %q", who, want, n, got)
			}
		}
	}
	onHub(t, h, func() {
		if len(h.execProxies) != 0 {
			t.Errorf("%d stand-ins left registered", len(h.execProxies))
		}
		for _, tr := range h.playersRef {
			if tr.p.exec != nil || strings.HasPrefix(tr.p.name, "@exec") {
				t.Errorf("a stand-in stayed in the players map: %s", tr.p.name)
			}
		}
	})
}

// The source's position, rotation, anchor and dimension carry into the
// command: positioned (whole numbers centred), align, rotated and facing
// with local coordinates, anchored eyes, at, and in (the position scaled
// by the dimensions' coordinate scale).
func TestExecuteMovesTheSource(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	stone, _ := parseBlockState("stone")
	gold, _ := parseBlockState("gold_block")
	diamond, _ := parseBlockState("diamond_block")

	run("execute positioned 10 100 10 run setblock ~ ~ ~ stone")
	onHub(t, h, func() {
		if got := h.world.At(10, 100, 10); got != stone {
			t.Errorf("positioned: block at 10 100 10 is %d, want stone", got)
		}
	})
	bobAt := func(what string, x, y, z float64) {
		t.Helper()
		onHub(t, h, func() {
			b := exTracked(h, "bob")
			if b == nil || !exNear(b.x, x) || !exNear(b.y, y) || !exNear(b.z, z) {
				t.Errorf("%s: bob at %v, want %v %v %v", what, b, x, y, z)
			}
		})
	}
	run("execute positioned 3.7 100.5 4.2 align xyz run tp bob ~ ~ ~")
	bobAt("align", 3, 100, 4)
	run("execute positioned 0.5 100 0.5 rotated 90 0 run tp bob ^ ^ ^2")
	bobAt("rotated", -1.5, 100, 0.5)
	run("execute positioned 0.5 100 0.5 facing 10.5 100 0.5 run tp bob ^ ^ ^1")
	bobAt("facing", 1.5, 100, 0.5)
	run("execute at bob run setblock ~ ~2 ~ gold_block")
	onHub(t, h, func() {
		if got := h.world.At(1, 102, 0); got != gold {
			t.Errorf("at: block over bob is %d, want gold", got)
		}
	})
	var eye float64
	onHub(t, h, func() { eye = exTracked(h, "alice").eyeHeight() })
	run("execute positioned 0.5 100 0.5 anchored eyes positioned ^ ^ ^ run tp bob ~ ~ ~")
	bobAt("anchored eyes", 0.5, 100+eye, 0.5)
	run("execute positioned 80 100 16 in minecraft:the_nether run setblock ~ ~ ~ diamond_block")
	onHub(t, h, func() {
		if got := h.world.At(10, 100, 2); got != diamond { // 80.5/8, 16.5/8
			t.Errorf("in: block at 10 100 2 is %d, want diamond", got)
		}
	})
	if got := run("execute in minecraft:the_nether if dimension minecraft:the_nether"); !hasLine(got, "Test passed") {
		t.Errorf("in + if dimension: %q", got)
	}
	if got := run("execute positioned 5 0 5 positioned over world_surface if block ~ ~ ~ air"); !hasLine(got, "Test passed") {
		t.Errorf("positioned over: the cell above the surface should be air: %q", got)
	}
	if got := run("execute positioned 5 0 5 positioned over world_surface if block ~ ~-1 ~ air"); !hasLine(got, "Test failed") {
		t.Errorf("positioned over: the surface block should not be air: %q", got)
	}
}

// A chain that ends on a condition reports it — "Test passed. Count: N"
// for a counting condition, "Test failed" and the rest — and argument
// failures are told when nothing has forked yet.
func TestExecuteConditions(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("setblock 20 100 20 stone")
	run("setblock 20 100 22 stone")
	run("setblock 20 100 24 dirt")
	run("setblock 25 120 25 air")
	run("give alice diamond 3")
	run("stopwatch create t1")
	cases := []struct{ cmd, want string }{
		{"execute if entity @a", "Test passed. Count: 3"},
		{"execute unless entity @a", "Test failed. Count: 3"},
		{"execute if entity @e[type=pig]", "Test failed"},
		{"execute unless entity @e[type=pig]", "Test passed"},
		{"execute if block 20 100 20 stone", "Test passed"},
		{"execute if block 20 100 20 dirt", "Test failed"},
		{"execute unless block 20 100 20 dirt", "Test passed"},
		{"execute if blocks 20 100 20 20 100 20 20 100 22 all", "Test passed. Count: 1"},
		{"execute unless blocks 20 100 20 20 100 20 20 100 22 all", "Test failed. Count: 1"},
		{"execute if blocks 20 100 20 20 100 20 20 100 24 all", "Test failed"},
		{"execute if blocks 25 120 25 25 120 25 20 100 20 masked", "Test passed. Count: 0"},
		{"execute if loaded 0 64 0", "Test passed"},
		{"execute if loaded 100000 64 0", "Test failed"},
		{"execute if dimension minecraft:overworld", "Test passed"},
		{"execute if dimension minecraft:the_end", "Test failed"},
		{"execute if items entity alice container.* diamond", "Test passed. Count: 3"},
		{"execute if items entity alice container.* emerald", "Test failed"},
		{"execute if stopwatch t1 0..", "Test passed"},
		{"execute if stopwatch nope 0..", "Stopwatch 'minecraft:nope' does not exist"},
		{"execute if block 1000000 100 0 stone", "That position is not loaded"},
		{"execute if block 0 5000 0 stone", "That position is out of this world!"},
		{"execute if predicate foo:bar", "Can't find element 'foo:bar' in registry 'minecraft:predicate'"},
		{"execute if function foo:bar", execIncomplete},
		{"execute if data entity @s Pos", execNeedsData},
		{"execute as @a", execIncomplete},
		{"execute if blocks 0 0 0 40 40 40 0 0 0 all", "Too many blocks in the specified area (maximum 32768, but specified 68921)"},
	}
	for _, c := range cases {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
	// `if function` with no function runtime: no function exists, and the
	// failure is a forked one — nothing runs and nobody hears of it.
	if got := run("execute if function foo:bar run say nope"); len(got) != 0 {
		t.Errorf("if function: %q", got)
	}
}

// Scores compare with the five operators and match ranges; a missing
// score fails the test.
func TestExecuteScoreConditions(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("scoreboard objectives add pts dummy")
	run("scoreboard players set alice pts 5")
	run("scoreboard players set bob pts 42")
	cases := []struct{ cmd, want string }{
		{"execute if score alice pts < bob pts", "Test passed"},
		{"execute if score alice pts <= bob pts", "Test passed"},
		{"execute if score alice pts = bob pts", "Test failed"},
		{"execute if score alice pts > bob pts", "Test failed"},
		{"execute if score bob pts >= alice pts", "Test passed"},
		{"execute if score alice pts matches 1..5", "Test passed"},
		{"execute if score alice pts matches 6..", "Test failed"},
		{"execute if score @s pts matches 5", "Test passed"},
		{"execute if score carol pts matches ..100", "Test failed"},
		{"execute unless score carol pts matches ..100", "Test passed"},
		{"execute if score alice nothing matches 1", "Unknown scoreboard objective 'nothing'"},
		{"execute if score @a pts matches 1", "Only one entity is allowed, but the provided selector allows more than one"},
	}
	for _, c := range cases {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
	// A condition in the chain keeps or drops the source.
	if got := run("execute if score alice pts < bob pts run say kept"); !hasLine(got, "[alice] kept") {
		t.Errorf("a passing condition should let the command run: %q", got)
	}
	if got := run("execute if score alice pts > bob pts run say dropped"); hasLine(got, "[alice] dropped") {
		t.Errorf("a failing condition let the command run: %q", got)
	}
}

// store writes the command's result — a condition's count, a queried
// score, a kill count, the game time — or its success into scores and a
// bossbar; a command that fails stores 0.
func TestExecuteStore(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("scoreboard objectives add res dummy")
	run("scoreboard objectives add pts dummy")
	score := func(owner, obj string) (int32, bool) {
		var v int32
		var ok bool
		onHub(t, h, func() { v, ok = h.sb.Scores[owner][obj] })
		return v, ok
	}
	if got := run("execute store result score alice res if entity @a"); !hasLine(got, "Test passed. Count: 3") {
		t.Errorf("store + condition: %q", got)
	}
	if v, _ := score("alice", "res"); v != 3 {
		t.Errorf("alice res = %d, want 3", v)
	}
	run("scoreboard players set bob pts 42")
	if got := run("execute store result score carol res run scoreboard players get bob pts"); !hasLine(got, "bob has 42 [pts]") {
		t.Errorf("the stored command's own line is missing: %q", got)
	}
	if v, _ := score("carol", "res"); v != 42 {
		t.Errorf("carol res = %d, want 42", v)
	}
	if got := run("execute store success score alice pts run give @s[name=nobody] diamond"); !hasLine(got, "No player was found") {
		t.Errorf("an unforked failure should be told: %q", got)
	}
	if v, ok := score("alice", "pts"); !ok || v != 0 {
		t.Errorf("a failed command stores success 0, got %d (%v)", v, ok)
	}
	run("execute store success score @a pts if entity @s")
	for _, who := range []string{"alice", "bob", "carol"} {
		if v, _ := score(who, "pts"); v != 1 {
			t.Errorf("%s pts = %d, want 1", who, v)
		}
	}
	run("execute store result score alice res run kill @e[type=pig]")
	if v, _ := score("alice", "res"); v != 0 {
		t.Errorf("a kill that found nothing stores %d", v)
	}
	run("summon pig 5 100 5")
	run("summon pig 6 100 5")
	run("execute store result score alice res run kill @e[type=pig]")
	if v, _ := score("alice", "res"); v != 2 {
		t.Errorf("kill of two pigs stores %d, want 2", v)
	}
	run("execute store result score alice res run time query gametime")
	if v, _ := score("alice", "res"); v <= 0 {
		t.Errorf("time query gametime stores %d", v)
	}
	run(`bossbar add test "T"`)
	run("execute store result bossbar test value if entity @a")
	run("scoreboard players set bob pts 50")
	run("execute store result bossbar minecraft:test max run scoreboard players get bob pts")
	onHub(t, h, func() {
		b := h.rules.Bossbars["minecraft:test"]
		if b == nil || b.Value != 3 || b.Max != 50 {
			t.Errorf("bossbar %+v, want value 3 of 50", b)
		}
	})
	for _, cmd := range []string{
		"execute store result entity @s Health float 1 run say x",
		"execute store success block 0 0 0 Items byte 1 run say x",
		"execute store result storage foo:bar x int 1 run say x",
	} {
		if got := run(cmd); !hasLine(got, execNeedsData) {
			t.Errorf("%s: %q", cmd, got)
		}
	}
	if got := run("execute store result score alice nothing run say x"); !hasLine(got, "Unknown scoreboard objective 'nothing'") {
		t.Errorf("store into a missing objective: %q", got)
	}
}

// Once the chain has forked, a failing command's error goes unsaid; an
// unforked one is told. Successes are heard either way, and only by the one
// who ran /execute.
func TestExecuteForkedFailuresAreSilent(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	if got := run("execute as @a run give @s[name=nobody] diamond"); len(got) != 0 {
		t.Errorf("forked failures were told: %q", got)
	}
	if got := run("execute positioned 0 100 0 run give @s[name=nobody] diamond"); !hasLine(got, "No player was found") {
		t.Errorf("an unforked failure went unsaid: %q", got)
	}
	settle(t, h, logs, "F0")
	s.handleCommand(ps["alice"], "execute as @a run give @s diamond")
	settle(t, h, logs, "F1")
	a := linesBetween(logs["alice"], "F0", "F1")
	for _, who := range []string{"alice", "bob", "carol"} {
		if want := "Gave 1 [Diamond] to " + who; !hasLine(a, want) {
			t.Errorf("alice missed %q: %q", want, a)
		}
	}
	if c := linesBetween(logs["carol"], "F0", "F1"); hasLine(c, "Gave 1 [Diamond] to carol") {
		t.Errorf("the executor heard the runner's feedback: %q", c)
	}
}

// @s is the executor whatever it is: a summoned pig takes a tag, mobs
// kill themselves, a tamed wolf's owner is reached with `on owner`, and
// the commands with an implicit "me" (tp to a place, gamemode) act on the
// executing player.
func TestExecuteExecutors(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("execute summon pig run tag @s add fresh")
	var pigs int
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityPig && m.dying == 0 {
				pigs++
				if !m.tags["fresh"] {
					t.Errorf("the summoned pig has no tag: %v", m.tags)
				}
			}
		}
	})
	if pigs != 1 {
		t.Fatalf("%d pigs, want the one summoned", pigs)
	}
	run("execute as @e[type=pig] run kill @s")
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityPig && m.dying == 0 {
				t.Error("the pig survived killing itself")
			}
		}
	})
	run("summon wolf")
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityWolf {
				m.tamed, m.owner = true, exTracked(h, "carol").p.eid
			}
		}
	})
	run("execute as @e[type=wolf] on owner run tag @s add wolfowner")
	onHub(t, h, func() {
		if c := exTracked(h, "carol"); c == nil || !c.tags["wolfowner"] {
			t.Error("on owner did not reach the wolf's owner")
		}
		if a := exTracked(h, "alice"); a != nil && a.tags["wolfowner"] {
			t.Error("on owner tagged the runner")
		}
	})
	run("execute as bob run tp 0.5 110 0.5")
	onHub(t, h, func() {
		if b := exTracked(h, "bob"); b == nil || !exNear(b.y, 110) {
			t.Errorf("tp <pos> as bob moved %v, want bob to y 110", b)
		}
		if a := exTracked(h, "alice"); a != nil && exNear(a.y, 110) {
			t.Error("tp <pos> moved the runner")
		}
	})
	run("execute as bob run gamemode survival")
	onHub(t, h, func() {
		if b := exTracked(h, "bob"); b == nil || b.gamemode != gmSurvival {
			t.Errorf("gamemode as bob: %v", b)
		}
		if a := exTracked(h, "alice"); a == nil || a.gamemode != gmCreative {
			t.Error("gamemode as bob changed the runner's mode")
		}
	})
	// Unforked, the refusal is told; after `as` it would be a forked
	// failure, and silent.
	if got := run("execute positioned ~ ~ ~ run hud off"); !hasLine(got, "A player is required to run this command here") {
		t.Errorf("a connection command ran for a stand-in: %q", got)
	}
}

// /execute needs level 2; the console runs it with its own output.
func TestExecutePermissionAndConsole(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	if got := exRunner(t, s, h, logs, ps["carol"])("execute run say hi"); !hasLine(got, "You don't have permission.") {
		t.Errorf("carol ran /execute: %q", got)
	}
	h.runConsole = s.runAsConsole
	args, _ := json.Marshal(map[string]string{"command": "execute if entity @a"})
	data, errStr := executeCommand(h, "run", args)
	if errStr != "" {
		t.Fatalf("run: %s", errStr)
	}
	if lines := data.(map[string]any)["lines"].([]string); !hasLine(lines, "Test passed. Count: 3") {
		t.Errorf("console heard %q", lines)
	}
	args, _ = json.Marshal(map[string]string{"command": "execute as bob run say from bob"})
	if _, errStr := executeCommand(h, "run", args); errStr != "" {
		t.Fatalf("run: %s", errStr)
	}
	settle(t, h, logs, "K1")
	if !hasLine(logs["carol"].all(), "[bob] from bob") {
		t.Errorf("console execute as bob: %q", logs["carol"].all())
	}
}

// The parser: each subcommand's words, run execute continuing the chain,
// and the incomplete forms.
func TestParseExecute(t *testing.T) {
	steps, run, msg := parseExecute(strings.Fields("as @a at @s positioned ~ ~1 ~ rotated as @p facing entity @e eyes align xz anchored eyes in minecraft:overworld store result score @s x if score @s a matches 1.. run execute unless entity @e[type=pig] run say hi there"))
	if msg != "" || run != "say hi there" {
		t.Fatalf("parse: %q %q", run, msg)
	}
	var ops []string
	for _, st := range steps {
		ops = append(ops, st.op+fmt.Sprint(len(st.args)))
	}
	want := "as1 at1 positioned3 rotated2 facing3 align1 anchored1 in1 store4 if5 unless2"
	if got := strings.Join(ops, " "); got != want {
		t.Errorf("steps %q, want %q", got, want)
	}
	for _, bad := range []string{"", "as", "run", "positioned 1 2", "if nothing", "store result", "as @a"} {
		if _, _, msg := parseExecute(strings.Fields(bad)); msg != execIncomplete {
			t.Errorf("%q parsed (%q)", bad, msg)
		}
	}
}
