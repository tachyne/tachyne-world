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
	"ban", "difficulty", "effect", "end", "gamemode", "gamerule", "give",
	"gm", "help", "hud", "kill", "list", "nether", "pardon", "refresh",
	"rescue", "say", "summon", "teleport", "time", "tp", "weather",
	"scoreboard", "team", "where", "whitelist", "xp",
}

// commandTreeBody is the static Commands packet body: node 0 = root, node 1 =
// one shared greedy-string argument node, nodes 2.. = one literal per command,
// all pointing at node 1. Sharing the argument node keeps the packet tiny
// (brigadier trees are DAGs; vanilla shares nodes via redirects the same way).
var commandTreeBody = buildCommandTree()

func buildCommandTree() []byte {
	b := protocol.AppendVarInt(nil, int32(2+len(commandNames)))
	// node 0: root (type 0), children = every literal
	b = protocol.AppendU8(b, 0x00)
	b = protocol.AppendVarInt(b, int32(len(commandNames)))
	for i := range commandNames {
		b = protocol.AppendVarInt(b, int32(2+i))
	}
	// node 1: argument "args" (type 2 | executable 0x04), greedy string
	b = protocol.AppendU8(b, 0x06)
	b = protocol.AppendVarInt(b, 0) // no children
	b = protocol.AppendString(b, "args")
	b = protocol.AppendVarInt(b, parserBrigadierString)
	b = protocol.AppendVarInt(b, stringPropGreedy)
	// nodes 2..: literals (type 1 | executable 0x04), child = node 1
	for _, name := range commandNames {
		b = protocol.AppendU8(b, 0x05)
		b = protocol.AppendVarInt(b, 1)
		b = protocol.AppendVarInt(b, 1)
		b = protocol.AppendString(b, name)
	}
	return protocol.AppendVarInt(b, 0) // root index
}
