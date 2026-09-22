package server

import "testing"

// cullSpecies is the one-time sweep for a species that has built up: it takes
// the WILD ones and leaves anything a player has a claim on.
func TestCullSpeciesKeepsWhatPlayersOwn(t *testing.T) {
	s := newMobStore("")
	s.m.Chunks = map[string][]savedMob{
		"0,0": {
			{Etype: entityEnderman},                      // wild — goes
			{Etype: entityEnderman, CarriedBlk: 10},      // wild carrier — goes
			{Etype: entityEnderman, CustomName: "Steve"}, // named — stays
			{Etype: entityEnderman, Persistent: true},    // gear — stays
			{Etype: entityZombie},                        // another species — stays
		},
		"1,1": {
			{Etype: entityEnderman, Tamed: true}, // not a thing, but the rule must hold
			{Etype: entityEnderman},              // wild — goes
		},
		"2,2": {{Etype: entityEnderman}}, // the whole bucket empties and the key goes
	}

	before, after, removed := s.cullSpecies(map[int]bool{entityEnderman: true})
	if before != 8 || after != 4 || removed != 4 {
		t.Fatalf("before/after/removed = %d/%d/%d, want 8/4/4", before, after, removed)
	}
	if _, still := s.m.Chunks["2,2"]; still {
		t.Error("an emptied chunk should be dropped from the store")
	}
	kept := s.m.Chunks["0,0"]
	if len(kept) != 3 {
		t.Fatalf("chunk 0,0 kept %d, want 3", len(kept))
	}
	for _, m := range kept {
		if m.Etype == entityEnderman && m.CustomName == "" && !m.Persistent {
			t.Errorf("a wild enderman survived the cull: %+v", m)
		}
	}
	// A species not named is untouched.
	if _, _, n := s.cullSpecies(map[int]bool{entityCreeper: true}); n != 0 {
		t.Errorf("culling creepers removed %d mobs, want 0", n)
	}
}
