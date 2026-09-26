package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// The brigadier command tree the client is sent at join. It used to be one
// literal per command, each with a single greedy-string argument, so the
// client could complete a command NAME and nothing after it: no player-name
// completion, no enumerated values, and every argument rendered valid.
//
// This builds the real shape for the commands whose grammar is worth
// describing, and leaves the rest on the greedy argument. The tree is
// advisory — the dispatcher still validates everything — so it has to agree
// with the dispatcher or the client reddens input that would have worked.
//
// ONLY parser ids 0-15 are used. Those are identical on every protocol
// tachyne serves; 1.21.6 inserted minecraft:style at 19 and shifted every id
// above it, and nothing translates parser ids per version. Enumerations are
// literals rather than the (shifted) minecraft:gamemode and minecraft:time
// parsers, which completes better anyway.
const (
	parserBool      = 0
	parserFloat     = 1
	parserInteger   = 3
	parserString    = 5
	parserEntity    = 6
	parserProfile   = 7
	parserBlockPos  = 8
	parserColumnPos = 9
	parserVec3      = 10
	parserVec2      = 11
	parserItemStack = 14
)

// String parser properties.
const (
	stringPropWord = 0 // a single unquoted word
	// stringPropGreedy is in parity.go: the rest of the line.
)

// Entity parser properties (a flags byte): 0x01 one target only, 0x02 players
// only.
const (
	entitySingle  = 0x01
	entityPlayers = 0x02
)

// cmdNode is one node of the tree: a literal word, or an argument with a
// parser. exec marks a node a command may legally end on.
type cmdNode struct {
	lit      string // literal name ("" for an argument)
	arg      string // argument name
	parser   int32
	props    []byte
	exec     bool
	suggest  bool // ask_server: the client asks the world for completions here
	children []cmdNode
}

func lit(name string, exec bool, kids ...cmdNode) cmdNode {
	return cmdNode{lit: name, exec: exec, children: kids}
}

func argN(name string, parser int32, props []byte, exec bool, kids ...cmdNode) cmdNode {
	return cmdNode{arg: name, parser: parser, props: props, exec: exec, children: kids}
}

// Argument shorthands.
func argEntity(name string, flags byte, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserEntity, []byte{flags}, exec, kids...)
}
func argWord(name string, exec bool, kids ...cmdNode) cmdNode {
	n := argN(name, parserString, protocol.AppendVarInt(nil, stringPropWord), exec, kids...)
	n.suggest = true
	return n
}
func argGreedy(name string, exec bool, kids ...cmdNode) cmdNode {
	n := argN(name, parserString, protocol.AppendVarInt(nil, stringPropGreedy), exec, kids...)
	n.suggest = true
	return n
}
func argInt(name string, min, max int32, exec bool, kids ...cmdNode) cmdNode {
	// brigadier:integer properties: a flags byte (0x01 has min, 0x02 has max)
	// then the bounds that are present.
	p := []byte{0x03}
	p = protocol.AppendI32(p, min)
	p = protocol.AppendI32(p, max)
	return argN(name, parserInteger, p, exec, kids...)
}
func argVec3(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserVec3, nil, exec, kids...)
}
func argVec2(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserVec2, nil, exec, kids...)
}
func argBlockPos(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserBlockPos, nil, exec, kids...)
}
func argColumnPos(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserColumnPos, nil, exec, kids...)
}
func argBool(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserBool, nil, exec, kids...)
}

// argFloat is brigadier:float with a lower bound: the flags byte (0x01, a
// minimum) and the bound as a big-endian float32.
func argFloat(name string, min float32, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserFloat, protocol.AppendF32([]byte{0x01}, min), exec, kids...)
}
func argItem(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserItemStack, nil, exec, kids...)
}
func argProfile(name string, exec bool, kids ...cmdNode) cmdNode {
	return argN(name, parserProfile, nil, exec, kids...)
}

// lits makes one exec-leaf literal per name — an enumeration.
func lits(names ...string) []cmdNode {
	out := make([]cmdNode, 0, len(names))
	for _, n := range names {
		out = append(out, lit(n, true))
	}
	return out
}

// litsWith makes one literal per name, each carrying the same children.
func litsWith(names []string, kids ...cmdNode) []cmdNode {
	out := make([]cmdNode, 0, len(names))
	for _, n := range names {
		out = append(out, lit(n, false, kids...))
	}
	return out
}

// modelledCommands is the grammar of the commands worth describing. Anything
// not here keeps the literal + greedy-argument shape.
func modelledCommands() []cmdNode {
	target := argEntity("targets", 0, true)
	player := argEntity("player", entitySingle|entityPlayers, true)
	gamemodes := []string{"survival", "creative", "adventure", "spectator"}
	gmNode := func(name string) cmdNode {
		return lit(name, false, litsWith(gamemodes, target)...)
	}
	// /gamemode <mode> is itself executable (it means "me").
	gm := func(name string) cmdNode {
		kids := make([]cmdNode, 0, len(gamemodes))
		for _, m := range gamemodes {
			kids = append(kids, lit(m, true, target))
		}
		return lit(name, false, kids...)
	}
	_ = gmNode

	ampTail := argInt("amplifier", 0, 255, true, argBool("hideParticles", true))
	effectName := argWord("effect", true,
		argInt("seconds", 1, 1000000, true, ampTail),
		lit("infinite", true, ampTail))

	return []cmdNode{
		gm("gamemode"),
		gm("gm"),
		lit("give", false, argEntity("targets", 0, false,
			argItem("item", true, argInt("count", 1, math.MaxInt32, true)))),
		lit("kill", true, target),
		// ClearInventoryCommands: [targets [item [maxCount]]].
		lit("clear", true, argEntity("targets", entityPlayers, true,
			argItem("item", true, argInt("maxCount", 0, 2147483647, true)))),
		xpTree("xp"), xpTree("experience"),
		tpTree("tp"), tpTree("teleport"),
		lit("tellraw", false, argEntity("targets", entityPlayers, false, argGreedy("message", true))),
		lit("stopsound", false, argEntity("targets", entityPlayers, true, stopsoundSources()...)),
		lit("effect", false,
			lit("give", false, argEntity("targets", 0, false, effectName)),
			lit("clear", true, argEntity("targets", 0, true, argWord("effect", true)))),
		// TimeCommand: set <day|noon|night|midnight|time>, add <time>,
		// query <daytime|gametime|day>; the bare shorthand the dispatcher
		// also takes stays.
		lit("time", true, append(lits("day", "noon", "night", "midnight"),
			argInt("value", 0, 24000000, true),
			lit("set", false, append(lits("day", "noon", "night", "midnight"), argInt("time", 0, 2147483647, true))...),
			lit("add", false, argInt("time", -2147483648, 2147483647, true)),
			lit("query", false, lits("daytime", "gametime", "day")...))...),
		lit("weather", false,
			lit("clear", true, argInt("duration", 0, 1000000, true)),
			lit("rain", true, argInt("duration", 0, 1000000, true)),
			lit("thunder", true, argInt("duration", 0, 1000000, true))),
		lit("difficulty", true, lits("peaceful", "easy", "normal", "hard")...),
		lit("gamerule", true, gameruleNodes()...),
		lit("summon", false, argWord("entity", true, argVec3("pos", true))),
		lit("setblock", false, argVec3("pos", false, argGreedy("block", true))),
		lit("fill", false, argVec3("from", false, argVec3("to", false, argGreedy("block", true)))),
		// Bar ids carry ':', which a word refuses, so the id and what follows
		// it ride one greedy argument.
		lit("bossbar", false,
			lit("add", false, argGreedy("id name", true)),
			lit("remove", false, argGreedy("id", true)),
			lit("list", true),
			lit("get", false, argGreedy("id value|max|visible|players", true)),
			lit("set", false, argGreedy("id name|color|style|value|max|visible|players …", true))),
		lit("save-all", true, lit("flush", true)),
		lit("save-off", true),
		lit("version", true),
		lit("loot", false,
			lit("give", false, argEntity("players", entityPlayers, false, argGreedy("source", true))),
			lit("insert", false, argBlockPos("targetPos", false, argGreedy("source", true))),
			lit("spawn", false, argVec3("targetPos", false, argGreedy("source", true))),
			lit("replace", false,
				lit("block", false, argBlockPos("targetPos", false, argGreedy("slot [count] source", true))),
				lit("entity", false, argEntity("entities", 0, false, argGreedy("slot [count] source", true))))),
		lit("item", false, litsWith([]string{"replace", "fill", "override"},
			lit("block", false, argBlockPos("pos", false, argGreedy("slots with|from …", true))),
			lit("entity", false, argEntity("targets", 0, false, argGreedy("slots with|from …", true))))...),
		lit("stop", true),
		lit("save-on", true),
		lit("clone", false,
			lit("from", false, argGreedy("sourceDimension begin end destination", true)),
			argBlockPos("begin", false, argBlockPos("end", false,
				argBlockPos("destination", true, argGreedy("mode", true)),
				lit("to", false, argGreedy("targetDimension destination", true))))),
		lit("seed", true),
		lit("enchant", false, argEntity("targets", 0, false, argWord("enchantment", true, argInt("level", 0, 255, true)))),
		lit("me", false, argGreedy("action", true)),
		lit("whitelist", false, append(lits("on", "off", "list"),
			lit("add", false, argProfile("player", true)),
			lit("remove", false, argProfile("player", true)))...),
		lit("say", false, argGreedy("message", true)),
		lit("title", true, argEntity("targets", 0, false,
			lit("title", false, argGreedy("text", true)),
			lit("subtitle", false, argGreedy("text", true)),
			lit("actionbar", false, argGreedy("text", true)),
			lit("times", false, argInt("fade in", 0, 0, false,
				argInt("stay", 0, 0, false, argInt("fade out", 0, 0, true)))),
			lit("clear", true),
			lit("reset", true))),
		lit("bug", false, argGreedy("what went wrong", true),
			lit("re", false, argGreedy("what you want to add", true)),
			lit("list", false)),
		lit("msg", false, argEntity("target", entitySingle|entityPlayers, false, argGreedy("message", true))),
		lit("tell", false, argEntity("target", entitySingle|entityPlayers, false, argGreedy("message", true))),
		lit("w", false, argEntity("target", entitySingle|entityPlayers, false, argGreedy("message", true))),
		lit("kick", false, argProfile("player", true, argGreedy("reason", true))),
		lit("ban", false, argProfile("player", true, argGreedy("reason", true))),
		lit("pardon", false, argProfile("player", true)),
		lit("hud", true, lits("on", "off")...),
		lit("list", true),
		lit("help", true, argGreedy("command", true)),
		lit("where", true, player),
		lit("nether", true),
		lit("end", true),
		lit("refresh", true),
		lit("rescue", true),
		lit("spawnpoint", true, argVec3("pos", true)),
		lit("advancement", false, litsWith([]string{"grant", "revoke"},
			argEntity("targets", entityPlayers, false,
				lit("everything", true),
				// Advancement ids carry ':' and '/', which a word refuses.
				lit("only", false, argGreedy("advancement [criterion]", true)),
				lit("from", false, argGreedy("advancement", true)),
				lit("through", false, argGreedy("advancement", true)),
				lit("until", false, argGreedy("advancement", true))))...),
		lit("attribute", false, argEntity("target", entitySingle, false, argGreedy("attribute", true))),
		lit("recipe", false, litsWith([]string{"give", "take"},
			argEntity("targets", entityPlayers, false, argGreedy("recipe", true)))...),
		lit("tag", false, argEntity("targets", 0, false,
			lit("add", false, argWord("name", true)),
			lit("remove", false, argWord("name", true)),
			lit("list", true))),
		lit("ride", false, argEntity("target", entitySingle, false,
			lit("mount", false, argEntity("vehicle", entitySingle, true)),
			lit("dismount", true))),
		lit("damage", false, argEntity("target", entitySingle, false,
			argFloat("amount", 0, true, argGreedy("damageType", true)))),
		lit("spreadplayers", false, argVec2("center", false,
			argFloat("spreadDistance", 0, false, argFloat("maxRange", 1, false,
				argBool("respectTeams", false, argEntity("targets", 0, true)),
				lit("under", false, argInt("maxHeight", -64, 4064, false,
					argBool("respectTeams", false, argEntity("targets", 0, true)))))))),
		lit("forceload", false,
			lit("add", false, argColumnPos("from", true, argColumnPos("to", true))),
			lit("remove", false, lit("all", true), argColumnPos("from", true, argColumnPos("to", true))),
			lit("query", true, argColumnPos("pos", true))),
		lit("setworldspawn", true, argBlockPos("pos", true, argGreedy("rotation", true))),
		lit("defaultgamemode", false, lits(gamemodes...)...),
		lit("random", false,
			lit("value", false, argWord("range", true, argGreedy("sequence", true))),
			lit("roll", false, argWord("range", true, argGreedy("sequence", true))),
			lit("reset", false, argGreedy("sequence", true))),
		computeTree(),
		lit("swing", true, argEntity("targets", 0, true, lits("mainhand", "offhand")...)),
		lit("setidletimeout", false, argInt("minutes", 0, math.MaxInt32, true)),
		lit("stopwatch", false,
			lit("create", false, argGreedy("id", true)),
			lit("query", false, argGreedy("id", true)),
			lit("restart", false, argGreedy("id", true)),
			lit("remove", false, argGreedy("id", true))),
		lit("teammsg", false, argGreedy("message", true)),
		lit("tm", false, argGreedy("message", true)),
	}
}

// computeTree is ComputeCommand: a context source (default, block <pos>,
// entity <target>) then float <provider> [<scale>] or integer <provider>.
// A provider may be inline SNBT with spaces in it, and the scale follows it,
// so each rides one greedy argument the dispatcher splits.
func computeTree() cmdNode {
	kinds := []cmdNode{
		lit("float", false, argGreedy("provider [scale]", true)),
		lit("integer", false, argGreedy("provider", true)),
	}
	return lit("compute", false,
		lit("default", false, kinds...),
		lit("block", false, argBlockPos("computePos", false, kinds...)),
		lit("entity", false, argEntity("computeTarget", entitySingle, false, kinds...)))
}

// gameruleNodes is a literal per rule: the boolean ones take true|false, the
// numeric ones a bounded integer, which is what cmdGamerule accepts.
func gameruleNodes() []cmdNode {
	out := make([]cmdNode, 0, len(booleanRules)+len(numericRules))
	for _, r := range booleanRules {
		out = append(out, lit(r, true, lits("true", "false")...))
	}
	for _, r := range numericRules {
		lo, hi := gameruleBounds(r) // the client's argument check matches the server's
		out = append(out, lit(r, true, argInt("value", int32(lo), int32(hi), true)))
	}
	return out
}

// xpTree is ExperienceCommand: add|set <targets> <amount> [points|levels],
// query <target> points|levels.
func xpTree(name string) cmdNode {
	unit := lits("points", "levels")
	return lit(name, false,
		lit("add", false, argEntity("targets", entityPlayers, false, argInt("amount", -2147483648, 2147483647, true, unit...))),
		lit("set", false, argEntity("targets", entityPlayers, false, argInt("amount", 0, 2147483647, true, unit...))),
		lit("query", false, argEntity("target", entitySingle|entityPlayers, false, unit...)))
}

// tpTree is TeleportCommand: <location>, <destination>, and
// <targets> <location> [<rotation> | facing <facingLocation> |
// facing entity <facingEntity> [eyes|feet]], <targets> <destination>.
func tpTree(name string) cmdNode {
	facing := lit("facing", false,
		argVec3("facingLocation", true),
		lit("entity", false, argEntity("facingEntity", entitySingle, true, lits("eyes", "feet")...)))
	return lit(name, false,
		argVec3("location", true),
		argEntity("destination", entitySingle, true),
		argEntity("targets", 0, false,
			argVec3("location", true, argVec2("rotation", true), facing),
			argEntity("destination", entitySingle, true)))
}

// stopsoundSources: StopSoundCommand's [<source>|*] [<sound>] — each source
// ends the command or takes a sound.
func stopsoundSources() []cmdNode {
	var out []cmdNode
	for _, n := range append([]string{"*"}, soundSourceNames()...) {
		out = append(out, lit(n, true, argGreedy("sound", true)))
	}
	return out
}

// soundSourceNames are SoundSource's names in ordinal order.
func soundSourceNames() []string {
	out := make([]string, len(soundSources))
	for n, i := range soundSources {
		out[i] = n
	}
	return out
}
