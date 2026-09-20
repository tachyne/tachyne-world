package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// /title's shape is vanilla's: a target, a verb, and what follows. The text
// verbs take the rest of the line, so a title can have spaces in it.
func TestTitleCommandBuildsTheFrame(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want attachproto.Title
		bar  string
	}{
		{[]string{"@a", "title", "The", "End"}, attachproto.Title{Title: "The End"}, ""},
		{[]string{"@a", "subtitle", "well", "done"}, attachproto.Title{Subtitle: "well done"}, ""},
		{[]string{"@a", "times", "10", "70", "20"}, attachproto.Title{FadeIn: 10, Stay: 70, FadeOut: 20}, ""},
		{[]string{"@a", "title", "a  b"}, attachproto.Title{Title: "a  b"}, ""},
		{[]string{"@a", "clear"}, attachproto.Title{Clear: true}, ""},
		{[]string{"@a", "reset"}, attachproto.Title{Clear: true, Reset: true}, ""},
		{[]string{"@a", "actionbar", "watch", "out"}, attachproto.Title{}, "watch out"},
	} {
		h := newHub(world.New(1))
		by := newPlayer(1, "op", [16]byte{})
		tr := &tracked{p: by}
		players := map[int32]*tracked{1: tr}
		// The command's own parser, then the hub's handler.
		ev, complaint := parseTitle(by, tc.args)
		if complaint != "" {
			t.Fatalf("%v was refused: %s", tc.args, complaint)
		}
		drainEvents(tr)
		h.onTitle(players, ev)
		got := takeEvents(tr)
		if len(got) != 1 {
			t.Fatalf("%v: %d frames, want 1", tc.args, len(got))
		}
		if tc.bar != "" {
			c, ok := got[0].(attachproto.Chat)
			if !ok || !c.ActionBar || c.Text != tc.bar {
				t.Errorf("%v: sent %#v, want an action-bar chat %q", tc.args, got[0], tc.bar)
			}
			continue
		}
		title, ok := got[0].(attachproto.Title)
		if !ok {
			t.Fatalf("%v: sent %T, want Title", tc.args, got[0])
		}
		if title != tc.want {
			t.Errorf("%v: got %+v, want %+v", tc.args, title, tc.want)
		}
	}
}

// …and what it refuses, with a word about why rather than silence.
func TestTitleCommandRefusals(t *testing.T) {
	by := newPlayer(1, "op", [16]byte{})
	for _, tc := range []struct {
		args []string
		want string // a fragment of the complaint
	}{
		{[]string{}, "Usage"},
		{[]string{"@a"}, "Usage"},
		{[]string{"@a", "wobble"}, "Usage"},
		{[]string{"@a", "title"}, "Say what to show"},
		{[]string{"@a", "actionbar", "   "}, "Say what to show"},
		{[]string{"@a", "times", "10", "70"}, "fade in"},
		{[]string{"@a", "times", "10", "seventy", "20"}, "whole numbers"},
		{[]string{"@a", "times", "10", "-5", "20"}, "whole numbers"},
	} {
		_, complaint := parseTitle(by, tc.args)
		if complaint == "" {
			t.Errorf("%v was accepted, want a complaint", tc.args)
			continue
		}
		if !strings.Contains(complaint, tc.want) {
			t.Errorf("%v: complaint %q, want it to mention %q", tc.args, complaint, tc.want)
		}
	}
}

// A kick shows the reason on the disconnect screen rather than a chat line
// the player never gets to read before the socket closes.
func TestKickSendsADisconnectScreen(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   string
	}{
		{"building in spawn", "building in spawn"},
		{"   ", "Kicked by an operator"}, // no reason given still says something
	} {
		h := newHub(world.New(1))
		victim := &tracked{p: newPlayer(2, "Legion", [16]byte{})}
		by := newPlayer(1, "op", [16]byte{})
		players := map[int32]*tracked{2: victim}
		drainEvents(victim)

		h.onKick(players, evKick{by: by, name: "legion", reason: tc.reason})

		var got *attachproto.Disconnect
		for _, ev := range takeEvents(victim) {
			if d, ok := ev.(attachproto.Disconnect); ok {
				got = &d
			}
		}
		if got == nil {
			t.Fatalf("%q: no disconnect frame was sent", tc.reason)
		}
		if got.Reason != tc.want {
			t.Errorf("%q: the screen says %q, want %q", tc.reason, got.Reason, tc.want)
		}
	}
}
