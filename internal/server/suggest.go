package server

import (
	"slices"
	"sort"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Command suggestions (ServerGamePacketListenerImpl.handleCustomCommandSuggestions).
// The command tree marks every free-text argument ask_server, so while a
// player types one the client asks, and the world answers from what it
// knows about that command's argument there: sub-commands, rule names,
// effect, enchantment, entity, block, particle, structure and biome ids,
// players online. The range is the word being typed, in the text as sent
// (its leading slash included), as brigadier reports it.

type evSuggest struct {
	eid  int32
	id   int32
	text string
}

func (evSuggest) isHubEvent() {}

func (h *hub) onSuggest(players map[int32]*tracked, e evSuggest) {
	t := players[e.eid]
	if t == nil {
		return
	}
	start, matches := h.suggest(players, e.text)
	t.p.trySendEv(attachproto.Suggestions{ID: e.id, Start: int32(start), Length: int32(len(e.text) - start), Matches: matches})
}

// suggest is the whole answer: where the word being typed starts, and the
// candidates for it.
func (h *hub) suggest(players map[int32]*tracked, text string) (int, []string) {
	off := 0
	body := text
	if strings.HasPrefix(body, "/") {
		off, body = 1, body[1:]
	}
	words := strings.Split(body, " ")
	last := words[len(words)-1]
	start := off + len(body) - len(last)
	if len(words) == 1 {
		return start, prefixed(allCommandNames(), last)
	}
	cands := h.argCandidates(players, strings.ToLower(words[0]), words[1:len(words)-1])
	return start, prefixed(cands, last)
}

// prefixed keeps the candidates the typed text could lead to — vanilla's
// SharedSuggestionProvider.matchesSubStr also matches after a namespace or
// an underscore.
func prefixed(cands []string, typed string) []string {
	typed = strings.ToLower(typed)
	var out []string
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			continue
		}
		lc := strings.ToLower(c)
		ok := strings.HasPrefix(lc, typed)
		if !ok && !strings.Contains(typed, ":") {
			if i := strings.Index(lc, ":"); i >= 0 {
				ok = strings.HasPrefix(lc[i+1:], typed)
			}
		}
		if !ok && typed != "" {
			for _, part := range strings.FieldsFunc(lc, func(r rune) bool { return r == '_' || r == ':' || r == '/' }) {
				if strings.HasPrefix(part, typed) {
					ok = true
					break
				}
			}
		}
		if ok {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func allCommandNames() []string {
	out := append([]string(nil), commandNames...)
	for _, n := range modelledCommands() {
		out = append(out, n.lit)
	}
	return out
}

// argCandidates is what may come next after a command and the arguments
// already typed.
func (h *hub) argCandidates(players map[int32]*tracked, cmd string, prev []string) []string {
	n := len(prev) // the index of the argument being typed
	at := func(i int) string {
		if i < len(prev) {
			return strings.ToLower(prev[i])
		}
		return ""
	}
	names := func() []string {
		out := []string{"@a", "@e", "@p", "@r", "@s"}
		for _, t := range players {
			out = append(out, t.p.name)
		}
		return out
	}
	switch cmd {
	case "gamerule":
		switch n {
		case 0:
			return append(append([]string(nil), booleanRules...), numericRules...)
		case 1:
			if r, ok := canonicalRule(prev[0]); ok && !isNumericRule(r) {
				return []string{"true", "false"}
			}
		}
	case "effect":
		switch {
		case n == 0:
			return []string{"give", "clear"}
		case n == 1:
			return names()
		case n == 2:
			return nsNames(mapKeys(effectNames))
		case n == 4 && at(0) == "give":
			return []string{"0", "1", "2", "3", "4"}
		}
	case "enchant":
		switch n {
		case 0:
			return names()
		case 1:
			var out []string
			for _, d := range enchDefs {
				if d.name != "" {
					out = append(out, "minecraft:"+d.name)
				}
			}
			return out
		}
	case "summon":
		if n == 0 { // SUMMONABLE_ENTITIES, of the types the engine models
			var out []string
			for name, et := range entityByName {
				if _, ok := summonableType(name); ok && !notSummonable[et] {
					out = append(out, name)
				}
			}
			return nsNames(out)
		}
	case "setblock":
		if n == 3 {
			return nsNames(worldgen.AllBlockNames())
		}
		if n == 4 {
			return []string{"destroy", "keep", "replace", "strict"}
		}
	case "fill":
		if n == 6 {
			return nsNames(worldgen.AllBlockNames())
		}
		if n == 7 {
			return []string{"destroy", "hollow", "keep", "outline", "replace", "strict"}
		}
		if n == 8 && at(7) == "replace" { // a BlockPredicate: a block or any #tag
			out := nsNames(worldgen.AllBlockNames())
			for _, t := range worldgen.BlockTagList() {
				out = append(out, "#minecraft:"+t)
			}
			return out
		}
	case "compute":
		kindAt := map[string]int{"default": 1, "block": 4, "entity": 2}[at(0)]
		switch {
		case n == 0:
			return []string{"default", "block", "entity"}
		case n == 1 && at(0) == "entity":
			return names()
		case kindAt > 0 && n == kindAt:
			return []string{"float", "integer"}
		case kindAt > 0 && n == kindAt+1:
			return computeProviderIDs(at(kindAt) == "float")
		}
	case "particle":
		if n == 0 {
			return nsNames(mapKeys(particleByName))
		}
	case "locate":
		switch {
		case n == 0:
			return []string{"biome", "structure", "poi"}
		case n == 1 && at(0) == "structure":
			return nsNames(worldgen.StructureNames())
		case n == 1 && at(0) == "biome":
			return biomeIDs()
		}
	case "weather":
		if n == 0 {
			return []string{"clear", "rain", "thunder"}
		}
	case "difficulty":
		if n == 0 {
			return []string{"peaceful", "easy", "normal", "hard"}
		}
	case "defaultgamemode":
		if n == 0 {
			return []string{"survival", "creative", "adventure", "spectator"}
		}
	case "time":
		verbs := []string{"add", "pause", "query", "rate", "resume", "set"}
		i := 0 // where the verb is: after of <clock>, two further on
		if at(0) == "of" {
			if n == 1 {
				return clockKeys[:]
			}
			i = 2
		}
		switch {
		case n == 0:
			return append(verbs, "of")
		case n == i:
			return verbs
		case n == i+1 && at(i) == "set": // commandTimeMarkersForClock: the ones shown in commands
			return []string{"day", "midnight", "night", "noon"}
		case n == i+1 && at(i) == "query":
			out := []string{"time"}
			if i == 0 {
				out = append(out, "gametime")
			}
			return append(out, mapKeys(timelines)...)
		case n == i+2 && at(i) == "query" && at(i+1) != "time" && at(i+1) != "gametime":
			return []string{"repetition"}
		}
	case "waypoint":
		switch {
		case n == 0:
			return []string{"list", "modify"}
		case n == 1 && at(0) == "modify":
			return names()
		case n == 2 && at(0) == "modify":
			return []string{"color", "style"}
		case n == 3 && at(2) == "color":
			return append(teamColorNames(), "hex", "reset")
		case n == 3 && at(2) == "style":
			return []string{"reset", "set"}
		case n == 4 && at(2) == "style" && at(3) == "set":
			return []string{"minecraft:bowtie", "minecraft:default"}
		}
	case "tick":
		switch {
		case n == 0:
			return []string{"freeze", "query", "rate", "sprint", "step", "unfreeze"}
		case n == 1 && (at(0) == "step" || at(0) == "sprint"):
			return []string{"stop"}
		}
	case "posteffect":
		switch n {
		case 0:
			return []string{"add", "clear", "list", "remove"}
		case 1:
			return names()
		}
	case "spectate", "kill", "clear", "spawnpoint", "kick", "msg", "tell", "w", "transfer", "xp", "experience":
		if (cmd == "transfer" && n == 2) || (cmd != "transfer" && n == 0) {
			return names()
		}
	case "bossbar":
		switch {
		case n == 0:
			return []string{"add", "get", "list", "remove", "set"}
		case n == 1 && (at(0) == "get" || at(0) == "remove" || at(0) == "set"):
			var out []string
			for id := range h.rules.Bossbars {
				out = append(out, id)
			}
			return out
		case n == 2 && at(0) == "set":
			return []string{"color", "max", "name", "players", "style", "value", "visible"}
		case n == 3 && at(0) == "set" && at(2) == "color":
			return bossColours
		case n == 3 && at(0) == "set" && at(2) == "style":
			return bossStyles
		}
	case "worldborder":
		if n == 0 {
			return []string{"add", "center", "damage", "get", "set", "warning"}
		}
	case "forceload":
		if n == 0 {
			return []string{"add", "query", "remove"}
		}
	case "advancement":
		switch n {
		case 0:
			return []string{"grant", "revoke"}
		case 1:
			return names()
		case 2:
			return []string{"everything", "from", "only", "through", "until"}
		case 3:
			var out []string
			for _, a := range curAdv().nodes {
				out = append(out, a.id)
			}
			return out
		}
	case "help":
		if n == 0 {
			return allCommandNames()
		}
	}
	return nil
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfInt(m map[string]int) []string { return mapKeys(m) }

// nsNames puts the minecraft namespace on bare ids.
func nsNames(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !strings.Contains(s, ":") {
			s = "minecraft:" + s
		}
		out = append(out, s)
	}
	return out
}

// biomeIDs is the canonical biome registry's entries: the shared list
// every client is sent, then the biomes 26.x added (sulfur caves, dappled
// forest), which the 1.21.5 list lacks — without them vanilla's
// #is_overworld and every tag naming it failed to load.
func biomeIDs() []string {
	var out []string
	for _, r := range protocol.SyncedRegistries {
		if r.ID == "minecraft:worldgen/biome" {
			out = append(out, r.Entries...)
		}
	}
	for _, b := range worldgen.BiomeNames() {
		if !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}
