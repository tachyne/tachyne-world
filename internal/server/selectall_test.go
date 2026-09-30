package server

import (
	"reflect"
	"testing"
)

// @e reaches the dropped items as vanilla's does: /tp moves one, /kill
// takes them all and answers "Killed <n> entities", and the players and
// mobs a narrower selector leaves alone stay.
func TestSelectorReachesItems(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	stone := itemByName["stone"]
	var y float64
	onHub(t, h, func() {
		y = h.world.SurfaceY(6, 6)
		h.spawnItemIn(h.playersRef, dimOverworld, stone, 3, 6.5, y+1, 6.5)
		h.spawnItemIn(h.playersRef, dimOverworld, stone, 2, -6.5, y+1, -6.5)
	})
	s.handleCommand(alice, "summon pig 3 ~ 0")
	s.handleCommand(alice, "tp @e[type=minecraft:item,sort=nearest,limit=1,x=6,y=0,z=6] 9 150 9")
	settle(t, h, logs, "IT1")
	onHub(t, h, func() {
		moved := 0
		for _, it := range h.items {
			if it.x > 7.5 && it.x < 10.5 && it.z > 7.5 && it.z < 10.5 {
				moved++
			}
		}
		if moved != 1 {
			t.Errorf("/tp @e[type=item,limit=1] moved %d items, want 1", moved)
		}
		if n := len(h.otherEntities()); n < 2 {
			t.Errorf("otherEntities saw %d entities, want the two items", n)
		}
	})
	s.handleCommand(alice, "kill @e[type=item]")
	settle(t, h, logs, "IT2")
	onHub(t, h, func() {
		if len(h.items) != 0 {
			t.Errorf("/kill @e[type=item] left %d items", len(h.items))
		}
		if len(h.mobs) != 1 {
			t.Errorf("/kill @e[type=item] touched the pig: %d mobs left", len(h.mobs))
		}
	})
	if !hasLine(linesBetween(logs["alice"], "IT1", "IT2"), "Killed 2 entities") {
		t.Errorf("kill feedback: %q", linesBetween(logs["alice"], "IT1", "IT2"))
	}
}

// advancements=, nbt= and predicate= through the dispatcher.
func TestSelectorAdvancementsNBTPredicate(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"advancement grant bob only minecraft:story/root",
		"xp set carol 7 levels",
		"tag @a[advancements={minecraft:story/root=true}] add adv",
		"tag @a[advancements={story/root=false}] add noadv",
		"tag @a[nbt={XpLevel:7}] add nb",
		"tag @a[nbt=!{XpLevel:7}] add nnb",
		"tag @a[predicate=custom:thing] add pr",
		"tag @a[predicate=!custom:thing] add npr",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "ADV")
	onHub(t, h, func() {
		got := func(tag string) []string {
			var out []string
			for _, n := range []string{"alice", "bob", "carol"} {
				for _, tr := range h.playersRef {
					if tr.p.name == n && tr.tags[tag] {
						out = append(out, n)
					}
				}
			}
			return out
		}
		want := map[string][]string{
			"adv": {"bob"}, "noadv": {"alice", "carol"},
			"nb": {"carol"}, "nnb": {"alice", "bob"},
			"pr": nil, "npr": {"alice", "bob", "carol"},
		}
		for tag, names := range want {
			if g := got(tag); !reflect.DeepEqual(g, names) {
				t.Errorf("tag %s went to %v, want %v", tag, g, names)
			}
		}
	})
}

// The option parsers on their own: nested advancement criteria, SNBT with
// quoted commas, and NbtUtils.compareNbt's partial list rule.
func TestSelectorOptionParsing(t *testing.T) {
	spec, ok := parseTargetSpec(`@a[advancements={story/root={crafting_table=true},minecraft:story/mine_stone=false},nbt={Tags:["a,b"]}]`)
	if !ok {
		t.Fatal("selector did not parse")
	}
	if len(spec.advs) != 2 || len(spec.nbts) != 1 {
		t.Fatalf("parsed %d advancement and %d nbt options", len(spec.advs), len(spec.nbts))
	}
	actual := map[string]any{"Tags": []any{"x", "a,b"}, "Health": 20.0}
	if !nbtMatches(spec.nbts[0].tag, actual, true) {
		t.Error("partial list match failed")
	}
	if nbtMatches(map[string]any{"Tags": []any{}}, actual, true) {
		t.Error("an empty expected list must match only an empty list")
	}
	if nbtMatches(map[string]any{"Health": int64(19)}, actual, true) {
		t.Error("a different number matched")
	}
	if _, ok := parseTargetSpec(`@e[nbt=notacompound]`); ok {
		t.Error("nbt= must take a compound")
	}
}

// The tokenizer keeps quoted, bracketed and braced arguments whole.
func TestCommandFieldsQuotes(t *testing.T) {
	got := commandFields(`team add red "Red Team" @e[type=cow, limit=1] {a: 1, b: "x y"} it's 'a b'`)
	want := []string{"team", "add", "red", `"Red Team"`, "@e[type=cow, limit=1]", `{a: 1, b: "x y"}`, "it's", "'a b'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commandFields = %q, want %q", got, want)
	}
	if u := unquoteArg(`"Red \"Team\""`); u != `Red "Team"` {
		t.Errorf("unquoteArg = %q", u)
	}
	if u := unquoteArg("plain"); u != "plain" {
		t.Errorf("unquoteArg(plain) = %q", u)
	}
}

// /msg takes a player selector and whispers to each target.
func TestMsgTakesSelectors(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	s.handleCommand(ps["alice"], "msg @a[name=!alice] hello there")
	s.handleCommand(ps["alice"], "msg @e[type=pig] nobody")
	settle(t, h, logs, "MSG")
	for _, n := range []string{"bob", "carol"} {
		if !hasLine(logs[n].all(), "alice whispers to you: hello there") {
			t.Errorf("%s did not hear the whisper: %q", n, logs[n].all())
		}
	}
	a := logs["alice"].all()
	if !hasLine(a, "You whisper to bob: hello there") || !hasLine(a, "You whisper to carol: hello there") {
		t.Errorf("alice's outgoing lines: %q", a)
	}
	if !hasLine(a, "Only players may be affected by this command, but the provided selector includes entities") {
		t.Errorf("@e[type=pig] is not a players selector: %q", a)
	}
}

// /enchant reaches a mob's main hand.
func TestEnchantMob(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	sword := itemByName["diamond_sword"]
	var z *mob
	onHub(t, h, func() {
		y := h.world.SurfaceY(4, 4)
		z = h.spawnMob(h.playersRef, entityZombie, 4.5, y, 4.5)
		if z == nil {
			t.Error("no zombie")
			return
		}
		z.setHeld(invStack{item: sword, count: 1})
	})
	s.handleCommand(ps["alice"], "enchant @e[type=zombie] sharpness 3")
	settle(t, h, logs, "EN")
	onHub(t, h, func() {
		if z == nil {
			return
		}
		found := false
		for _, e := range z.heldStack().ench {
			if e.id == enchByName["sharpness"] && e.lvl == 3 {
				found = true
			}
		}
		if !found {
			t.Errorf("zombie's sword not enchanted: %v", z.heldStack().ench)
		}
	})
	if !hasLine(logs["alice"].all(), "Applied enchantment sharpness to Zombie's item") {
		t.Errorf("enchant feedback: %q", logs["alice"].all())
	}
}
