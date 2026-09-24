package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Oracle-diff parity batch 1 (see docs/MECHANICS.md "Vanilla oracle"): the
// join-sequence packets vanilla sends that we lacked. Every layout here is
// pinned per-version — minecraft-data for 770, the wiki for 773, ViaVersion
// rewriters (facts only) for the 26.x deltas. server_data is deliberately
// omitted: 770 and 773 sources disagree on a trailing boolean with no
// ViaVersion rewriter to arbitrate, and a wrong guess is a decode kick.
const (

	// brigadier:string is parser id 5 on EVERY version we serve: the argument
	// type registry grows append-only 770→776 (55→57 entries, verified against
	// Mojang's datagen report and ViaVersion's 26.2 mapping).
	parserBrigadierString = 5
	stringPropGreedy      = 2 // greedy phrase: the rest of the line is one arg
)

// commandNames: every /command the dispatcher accepts (chat.go + admin.go).
// The tree is advisory — it buys client-side tab-completion and un-reddened
// input; execution still validates ops and arguments server-side.
var commandNames = []string{
	"advancement", "attribute", "ban", "bossbar", "bug", "clear", "clone", "damage", "defaultgamemode",
	"difficulty", "effect", "end", "forceload", "gamemode", "gamerule",
	"give", "gm", "help", "hud", "kick", "kill", "list", "locate", "msg", "nether",
	"pardon", "particle", "playsound", "plugin", "random", "recipe", "refresh",
	"rescue", "ride", "save-all", "save-off", "save-on", "say", "scoreboard", "setworldspawn", "spawnpoint",
	"spreadplayers", "stop", "summon", "swing", "tag", "team", "teammsg", "teleport",
	"tell", "time", "tm", "tp", "version", "w", "weather", "where", "whitelist", "worldborder", "xp",
}

// commandTreeBody is the Commands packet body sent at join.
var commandTreeBody = buildCommandTree()

// buildCommandTree composes the tree: the modelled grammars (commandtree.go)
// for the commands worth describing, and a literal with one greedy argument
// for everything else, including the plugin-registered names passed in.
func buildCommandTree(extra ...string) []byte {
	roots := modelledCommands()
	have := map[string]bool{}
	for _, n := range roots {
		have[n.lit] = true
	}
	for _, name := range append(append([]string{}, commandNames...), extra...) {
		if have[name] {
			continue
		}
		have[name] = true
		roots = append(roots, lit(name, true, argGreedy("args", true)))
	}
	return encodeCommandTree(roots)
}

// encodeCommandTree flattens the tree and writes the Commands packet body:
// node count, the nodes, then the root's index. Node 0 is the root; children
// are laid out depth-first after it.
func encodeCommandTree(roots []cmdNode) []byte {
	type flat struct {
		n    *cmdNode
		kids []int32
	}
	nodes := []flat{{}} // node 0: the root, children filled in below
	var add func(n *cmdNode) int32
	add = func(n *cmdNode) int32 {
		idx := int32(len(nodes))
		nodes = append(nodes, flat{n: n})
		kids := make([]int32, 0, len(n.children))
		for i := range n.children {
			kids = append(kids, add(&n.children[i]))
		}
		nodes[idx].kids = kids
		return idx
	}
	for i := range roots {
		nodes[0].kids = append(nodes[0].kids, add(&roots[i]))
	}

	b := protocol.AppendVarInt(nil, int32(len(nodes)))
	for _, f := range nodes {
		var flags byte
		switch {
		case f.n == nil:
			flags = 0x00 // root
		case f.n.lit != "":
			flags = 0x01 // literal
		default:
			flags = 0x02 // argument
		}
		if f.n != nil && f.n.exec {
			flags |= 0x04
		}
		b = protocol.AppendU8(b, flags)
		b = protocol.AppendVarInt(b, int32(len(f.kids)))
		for _, k := range f.kids {
			b = protocol.AppendVarInt(b, k)
		}
		if f.n == nil {
			continue
		}
		if f.n.lit != "" {
			b = protocol.AppendString(b, f.n.lit)
			continue
		}
		b = protocol.AppendString(b, f.n.arg)
		b = protocol.AppendVarInt(b, f.n.parser)
		b = append(b, f.n.props...)
	}
	return protocol.AppendVarInt(b, 0) // root index
}
