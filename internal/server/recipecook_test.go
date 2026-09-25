package server

import (
	"strconv"
	"testing"
)

// /recipe takes vanilla's cooking recipe names: a single-input recipe
// unlocks its one book entry, a tag recipe (charcoal) one per log, and the
// smoker's variant is its own recipe.
func TestRecipeCommandCookingNames(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "recipe give bob minecraft:iron_ingot_from_smelting_raw_iron")
	s.handleCommand(alice, "recipe give bob cooked_beef_from_smoking")
	s.handleCommand(alice, "recipe give bob charcoal")
	settle(t, h, logs, "C1")
	a := linesBetween(logs["alice"], "", "C1")
	for _, want := range []string{"Unlocked 1 recipe(s) for bob", "Unlocked 1 recipe(s) for bob"} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	if hasPrefixLine(a, "Unknown recipe") {
		t.Errorf("a vanilla cooking name was refused: %q", a)
	}
	n := len(cookRecipeKeys["charcoal"])
	if n < 10 || !hasLine(a, "Unlocked "+strconv.Itoa(n)+" recipe(s) for bob") {
		t.Errorf("charcoal: %d entries, lines %q", n, a)
	}
	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			if tr.p.name != "bob" {
				continue
			}
			for _, k := range []string{"cook/furnace/raw_iron", "cook/smoker/beef", "cook/furnace/oak_log"} {
				if !tr.rbKnown[recipeIDByName[k]] {
					t.Errorf("bob does not know %s", k)
				}
			}
			if tr.rbKnown[recipeIDByName["cook/furnace/beef"]] {
				t.Error("the smoker's recipe unlocked the furnace's")
			}
		}
	})
}
