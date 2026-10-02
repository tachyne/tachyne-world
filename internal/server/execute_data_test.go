package server

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// if|unless data counts what the path matches in an entity's, a block
// entity's or a storage's data; a block that holds no block entity is
// refused.
func TestExecuteIfData(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("data merge storage t:s {a:1,list:[1,2,3]}")
	run("setblock 20 100 20 stone")
	run("setblock 22 100 22 chest")
	run("item replace block 22 100 22 container.0 with diamond 5")
	for _, c := range []struct{ cmd, want string }{
		{"execute if data storage t:s a", "Test passed. Count: 1"},
		{"execute if data storage t:s list[]", "Test passed. Count: 3"},
		{"execute if data storage t:s nope", "Test failed"},
		{"execute unless data storage t:s nope", "Test passed"},
		{"execute unless data storage t:s list[]", "Test failed. Count: 3"},
		{"execute if data entity @s Pos[]", "Test passed. Count: 3"},
		{`execute if data block 22 100 22 Items[{id:"minecraft:diamond"}]`, "Test passed. Count: 1"},
		{"execute if data block 20 100 20 Items", "The target block is not a block entity"},
		{"execute if data entity @e[type=pig] Pos", "No entity was found"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
	// As a condition in the chain it keeps or drops the source.
	if got := run("execute if data storage t:s a run say kept"); !hasLine(got, "[alice] kept") {
		t.Errorf("a matching path should let the command run: %q", got)
	}
	if got := run("execute if data storage t:s b run say dropped"); hasLine(got, "[alice] dropped") {
		t.Errorf("a path that matches nothing let the command run: %q", got)
	}
}

// store … storage|entity|block writes the result (times the scale, as the
// tag type asked for) at the path; a player's data refuses the write and
// the command still runs.
func TestExecuteStoreData(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("execute store result storage t:s n int 2 if entity @a")
	run("execute store result storage t:s d double 0.5 if entity @a")
	run("execute store result storage t:s f float 1.5 if entity @a")
	run("execute store result storage t:s b byte 100 if entity @a") // 300 as a byte
	run("execute store result storage t:s l long 1 if entity @a")
	run("execute store result storage t:s sh short 1 if entity @a")
	run("execute store success storage t:s deep.ok byte 1 run say x")
	onHub(t, h, func() {
		got := h.storage().get("t:s")
		for k, want := range map[string]any{
			"n": nbtInt(6), "d": nbtDouble(1.5), "f": nbtFloat(4.5), "b": nbtByte(44),
			"l": nbtLong(3), "sh": nbtShort(3),
		} {
			if got[k] != want {
				t.Errorf("storage %s = %#v, want %#v", k, got[k], want)
			}
		}
		if deep, ok := got["deep"].(map[string]any); !ok || deep["ok"] != nbtByte(1) {
			t.Errorf("store success into a made path: %#v", got["deep"])
		}
	})
	run("summon pig 3 100 3")
	run("execute store result entity @e[type=pig,limit=1] Health float 1 if entity @a")
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityPig && m.health != 3 {
				t.Errorf("the pig's health after the store: %d, want 3", m.health)
			}
		}
	})
	if got := run("execute store result entity @s Health float 1 run say still"); !hasLine(got, "[alice] still") ||
		hasLine(got, "Unable to modify player data") {
		t.Errorf("a store into a player's data: %q", got)
	}
	for _, c := range []struct{ cmd, want string }{
		{"execute store result storage t:s n nibble 1 run say x", execIncomplete},
		{"execute store result storage t:s n int x run say x", "Invalid double 'x'"},
		{"execute store result block 20 5000 20 Items int 1 run say x", "That position is out of this world!"},
		{"execute store result entity @e[type=cow] Health float 1 run say x", "No entity was found"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
}

// The Java casts storeData's constructors make.
func TestExecNumTagCasts(t *testing.T) {
	for _, c := range []struct {
		typ   string
		v     int
		scale float64
		want  any
	}{
		{"byte", 200, 1, nbtByte(-56)},
		{"short", 40000, 1, nbtShort(-25536)},
		{"int", 3, 1e12, nbtInt(math.MaxInt32)},
		{"int", 7, 0.5, nbtInt(3)},
		{"int", -7, 0.5, nbtInt(-3)},
		{"long", 3, math.NaN(), nbtLong(0)},
		{"float", 1, 0.1, nbtFloat(float32(0.1))},
		{"double", 1, 0.1, nbtDouble(0.1)},
	} {
		if got := execNumTag(c.typ, c.v, c.scale); got != c.want {
			t.Errorf("%s %d×%v = %#v, want %#v", c.typ, c.v, c.scale, got, c.want)
		}
	}
}

// if slots counts the slots a slot source picks, empty ones too: a range,
// an inline group, a limit, a filter; an id names no registered source.
func TestExecuteIfSlots(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("give alice diamond 3")
	for _, c := range []struct{ cmd, want string }{
		{"execute if slots entity alice hotbar.*", "Test passed. Count: 9"},
		{"execute if slots entity alice weapon.offhand", "Test passed. Count: 1"},
		{`execute if slots entity alice [{type:"slot_range",slots:"hotbar.*"},{type:"slot_range",slots:"armor.head"}]`, "Test passed. Count: 10"},
		{`execute if slots entity alice {type:"limit_slots",slot_source:{type:"slot_range",slots:"container.*"},limit:4}`, "Test passed. Count: 4"},
		{`execute if slots entity alice {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:diamond"}}`, "Test passed. Count: 1"},
		{`execute if slots entity alice {type:"empty"}`, "Test failed"},
		{`execute unless slots entity alice {type:"empty"}`, "Test passed"},
		{"execute if slots entity alice foo", "Can't find element 'minecraft:foo' in registry 'minecraft:slot_source'"},
		{`execute if slots entity alice {type:"contents",slot_source:{type:"slot_range",slots:"hotbar.0"},component:"minecraft:container"}`, "The contents slot source can't be read on this server yet"},
		{"execute if slots block 20 100 20 container.*", "Source position 20, 100, 20 is not a container"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
}

// Item predicates: tags, the count pseudo component, component values and
// presence, predicates, negation and alternatives.
func TestExecuteItemPredicates(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("give alice diamond 3")
	run("give alice oak_log 2")
	run("give alice diamond_sword[damage=5] 1")
	for _, c := range []struct{ cmd, want string }{
		{"execute if items entity alice container.* #minecraft:logs", "Test passed. Count: 2"},
		{"execute if items entity alice container.* #logs", "Test passed. Count: 2"},
		{"execute if items entity alice container.* *[count~{min:2}]", "Test passed. Count: 5"},
		{"execute if items entity alice container.* *[count=3]", "Test passed. Count: 3"},
		{"execute if items entity alice container.* *[damage=5]", "Test passed. Count: 1"},
		{"execute if items entity alice container.* *[damage=4]", "Test failed"},
		{"execute if items entity alice container.* *[damage~{damage:{min:1}}]", "Test passed. Count: 1"},
		{"execute if items entity alice container.* *[!damage]", "Test passed. Count: 5"},
		{"execute if items entity alice container.* *[damage=5|count=3]", "Test passed. Count: 4"},
		{"execute if items entity alice container.* diamond_sword[damage]", "Test passed. Count: 1"},
		{"execute if items entity alice container.* *[max_stack_size=1]", "Test passed. Count: 1"},
		{"execute if items entity alice container.* *[enchantments={}]", "Test passed. Count: 6"},
		{"execute if items entity alice container.* *[enchantments~[{}]]", "Test failed"},
		{"execute if items entity alice container.* #minecraft:nope", "Unknown item tag 'minecraft:nope'"},
		{"execute if items entity alice container.* *[bogus]", "Unknown item component 'minecraft:bogus'"},
		{"execute if items entity alice container.* *[bogus~{}]", "Unknown item predicate 'minecraft:bogus'"},
		{"execute if items entity alice container.* *[food]", "Item predicate component 'minecraft:food' can't be tested on this server yet"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
}

// if biome takes a biome tag.
func TestExecuteIfBiomeTag(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	for _, c := range []struct{ cmd, want string }{
		{"execute if biome 0 64 0 #minecraft:is_overworld", "Test passed"},
		{"execute if biome 0 64 0 #minecraft:is_nether", "Test failed"},
		{"execute unless biome 0 64 0 #is_nether", "Test passed"},
		{"execute if biome 0 64 0 #minecraft:nope", "Can't find tag 'minecraft:nope' of type 'minecraft:worldgen/biome'"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
}

// After anchored eyes, the run command's local coordinates are measured
// from the executor's eyes; after positioned (feet again) from the feet.
func TestExecuteRunLocalCoordsFromEyes(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	var feetY float64
	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			if tr.p.name == "alice" {
				feetY = tr.y
			}
		}
	})
	eyeBlock := floorInt(feetY + 1.62)
	if eyeBlock == floorInt(feetY) {
		t.Fatalf("eyes and feet in one block at y=%v", feetY)
	}
	run("execute anchored eyes run setblock ^ ^ ^2 gold_block")
	run("execute anchored feet run setblock ^ ^ ^3 diamond_block")
	onHub(t, h, func() {
		w := h.worldFor(0)
		if got := w.At(0, eyeBlock, 2); got != worldgen.BlockBase("gold_block") {
			t.Errorf("anchored eyes: the block at eye height is %d", got)
		}
		if got := w.At(0, floorInt(feetY), 3); got != worldgen.BlockBase("diamond_block") {
			t.Errorf("anchored feet: the block at foot height is %d", got)
		}
	})
	// The rewrite itself: three local words become absolute numbers that
	// never read as whole blocks.
	line := execLocalEyes("tp @s ^ ^ ^", &execSource{x: 1, y: 10, z: 2, eyes: true, eyeY: 1.5})
	f := strings.Fields(line)
	if len(f) != 5 || f[2] != "1.0" || f[4] != "2.0" {
		t.Fatalf("execLocalEyes: %q", line)
	}
	if y, err := strconv.ParseFloat(f[3], 64); err != nil || y != 11.5 {
		t.Errorf("execLocalEyes y: %q", line)
	}
	if got := execLocalEyes("tp @s ^ ^ ^", &execSource{y: 10}); got != "tp @s ^ ^ ^" {
		t.Errorf("feet anchor rewrote the line: %q", got)
	}
}
