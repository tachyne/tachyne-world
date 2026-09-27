package server

import "testing"

// /waypoint through the dispatcher: the transmitters in the caller's
// dimension, and a player's icon recoloured, restyled and reset — which the
// locator bar's track frame then carries and the player's data keeps.
func TestWaypointCommand(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice, bob := ps["alice"], ps["bob"]
	for _, cmd := range []string{
		"waypoint list",
		"waypoint modify bob color red",
		"waypoint modify bob color pink",
		"waypoint modify bob color hex zzz",
		"waypoint modify nobody color red",
		"waypoint modify bob style set bowtie",
		"waypoint modify bob color hex 0a0",
	} {
		s.handleCommand(alice, cmd)
	}
	settle(t, h, logs, "W1")
	a := linesBetween(logs["alice"], "", "W1")
	for _, want := range []string{
		"3 waypoint(s) in minecraft:overworld: alice, bob, carol",
		"Waypoint color is now red",
		"Unknown color 'pink'",
		"Invalid hex color code 'zzz'",
		"No entity was found",
		"Waypoint style changed",
		"Waypoint color is now 00AA00",
	} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		tb := h.playersRef[bob.eid]
		f := waypointFor(tb, waypointTrack)
		if f.Style != "minecraft:bowtie" || !f.HasColor || f.Color != 0x00AA00 {
			t.Fatalf("bob's track frame %+v", f)
		}
		saved := tb.wpIcon.save().load()
		if saved != tb.wpIcon {
			t.Fatalf("the icon does not survive the player's data: %+v vs %+v", saved, tb.wpIcon)
		}
	})
	s.handleCommand(alice, "waypoint modify bob color reset")
	s.handleCommand(alice, "waypoint modify bob style reset")
	s.handleCommand(alice, "gamemode spectator bob")
	settle(t, h, logs, "W2")
	s.handleCommand(alice, "waypoint list")
	settle(t, h, logs, "W3")
	a = linesBetween(logs["alice"], "W1", "W3")
	if !hasLine(a, "Reset waypoint color") || !hasLine(a, "2 waypoint(s) in minecraft:overworld: alice, carol") {
		t.Errorf("after the reset: %q", a)
	}
	onHub(t, h, func() {
		if ic := h.playersRef[bob.eid].wpIcon; ic != (waypointIcon{}) || ic.save() != nil {
			t.Errorf("reset icon %+v", ic)
		}
	})
}
