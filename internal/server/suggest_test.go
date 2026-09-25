package server

import (
	"slices"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Tab completion answers from what the world knows, for the word being
// typed, in the text's own coordinates (its slash counted).
func TestCommandSuggestions(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	for _, tc := range []struct {
		text  string
		start int
		want  string
	}{
		{"/gamer", 1, "gamerule"},
		{"/gamerule keep_i", 10, "keep_inventory"},
		{"/gamerule keep_inventory ", 25, "true"},
		{"/effect give @s speed", 16, "minecraft:speed"},
		{"/summon cre", 8, "minecraft:creeper"},
		{"/locate structure vill", 18, "minecraft:village_plains"},
		{"/weather th", 9, "thunder"},
	} {
		start, got := h.suggest(players, tc.text)
		if start != tc.start || !slices.Contains(got, tc.want) {
			t.Errorf("%q: start %d (want %d), %d matches, want %q among them", tc.text, start, tc.start, len(got), tc.want)
		}
	}
	// Through the frame: the request comes back as suggestions.
	drainOut(pl.p)
	h.onSuggest(players, evSuggest{eid: pl.p.eid, id: 7, text: "/tick fr"})
	for _, ev := range drainEvs(pl.p) {
		if s, ok := ev.(attachproto.Suggestions); ok && s.ID == 7 && s.Start == 6 && s.Length == 2 && slices.Contains(s.Matches, "freeze") {
			return
		}
	}
	t.Fatal("no suggestions frame for /tick fr")
}
