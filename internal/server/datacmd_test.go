package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func mustTyped(t *testing.T, s string) any {
	t.Helper()
	v, err := parseSNBTTyped(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func mustPath(t *testing.T, s string) nbtPath {
	t.Helper()
	p, msg := parseNBTPath(s)
	if msg != "" {
		t.Fatalf("path %q: %s", s, msg)
	}
	return p
}

// Typed SNBT keeps each tag's type, prints it back the way /data shows it,
// and compares type-strictly.
func TestTypedSNBT(t *testing.T) {
	v := mustTyped(t, `{b: 1b, s: 3s, i: 5, l: 7L, f: 20.0f, d: 1.5, t: true, str: 'it"s', arr: [I; 1, 2], ba: [B; 1b], list: [1, 2], "d:e": {}}`)
	m := v.(map[string]any)
	for k, want := range map[string]any{"b": nbtByte(1), "s": nbtShort(3), "i": nbtInt(5), "l": nbtLong(7),
		"f": nbtFloat(20), "d": nbtDouble(1.5), "t": nbtByte(1), "str": `it"s`} {
		if m[k] != want {
			t.Errorf("%s = %#v, want %#v", k, m[k], want)
		}
	}
	if l, ok := m["arr"].(*nbtList); !ok || l.arr != 'I' || len(l.elems) != 2 || l.elems[1] != nbtInt(2) {
		t.Errorf("int array: %#v", m["arr"])
	}
	got := tagString(v)
	want := `{arr: [I; 1, 2], b: 1b, ba: [B; 1b], d: 1.5d, "d:e": {}, f: 20.0f, i: 5, l: 7L, list: [1, 2], s: 3s, str: 'it"s', t: 1b}`
	if got != want {
		t.Errorf("printed\n %s\nwant\n %s", got, want)
	}
	// What is printed parses back to the same tag (the storage file relies on it).
	if back := mustTyped(t, got); !tagEqual(back, v) {
		t.Errorf("round trip changed the tag: %s", tagString(back))
	}
	if tagEqual(nbtInt(1), nbtByte(1)) {
		t.Error("an int and a byte of the same value must differ")
	}
	if !tagCompare(mustTyped(t, `{a: [1]}`), mustTyped(t, `{a: [2, 1], b: 3}`), true) {
		t.Error("partial list match")
	}
}

// NBT paths: every node kind reads, and set/insert/remove change the tag in
// place as vanilla's nodes do.
func TestNBTPath(t *testing.T) {
	root := mustTyped(t, `{Items: [{Slot: 0b, id: "minecraft:stone"}, {Slot: 1b, id: "minecraft:dirt"}], n: 5, o: {k: 1}}`).(map[string]any)
	get := func(path string) any {
		t.Helper()
		tags, msg := mustPath(t, path).get(root)
		if msg != "" {
			t.Fatalf("get %s: %s", path, msg)
		}
		if len(tags) != 1 {
			t.Fatalf("get %s: %d tags", path, len(tags))
		}
		return tags[0]
	}
	if got := get("Items[1].id"); got != "minecraft:dirt" {
		t.Errorf("Items[1].id = %v", got)
	}
	if got := get("Items[{Slot:0b}].id"); got != "minecraft:stone" {
		t.Errorf("match element = %v", got)
	}
	if got := get("Items[-1].Slot"); got != nbtByte(1) {
		t.Errorf("negative index = %#v", got)
	}
	if got := get(`"o".k`); got != nbtInt(1) {
		t.Errorf("quoted name = %#v", got)
	}
	if got := get("o{k:1}.k"); got != nbtInt(1) {
		t.Errorf("match object = %#v", got)
	}
	if got := get("{n:5}.n"); got != nbtInt(5) {
		t.Errorf("match root = %#v", got)
	}
	if n := mustPath(t, "Items[].id").countMatching(root); n != 2 {
		t.Errorf("Items[].id matches %d", n)
	}
	if _, msg := mustPath(t, "o.missing.x").get(root); msg != "Found no elements matching o.missing" {
		t.Errorf("not found: %q", msg)
	}
	if _, msg := parseNBTPath("a..b"); msg == "" {
		t.Error("an empty element must be refused")
	}

	// set makes the missing parents.
	if n, msg := mustPath(t, "a.b[0]").set(root, nbtInt(1)); msg != "" || n != 0 {
		t.Errorf("an index into a fresh list sets nothing: %d %q", n, msg)
	}
	if n, msg := mustPath(t, "x.y").set(root, "v"); msg != "" || n != 1 {
		t.Errorf("set x.y: %d %q", n, msg)
	}
	if get("x.y") != "v" {
		t.Error("x.y not set")
	}
	if n, _ := mustPath(t, "x.y").set(root, "v"); n != 0 {
		t.Error("setting the same value again changes nothing")
	}
	// insert: append, prepend and a negative index.
	p := mustPath(t, "list")
	if n, msg := p.insert(-1, root, []any{nbtInt(1), nbtInt(2)}); n != 1 || msg != "" {
		t.Errorf("append: %d %q", n, msg)
	}
	p.insert(0, root, []any{nbtInt(0)})
	p.insert(-2, root, []any{nbtInt(9)})
	if got := tagString(root["list"]); got != "[0, 1, 9, 2]" {
		t.Errorf("list = %s", got)
	}
	if _, msg := p.insert(7, root, []any{nbtInt(3)}); msg != "Invalid list index: 7" {
		t.Errorf("bad index: %q", msg)
	}
	if _, msg := mustPath(t, "n").insert(0, root, []any{nbtInt(3)}); msg != "Expected a list: got 5" {
		t.Errorf("not a list: %q", msg)
	}
	// remove every Slot, then a matched element.
	if n := mustPath(t, "Items[].Slot").remove(root); n != 2 {
		t.Errorf("removed %d slots", n)
	}
	if n := mustPath(t, `Items[{id:"minecraft:dirt"}]`).remove(root); n != 1 {
		t.Errorf("removed %d matched elements", n)
	}
	if got := tagString(root["Items"]); got != `[{id: "minecraft:stone"}]` {
		t.Errorf("Items = %s", got)
	}
}

// /data on command storage through the dispatcher: merge, get (whole, by
// path, scaled), every modify operation, a string source with a substring,
// remove, and the unchanged / missing / non-numeric failures.
func TestCommandDataStorage(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		`data merge storage t:s {a: 1b, name: "hello world", list: [1, 2]}`,
		`data get storage t:s`,
		`data get storage t:s a`,
		`data get storage t:s list[1] 2.5`,
		`data merge storage t:s {a: 1b}`,
		`data get storage t:s nope`,
		`data get storage t:s name 2`,
		`data modify storage t:s list append value 3`,
		`data modify storage t:s list prepend value 0`,
		`data modify storage t:s list insert 1 value 7`,
		`data modify storage t:s obj merge value {k: "v"}`,
		`data modify storage t:s copy set from storage t:s list`,
		`data modify storage t:s word set string storage t:s name 6`,
		`data modify storage t:s word2 set string storage t:s name 0 -6`,
		`data remove storage t:s a`,
		`data remove storage t:s a`,
		`data get storage t:s`,
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "D1")
	a := linesBetween(logs["alice"], "", "D1")
	for _, want := range []string{
		"Modified storage t:s",
		`Storage t:s has the following contents: {a: 1b, list: [1, 2], name: "hello world"}`,
		"Storage t:s has the following contents: 1b",
		"list[1] in storage t:s after scale factor of 2.50 is 5",
		"Nothing changed. The specified properties already have these values",
		"Found no elements matching nope",
		"Can't get name; only numeric tags are allowed",
		`Storage t:s has the following contents: {copy: [0, 7, 1, 2, 3], list: [0, 7, 1, 2, 3], name: "hello world", obj: {k: "v"}, word: "world", word2: "hello"}`,
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	// A non-operator is refused.
	s.handleCommand(ps["carol"], "data get storage t:s")
	settle(t, h, logs, "D2")
	if c := linesBetween(logs["carol"], "D1", "D2"); permissionRefusals(c) != 1 {
		t.Errorf("non-op: %q", c)
	}
}

// /data on a block entity: a chest's Items read in vanilla's form, an item
// appended by path lands in the chest, a sign's text merged in, and a block
// without a block entity refused.
func TestCommandDataBlock(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	chestSt, ok := parseBlockState("chest[type=single,waterlogged=false]")
	if !ok {
		t.Fatal("chest state")
	}
	pos := simPos{dim: 0, blockPos: blockPos{2, 100, 2}}
	onHub(t, h, func() {
		h.world.SetBlock(2, 100, 2, chestSt)
		h.world.SetBlock(3, 100, 2, parseBlockOrAir("stone"))
		c := &chest{}
		c.slots[4] = invStack{item: itemByName["diamond"], count: 7}
		h.chests[pos] = c
	})
	s.handleCommand(alice, "data get block 2 100 2 Items[0]")
	s.handleCommand(alice, "data get block 2 100 2 id")
	s.handleCommand(alice, `data modify block 2 100 2 Items append value {Slot: 1b, id: "minecraft:stone", count: 5}`)
	s.handleCommand(alice, "data modify block 2 100 2 Items[{Slot:4b}].count set value 9")
	s.handleCommand(alice, "data get block 3 100 2")
	settle(t, h, logs, "B1")
	a := linesBetween(logs["alice"], "", "B1")
	for _, want := range []string{
		`2, 100, 2 has the following block data: {Slot: 4b, count: 7, id: "minecraft:diamond"}`,
		`2, 100, 2 has the following block data: "minecraft:chest"`,
		"Modified block data of 2, 100, 2",
		"The target block is not a block entity",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	onHub(t, h, func() {
		c := h.chests[pos]
		if c == nil || c.slots[1].item != itemByName["stone"] || c.slots[1].count != 5 {
			t.Errorf("appended item not in the chest: %+v", c)
		}
		if c != nil && c.slots[4].count != 9 {
			t.Errorf("slot 4 count = %d, want 9", c.slots[4].count)
		}
	})
}

// parseBlockOrAir is a test shorthand for a plain block's default state.
func parseBlockOrAir(name string) uint32 {
	st, _ := parseBlockState(name)
	return st
}

// /data on entities: a mob's data is read and merged (name, tags, custom
// data), a player's is read but refused a write, and a selector naming
// several entities is refused.
func TestCommandDataEntity(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "summon pig 4 ~ 0")
	s.handleCommand(alice, "summon pig 6 ~ 0")
	settle(t, h, logs, "E0")
	for _, c := range []string{
		`data merge entity @e[type=pig,limit=1,sort=nearest] {CustomName: "Bob", Tags: ["x"], data: {q: 1}}`,
		`data get entity @e[type=pig,limit=1,sort=nearest] data.q`,
		`data merge entity alice {foodLevel: 3}`,
		`data get entity alice foodLevel`,
		`data get entity @e[type=pig]`,
		`data modify entity @e[type=pig,limit=1,sort=nearest] Tags append value "y"`,
		`data remove entity @e[type=pig,limit=1,sort=nearest] CustomName`,
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "E1")
	a := linesBetween(logs["alice"], "E0", "E1")
	for _, want := range []string{
		"Modified entity data of Pig",
		"Bob has the following entity data: 1",
		"Unable to modify player data",
		"Only one entity is allowed, but the provided selector allows more than one",
		"Modified entity data of Bob",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	food := false
	for _, l := range a {
		food = food || strings.HasPrefix(l, "alice has the following entity data: ")
	}
	if !food {
		t.Errorf("no foodLevel reply in\n%s", strings.Join(a, "\n"))
	}
	onHub(t, h, func() {
		var named *mob
		for _, m := range h.mobs {
			if m.etype == entityPig && m.tags["x"] {
				named = m
			}
		}
		if named == nil {
			t.Error("no pig carries the merged tag")
			return
		}
		if named.customName != "" {
			t.Errorf("CustomName should be removed, is %q", named.customName)
		}
		if !named.tags["y"] {
			t.Error("appended tag missing")
		}
		if named.custom["q"] != nbtInt(1) {
			t.Errorf("custom data = %#v", named.custom)
		}
	})
}

// /data modify … compute: a number provider's value in a loot context,
// stored as a float or an int tag; an exception reads as 0 (getInt), and the
// block and entity contexts are resolved as /compute resolves them.
func TestCommandDataModifyCompute(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	onHub(t, h, func() { h.world.SetBlock(3, 200, 3, worldgen.BlockBase("smoker")) })
	for _, line := range []string{
		`data modify storage t:c f set compute default float {type:div,left:5,right:2}`,
		`data modify storage t:c i set compute default integer {type:add,inputs:[2,3]}`,
		`data modify storage t:c bad set compute default integer {type:"minecraft:div",left:1,right:0}`,
		`data modify storage t:c id set compute default integer minecraft:brewing/uses_default`,
		`data modify storage t:c sm set compute block 3 200 3 integer cooking/time_coal`,
		`data modify storage t:c list append compute default integer {type:add,inputs:[1,1]}`,
		`data modify storage t:c e set compute entity @e[type=pig] integer {type:add,inputs:[1,1]}`,
		`data modify storage t:c x set compute default integer minecraft:nope`,
	} {
		s.handleCommand(alice, line)
	}
	settle(t, h, logs, "DC1")
	a := linesBetween(logs["alice"], "", "DC1")
	for _, want := range []string{
		"Modified storage t:c",
		"No entity was found",
		"Can't find element 'minecraft:nope' in registry 'minecraft:context_int_provider'",
	} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		got := h.storage().get("t:c")
		for k, want := range map[string]any{
			"f": nbtFloat(2.5), "i": nbtInt(5), "bad": nbtInt(0), "id": nbtInt(20), "sm": nbtInt(800),
		} {
			if got[k] != want {
				t.Errorf("%s = %#v, want %#v", k, got[k], want)
			}
		}
		if l, ok := got["list"].(*nbtList); !ok || len(l.elems) != 1 || l.elems[0] != nbtInt(2) {
			t.Errorf("append compute: %#v", got["list"])
		}
		if _, made := got["e"]; made {
			t.Error("a compute whose entity was not found still wrote")
		}
	})
}
