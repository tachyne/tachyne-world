package server

import (
	"testing"
)

// /particle through the dispatcher: pos, the delta/speed/count group,
// force|normal and viewers; the 32-block reach of normal and 512 of force;
// "not visible for anybody"; and the refusals — an unknown particle, and one
// with options the particle frame cannot carry.
func TestParticleCommandDepth(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	count := func(name string) int {
		c := logs[name]
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.particles)
	}
	s.handleCommand(alice, "particle minecraft:crit ~ ~1 ~ 0.5 1 0.25 0.1 12 normal @s")
	settle(t, h, logs, "P1")
	if count("alice") != 1 || count("bob") != 0 {
		t.Fatalf("viewers @s: alice %d, bob %d", count("alice"), count("bob"))
	}
	logs["alice"].mu.Lock()
	pa := logs["alice"].particles[0]
	logs["alice"].mu.Unlock()
	if pa.PID != particleByName["crit"] || pa.Count != 12 || pa.Speed != 0.1 || pa.Spread != 1 {
		t.Errorf("burst %+v", pa)
	}
	s.handleCommand(alice, "particle crit ~ ~ ~100 0 0 0 0 1 normal")
	s.handleCommand(alice, "particle crit ~ ~ ~100 0 0 0 0 1 force @a")
	s.handleCommand(alice, "particle crit")
	s.handleCommand(alice, "particle no_such_particle")
	s.handleCommand(alice, "particle dust{color:[1.0,0.0,0.0],scale:1.0} ~ ~ ~")
	s.handleCommand(alice, "particle crit ~ ~ ~ 0 0 0 0.1")
	settle(t, h, logs, "P2")
	a := linesBetween(logs["alice"], "P1", "P2")
	for _, want := range []string{
		"The particle was not visible for anybody",
		"Displaying particle minecraft:crit",
		"Unknown particle: minecraft:no_such_particle",
		"The particle minecraft:dust can't be shown yet: particles with options (or new in 26.x) have no way to the client",
		"Usage: /particle <name>[<options>] [<pos> [<delta> <speed> <count> [force|normal [<viewers>]]]]",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	// force at 100 blocks reached all three; the bare /particle reached all three.
	if count("bob") != 2 || count("alice") != 3 {
		t.Errorf("after force and a bare burst: alice %d, bob %d", count("alice"), count("bob"))
	}
}
