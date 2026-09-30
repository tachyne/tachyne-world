package server

import (
	"fmt"
	"strconv"
)

// NBT paths (NbtPathArgument): foo.bar, foo[0], foo[-1], foo[], foo[{k:v}],
// foo{k:v}, a leading {k:v}, and quoted names. Each node selects tags from
// the ones the node before it selected; get fails naming the path up to the
// node that found nothing, and the write operations work on the typed tags
// in place (nbttag.go), node by node as vanilla's Node methods do.

// nbtPath is a parsed NbtPath.
type nbtPath struct {
	original string
	nodes    []nbtPathNode
	ends     []int // each node's end in original (nodeToOriginalPosition)
}

func (p nbtPath) String() string { return p.original }

// nbtPathNode is NbtPathArgument.Node.
type nbtPathNode interface {
	getTag(parent any, out []any) []any
	getOrCreateTag(parent any, child func() any, out []any) []any
	preferredParent() any
	setTag(parent any, toAdd func() any) int
	removeTag(parent any) int
}

const errInvalidPathNode = "Invalid NBT path element"

// parseNBTPath reads a path that fills s (the command splits arguments on
// spaces, so a path never has one).
func parseNBTPath(s string) (nbtPath, string) {
	p := nbtPath{original: s}
	i := 0
	first := true
	for i < len(s) && s[i] != ' ' {
		n, next, msg := parsePathNode(s, i, first)
		if msg != "" {
			return nbtPath{}, msg
		}
		i = next
		p.nodes = append(p.nodes, n)
		p.ends = append(p.ends, i)
		first = false
		if i < len(s) {
			switch s[i] {
			case ' ', '[', '{':
			case '.':
				i++
			default:
				return nbtPath{}, "Expected '.'"
			}
		}
	}
	if len(p.nodes) == 0 {
		return nbtPath{}, errInvalidPathNode
	}
	return p, ""
}

func parsePathNode(s string, i int, first bool) (nbtPathNode, int, string) {
	switch s[i] {
	case '"', '\'':
		sp := &snbtParser{s: s, i: i}
		name, err := sp.quoted()
		if err != nil {
			return nil, 0, err.Error()
		}
		return readObjectNode(s, sp.i, name)
	case '[':
		i++
		if i >= len(s) {
			return nil, 0, errInvalidPathNode
		}
		switch s[i] {
		case '{':
			pat, n, msg := pathPattern(s[i:])
			if msg != "" {
				return nil, 0, msg
			}
			i += n
			if i >= len(s) || s[i] != ']' {
				return nil, 0, "Expected ']'"
			}
			return pathMatchElem{pattern: pat}, i + 1, ""
		case ']':
			return pathAll{}, i + 1, ""
		}
		j := i
		for j < len(s) && (s[j] == '-' || s[j] >= '0' && s[j] <= '9') {
			j++
		}
		idx, err := strconv.Atoi(s[i:j])
		if err != nil {
			return nil, 0, "Expected integer"
		}
		if j >= len(s) || s[j] != ']' {
			return nil, 0, "Expected ']'"
		}
		return pathIndex{index: idx}, j + 1, ""
	case '{':
		if !first {
			return nil, 0, errInvalidPathNode
		}
		pat, n, msg := pathPattern(s[i:])
		if msg != "" {
			return nil, 0, msg
		}
		return pathMatchRoot{pattern: pat}, i + n, ""
	}
	j := i
	for j < len(s) && pathUnquotedChar(s[j]) {
		j++
	}
	if j == i {
		return nil, 0, errInvalidPathNode
	}
	return readObjectNode(s, j, s[i:j])
}

// readObjectNode is a named child, or with a {…} straight after the name a
// named child that must match it.
func readObjectNode(s string, i int, name string) (nbtPathNode, int, string) {
	if name == "" {
		return nil, 0, errInvalidPathNode
	}
	if i < len(s) && s[i] == '{' {
		pat, n, msg := pathPattern(s[i:])
		if msg != "" {
			return nil, 0, msg
		}
		return pathMatchObj{name: name, pattern: pat}, i + n, ""
	}
	return pathChild{name: name}, i, ""
}

// pathPattern parses the compound at the start of s (TagParser.
// parseCompoundAsArgument) and reports how much of s it took.
func pathPattern(s string) (map[string]any, int, string) {
	v, n, err := parseSNBTTypedPrefix(s)
	if err != nil {
		return nil, 0, err.Error()
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, 0, "Expected '{'"
	}
	return m, n, ""
}

func pathUnquotedChar(c byte) bool {
	return c != ' ' && c != '"' && c != '\'' && c != '[' && c != ']' && c != '.' && c != '{' && c != '}'
}

// ---- the nodes ----------------------------------------------------------------

// listAdd is CollectionTag.addTag: a ListTag takes any tag, a typed array
// only a number (cast to its element type). i must be within 0..len.
func listAdd(l *nbtList, i int, v any) bool {
	if l.arr != 0 {
		e, ok := arrayElem(l.arr, v)
		if !ok {
			return false
		}
		v = e
	}
	l.elems = append(l.elems, nil)
	copy(l.elems[i+1:], l.elems[i:])
	l.elems[i] = v
	return true
}

// listSet is CollectionTag.setTag.
func listSet(l *nbtList, i int, v any) bool {
	if l.arr != 0 {
		e, ok := arrayElem(l.arr, v)
		if !ok {
			return false
		}
		v = e
	}
	l.elems[i] = v
	return true
}

func listRemove(l *nbtList, i int) {
	l.elems = append(l.elems[:i], l.elems[i+1:]...)
}

// pathAll is AllElementsNode: foo[].
type pathAll struct{}

func (pathAll) getTag(parent any, out []any) []any {
	if l, ok := parent.(*nbtList); ok {
		out = append(out, l.elems...)
	}
	return out
}

func (pathAll) getOrCreateTag(parent any, child func() any, out []any) []any {
	l, ok := parent.(*nbtList)
	if !ok {
		return out
	}
	if len(l.elems) == 0 {
		v := child()
		if listAdd(l, 0, v) {
			out = append(out, l.elems[0])
		}
		return out
	}
	return append(out, l.elems...)
}

func (pathAll) preferredParent() any { return &nbtList{} }

func (pathAll) setTag(parent any, toAdd func() any) int {
	l, ok := parent.(*nbtList)
	if !ok {
		return 0
	}
	size := len(l.elems)
	if size == 0 {
		listAdd(l, 0, toAdd())
		return 1
	}
	nv := toAdd()
	same := 0
	for _, e := range l.elems {
		if tagEqual(nv, e) {
			same++
		}
	}
	changed := size - same
	if changed == 0 {
		return 0
	}
	l.elems = l.elems[:0]
	if !listAdd(l, 0, nv) {
		return 0
	}
	for i := 1; i < size; i++ {
		listAdd(l, i, toAdd())
	}
	return changed
}

func (pathAll) removeTag(parent any) int {
	if l, ok := parent.(*nbtList); ok && len(l.elems) > 0 {
		n := len(l.elems)
		l.elems = l.elems[:0]
		return n
	}
	return 0
}

// pathChild is CompoundChildNode: a key of a compound.
type pathChild struct{ name string }

func (n pathChild) getTag(parent any, out []any) []any {
	if m, ok := parent.(map[string]any); ok {
		if v, ok := m[n.name]; ok {
			out = append(out, v)
		}
	}
	return out
}

func (n pathChild) getOrCreateTag(parent any, child func() any, out []any) []any {
	m, ok := parent.(map[string]any)
	if !ok {
		return out
	}
	v, ok := m[n.name]
	if !ok {
		v = child()
		m[n.name] = v
	}
	return append(out, v)
}

func (pathChild) preferredParent() any { return map[string]any{} }

func (n pathChild) setTag(parent any, toAdd func() any) int {
	m, ok := parent.(map[string]any)
	if !ok {
		return 0
	}
	nv := toAdd()
	prev, had := m[n.name]
	m[n.name] = nv
	if had && tagEqual(nv, prev) {
		return 0
	}
	return 1
}

func (n pathChild) removeTag(parent any) int {
	if m, ok := parent.(map[string]any); ok {
		if _, ok := m[n.name]; ok {
			delete(m, n.name)
			return 1
		}
	}
	return 0
}

// pathIndex is IndexedElementNode: foo[i], counting from the end when i < 0.
type pathIndex struct{ index int }

func (n pathIndex) at(l *nbtList) (int, bool) {
	i := n.index
	if i < 0 {
		i += len(l.elems)
	}
	return i, i >= 0 && i < len(l.elems)
}

func (n pathIndex) getTag(parent any, out []any) []any {
	if l, ok := parent.(*nbtList); ok {
		if i, ok := n.at(l); ok {
			out = append(out, l.elems[i])
		}
	}
	return out
}

func (n pathIndex) getOrCreateTag(parent any, _ func() any, out []any) []any {
	return n.getTag(parent, out)
}

func (pathIndex) preferredParent() any { return &nbtList{} }

func (n pathIndex) setTag(parent any, toAdd func() any) int {
	l, ok := parent.(*nbtList)
	if !ok {
		return 0
	}
	i, ok := n.at(l)
	if !ok {
		return 0
	}
	nv := toAdd()
	if !tagEqual(nv, l.elems[i]) && listSet(l, i, nv) {
		return 1
	}
	return 0
}

func (n pathIndex) removeTag(parent any) int {
	if l, ok := parent.(*nbtList); ok {
		if i, ok := n.at(l); ok {
			listRemove(l, i)
			return 1
		}
	}
	return 0
}

// pathMatchElem is MatchElementNode: foo[{k:v}], the list elements matching
// a pattern (ListTag only, not the typed arrays).
type pathMatchElem struct{ pattern map[string]any }

func plainList(v any) (*nbtList, bool) {
	l, ok := v.(*nbtList)
	return l, ok && l.arr == 0
}

func (n pathMatchElem) getTag(parent any, out []any) []any {
	if l, ok := plainList(parent); ok {
		for _, e := range l.elems {
			if tagCompare(n.pattern, e, true) {
				out = append(out, e)
			}
		}
	}
	return out
}

func (n pathMatchElem) getOrCreateTag(parent any, _ func() any, out []any) []any {
	l, ok := plainList(parent)
	if !ok {
		return out
	}
	found := false
	for _, e := range l.elems {
		if tagCompare(n.pattern, e, true) {
			out = append(out, e)
			found = true
		}
	}
	if !found {
		nt := tagCopy(n.pattern)
		l.elems = append(l.elems, nt)
		out = append(out, nt)
	}
	return out
}

func (pathMatchElem) preferredParent() any { return &nbtList{} }

func (n pathMatchElem) setTag(parent any, toAdd func() any) int {
	l, ok := plainList(parent)
	if !ok {
		return 0
	}
	if len(l.elems) == 0 {
		l.elems = append(l.elems, toAdd())
		return 1
	}
	changed := 0
	for i, cur := range l.elems {
		if tagCompare(n.pattern, cur, true) {
			nv := toAdd()
			if !tagEqual(nv, cur) {
				l.elems[i] = nv
				changed++
			}
		}
	}
	return changed
}

func (n pathMatchElem) removeTag(parent any) int {
	l, ok := plainList(parent)
	if !ok {
		return 0
	}
	changed := 0
	for i := len(l.elems) - 1; i >= 0; i-- {
		if tagCompare(n.pattern, l.elems[i], true) {
			listRemove(l, i)
			changed++
		}
	}
	return changed
}

// pathMatchObj is MatchObjectNode: foo{k:v}, a compound's key whose value
// matches a pattern.
type pathMatchObj struct {
	name    string
	pattern map[string]any
}

func (n pathMatchObj) getTag(parent any, out []any) []any {
	if m, ok := parent.(map[string]any); ok {
		if v := m[n.name]; tagCompare(n.pattern, v, true) {
			out = append(out, v)
		}
	}
	return out
}

func (n pathMatchObj) getOrCreateTag(parent any, _ func() any, out []any) []any {
	m, ok := parent.(map[string]any)
	if !ok {
		return out
	}
	v, had := m[n.name]
	switch {
	case !had:
		nt := tagCopy(n.pattern)
		m[n.name] = nt
		out = append(out, nt)
	case tagCompare(n.pattern, v, true):
		out = append(out, v)
	}
	return out
}

func (pathMatchObj) preferredParent() any { return map[string]any{} }

func (n pathMatchObj) setTag(parent any, toAdd func() any) int {
	m, ok := parent.(map[string]any)
	if !ok {
		return 0
	}
	cur := m[n.name]
	if !tagCompare(n.pattern, cur, true) {
		return 0
	}
	nv := toAdd()
	if tagEqual(nv, cur) {
		return 0
	}
	m[n.name] = nv
	return 1
}

func (n pathMatchObj) removeTag(parent any) int {
	m, ok := parent.(map[string]any)
	if !ok {
		return 0
	}
	if tagCompare(n.pattern, m[n.name], true) {
		if _, had := m[n.name]; had {
			delete(m, n.name)
			return 1
		}
	}
	return 0
}

// pathMatchRoot is MatchRootObjectNode: a leading {k:v}, the root itself when
// it matches.
type pathMatchRoot struct{ pattern map[string]any }

func (n pathMatchRoot) getTag(parent any, out []any) []any {
	if m, ok := parent.(map[string]any); ok && tagCompare(n.pattern, m, true) {
		out = append(out, m)
	}
	return out
}

func (n pathMatchRoot) getOrCreateTag(parent any, _ func() any, out []any) []any {
	return n.getTag(parent, out)
}

func (pathMatchRoot) preferredParent() any       { return map[string]any{} }
func (pathMatchRoot) setTag(any, func() any) int { return 0 }
func (pathMatchRoot) removeTag(any) int          { return 0 }

// ---- NbtPath's operations -------------------------------------------------------

func collectNodes(tags []any, fn func(t any, out []any) []any) []any {
	var out []any
	for _, t := range tags {
		out = fn(t, out)
	}
	return out
}

func (p nbtPath) notFound(i int) string {
	return fmt.Sprintf("Found no elements matching %s", p.original[:p.ends[i]])
}

// get is NbtPath.get: the tags the path selects, failing where a node finds
// nothing.
func (p nbtPath) get(root any) ([]any, string) {
	res := []any{root}
	for i, n := range p.nodes {
		res = collectNodes(res, n.getTag)
		if len(res) == 0 {
			return nil, p.notFound(i)
		}
	}
	return res, ""
}

// countMatching is NbtPath.countMatching.
func (p nbtPath) countMatching(root any) int {
	res := []any{root}
	for _, n := range p.nodes {
		res = collectNodes(res, n.getTag)
		if len(res) == 0 {
			return 0
		}
	}
	return len(res)
}

func (p nbtPath) getOrCreateParents(root any) ([]any, string) {
	res := []any{root}
	for i := 0; i < len(p.nodes)-1; i++ {
		n, next := p.nodes[i], p.nodes[i+1]
		res = collectNodes(res, func(t any, out []any) []any {
			return n.getOrCreateTag(t, next.preferredParent, out)
		})
		if len(res) == 0 {
			return nil, p.notFound(i)
		}
	}
	return res, ""
}

// getOrCreate is NbtPath.getOrCreate: the path's tags, making missing
// compounds and lists on the way and newValue at the end.
func (p nbtPath) getOrCreate(root any, newValue func() any) ([]any, string) {
	parents, msg := p.getOrCreateParents(root)
	if msg != "" {
		return nil, msg
	}
	last := p.nodes[len(p.nodes)-1]
	return collectNodes(parents, func(t any, out []any) []any {
		return last.getOrCreateTag(t, newValue, out)
	}), ""
}

const errNBTTooDeep = "Resulting NBT too deeply nested"

// set is NbtPath.set: the value goes at every place the path reaches; the
// count is how many places changed.
func (p nbtPath) set(root, v any) (int, string) {
	if tagTooDeep(v, len(p.nodes)) {
		return 0, errNBTTooDeep
	}
	first := tagCopy(v)
	parents, msg := p.getOrCreateParents(root)
	if msg != "" {
		return 0, msg
	}
	last := p.nodes[len(p.nodes)-1]
	used := false
	supply := func() any {
		if !used {
			used = true
			return first
		}
		return tagCopy(first)
	}
	n := 0
	for _, t := range parents {
		n += last.setTag(t, supply)
	}
	return n, ""
}

// insert is NbtPath.insert: the values go into every list the path reaches,
// at index (from the end when negative, -1 appending).
func (p nbtPath) insert(index int, root any, values []any) (int, string) {
	copies := make([]any, len(values))
	for i, v := range values {
		copies[i] = tagCopy(v)
		if tagTooDeep(copies[i], len(p.nodes)) {
			return 0, errNBTTooDeep
		}
	}
	targets, msg := p.getOrCreate(root, func() any { return &nbtList{} })
	if msg != "" {
		return 0, msg
	}
	modified, usedFirst := 0, false
	for _, t := range targets {
		l, ok := t.(*nbtList)
		if !ok {
			return 0, "Expected a list: got " + tagString(t)
		}
		changed := false
		at := index
		if index < 0 {
			at = len(l.elems) + index + 1
		}
		for _, v := range copies {
			if at < 0 || at > len(l.elems) {
				return 0, fmt.Sprintf("Invalid list index: %d", at)
			}
			if usedFirst {
				v = tagCopy(v)
			}
			if listAdd(l, at, v) {
				at++
				changed = true
			}
		}
		usedFirst = true
		if changed {
			modified++
		}
	}
	return modified, ""
}

// remove is NbtPath.remove: the count of tags taken out.
func (p nbtPath) remove(root any) int {
	res := []any{root}
	for i := 0; i < len(p.nodes)-1; i++ {
		res = collectNodes(res, p.nodes[i].getTag)
	}
	last := p.nodes[len(p.nodes)-1]
	n := 0
	for _, t := range res {
		n += last.removeTag(t)
	}
	return n
}
