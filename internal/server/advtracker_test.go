package server

import "testing"

// TestAdvTableIntegrity sanity-checks the generated tree: parents resolve,
// every requirement name is a declared criterion, displayed roots carry a
// background, and the tab roots exist.
func TestAdvTableIntegrity(t *testing.T) {
	for i := range advTable {
		n := &advTable[i]
		if n.parent != "" {
			if _, ok := advByID[n.parent]; !ok {
				t.Errorf("%s: parent %s missing", n.id, n.parent)
			}
		} else if n.display == nil || n.display.background == "" {
			t.Errorf("%s: root without display background", n.id)
		}
		crits := map[string]bool{}
		for j := range n.criteria {
			crits[n.criteria[j].name] = true
		}
		for _, g := range n.reqs {
			for _, c := range g {
				if !crits[c] {
					t.Errorf("%s: requirement %q not a criterion", n.id, c)
				}
			}
		}
	}
	for _, root := range []string{"minecraft:story/root", "minecraft:nether/root",
		"minecraft:end/root", "minecraft:adventure/root", "minecraft:husbandry/root"} {
		if _, ok := advByID[root]; !ok {
			t.Errorf("missing tab root %s", root)
		}
	}
	if len(advTable) < 120 {
		t.Errorf("only %d advancements generated", len(advTable))
	}
}

// TestAdvGrantSemantics exercises requirements OR-of-AND completion and
// idempotent grants on a real table entry (mine_stone: one criterion) and a
// synthetic multi-group node.
func TestAdvGrantSemantics(t *testing.T) {
	s := advState{}
	ms := advByID["minecraft:story/mine_stone"]
	if s.done(ms) {
		t.Fatal("empty state already done")
	}
	fresh, completed := s.grant(ms, "get_stone")
	if !fresh || !completed {
		t.Fatalf("first grant: fresh=%v completed=%v", fresh, completed)
	}
	fresh, completed = s.grant(ms, "get_stone")
	if fresh || completed {
		t.Fatalf("re-grant not idempotent: fresh=%v completed=%v", fresh, completed)
	}

	n := &advNode{id: "t:multi", reqs: [][]string{{"a", "b"}, {"c"}}}
	s2 := advState{}
	s2.grant(n, "a")
	if s2.done(n) {
		t.Fatal("one group satisfied should not complete")
	}
	if _, completed := s2.grant(n, "c"); !completed {
		t.Fatal("both groups satisfied should complete")
	}
	if _, completed := s2.grant(n, "b"); completed {
		t.Fatal("extra criterion re-completed")
	}
}

// TestAdvMatch covers each distilled matcher shape against real table rows.
func TestAdvMatch(t *testing.T) {
	// inventory_changed with a tag-expanded set (mine_stone: stone tool materials)
	ms := advByID["minecraft:story/mine_stone"].criteria[0]
	if !(advMatch{inv: []int32{5, 9}}).criterion(&ms) {
		t.Error("cobblestone-family inventory should match mine_stone")
	}
	if (advMatch{inv: []int32{2000}}).criterion(&ms) {
		t.Error("unrelated inventory matched mine_stone")
	}
	// changed_dimension
	var nether *advCriterion
	for _, ref := range advByTrigger["changed_dimension"] {
		if ref.node.id == "minecraft:story/enter_the_nether" {
			nether = ref.crit
		}
	}
	if nether == nil {
		t.Fatal("enter_the_nether not indexed")
	}
	if !(advMatch{dim: 1}).criterion(nether) || (advMatch{dim: 2}).criterion(nether) {
		t.Error("dimension matching wrong")
	}
	// player_killed_entity species filter (kill_a_mob: any of 41 criteria)
	refs := advByTrigger["player_killed_entity"]
	var blaze *advCriterion
	for _, r := range refs {
		if r.crit.entity == "blaze" && r.node.id == "minecraft:adventure/kill_a_mob" {
			blaze = r.crit
		}
	}
	if blaze == nil {
		t.Fatal("blaze kill criterion not indexed")
	}
	if !(advMatch{entity: "blaze"}).criterion(blaze) || (advMatch{entity: "zombie"}).criterion(blaze) {
		t.Error("entity matching wrong")
	}
	// unmatchable criteria are excluded from the index
	for trig, refs := range advByTrigger {
		for _, r := range refs {
			if r.crit.unmatchable {
				t.Errorf("unmatchable criterion indexed under %s", trig)
			}
		}
	}
}

// TestAdvVisibility ports the vanilla evaluator's semantics: nothing visible
// on an empty state; done nodes reveal a 2-deep frontier; hidden nodes stay
// invisible until earned, even under a done parent.
func TestAdvVisibility(t *testing.T) {
	s := advState{}
	if v := s.visible(); len(v) != 0 {
		t.Fatalf("fresh state should see nothing, got %d", len(v))
	}

	// complete story/root → root + children + grandchildren visible, depth-3 not
	s.grant(advByID["minecraft:story/root"], "crafting_table")
	v := s.visible()
	for _, want := range []string{"minecraft:story/root", "minecraft:story/mine_stone",
		"minecraft:story/upgrade_tools"} {
		if !v[want] {
			t.Errorf("%s should be visible", want)
		}
	}
	if v["minecraft:story/smelt_iron"] { // depth 3 from the done root
		t.Error("smelt_iron should still be beyond the frontier")
	}
	if v["minecraft:nether/root"] {
		t.Error("untouched tab leaked")
	}

	// hidden stays invisible under a done parent until earned
	s2 := advState{}
	ka := advByID["minecraft:adventure/kill_a_mob"] // voluntary_exile's parent
	for _, g := range ka.reqs {
		s2.grant(ka, g[0])
	}
	if !s2.done(ka) {
		t.Fatal("kill_a_mob should be done")
	}
	v2 := s2.visible()
	if !v2["minecraft:adventure/kill_a_mob"] {
		t.Fatal("done node invisible")
	}
	if v2["minecraft:adventure/voluntary_exile"] {
		t.Error("hidden voluntary_exile leaked before being earned")
	}
	s2.grant(advByID["minecraft:adventure/voluntary_exile"], "voluntary_exile")
	if !s2.visible()["minecraft:adventure/voluntary_exile"] {
		t.Error("earned hidden node should be visible")
	}
}
