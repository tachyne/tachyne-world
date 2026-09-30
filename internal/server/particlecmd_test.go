package server

import (
	"testing"
)

// /particle through the dispatcher: pos, the delta/speed/count group,
// force|normal and viewers; the 32-block reach of normal and 512 of force;
// "not visible for anybody"; options for the types that take them; and the
// refusals — an unknown particle, and one new in 26.x.
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
	if pa.PID != particleByName["crit"] || pa.Count != 12 || pa.Speed != 0.1 ||
		pa.DX != 0.5 || pa.DY != 1 || pa.DZ != 0.25 || pa.Spread != 0 || pa.Force || pa.Options != nil {
		t.Errorf("burst %+v", pa)
	}
	s.handleCommand(alice, "particle crit ~ ~ ~100 0 0 0 0 1 normal")
	s.handleCommand(alice, "particle crit ~ ~ ~100 0 0 0 0 1 force @a")
	s.handleCommand(alice, "particle crit")
	s.handleCommand(alice, "particle no_such_particle")
	s.handleCommand(alice, "particle dust{color:[1.0,0.0,0.0],scale:1.0} ~ ~ ~")
	s.handleCommand(alice, "particle crit ~ ~ ~ 0 0 0 0.1")
	s.handleCommand(alice, "particle sulfur_bubbles")
	s.handleCommand(alice, "particle dust{color:[1.0,0.0,0.0]}")
	settle(t, h, logs, "P2")
	a := linesBetween(logs["alice"], "P1", "P2")
	for _, want := range []string{
		"The particle was not visible for anybody",
		"Displaying particle minecraft:crit",
		"Unknown particle: minecraft:no_such_particle",
		"Displaying particle minecraft:dust",
		"The particle minecraft:sulfur_bubbles can't be shown yet: it is new in 26.x and has no canonical id",
		"Can't parse particle options: No key scale in MapLike",
		"Usage: /particle <name>[<options>] [<pos> [<delta> <speed> <count> [force|normal [<viewers>]]]]",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	// force at 100 blocks reached all three; the bare /particle and the dust
	// reached all three.
	if count("bob") != 3 || count("alice") != 4 {
		t.Errorf("after force and a bare burst: alice %d, bob %d", count("alice"), count("bob"))
	}
}

// Each option class decodes as its CODEC does: colours as ints or unit-float
// lists, scale clamped, SpellParticleOption's defaults, block states by
// string or {Name, Properties}, items by id or {id, count}, and a trail's
// target, colour and duration.
func TestParticleOptionsParse(t *testing.T) {
	_, _, o, msg := parseParticleArg("dust{color:[1.0,0.5,0.0],scale:9}")
	if msg != "" || o == nil || o.Color != 0xFF7F00 || o.Scale != 4 {
		t.Errorf("dust: %+v %q", o, msg)
	}
	if pid, _, o, msg := parseParticleArg("minecraft:dust_color_transition{from_color:16711680,to_color:[0,0,1],scale:0.5}"); msg != "" || pid != 14 || o.Color != 0xFF0000 || o.ToColor != 0x0000FF || o.Scale != 0.5 {
		t.Errorf("dust_color_transition: %d %+v %q", pid, o, msg)
	}
	if _, _, o, msg := parseParticleArg("effect"); msg != "" || o == nil || o.Color != -1 || o.Power != 1 {
		t.Errorf("bare effect: %+v %q, want the defaults", o, msg)
	}
	if _, _, o, msg := parseParticleArg("entity_effect{color:[1.0,0.0,0.0,1.0]}"); msg != "" || uint32(o.Color) != 0xFFFF0000 {
		t.Errorf("entity_effect: %+v %q", o, msg)
	}
	if _, _, _, msg := parseParticleArg("flash"); msg == "" {
		t.Error("flash without its colour was taken")
	}
	stone, _ := parseBlockState("stone")
	log, _ := parseBlockState("oak_log[axis=x]")
	if _, _, o, msg := parseParticleArg(`block{block_state:"minecraft:stone"}`); msg != "" || o.State != int32(stone) {
		t.Errorf("block by string: %+v %q", o, msg)
	}
	if _, _, o, msg := parseParticleArg(`falling_dust{block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`); msg != "" || o.State != int32(log) {
		t.Errorf("block by compound: %+v %q", o, msg)
	}
	if _, _, o, msg := parseParticleArg(`item{item:{id:"minecraft:diamond",count:3}}`); msg != "" || o.Item == nil || o.Item.ID != itemByName["diamond"] || o.Item.Count != 3 {
		t.Errorf("item: %+v %q", o, msg)
	}
	if _, _, o, msg := parseParticleArg(`trail{target:[1.5,2.0,3.0],color:255,duration:20}`); msg != "" || o.TX != 1.5 || o.TY != 2 || o.TZ != 3 || o.Color != 255 || o.Ticks != 20 {
		t.Errorf("trail: %+v %q", o, msg)
	}
	if _, _, o, msg := parseParticleArg(`vibration{destination:{type:"minecraft:block",pos:[I;4,5,6]},arrival_in_ticks:10}`); msg != "" || o.TX != 4 || o.TY != 5 || o.TZ != 6 || o.Ticks != 10 {
		t.Errorf("vibration: %+v %q", o, msg)
	}
	if _, _, o, msg := parseParticleArg("shriek{delay:7}"); msg != "" || o.Delay != 7 {
		t.Errorf("shriek: %+v %q", o, msg)
	}
}
