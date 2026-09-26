package server

import (
	"bytes"
	"io"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// treeNode is one decoded node of the Commands packet.
type treeNode struct {
	flags  byte
	kids   []int32
	name   string
	parser int32
}

// decodeCommandTree re-reads the packet body the way a client does, which is
// the only way to know the writer and the wire agree.
func decodeCommandTree(t *testing.T, body []byte) ([]treeNode, int32) {
	t.Helper()
	r := bytes.NewReader(body)
	n, err := protocol.ReadVarInt(r)
	if err != nil {
		t.Fatalf("node count: %v", err)
	}
	out := make([]treeNode, n)
	for i := range out {
		f, err := r.ReadByte()
		if err != nil {
			t.Fatalf("node %d flags: %v", i, err)
		}
		out[i].flags = f
		kn, err := protocol.ReadVarInt(r)
		if err != nil {
			t.Fatalf("node %d children: %v", i, err)
		}
		for j := int32(0); j < kn; j++ {
			k, err := protocol.ReadVarInt(r)
			if err != nil {
				t.Fatalf("node %d child %d: %v", i, j, err)
			}
			if k < 0 || k >= n {
				t.Fatalf("node %d child %d out of range: %d", i, j, k)
			}
			out[i].kids = append(out[i].kids, k)
		}
		switch f & 0x03 {
		case 0: // root
			continue
		case 1: // literal
			out[i].name, err = protocol.ReadString(r)
		case 2: // argument
			if out[i].name, err = protocol.ReadString(r); err != nil {
				break
			}
			if out[i].parser, err = protocol.ReadVarInt(r); err != nil {
				break
			}
			switch out[i].parser {
			case parserString:
				_, err = protocol.ReadVarInt(r)
			case parserEntity:
				_, err = r.ReadByte()
			case parserFloat:
				// FloatArgumentInfo: a flags byte then the present bounds,
				// each a big-endian float32.
				var fl byte
				if fl, err = r.ReadByte(); err == nil {
					for _, bit := range []byte{0x01, 0x02} {
						if fl&bit == 0 {
							continue
						}
						var skip [4]byte
						if _, err = io.ReadFull(r, skip[:]); err != nil {
							break
						}
					}
				}
			case parserInteger:
				// IntegerArgumentInfo: a flags byte then the bounds that are
				// present, each a plain big-endian int32 — NOT a VarInt.
				var fl byte
				if fl, err = r.ReadByte(); err == nil {
					for _, bit := range []byte{0x01, 0x02} {
						if fl&bit == 0 {
							continue
						}
						var skip [4]byte
						if _, err = io.ReadFull(r, skip[:]); err != nil {
							break
						}
					}
				}
			}
			if err == nil && f&0x10 != 0 { // custom suggestions: an identifier after the properties
				var sug string
				if sug, err = protocol.ReadString(r); err == nil && sug != "minecraft:ask_server" {
					t.Fatalf("node %d suggests %q", i, sug)
				}
			}
		default:
			t.Fatalf("node %d has an unknown type in flags %#x", i, f)
		}
		if err != nil {
			t.Fatalf("node %d payload: %v", i, err)
		}
	}
	root, err := protocol.ReadVarInt(r)
	if err != nil {
		t.Fatalf("root index: %v", err)
	}
	if r.Len() != 0 {
		t.Fatalf("%d bytes left over — the writer and the reader disagree", r.Len())
	}
	return out, root
}

// The tree decodes cleanly, every command the dispatcher knows is in it, and
// the ones with a real grammar have one.
func TestCommandTreeShape(t *testing.T) {
	nodes, root := decodeCommandTree(t, buildCommandTree("myplugincmd"))
	if root != 0 || nodes[root].flags&0x03 != 0 {
		t.Fatalf("node %d should be the root, flags %#x", root, nodes[root].flags)
	}
	byName := map[string]treeNode{}
	for _, k := range nodes[root].kids {
		byName[nodes[k].name] = nodes[k]
	}
	for _, name := range append(append([]string{}, commandNames...), "myplugincmd") {
		if _, ok := byName[name]; !ok {
			t.Errorf("/%s is missing from the tree", name)
		}
	}
	// /gamemode takes the four modes as literals, each of which may end the
	// command or carry a target.
	gm, ok := byName["gamemode"]
	if !ok || len(gm.kids) != 4 {
		t.Fatalf("/gamemode should offer four modes, got %d", len(gm.kids))
	}
	modes := map[string]bool{}
	for _, k := range gm.kids {
		modes[nodes[k].name] = true
		if nodes[k].flags&0x04 == 0 {
			t.Errorf("/gamemode %s should be executable on its own", nodes[k].name)
		}
		if len(nodes[k].kids) != 1 || nodes[nodes[k].kids[0]].parser != parserEntity {
			t.Errorf("/gamemode %s should take a target selector", nodes[k].name)
		}
	}
	for _, m := range []string{"survival", "creative", "adventure", "spectator"} {
		if !modes[m] {
			t.Errorf("/gamemode is missing %s", m)
		}
	}
	// /give <targets> <item> [count]
	give := byName["give"]
	if len(give.kids) != 1 || nodes[give.kids[0]].parser != parserEntity {
		t.Fatal("/give should start with a target selector")
	}
	item := nodes[nodes[give.kids[0]].kids[0]]
	if item.parser != parserItemStack || item.flags&0x04 == 0 {
		t.Fatalf("/give <targets> <item> should be an executable item argument, got parser %d", item.parser)
	}
	// Every gamerule is named, so the client can complete them.
	rules := map[string]bool{}
	for _, k := range byName["gamerule"].kids {
		rules[nodes[k].name] = true
	}
	for _, r := range append(append([]string{}, booleanRules...), numericRules...) {
		if !rules[r] {
			t.Errorf("gamerule %s is missing from the tree", r)
		}
	}
}

// Only parser ids that mean the same thing on every protocol tachyne serves
// may appear: 1.21.6 inserted minecraft:style at 19 and shifted everything
// above it, and nothing translates parser ids per version.
func TestCommandTreeUsesVersionStableParsers(t *testing.T) {
	nodes, _ := decodeCommandTree(t, buildCommandTree())
	for i, n := range nodes {
		if n.flags&0x03 != 2 {
			continue
		}
		if n.parser > 15 {
			t.Errorf("node %d (%s) uses parser %d, which is not stable across 770-777",
				i, n.name, n.parser)
		}
	}
}

// The tree reaches the forms the dispatcher now takes (a tree that lags the
// handlers reddens valid input), and no node has two children of one name.
func TestCommandTreeCoversTheNewForms(t *testing.T) {
	nodes, root := decodeCommandTree(t, buildCommandTree())
	for i, n := range nodes {
		seen := map[string]bool{}
		for _, k := range n.kids {
			if nm := nodes[k].name; seen[nm] {
				t.Errorf("node %d has two children named %q", i, nm)
			} else {
				seen[nm] = true
			}
		}
	}
	// walk follows a path of node names (literals and argument names) and
	// reports whether the last one may end the command.
	walk := func(path ...string) bool {
		cur := root
		for _, want := range path {
			next := int32(-1)
			for _, k := range nodes[cur].kids {
				if nodes[k].name == want {
					next = k
				}
			}
			if next < 0 {
				t.Errorf("%v: no %q under %q", path, want, nodes[cur].name)
				return false
			}
			cur = next
		}
		return nodes[cur].flags&0x04 != 0
	}
	for _, p := range [][]string{
		{"xp", "set", "targets", "amount", "levels"},
		{"experience", "query", "target", "points"},
		{"tp", "targets", "location", "rotation"},
		{"tp", "targets", "location", "facing", "entity", "facingEntity", "eyes"},
		{"teleport", "targets", "destination"},
		{"time", "set", "noon"},
		{"time", "add", "time"},
		{"time", "query", "gametime"},
		{"clear", "targets", "item", "maxCount"},
		{"tellraw", "targets", "message"},
		{"stopsound", "targets", "record", "sound"},
		{"stopsound", "targets", "*"},
		{"compute", "default", "integer", "provider"},
		{"compute", "block", "computePos", "float", "provider [scale]"},
		{"compute", "entity", "computeTarget", "integer", "provider"},
	} {
		if !walk(p...) {
			t.Errorf("%v does not end the command", p)
		}
	}
}
