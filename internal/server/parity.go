package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Oracle-diff parity batch 1 (see docs/MECHANICS.md "Vanilla oracle"): the
// join-sequence packets vanilla sends that we lacked. Every layout here is
// pinned per-version — minecraft-data for 770, the wiki for 773, ViaVersion
// rewriters (facts only) for the 26.x deltas. server_data is the gateway's
// (it holds the server description).
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
	"advancement", "attribute", "ban", "ban-ip", "banlist", "bossbar", "bug", "clear", "clone", "compute", "damage", "data", "defaultgamemode", "deop",
	"dialog", "difficulty", "effect", "end", "execute", "experience", "fillbiome", "forceload", "gamemode", "gamerule",
	"give", "gm", "help", "hud", "item", "kick", "kill", "list", "locate", "loot", "msg", "nether", "op",
	"pardon", "pardon-ip", "particle", "place", "playsound", "plugin", "posteffect", "random", "recipe", "refresh",
	"rescue", "ride", "rotate", "save-all", "save-off", "save-on", "say", "scoreboard", "setidletimeout", "setworldspawn", "spawnpoint",
	"spectate", "spreadplayers", "stop", "stopsound", "stopwatch", "summon", "swing", "tag", "team", "teammsg", "teleport", "tellraw",
	"tell", "tick", "time", "tm", "tp", "transfer", "trigger", "version", "w", "waypoint", "weather", "where", "whitelist", "worldborder", "xp",
	"function", "return", "schedule", "reload", "datapack",
}

// buildCommandTree composes the whole tree, every command in it (what a
// level-4 operator is sent).
func buildCommandTree(extra ...string) []byte {
	return buildCommandTreeFor(permOwners, nil, extra...)
}

// buildCommandTreeFor composes the tree a permission level is sent: the
// modelled grammars (commandtree.go) for the commands worth describing, and
// a literal with one greedy argument for everything else, including the
// plugin-registered names passed in — each only when the level may run it
// (Commands.fillUsableCommands: a root node whose requires() the source
// fails is left out). opOnly names the plugin commands for operators.
func buildCommandTreeFor(level int, opOnly map[string]bool, extra ...string) []byte {
	usable := func(name string) bool {
		if need, ok := cmdPermission[name]; ok {
			return level >= need
		}
		return !opOnly[name] || level >= permGamemasters
	}
	var roots []cmdNode
	have := map[string]bool{}
	for _, n := range modelledCommands() {
		have[n.lit] = true
		if usable(n.lit) {
			roots = append(roots, n)
		}
	}
	for _, name := range append(append([]string{}, commandNames...), extra...) {
		if have[name] {
			continue
		}
		have[name] = true
		if usable(name) {
			roots = append(roots, lit(name, true, argGreedy("args", true)))
		}
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
		if f.n != nil && f.n.lit == "" && f.n.suggest {
			flags |= 0x10 // custom suggestions follow the properties
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
		if f.n.suggest {
			b = protocol.AppendString(b, "minecraft:ask_server")
		}
	}
	return protocol.AppendVarInt(b, 0) // root index
}
