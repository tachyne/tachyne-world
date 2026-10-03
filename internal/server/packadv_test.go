package server

import (
	"testing"
	"time"
)

// Data pack advancements: a root that minecraft:tick earns at once and
// pays its rewards (experience, a function run as the player, a recipe), a
// child earned by holding a diamond, an impossible one only /advancement
// grants, an orphan (its parent never loads) left out, and a vanilla
// advancement a broken file at its id removes along with its subtree. The
// pack's tree is laid out (the child one column right of its root).
func TestDataPackAdvancements(t *testing.T) {
	disp := func(title string) string {
		return `"display":{"title":"` + title + `","description":{"text":"d"},"icon":{"id":"minecraft:stone"}}`
	}
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/advancement/root.json": `{` + disp("Test Root") + `,"criteria":{"t":{"trigger":"minecraft:tick"}},` +
			`"rewards":{"experience":5,"function":"test:reward","recipes":["minecraft:crafting_table"]}}`,
		"data/test/advancement/dig.json": `{"parent":"test:root",` + disp("Dig") + `,"criteria":{"get":{"trigger":"minecraft:inventory_changed",` +
			`"conditions":{"items":[{"items":"minecraft:diamond"}]}}}}`,
		"data/test/advancement/never.json":                `{"parent":"test:root",` + disp("Never") + `,"criteria":{"x":{"trigger":"minecraft:impossible"}}}`,
		"data/test/advancement/orphan.json":               `{"parent":"test:missing","criteria":{"t":{"trigger":"minecraft:tick"}}}`,
		"data/minecraft/advancement/story/mine_stone.json": `{}`,
		"data/test/function/reward.mcfunction":            "tag @s add rewarded\n",
	})
	alice := ps["alice"]
	run := exRunner(t, s, h, logs, alice)
	settle(t, h, logs, "A0")
	reg := curAdv()
	for _, id := range []string{"test:root", "test:dig", "test:never"} {
		if reg.byID[id] == nil {
			t.Errorf("%s did not load", id)
		}
	}
	for _, id := range []string{"test:orphan", "minecraft:story/mine_stone", "minecraft:story/upgrade_tools"} {
		if reg.byID[id] != nil {
			t.Errorf("%s loaded", id)
		}
	}
	if r, d := reg.byID["test:root"], reg.byID["test:dig"]; r == nil || d == nil || r.display.x != 0 || d.display.x != 1 {
		t.Errorf("layout: root %+v, dig %+v", r, d)
	}
	if advByID["minecraft:story/mine_stone"] == nil {
		t.Error("the generated table lost a node")
	}
	waitAdv := func(id string) {
		t.Helper()
		deadline := time.Now().Add(hubTestWait)
		for {
			var done bool
			onHub(t, h, func() {
				if tr := h.playersRef[alice.eid]; tr != nil && tr.adv != nil {
					done = tr.adv.done(curAdv().byID[id])
				}
			})
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never completed", id)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitAdv("test:root")
	run("give alice diamond")
	waitAdv("test:dig")
	if got := run("advancement grant alice only test:never"); !hasLine(got, "Granted the advancement [Never] to alice") {
		t.Errorf("grant: %q", got)
	}
	deadline := time.Now().Add(hubTestWait)
	for !hasLine(run("execute if entity @a[name=alice,tag=rewarded]"), "Test passed. Count: 1") {
		if time.Now().After(deadline) {
			t.Fatal("the reward function never ran as alice")
		}
		time.Sleep(50 * time.Millisecond)
	}
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		known := false
		for _, id := range recipeIDsByName("crafting_table") {
			known = known || tr.rbKnown[id]
		}
		if !known {
			t.Error("the reward recipe was not unlocked")
		}
		if tr.xpLevel == 0 && tr.xpPoints == 0 {
			t.Error("no experience was paid")
		}
		if !tr.advVisible["test:root"] {
			t.Error("the pack's root is not in alice's tree")
		}
	})
}
