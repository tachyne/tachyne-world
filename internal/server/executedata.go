package server

import (
	"fmt"
	"strconv"
	"strings"
)

// /execute's NBT forms, over /data's accessors (datacmd.go):
//
//	execute store result|success block <pos>|entity <target>|storage <id> <path> byte|short|int|long|float|double <scale> …
//	execute if|unless data block <pos>|entity <target>|storage <id> <path>
//
// and its slot conditions (slotsource.go):
//
//	execute if|unless items block <pos>|entity <targets> <slots> <item predicate>
//	execute if|unless slots block <pos>|entity <targets> <slots>
//
// The accessor is resolved when the chain reaches it, against that
// source (DataCommands' ArgProvider.access): one entity, a loaded block
// entity, or a storage id.

// execDataAccess reads `block <pos>`, `entity <target>` or `storage <id>`
// from a and resolves it for a source; rest is what follows.
func (h *hub) execDataAccess(players map[int32]*tracked, s *execSource, a []string) (dataAccessor, []string, string) {
	if len(a) < 2 {
		return dataAccessor{}, nil, execIncomplete
	}
	switch a[0] {
	case "entity":
		ens, msg := h.execSelect(players, s, a[1])
		if msg != "" {
			return dataAccessor{}, nil, msg
		}
		switch {
		case len(ens) == 0:
			return dataAccessor{}, nil, "No entity was found"
		case len(ens) > 1:
			return dataAccessor{}, nil, "Only one entity is allowed, but the provided selector allows more than one"
		case ens[0].o != nil:
			// entityNBT models players and mobs; an item, a projectile or
			// a vehicle has no data to read or write here.
			return dataAccessor{}, nil, "The data of " + ens[0].name() + " can't be read on this server yet"
		}
		return dataAccessor{kind: "entity", en: ens[0]}, a[2:], ""
	case "block":
		if len(a) < 4 {
			return dataAccessor{}, nil, execIncomplete
		}
		pos, msg := h.execLoadedPos(s, a[1:4])
		if msg != "" {
			return dataAccessor{}, nil, msg
		}
		st := h.worldFor(s.dim).At(pos.x, pos.y, pos.z)
		if !hasBlockEntity(st) {
			return dataAccessor{}, nil, "The target block is not a block entity"
		}
		return dataAccessor{kind: "block", pos: simPos{dim: s.dim, blockPos: pos}, state: st}, a[4:], ""
	case "storage":
		id, ok := parseResourceID(a[1])
		if !ok {
			return dataAccessor{}, nil, "Invalid ID: " + a[1]
		}
		return dataAccessor{kind: "storage", id: id}, a[2:], ""
	}
	return dataAccessor{}, nil, execIncomplete
}

// execStoreDataStep is `store result|success block|entity|storage …`: the
// accessor, the path, the tag type and the scale, resolved now.
func (h *hub) execStoreDataStep(players map[int32]*tracked, s *execSource, a []string) (execStore, string) {
	st := execStore{result: a[0] == "result"}
	acc, rest, msg := h.execDataAccess(players, s, a[1:])
	if msg != "" {
		return st, msg
	}
	if len(rest) != 3 {
		return st, execIncomplete
	}
	path, msg := parseNBTPath(rest[0])
	if msg != "" {
		return st, msg
	}
	switch rest[1] {
	case "byte", "short", "int", "long", "float", "double":
	default:
		return st, execIncomplete
	}
	scale, err := strconv.ParseFloat(rest[2], 64)
	if err != nil {
		return st, fmt.Sprintf("Invalid double '%s'", rest[2])
	}
	st.data, st.path, st.numType, st.scale = &acc, &path, rest[1], scale
	return st, ""
}

// execNumTag is storeData's constructor: the value times the scale, as the
// tag type asked for, cast as Java casts a double ((byte) and (short) go
// through int).
func execNumTag(typ string, v int, scale float64) any {
	f := float64(v) * scale
	switch typ {
	case "byte":
		return nbtByte(int8(javaF2I(f)))
	case "short":
		return nbtShort(int16(javaF2I(f)))
	case "int":
		return nbtInt(javaF2I(f))
	case "long":
		return nbtLong(javaF2L(f))
	case "float":
		return nbtFloat(float32(f))
	}
	return nbtDouble(f)
}

// execStoreData is storeData's callback: the accessor's data read afresh,
// the value set at the path, and the data written back. Anything that
// refuses (a player's data, a path that cannot be made, an accessor that is
// gone) is dropped silently, as the callback's CommandSyntaxException is.
func (h *hub) execStoreData(players map[int32]*tracked, st execStore, v int) {
	a := *st.data
	switch a.kind {
	case "entity":
		if _, ok := h.execSelf(players, a.en); !ok {
			return
		}
	case "block":
		w := h.worldFor(a.pos.dim)
		if w == nil {
			return
		}
		cur := w.At(a.pos.x, a.pos.y, a.pos.z)
		if !hasBlockEntity(cur) {
			return
		}
		a.state = cur
	}
	old := h.dataGet(a)
	data := tagCopy(old).(map[string]any)
	if _, msg := st.path.set(data, execNumTag(st.numType, v, st.scale)); msg != "" {
		return
	}
	h.dataSet(players, a, old, data)
}

// execDataTest is `if|unless data …`: how many tags the path matches in
// the accessor's data (checkMatchingData).
func (h *hub) execDataTest(players map[int32]*tracked, s *execSource, a []string) (int, string) {
	acc, rest, msg := h.execDataAccess(players, s, a)
	if msg != "" {
		return 0, msg
	}
	if len(rest) != 1 {
		return 0, execIncomplete
	}
	path, msg := parseNBTPath(rest[0])
	if msg != "" {
		return 0, msg
	}
	return path.countMatching(h.dataGet(acc)), ""
}

// execSlotHolders reads `entity <targets>` or `block <pos>` for the slot
// conditions (ItemCommands' SOURCE_PROVIDERS); rest is what follows.
func (h *hub) execSlotHolders(players map[int32]*tracked, s *execSource, a []string) ([]itemTarget, []string, string) {
	if len(a) < 2 {
		return nil, nil, execIncomplete
	}
	switch a[0] {
	case "entity":
		ens, msg := h.execSelect(players, s, a[1])
		if msg != "" {
			return nil, nil, msg
		}
		if len(ens) == 0 {
			return nil, nil, "No entity was found"
		}
		var targets []itemTarget
		for _, en := range ens {
			targets = append(targets, h.entityItemTarget(en))
		}
		return targets, a[2:], ""
	case "block":
		if len(a) < 4 {
			return nil, nil, execIncomplete
		}
		pos, msg := h.execLoadedPos(s, a[1:4])
		if msg != "" {
			return nil, nil, msg
		}
		tg, ok := h.blockItemTarget(s.dim, pos)
		if !ok {
			return nil, nil, fmt.Sprintf("Source position %d, %d, %d is not a container", pos.x, pos.y, pos.z)
		}
		return []itemTarget{tg}, a[4:], ""
	}
	return nil, nil, execIncomplete
}

// execSlots is the slots a slot source picks over the holders, each
// holder's in turn (EntityItemAccessor.getSlots concatenates them); the
// source's own entity is this.
func (h *hub) execSlots(players map[int32]*tracked, s *execSource, targets []itemTarget, src slotSrc) []slotAccess {
	var this *itemTarget
	if en, ok := h.execSelf(players, s.self); ok {
		tg := h.entityItemTarget(en)
		this = &tg
	}
	var out []slotAccess
	for _, tg := range targets {
		out = append(out, src(tg, this)...)
	}
	return out
}

// execCountItems is `if items`: the items in the slots the predicate
// matches, counted (countItems).
func (h *hub) execCountItems(players map[int32]*tracked, s *execSource, a []string) (int, string) {
	targets, rest, msg := h.execSlotHolders(players, s, a)
	if msg != "" {
		return 0, msg
	}
	if len(rest) != 2 {
		return 0, execIncomplete
	}
	src, msg := parseSlotSourceArg(rest[0])
	if msg != "" {
		return 0, msg
	}
	match, msg := parseItemPredicate(rest[1])
	if msg != "" {
		return 0, msg
	}
	n := 0
	for _, sl := range h.execSlots(players, s, targets, src) {
		if st := sl.get(); st.item != 0 && st.count > 0 && match(st) {
			n += st.count
		}
	}
	return n, ""
}

// execCountSlots is `if slots`: how many slots the source picks
// (countSlots), empty ones included.
func (h *hub) execCountSlots(players map[int32]*tracked, s *execSource, a []string) (int, string) {
	targets, rest, msg := h.execSlotHolders(players, s, a)
	if msg != "" {
		return 0, msg
	}
	if len(rest) != 1 {
		return 0, execIncomplete
	}
	src, msg := parseSlotSourceArg(rest[0])
	if msg != "" {
		return 0, msg
	}
	return len(h.execSlots(players, s, targets, src)), ""
}

// execBiomeTest is `if biome <pos> <biome or #tag>` (ResourceOrTagArgument).
func execBiomeTest(biome, arg string) (bool, string) {
	if tag, ok := strings.CutPrefix(arg, "#"); ok {
		members, known := biomeTagMembers(nsID(tag)) // vanilla's tags, merged with the data packs'
		if !known {
			return false, fmt.Sprintf("Can't find tag '%s' of type 'minecraft:worldgen/biome'", nsID(tag))
		}
		for _, b := range members {
			if b == nsID(biome) {
				return true, ""
			}
		}
		return false, ""
	}
	return nsID(biome) == nsID(arg), ""
}

// coordWord writes an absolute coordinate so that it reads back as the same
// number and is never taken for a whole block (a decimal point always).
func coordWord(v float64) string {
	w := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.ContainsAny(w, ".eE") {
		w += ".0"
	}
	return w
}

// execLocalEyes writes the local (^) coordinates of the command after run
// as absolute ones measured from the source's anchor, when that anchor is
// its eyes (LocalCoordinates.getPosition: source.getAnchor().apply). The
// stand-in that runs the command stands at the source's feet, which is
// where every other coordinate is measured from.
func execLocalEyes(line string, src *execSource) string {
	if !src.eyes || src.eyeY == 0 {
		return line
	}
	fields := commandFields(line)
	changed := false
	for i := 0; i+2 < len(fields); i++ {
		if !strings.HasPrefix(fields[i], "^") || !strings.HasPrefix(fields[i+1], "^") || !strings.HasPrefix(fields[i+2], "^") {
			continue
		}
		x, y, z, ok := parsePosition(fields[i:i+3], src.x, src.y+src.eyeY, src.z, src.yaw, src.pitch)
		if !ok {
			continue
		}
		fields[i], fields[i+1], fields[i+2] = coordWord(x), coordWord(y), coordWord(z)
		changed = true
		i += 2
	}
	if !changed {
		return line
	}
	return strings.Join(fields, " ")
}
