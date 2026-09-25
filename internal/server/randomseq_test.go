package server

import (
	"fmt"
	"testing"
)

// /random's named sequences through the dispatcher, against draws taken
// from vanilla's own RandomSequence for the same seeds: a sequence is
// seeded from the world seed and its id, keeps its place between draws,
// starts over on reset, and takes a new salt and include flags from
// `reset * <seed> …`.
func TestRandomSequences(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice, carol := ps["alice"], ps["carol"]
	if h.world.Seed() != 1 {
		t.Fatalf("world seed %d, the oracle draws assume 1", h.world.Seed())
	}
	draws := func(mark string, n int, cmd string) []string {
		from := ""
		if len(logs["alice"].all()) > 0 {
			l := logs["alice"].all()
			from = l[len(l)-1]
		}
		for i := 0; i < n; i++ {
			s.handleCommand(alice, cmd)
		}
		settle(t, h, logs, mark)
		return linesBetween(logs["alice"], from, mark)
	}
	want := func(got []string, vals ...int) {
		t.Helper()
		var w []string
		for _, v := range vals {
			w = append(w, fmt.Sprintf("Randomized value: %d", v))
		}
		if fmt.Sprint(got) != fmt.Sprint(w) {
			t.Errorf("draws %q, want %q", got, w)
		}
	}
	want(draws("R1", 3, "random value 1..100 test"), 42, 61, 55)
	want(draws("R2", 3, "random value 1..100 minecraft:test"), 99, 24, 71)
	if got := draws("R3", 1, "random reset test"); fmt.Sprint(got) != "[Reset random sequence minecraft:test]" {
		t.Errorf("reset: %q", got)
	}
	want(draws("R4", 2, "random value 1..100 test"), 42, 61)
	if got := draws("R5", 1, "random reset * 42 false false"); fmt.Sprint(got) != "[Reset 1 random sequence(s)]" {
		t.Errorf("reset all: %q", got)
	}
	want(draws("R6", 3, "random value 1..100 anything"), 42, 32, 86)
	draws("R7", 1, "random reset foo:bar -7")
	want(draws("R8", 3, "random value 1..100 foo:bar"), 85, 17, 24)

	s.handleCommand(carol, "random value 1..6 test")
	settle(t, h, logs, "R9")
	if got := linesBetween(logs["carol"], "R8", "R9"); !hasLine(got, "You don't have permission.") {
		t.Errorf("a non-operator drew from a sequence: %q", got)
	}
	onHub(t, h, func() {
		if st := h.rules.RandomSequences; st == nil || st.Salt != 42 || !st.NoWorldSeed || len(st.Sequences) != 2 {
			t.Errorf("saved sequences: %+v", st)
		}
	})
}
