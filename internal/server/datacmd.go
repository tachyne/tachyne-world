package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /data get|merge|modify|remove (DataCommands) over the three accessors:
// an entity (EntityDataAccessor), a block entity (BlockDataAccessor) and the
// server's command storage (StorageDataAccessor, CommandStorage).
//
// The engine keeps no saved tag for an entity or a block entity, so each
// accessor composes one from what the engine models, in vanilla's field
// names and tag types (entityNBT, blockDataNBT), and a write reads back the
// fields the engine has and that the command changed. A player's data can
// be read but, as in vanilla, not written. Command storage is kept whole,
// as typed tags, and persisted.

// ---- command storage ------------------------------------------------------------

// commandStorage is CommandStorage: named compounds, persisted as SNBT so
// the tag types survive.
type commandStorage struct {
	mu   sync.Mutex
	path string
	m    map[string]map[string]any
}

func commandStoragePathFor(spawnPath string) string {
	if spawnPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(spawnPath), "commandstorage.json")
}

func newCommandStorage(path string) *commandStorage {
	s := &commandStorage{path: path, m: map[string]map[string]any{}}
	if path == "" {
		return s
	}
	var saved map[string]string
	if err := loadStore(path, &saved); err != nil {
		log.Fatal(err)
	}
	for id, text := range saved {
		v, err := parseSNBTTyped(text)
		if m, ok := v.(map[string]any); err == nil && ok {
			s.m[id] = m
		} else {
			log.Printf("command storage %s: unreadable, dropped: %v", id, err)
		}
	}
	return s
}

// get is CommandStorage.get: a copy of the compound, empty when none.
func (s *commandStorage) get(id string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.m[id]; ok {
		return tagCopy(m).(map[string]any)
	}
	return map[string]any{}
}

// set is CommandStorage.set: an empty compound removes the entry.
func (s *commandStorage) set(id string, tag map[string]any) {
	s.mu.Lock()
	if len(tag) == 0 {
		delete(s.m, id)
	} else {
		s.m[id] = tagCopy(tag).(map[string]any)
	}
	saved := make(map[string]string, len(s.m))
	for k, v := range s.m {
		saved[k] = tagString(v)
	}
	path := s.path
	s.mu.Unlock()
	if path != "" {
		data, _ := json.MarshalIndent(saved, "", "  ")
		writeStore(path, data)
	}
}

// keys is the stored ids (the storage argument's suggestions).
func (s *commandStorage) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}

// storage is the hub's command storage, an unsaved one when none was
// configured (tests).
func (h *hub) storage() *commandStorage {
	if h.cmdStorage == nil {
		h.cmdStorage = newCommandStorage("")
	}
	return h.cmdStorage
}

// ---- parsing ----------------------------------------------------------------------

// dataTarget is one accessor argument: entity <target>, block <pos> or
// storage <id>.
type dataTarget struct {
	kind string
	sel  string
	pos  blockPos
	id   string
}

// dataReq is one parsed /data command.
type dataReq struct {
	by        int32
	verb      string // get, merge, modify, remove
	target    dataTarget
	path      *nbtPath // get/remove's path, modify's target path
	scale     float64
	hasScale  bool
	nbt       map[string]any // merge's compound
	op        string         // modify: append, prepend, insert, set, merge
	index     int            // …insert's index
	values    []any          // modify … value <tag>
	source    *dataTarget    // modify … from|string <source>
	srcPath   *nbtPath
	stringify bool // modify … string
	start     int  // …its substring
	end       int
	hasStart  bool
	hasEnd    bool
	compute   *dataCompute // modify … compute <context> float|integer <provider>
}

// dataCompute is `compute default|block <pos>|entity <target>
// float|integer <provider>` (LootContextSources): the value a number
// provider gives in a loot context, as a float or an int tag.
type dataCompute struct {
	hasPos bool
	pos    blockPos
	target string
	prov   computeProvider
}

const dataUsage = "Usage: /data get <target> [<path> [<scale>]] | merge <target> <nbt> | remove <target> <path> | " +
	"modify <target> <path> append|prepend|insert <index>|set|merge value <nbt>|from <source> [<path>]|string <source> [<path> [<start> [<end>]]]|compute default|block <pos>|entity <target> float|integer <provider>" +
	" — a target is entity <target>, block <pos> or storage <id>"

// parseDataTarget reads an accessor at the start of args and returns the
// arguments after it.
func parseDataTarget(p *player, args []string) (dataTarget, []string, string) {
	if len(args) < 2 {
		return dataTarget{}, nil, dataUsage
	}
	switch args[0] {
	case "entity":
		return dataTarget{kind: "entity", sel: args[1]}, args[2:], ""
	case "block":
		if len(args) < 4 {
			return dataTarget{}, nil, dataUsage
		}
		x, y, z, ok := parsePosition(args[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			return dataTarget{}, nil, dataUsage
		}
		return dataTarget{kind: "block", pos: blockPos{floorInt(x), floorInt(y), floorInt(z)}}, args[4:], ""
	case "storage":
		id, ok := parseResourceID(args[1])
		if !ok {
			return dataTarget{}, nil, "Invalid ID: " + args[1]
		}
		return dataTarget{kind: "storage", id: id}, args[2:], ""
	}
	return dataTarget{}, nil, dataUsage
}

func parsePathArg(s string) (*nbtPath, string) {
	p, msg := parseNBTPath(s)
	if msg != "" {
		return nil, msg
	}
	return &p, ""
}

// parseData reads a /data command line (after "data").
func parseData(p *player, args []string) (dataReq, string) {
	if len(args) < 2 {
		return dataReq{}, dataUsage
	}
	r := dataReq{by: p.eid, verb: args[0]}
	tgt, rest, msg := parseDataTarget(p, args[1:])
	if msg != "" {
		return dataReq{}, msg
	}
	r.target = tgt
	switch r.verb {
	case "get":
		if len(rest) > 2 {
			return dataReq{}, dataUsage
		}
		if len(rest) >= 1 {
			if r.path, msg = parsePathArg(rest[0]); msg != "" {
				return dataReq{}, msg
			}
		}
		if len(rest) == 2 {
			f, err := strconv.ParseFloat(rest[1], 64)
			if err != nil {
				return dataReq{}, "Expected double"
			}
			r.scale, r.hasScale = f, true
		}
	case "merge":
		if len(rest) == 0 {
			return dataReq{}, dataUsage
		}
		m, msg := parseCompoundArg(strings.Join(rest, " "))
		if msg != "" {
			return dataReq{}, msg
		}
		r.nbt = m
	case "remove":
		if len(rest) != 1 {
			return dataReq{}, dataUsage
		}
		if r.path, msg = parsePathArg(rest[0]); msg != "" {
			return dataReq{}, msg
		}
	case "modify":
		if len(rest) < 3 {
			return dataReq{}, dataUsage
		}
		if r.path, msg = parsePathArg(rest[0]); msg != "" {
			return dataReq{}, msg
		}
		r.op = rest[1]
		i := 2
		switch r.op {
		case "append", "prepend", "set", "merge":
		case "insert":
			n, err := strconv.Atoi(rest[2])
			if err != nil {
				return dataReq{}, "Expected integer"
			}
			r.index, i = n, 3
		default:
			return dataReq{}, dataUsage
		}
		if len(rest) <= i+1 {
			return dataReq{}, dataUsage
		}
		switch rest[i] {
		case "value":
			v, err := parseSNBTTyped(strings.Join(rest[i+1:], " "))
			if err != nil {
				return dataReq{}, err.Error()
			}
			r.values = []any{v}
		case "from", "string":
			src, after, msg := parseDataTarget(p, rest[i+1:])
			if msg != "" {
				return dataReq{}, msg
			}
			r.source, r.stringify = &src, rest[i] == "string"
			limit := 1
			if r.stringify {
				limit = 3
			}
			if len(after) > limit {
				return dataReq{}, dataUsage
			}
			if len(after) >= 1 {
				if r.srcPath, msg = parsePathArg(after[0]); msg != "" {
					return dataReq{}, msg
				}
			}
			for j, dst := range []*int{&r.start, &r.end} {
				if len(after) < j+2 {
					break
				}
				n, err := strconv.Atoi(after[j+1])
				if err != nil {
					return dataReq{}, "Expected integer"
				}
				*dst = n
			}
			r.hasStart, r.hasEnd = len(after) >= 2, len(after) >= 3
		case "compute":
			c, msg := parseDataCompute(p, rest[i+1:])
			if msg != "" {
				return dataReq{}, msg
			}
			r.compute = c
		default:
			return dataReq{}, dataUsage
		}
	default:
		return dataReq{}, dataUsage
	}
	return r, ""
}

// parseDataCompute reads `default|block <pos>|entity <target>
// float|integer <provider>`, the provider running to the end of the line.
func parseDataCompute(p *player, a []string) (*dataCompute, string) {
	c := &dataCompute{}
	switch {
	case len(a) >= 1 && a[0] == "default":
		a = a[1:]
	case len(a) >= 4 && a[0] == "block":
		x, y, z, ok := parsePosition(a[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			return nil, dataUsage
		}
		c.hasPos, c.pos, a = true, blockPos{floorInt(x), floorInt(y), floorInt(z)}, a[4:]
	case len(a) >= 2 && a[0] == "entity":
		c.target, a = a[1], a[2:]
	default:
		return nil, dataUsage
	}
	if len(a) < 2 || (a[0] != "float" && a[0] != "integer") {
		return nil, dataUsage
	}
	prov, tail, msg := parseComputeProvider(strings.Join(a[1:], " "), a[0] == "float")
	if msg != "" {
		return nil, msg
	}
	if tail != "" {
		return nil, "Incorrect argument for command"
	}
	c.prov = prov
	return c, ""
}

// dataComputeValue runs the provider in its loot context (the source as
// this, and the block or the target entity) and gives the tag it
// becomes: ContextFloatProvider.getFloat as a float, getInt as an int.
func (h *hub) dataComputeValue(players map[int32]*tracked, by int32, c *dataCompute) (any, string) {
	t := players[by]
	if t == nil {
		return nil, "No entity was found"
	}
	ctx := &computeCtx{h: h, this: t}
	if c.hasPos { // BlockPosArgument.getLoadedBlockPos
		if !h.hasDim(t.dim) || !h.cloneLoaded(t.dim, c.pos, c.pos) {
			return nil, "That position is not loaded"
		}
		if !h.inWorldYIn(t.dim, c.pos.y) {
			return nil, "That position is out of this world!"
		}
		ctx.hasBS, ctx.state = true, h.worldFor(t.dim).At(c.pos.x, c.pos.y, c.pos.z)
	}
	if c.target != "" {
		var fail string
		en, ok := h.singleEntity(players, by, c.target, func(m string) { fail = m })
		if !ok {
			return nil, fail
		}
		ctx.target = &en
	}
	if c.prov.float != nil {
		return nbtFloat(c.prov.float.getFloat(ctx)), ""
	}
	return nbtInt(c.prov.int.getInt(ctx)), ""
}

// parseCompoundArg is CompoundTagArgument: one typed SNBT compound.
func parseCompoundArg(s string) (map[string]any, string) {
	v, err := parseSNBTTyped(s)
	if err != nil {
		return nil, err.Error()
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "Expected '{'"
	}
	return m, ""
}

func (s *Server) cmdData(p *player, args []string) {
	if !s.isOp(p.name) { // DataCommands: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	r, msg := parseData(p, args)
	if msg != "" {
		p.tell(msg)
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		if msg := s.hub.runData(players, r); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// ---- accessors --------------------------------------------------------------------

// dataAccessor is a resolved target: one entity, one block entity or one
// storage id.
type dataAccessor struct {
	kind  string
	en    cmdEntity
	pos   simPos
	state uint32
	id    string
}

// dataAccess resolves a target on the hub (the accessor's PROVIDER).
func (h *hub) dataAccess(players map[int32]*tracked, by int32, t dataTarget) (dataAccessor, string) {
	switch t.kind {
	case "entity":
		targets := h.commandEntities(players, by, t.sel)
		switch {
		case len(targets) == 0:
			return dataAccessor{}, "No entity was found"
		case len(targets) > 1:
			return dataAccessor{}, "Only one entity is allowed, but the provided selector allows more than one"
		}
		return dataAccessor{kind: "entity", en: targets[0]}, ""
	case "block":
		caller := players[by]
		if caller == nil {
			return dataAccessor{}, "No entity was found"
		}
		pos := simPos{dim: caller.dim, blockPos: t.pos}
		// BlockPosArgument.getLoadedBlockPos, then the block entity.
		if !h.hasDim(pos.dim) || !h.cloneLoaded(pos.dim, pos.blockPos, pos.blockPos) {
			return dataAccessor{}, "That position is not loaded"
		}
		if !h.inWorldYIn(pos.dim, pos.y) {
			return dataAccessor{}, "That position is out of this world!"
		}
		st := h.worldFor(pos.dim).At(pos.x, pos.y, pos.z)
		if !hasBlockEntity(st) {
			return dataAccessor{}, "The target block is not a block entity"
		}
		return dataAccessor{kind: "block", pos: pos, state: st}, ""
	}
	return dataAccessor{kind: "storage", id: t.id}, ""
}

// get is DataAccessor.getData.
func (h *hub) dataGet(a dataAccessor) map[string]any {
	switch a.kind {
	case "entity":
		return h.entityNBT(a.en)
	case "block":
		return h.blockDataNBT(a.pos, a.state)
	}
	return h.storage().get(a.id)
}

// dataSet is DataAccessor.setData; old is what dataGet gave, so a write
// touches only what the command changed. A non-empty string refuses it.
func (h *hub) dataSet(players map[int32]*tracked, a dataAccessor, old, tag map[string]any) string {
	switch a.kind {
	case "entity":
		if a.en.t != nil {
			return "Unable to modify player data"
		}
		h.setMobNBT(players, a.en.m, old, tag)
		return ""
	case "block":
		return h.setBlockDataNBT(players, a.pos, a.state, old, tag)
	}
	h.storage().set(a.id, tag)
	return ""
}

func (a dataAccessor) modified() string {
	switch a.kind {
	case "entity":
		return "Modified entity data of " + a.en.name()
	case "block":
		return fmt.Sprintf("Modified block data of %d, %d, %d", a.pos.x, a.pos.y, a.pos.z)
	}
	return "Modified storage " + a.id
}

func (a dataAccessor) query(tag any) string {
	switch a.kind {
	case "entity":
		return a.en.name() + " has the following entity data: " + tagString(tag)
	case "block":
		return fmt.Sprintf("%d, %d, %d has the following block data: %s", a.pos.x, a.pos.y, a.pos.z, tagString(tag))
	}
	return fmt.Sprintf("Storage %s has the following contents: %s", a.id, tagString(tag))
}

func (a dataAccessor) scaled(path nbtPath, scale float64, v int) string {
	sc := fmt.Sprintf("%.2f", scale)
	switch a.kind {
	case "entity":
		return fmt.Sprintf("%s on %s after scale factor of %s is %d", path, a.en.name(), sc, v)
	case "block":
		return fmt.Sprintf("%s on block %d, %d, %d after scale factor of %s is %d", path, a.pos.x, a.pos.y, a.pos.z, sc, v)
	}
	return fmt.Sprintf("%s in storage %s after scale factor of %s is %d", path, a.id, sc, v)
}

// ---- running it ---------------------------------------------------------------------

const errDataUnchanged = "Nothing changed. The specified properties already have these values"

// runData executes a parsed /data on the hub; a non-empty string is the
// failure line.
func (h *hub) runData(players map[int32]*tracked, r dataReq) string {
	a, msg := h.dataAccess(players, r.by, r.target)
	if msg != "" {
		return msg
	}
	switch r.verb {
	case "get":
		data := h.dataGet(a)
		if r.path == nil {
			h.cmdInfo(players, r.by)(a.query(data))
			return ""
		}
		tags, msg := r.path.get(data)
		if msg != "" {
			return msg
		}
		if len(tags) > 1 {
			return "This argument accepts a single NBT value"
		}
		if !r.hasScale {
			h.cmdInfo(players, r.by)(a.query(tags[0]))
			return ""
		}
		f, ok := tagFloat64(tags[0])
		if !ok {
			return fmt.Sprintf("Can't get %s; only numeric tags are allowed", r.path)
		}
		h.cmdInfo(players, r.by)(a.scaled(*r.path, r.scale, int(math.Floor(f*r.scale))))
		return ""
	case "merge":
		old := h.dataGet(a)
		if tagTooDeep(r.nbt, 0) {
			return errNBTTooDeep
		}
		res := tagCopy(old).(map[string]any)
		tagMerge(res, r.nbt)
		if tagEqual(old, res) {
			return errDataUnchanged
		}
		return h.dataCommit(players, r, a, old, res)
	case "remove":
		old := h.dataGet(a)
		res := tagCopy(old).(map[string]any)
		if r.path.remove(res) == 0 {
			return errDataUnchanged
		}
		return h.dataCommit(players, r, a, old, res)
	}
	// modify: the source tags first (a source that finds nothing fails
	// before the target is touched), then the operation on a copy.
	src := r.values
	if r.compute != nil {
		v, msg := h.dataComputeValue(players, r.by, r.compute)
		if msg != "" {
			return msg
		}
		src = []any{v}
	}
	if r.source != nil {
		sa, msg := h.dataAccess(players, r.by, *r.source)
		if msg != "" {
			return msg
		}
		sd := h.dataGet(sa)
		src = []any{sd}
		if r.srcPath != nil {
			if src, msg = r.srcPath.get(sd); msg != "" {
				return msg
			}
		}
		if r.stringify {
			out := make([]any, len(src))
			for i, t := range src {
				text, ok := tagText(t)
				if !ok {
					return "Expected a value: got " + tagString(t)
				}
				if r.hasStart {
					if text, msg = dataSubstring(text, r.start, r.end, r.hasEnd); msg != "" {
						return msg
					}
				}
				out[i] = text
			}
			src = out
		}
	}
	old := h.dataGet(a)
	res := tagCopy(old).(map[string]any)
	n, msg := dataModify(r, res, src)
	if msg != "" {
		return msg
	}
	if n == 0 {
		return errDataUnchanged
	}
	return h.dataCommit(players, r, a, old, res)
}

// dataCommit writes a changed tag back and reports it.
func (h *hub) dataCommit(players map[int32]*tracked, r dataReq, a dataAccessor, old, res map[string]any) string {
	if msg := h.dataSet(players, a, old, res); msg != "" {
		return msg
	}
	h.cmdOK(players, r.by)(a.modified())
	return ""
}

// dataModify is the manipulator a modify names, run on the target's tag.
func dataModify(r dataReq, target map[string]any, src []any) (int, string) {
	switch r.op {
	case "append":
		return r.path.insert(-1, target, src)
	case "prepend":
		return r.path.insert(0, target, src)
	case "insert":
		return r.path.insert(r.index, target, src)
	case "set":
		if len(src) == 0 {
			return 0, ""
		}
		return r.path.set(target, src[len(src)-1])
	}
	// merge: the sources' compounds merged together, then into each
	// compound the path reaches (made when missing).
	combined := map[string]any{}
	for _, s := range src {
		if tagTooDeep(s, 0) {
			return 0, errNBTTooDeep
		}
		m, ok := s.(map[string]any)
		if !ok {
			return 0, "Expected an object: got " + tagString(s)
		}
		tagMerge(combined, m)
	}
	targets, msg := r.path.getOrCreate(target, func() any { return map[string]any{} })
	if msg != "" {
		return 0, msg
	}
	changed := 0
	for _, t := range targets {
		m, ok := t.(map[string]any)
		if !ok {
			return 0, "Expected an object: got " + tagString(t)
		}
		before := tagCopy(m)
		tagMerge(m, combined)
		if !tagEqual(before, m) {
			changed++
		}
	}
	return changed, ""
}

// dataSubstring is DataCommands.substring: negative indices count from the
// end; out-of-range or crossed indices are refused.
func dataSubstring(s string, start, end int, hasEnd bool) (string, string) {
	r := []rune(s)
	off := func(i int) int {
		if i >= 0 {
			return i
		}
		return len(r) + i
	}
	a, b := off(start), len(r)
	if hasEnd {
		b = off(end)
	}
	if a < 0 || b > len(r) || a > b {
		return "", fmt.Sprintf("Invalid substring indices: %d to %d", a, b)
	}
	return string(r[a:b]), ""
}

// ---- entities -------------------------------------------------------------------

// entityNBT is NbtPredicate.getEntityTagToCompare: Entity.saveWithoutId for
// the fields the engine models, plus the held item for a player.
func (h *hub) entityNBT(en cmdEntity) map[string]any {
	m := map[string]any{}
	l := en.living()
	if en.t != nil {
		t := en.t
		m["Pos"] = tagDoubles(t.x, t.y, t.z)
		m["Motion"] = tagDoubles(t.kvx, t.kvy, t.kvz)
		m["Rotation"] = tagFloats(t.yaw, t.pitch)
		m["Fire"] = nbtShort(fireTicksNBT(t.fireSecs, 20))
		m["Air"] = nbtShort(t.air)
		m["OnGround"] = tagBool(t.onGround)
		m["UUID"] = tagUUID(t.p.uuid)
		m["Health"] = nbtFloat(t.health)
		m["AbsorptionAmount"] = nbtFloat(t.absorption)
		m["foodLevel"] = nbtInt(t.food)
		m["foodSaturationLevel"] = nbtFloat(t.saturation)
		m["foodExhaustionLevel"] = nbtFloat(t.exhaustion)
		m["XpLevel"] = nbtInt(t.xpLevel)
		if next := xpToNext(t.xpLevel); next > 0 {
			m["XpP"] = nbtFloat(float32(t.xpPoints) / float32(next))
		}
		m["playerGameType"] = nbtInt(t.gamemode)
		m["Dimension"] = dimRegistryName(t.dim)
		held := t.p.heldSlot()
		m["SelectedItemSlot"] = nbtInt(held)
		inv := &nbtList{}
		if t.inv != nil {
			for i, st := range t.inv.slots {
				if st.item == 0 || st.count <= 0 {
					continue
				}
				e := tagFromView(stackNBT(st), "").(map[string]any)
				e["Slot"] = nbtByte(i)
				inv.elems = append(inv.elems, e)
			}
			if st := t.inv.slots[held]; st.item != 0 && st.count > 0 {
				m["SelectedItem"] = tagFromView(stackNBT(st), "")
			}
		}
		m["Inventory"] = inv
		eq := map[string]any{}
		for i, slot := range []string{"head", "chest", "legs", "feet"} {
			if st := t.armor[i]; st.item != 0 && st.count > 0 {
				eq[slot] = tagFromView(stackNBT(st), "")
			}
		}
		if st := t.offhand; st.item != 0 && st.count > 0 {
			eq["offhand"] = tagFromView(stackNBT(st), "")
		}
		if len(eq) > 0 {
			m["equipment"] = eq
		}
		if t.ender != nil {
			m["EnderItems"] = tagFromView(itemsNBT(t.ender.slots[:]), "Items")
		}
	} else {
		mb := en.m
		m["Pos"] = tagDoubles(mb.x, mb.y, mb.z)
		m["Motion"] = tagDoubles(mb.vx, mb.vy, mb.vz)
		m["Rotation"] = tagFloats(mb.yaw, 0)
		m["Fire"] = nbtShort(fireTicksNBT(mb.fireSecs, 1))
		m["Air"] = nbtShort(300)
		m["OnGround"] = tagBool(!mb.airborne)
		m["Invulnerable"] = tagBool(mb.invulnerable)
		m["UUID"] = tagUUID(mb.uuid)
		if mb.customName != "" {
			m["CustomName"] = mb.customName
			if !mb.nameHidden {
				m["CustomNameVisible"] = nbtByte(1)
			}
		}
		if mb.silent {
			m["Silent"] = nbtByte(1)
		}
		if mb.ticksFrozen > 0 {
			m["TicksFrozen"] = nbtInt(mb.ticksFrozen)
		}
		m["Health"] = nbtFloat(float32(mb.health))
		m["AbsorptionAmount"] = nbtFloat(float32(mb.absorption))
		m["PersistenceRequired"] = tagBool(mb.persistent || mb.customName != "")
		if mb.noAI {
			m["NoAI"] = nbtByte(1)
		}
		if eggOffspring[mb.etype] {
			age := 0
			if mb.baby {
				age = -mb.growLeft
			}
			m["Age"] = nbtInt(age)
		}
		if eggSetBaby[mb.etype] {
			m["IsBaby"] = tagBool(mb.baby)
		}
		switch mb.etype {
		case entitySheep:
			m["Color"] = nbtByte(mb.color)
			m["Sheared"] = tagBool(mb.sheared)
		case entityCreeper:
			m["powered"] = tagBool(mb.charged)
			m["ignited"] = tagBool(mb.ignited)
		}
	}
	if len(l.tags) > 0 {
		tags := &nbtList{}
		for _, s := range sortedTags(l.tags) {
			tags.elems = append(tags.elems, s)
		}
		m["Tags"] = tags
	}
	if len(l.custom) > 0 {
		m["data"] = tagCopy(l.custom)
	}
	if fx := effectsNBT(l); fx != nil {
		m["active_effects"] = fx
	}
	return m
}

// fireTicksNBT is Entity.remainingFireTicks as saved: the burn left in
// ticks, or minus the fire-immune ticks when not burning (a player's 20,
// anything else's 1).
func fireTicksNBT(secs, immune int) int {
	if secs > 0 {
		return secs * 20
	}
	return -immune
}

// effectsNBT is LivingEntity's active_effects: each MobEffectInstance as
// saved.
func effectsNBT(l *living) *nbtList {
	if len(l.effects) == 0 {
		return nil
	}
	names := make(map[int32]string, len(effectNames))
	for n, id := range effectNames {
		names[id] = n
	}
	out := &nbtList{}
	ids := make([]int, 0, len(l.effects))
	for id := range l.effects {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	for _, id := range ids {
		e := l.effects[int32(id)]
		n := names[int32(id)]
		if n == "" {
			continue
		}
		out.elems = append(out.elems, map[string]any{
			"id":             "minecraft:" + n,
			"amplifier":      nbtByte(e.amp),
			"duration":       nbtInt(e.left),
			"ambient":        tagBool(e.ambient),
			"show_particles": tagBool(!e.noParticles),
			"show_icon":      tagBool(!e.noParticles),
		})
	}
	return out
}

// setMobNBT is EntityDataAccessor.setData for a mob: Entity.load reads the
// changed fields the engine models, and the mob's viewers are shown the
// result. A field the tag dropped goes back to its default.
func (h *hub) setMobNBT(players map[int32]*tracked, m *mob, old, tag map[string]any) {
	changed := func(k string) bool { return !tagEqual(old[k], tag[k]) }
	view := tagToView(tag).(map[string]any)
	send := func(body []byte) { h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(body)) }
	moved := false
	if changed("Pos") {
		if pos, ok := view["Pos"].([]any); ok && len(pos) == 3 {
			x, ok1 := snbtFloat(pos[0])
			y, ok2 := snbtFloat(pos[1])
			z, ok3 := snbtFloat(pos[2])
			if ok1 && ok2 && ok3 && !math.IsNaN(x+y+z) {
				m.x, m.y, m.z = x, y, z
				moved = true
			}
		}
	}
	if changed("Rotation") {
		if yaw, _, ok := nbtRotation(view); ok {
			m.yaw, m.headYaw = yaw, yaw
			moved = true
		}
	}
	if moved {
		m.syaw = m.yaw
		m.snapLook()
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, false))
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entHead(m.eid, m.yaw))
	}
	if changed("Motion") {
		if vx, vy, vz, ok := nbtMotion(view); ok {
			m.vx, m.vy, m.vz = vx, vy, vz
		}
	}
	if changed("CustomName") || changed("CustomNameVisible") {
		shown, _ := nbtBool(view, "CustomNameVisible")
		m.customName, m.nameHidden = nbtName(view), !shown
		send(nameMetaVis(m.eid, m.customName, shown && m.customName != ""))
	}
	if changed("Tags") {
		m.tags = nil
		if list, ok := view["Tags"].([]any); ok {
			for _, t := range list {
				if s, ok := t.(string); ok {
					m.addTag(s)
				}
			}
		}
	}
	if changed("data") {
		m.custom = nil
		if d, ok := tag["data"].(map[string]any); ok && len(d) > 0 {
			m.custom = tagCopy(d).(map[string]any)
		}
	}
	if changed("Health") {
		// LivingEntity.setHealth clamps to the maximum; the engine keeps
		// whole hit points, and a mob set to none is left to its last one.
		if hp, ok := snbtFloat(view["Health"]); ok && hp > 0 {
			m.health = max(1, min(int(hp+0.5), m.maxHP()))
			send(mobHealthMeta(m.eid, m.health))
		}
	}
	if changed("Invulnerable") {
		m.invulnerable, _ = nbtBool(view, "Invulnerable")
	}
	if changed("Silent") {
		m.silent, _ = nbtBool(view, "Silent")
		send(boolMeta(m.eid, metaIndexSilent, m.silent))
	}
	if changed("NoAI") {
		m.noAI, _ = nbtBool(view, "NoAI")
		if m.noAI {
			m.vx, m.vy, m.vz = 0, 0, 0
		}
		send(mobFlagsByte(m.eid, m.mobFlags()))
	}
	if changed("PersistenceRequired") {
		m.persistent, _ = nbtBool(view, "PersistenceRequired")
	}
	if changed("Sheared") && m.etype == entitySheep {
		m.sheared, _ = nbtBool(view, "Sheared")
		send(sheepMeta(m, m.sheared))
	}
	if changed("ignited") && m.etype == entityCreeper {
		if on, _ := nbtBool(view, "ignited"); on {
			m.ignited = true
		}
	}
	// The species' own keys, read as /summon reads them.
	species := map[string]any{}
	for _, k := range []string{"Age", "IsBaby", "Color", "powered"} {
		if changed(k) {
			if v, ok := view[k]; ok {
				species[k] = v
			}
		}
	}
	if len(species) > 0 {
		h.applySummonSpeciesNBT(players, m, species)
	}
}

// ---- block entities -------------------------------------------------------------

// blockEntityTypeName is the block entity type a block's entity saves as its
// id (BlockEntityType's registered blocks, by the families that share one).
func blockEntityTypeName(state uint32) string {
	name, _ := worldgen.StateName(state)
	switch {
	case strings.HasSuffix(name, "hanging_sign"):
		return "hanging_sign"
	case strings.HasSuffix(name, "_sign"):
		return "sign"
	case strings.HasSuffix(name, "_banner"):
		return "banner"
	case strings.HasSuffix(name, "_bed"):
		return "bed"
	case strings.HasSuffix(name, "shulker_box"):
		return "shulker_box"
	case strings.HasSuffix(name, "_head") || strings.HasSuffix(name, "_skull"):
		return "skull"
	case strings.HasSuffix(name, "campfire"):
		return "campfire"
	case strings.HasSuffix(name, "command_block"):
		return "command_block"
	case name == "bee_nest":
		return "beehive"
	case name == "moving_piston":
		return "piston"
	case strings.HasSuffix(name, "_shelf"):
		return "shelf"
	case strings.HasSuffix(name, "copper_golem_statue"):
		return "copper_golem_statue"
	case name == "sculk_sensor":
		return "sculk_sensor"
	}
	return name
}

// blockDataNBT is BlockEntity.saveWithFullMetadata: the modelled data with
// the type id and the position.
func (h *hub) blockDataNBT(pos simPos, state uint32) map[string]any {
	m := map[string]any{}
	if v := h.blockEntityNBT(pos, state); v != nil {
		m = tagFromView(v, "").(map[string]any)
	}
	m["id"] = "minecraft:" + blockEntityTypeName(state)
	m["x"], m["y"], m["z"] = nbtInt(pos.x), nbtInt(pos.y), nbtInt(pos.z)
	return m
}

// setBlockDataNBT is BlockDataAccessor.setData: BlockEntity.loadWithComponents
// for the changed fields the engine models, then sendBlockUpdated. A
// container's Items the tag no longer has leave it empty, as loading a
// container does.
func (h *hub) setBlockDataNBT(players map[int32]*tracked, pos simPos, state uint32, old, tag map[string]any) string {
	changed := func(k string) bool { return !tagEqual(old[k], tag[k]) }
	view := tagToView(tag).(map[string]any)
	bad := func(field string) string { return fmt.Sprintf("Invalid block entity data: '%s'", field) }
	if changed("Items") {
		raw, ok := view["Items"]
		if !ok {
			raw = []any{}
		}
		switch {
		case isChestLikeContainer(state):
			slots, ok := itemsFromNBT(raw, 27)
			if !ok {
				return bad("Items")
			}
			c := h.chests[pos]
			if c == nil {
				c = &chest{}
				h.chests[pos] = c
			}
			copy(c.slots[:], slots)
		case isBinBlock(state):
			b := h.binAt(pos, state)
			slots, ok := itemsFromNBT(raw, len(b.slots))
			if !ok {
				return bad("Items")
			}
			copy(b.slots, slots)
		default:
			if kind, ok := furnaceKindOf(state); ok {
				slots, ok := itemsFromNBT(raw, 3)
				if !ok {
					return bad("Items")
				}
				f := h.furnaces[pos]
				if f == nil {
					f = &furnace{cookMax: 200, kind: kind}
					h.furnaces[pos] = f
				}
				copy(f.slots[:], slots)
			}
		}
		h.containerChanged(players, pos)
	}
	if changed("CustomName") && nameableBlock(state) && h.blockNames != nil {
		h.blockNames.set(pos, nbtName(view))
	}
	if _, isSign := signKind(state); isSign && (changed("front_text") || changed("back_text") || changed("is_waxed")) {
		sub := map[string]any{}
		for _, k := range []string{"front_text", "back_text", "is_waxed"} {
			if v, ok := view[k]; ok {
				sub[k] = v
			}
		}
		c, msg := h.carriedFromNBT(pos, state, sub)
		if msg != "" {
			return msg
		}
		h.placeBlockEntity(players, pos, carriedBE{sign: c.sign}, state)
	}
	if isBannerState(state) && changed("patterns") && h.banners != nil {
		layers := []attachproto.BannerLayer{}
		if v, ok := view["patterns"]; ok {
			l, ok := bannerLayersFromNBT(v)
			if !ok {
				return bad("patterns")
			}
			layers = l
		}
		if len(layers) == 0 {
			h.banners.remove(pos)
		} else {
			h.banners.set(pos, layers)
		}
		h.toNearbyEv(players, pos.dim, float64(pos.x), float64(pos.z), attachproto.BannerPatterns{
			X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Layers: layers})
	}
	if state == spawnerBlock && (changed("SpawnData") || changed("Delay")) {
		h.loadSpawnerNBT(pos, view)
	}
	return ""
}
