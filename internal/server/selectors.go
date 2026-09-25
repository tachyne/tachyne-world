package server

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Target selectors and coordinate arguments — the two pieces of vanilla's
// command grammar that every command shares. `@s`, `@p`, `@a`, `@r` and `@e`
// (with the common predicates) stand in anywhere a player name did, and
// `~` / `^` make a coordinate relative to where you stand or which way you
// look, so `/tp ~ ~10 ~` and `/kill @e[type=zombie,distance=..16]` work.
//
// This is not the full brigadier grammar — no NBT predicates, no scores —
// but it is the part players actually type.

// targetSpec is a parsed selector. An empty kind means a literal name.
type targetSpec struct {
	kind     byte    // 's', 'p', 'a', 'r', 'e', or 0 for a plain name
	name     string  // the literal name, or a name= predicate
	etype    string  // type= (without the namespace); "player" selects players
	notEtype string  // type=! …
	maxDist  float64 // distance=..max
	minDist  float64 // distance=min..
	limit    int     // limit=, 0 = unlimited
	nearest  bool    // sort=nearest (the default for @p, and for a limit on @e)
	hasDist  bool
	tags     []string // tag=… each one required ("" = no tags at all)
	notTags  []string // tag=!… each one forbidden ("" = at least one tag)

	// sort= (nearest, furthest, random, arbitrary; "" = the kind's default).
	sortBy string
	// notNames are name=! predicates, each one excluded.
	notNames []string
	// x=, y=, z= move the origin the distance and volume are measured
	// from; dx=, dy=, dz= make a volume from it.
	origin    [3]float64
	hasOrigin [3]bool
	delta     [3]float64
	hasDelta  bool
	// scores={objective=range,…}: every score in its range.
	scores map[string][2]int64
	// team=name, team=!name, team= (on no team), team=! (on some team).
	teams    []string
	notTeams []string
	// level=range (players only), gamemode=/gamemode=! (players only).
	level      [2]int64
	hasLevel   bool
	gamemodes  []int
	notGmodes  []int
	xRot, yRot [2]float64
	hasXRot    bool
	hasYRot    bool
}

// parseTargetSpec reads one selector argument. A bare word is a player name,
// which keeps every existing "/kill Steve" working.
func parseTargetSpec(arg string) (targetSpec, bool) {
	if arg == "" {
		return targetSpec{}, false
	}
	if arg[0] != '@' {
		return targetSpec{name: arg}, true
	}
	if len(arg) < 2 {
		return targetSpec{}, false
	}
	spec := targetSpec{kind: arg[1]}
	switch spec.kind {
	case 's', 'p', 'a', 'r', 'e':
	default:
		return targetSpec{}, false
	}
	if spec.kind == 'p' || spec.kind == 'r' {
		spec.limit, spec.nearest = 1, spec.kind == 'p'
	}
	rest := arg[2:]
	if rest == "" {
		return spec, true
	}
	if !strings.HasPrefix(rest, "[") || !strings.HasSuffix(rest, "]") {
		return targetSpec{}, false
	}
	for _, pred := range splitPredicates(rest[1 : len(rest)-1]) {
		k, v, ok := strings.Cut(pred, "=")
		if !ok {
			return targetSpec{}, false
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "type":
			if strings.HasPrefix(v, "!") {
				spec.notEtype = strings.TrimPrefix(strings.TrimPrefix(v, "!"), "minecraft:")
			} else {
				spec.etype = strings.TrimPrefix(v, "minecraft:")
			}
		case "name":
			if strings.HasPrefix(v, "!") {
				spec.notNames = append(spec.notNames, strings.Trim(v[1:], `"`))
			} else {
				spec.name = strings.Trim(v, `"`)
			}
		case "tag":
			if strings.HasPrefix(v, "!") {
				spec.notTags = append(spec.notTags, v[1:])
			} else {
				spec.tags = append(spec.tags, v)
			}
		case "limit", "c":
			n, err := strconv.Atoi(v)
			if err != nil {
				return targetSpec{}, false
			}
			spec.limit, spec.nearest = n, true
		case "sort":
			switch v {
			case "nearest", "furthest", "random", "arbitrary":
			default:
				return targetSpec{}, false
			}
			spec.sortBy, spec.nearest = v, v == "nearest"
		case "distance", "r":
			lo, hi, ranged := strings.Cut(v, "..")
			spec.hasDist = true
			if !ranged { // an exact distance: treat it as an upper bound
				d, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return targetSpec{}, false
				}
				spec.maxDist = d
				break
			}
			if lo != "" {
				d, err := strconv.ParseFloat(lo, 64)
				if err != nil {
					return targetSpec{}, false
				}
				spec.minDist = d
			}
			if hi != "" {
				d, err := strconv.ParseFloat(hi, 64)
				if err != nil {
					return targetSpec{}, false
				}
				spec.maxDist = d
			}
		case "x", "y", "z":
			d, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return targetSpec{}, false
			}
			i := int(k[0] - 'x')
			spec.origin[i], spec.hasOrigin[i] = d, true
		case "dx", "dy", "dz":
			d, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return targetSpec{}, false
			}
			spec.delta[int(k[1]-'x')], spec.hasDelta = d, true
		case "scores":
			if !strings.HasPrefix(v, "{") || !strings.HasSuffix(v, "}") {
				return targetSpec{}, false
			}
			if spec.scores == nil {
				spec.scores = map[string][2]int64{}
			}
			for _, sc := range splitPredicates(v[1 : len(v)-1]) {
				obj, rng, ok := strings.Cut(sc, "=")
				if !ok {
					return targetSpec{}, false
				}
				lo, hi, ok := parseSelectorIntRange(strings.TrimSpace(rng))
				if !ok {
					return targetSpec{}, false
				}
				spec.scores[strings.TrimSpace(obj)] = [2]int64{lo, hi}
			}
		case "team":
			if strings.HasPrefix(v, "!") {
				spec.notTeams = append(spec.notTeams, v[1:])
			} else {
				spec.teams = append(spec.teams, v)
			}
		case "level":
			lo, hi, ok := parseSelectorIntRange(v)
			if !ok {
				return targetSpec{}, false
			}
			spec.level, spec.hasLevel = [2]int64{lo, hi}, true
		case "gamemode", "m":
			neg := strings.HasPrefix(v, "!")
			mode, ok := ParseGamemode(strings.TrimPrefix(v, "!"))
			if !ok {
				return targetSpec{}, false
			}
			if neg {
				spec.notGmodes = append(spec.notGmodes, mode)
			} else {
				spec.gamemodes = append(spec.gamemodes, mode)
			}
		case "x_rotation", "y_rotation":
			lo, hi, ok := parseSelectorFloatRange(v)
			if !ok {
				return targetSpec{}, false
			}
			if k == "x_rotation" {
				spec.xRot, spec.hasXRot = [2]float64{lo, hi}, true
			} else {
				spec.yRot, spec.hasYRot = [2]float64{lo, hi}, true
			}
		default:
			// An unknown predicate is ignored rather than failing the whole
			// command — a selector that selects too much is easier to see
			// than one that silently does nothing.
		}
	}
	return spec, true
}

// splitPredicates splits a predicate list on commas that are not inside
// nested brackets or quotes.
func splitPredicates(s string) []string {
	var out []string
	depth, quoted, start := 0, false, 0
	for i, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '[' || r == '{':
			depth++
		case r == ']' || r == '}':
			depth--
		case r == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// selectsEntities reports whether the spec can match anything but players.
func (spec targetSpec) selectsEntities() bool {
	return spec.kind == 'e' && spec.etype != "player"
}

// selectPlayers resolves a spec against the online players, from the point of
// view of `from` (which may be nil for a console-ish caller).
func (h *hub) selectPlayers(players map[int32]*tracked, from *tracked, spec targetSpec) []*tracked {
	var out []*tracked
	for _, en := range h.selectEntities(players, from, spec, true, false) {
		out = append(out, en.t)
	}
	return out
}

// selectMobs resolves a spec against the loaded mobs (only @e does).
func (h *hub) selectMobs(from *tracked, spec targetSpec) []*mob {
	var out []*mob
	for _, en := range h.selectEntities(h.playersRef, from, spec, false, true) {
		out = append(out, en.m)
	}
	return out
}

// selectEntities is EntitySelector.findEntities over the players (withPlayers)
// and the mobs (withMobs): every entity the predicates keep, sorted by the
// selector's sort — nearest for @p, random for @r, arbitrary (a stable
// order) for @a and @e unless sort= says otherwise — and cut to its limit.
func (h *hub) selectEntities(players map[int32]*tracked, from *tracked, spec targetSpec, withPlayers, withMobs bool) []cmdEntity {
	var out []cmdEntity
	switch spec.kind {
	case 0: // a plain name: that online player
		if withPlayers {
			for _, t := range players {
				if t.p.name == spec.name {
					out = append(out, cmdEntity{t: t})
				}
			}
		}
		return out
	case 's':
		if withPlayers && from != nil && h.specMatches(spec, cmdEntity{t: from}, from) {
			out = append(out, cmdEntity{t: from})
		}
		return out
	}
	if withPlayers && !(spec.kind == 'e' && spec.etype != "" && spec.etype != "player") {
		for _, t := range players {
			if h.specMatches(spec, cmdEntity{t: t}, from) {
				out = append(out, cmdEntity{t: t})
			}
		}
	}
	if withMobs && spec.kind == 'e' && spec.etype != "player" {
		for _, m := range h.mobs {
			if m.dying == 0 && h.specMatches(spec, cmdEntity{m: m}, from) {
				out = append(out, cmdEntity{m: m})
			}
		}
	}
	sortBy := spec.sortBy
	if sortBy == "" {
		switch {
		case spec.kind == 'p' || spec.nearest:
			sortBy = "nearest"
		case spec.kind == 'r':
			sortBy = "random"
		default:
			sortBy = "arbitrary"
		}
	}
	ox, oy, oz, _, hasFrom := spec.originFrom(from)
	dist := func(en cmdEntity) float64 {
		x, y, z := en.pos()
		return (x-ox)*(x-ox) + (y-oy)*(y-oy) + (z-oz)*(z-oz)
	}
	arbitrary := func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.t != nil) != (b.t != nil) {
			return a.t != nil // players first, as the player list comes first
		}
		if a.t != nil {
			return a.t.p.name < b.t.p.name
		}
		return a.m.eid < b.m.eid
	}
	switch {
	case sortBy == "nearest" && hasFrom:
		sort.SliceStable(out, func(i, j int) bool {
			if di, dj := dist(out[i]), dist(out[j]); di != dj {
				return di < dj
			}
			return arbitrary(i, j)
		})
	case sortBy == "furthest" && hasFrom:
		sort.SliceStable(out, func(i, j int) bool {
			if di, dj := dist(out[i]), dist(out[j]); di != dj {
				return di > dj
			}
			return arbitrary(i, j)
		})
	case sortBy == "random":
		sort.SliceStable(out, arbitrary)
		h.rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	default:
		sort.SliceStable(out, arbitrary)
	}
	if spec.limit > 0 && len(out) > spec.limit {
		out = out[:spec.limit]
	}
	return out
}

// originFrom is where the selector measures from: the caller's position,
// with any of x=, y=, z= put in its place. ok is false when there is no
// caller and no complete origin.
func (spec targetSpec) originFrom(from *tracked) (x, y, z float64, dim int, ok bool) {
	if from != nil {
		x, y, z, dim, ok = from.x, from.y, from.z, from.dim, true
	}
	c := [3]*float64{&x, &y, &z}
	all := true
	for i := range c {
		if spec.hasOrigin[i] {
			*c[i] = spec.origin[i]
		} else {
			all = false
		}
	}
	return x, y, z, dim, ok || all
}

// specMatches applies the predicates to one entity.
func (h *hub) specMatches(spec targetSpec, en cmdEntity, from *tracked) bool {
	x, y, z := en.pos()
	var tags map[string]bool
	var name, owner, etype string
	var yaw, pitch float32
	var w, ht float64
	if t := en.t; t != nil {
		if t.dead {
			return false
		}
		tags, name, owner, etype = t.tags, t.p.name, t.p.name, "player"
		yaw, pitch, w, ht = t.yaw, t.pitch, 2*t.halfWidth(), 1.8*t.scale()
		if spec.hasLevel && (int64(t.xpLevel) < spec.level[0] || int64(t.xpLevel) > spec.level[1]) {
			return false
		}
		if len(spec.gamemodes) > 0 && !containsInt(spec.gamemodes, t.gamemode) {
			return false
		}
		if containsInt(spec.notGmodes, t.gamemode) {
			return false
		}
	} else {
		m := en.m
		tags, name, owner, etype = m.tags, en.name(), uuidString(m.uuid), entityTypeName(m.etype)
		b := m.box()
		yaw, w, ht = m.yaw, b.w, b.h
		if spec.hasLevel || len(spec.gamemodes) > 0 {
			return false // level= and gamemode= select players only
		}
	}
	if spec.etype != "" && etype != spec.etype {
		return false
	}
	if spec.notEtype != "" && etype == spec.notEtype {
		return false
	}
	if spec.name != "" && name != spec.name {
		return false
	}
	for _, n := range spec.notNames {
		if name == n {
			return false
		}
	}
	if !spec.tagsMatch(tags) {
		return false
	}
	ox, oy, oz, odim, hasOrigin := spec.originFrom(from)
	if spec.hasDist || spec.hasDelta {
		if !hasOrigin || en.dim() != odim {
			return false
		}
	}
	if spec.hasDist {
		d := math.Sqrt((x-ox)*(x-ox) + (y-oy)*(y-oy) + (z-oz)*(z-oz))
		if d < spec.minDist || (spec.maxDist > 0 && d > spec.maxDist) {
			return false
		}
	}
	if spec.hasDelta { // the entity's box meets the volume [origin, origin+delta+1)
		lo := [3]float64{ox, oy, oz}
		hi := [3]float64{ox, oy, oz}
		for i, d := range spec.delta {
			if d < 0 {
				lo[i] += d
			} else {
				hi[i] += d
			}
			hi[i]++
		}
		if x+w/2 <= lo[0] || x-w/2 >= hi[0] || y+ht <= lo[1] || y >= hi[1] || z+w/2 <= lo[2] || z-w/2 >= hi[2] {
			return false
		}
	}
	if spec.hasXRot && !inWrappedRange(float64(pitch), spec.xRot) {
		return false
	}
	if spec.hasYRot && !inWrappedRange(float64(yaw), spec.yRot) {
		return false
	}
	for obj, rng := range spec.scores {
		if h.sb == nil {
			return false
		}
		v, ok := h.sb.Scores[owner][obj]
		if !ok || int64(v) < rng[0] || int64(v) > rng[1] {
			return false
		}
	}
	if len(spec.teams) > 0 || len(spec.notTeams) > 0 {
		team := h.teamOf(owner)
		for _, want := range spec.teams {
			if team != want {
				return false
			}
		}
		for _, not := range spec.notTeams {
			if team == not {
				return false
			}
		}
	}
	return true
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// inWrappedRange is the rotation predicates' test: angles wrapped to
// [-180, 180), and a range whose low end is above its high end wraps round.
func inWrappedRange(a float64, r [2]float64) bool {
	wrap := func(v float64) float64 {
		v = math.Mod(v+180, 360)
		if v < 0 {
			v += 360
		}
		return v - 180
	}
	a, lo, hi := wrap(a), r[0], r[1]
	if lo > hi {
		return a >= lo || a <= hi
	}
	return a >= lo && a <= hi
}

// parseSelectorIntRange is MinMaxBounds.Ints: "a..b", "a..", "..b" or "a".
func parseSelectorIntRange(s string) (int64, int64, bool) {
	lo, hi := int64(math.MinInt64), int64(math.MaxInt64)
	a, b, ranged := strings.Cut(s, "..")
	if !ranged {
		b = a
	}
	if a == "" && b == "" {
		return 0, 0, false
	}
	if a != "" {
		v, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		lo = v
	}
	if b != "" {
		v, err := strconv.ParseInt(b, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		hi = v
	}
	return lo, hi, lo <= hi
}

// parseSelectorFloatRange is MinMaxBounds.Doubles for the rotation predicates.
func parseSelectorFloatRange(s string) (float64, float64, bool) {
	lo, hi := math.Inf(-1), math.Inf(1)
	a, b, ranged := strings.Cut(s, "..")
	if !ranged {
		b = a
	}
	if a == "" && b == "" {
		return 0, 0, false
	}
	if a != "" {
		v, err := strconv.ParseFloat(a, 64)
		if err != nil {
			return 0, 0, false
		}
		lo = v
	}
	if b != "" {
		v, err := strconv.ParseFloat(b, 64)
		if err != nil {
			return 0, 0, false
		}
		hi = v
	}
	return lo, hi, true
}

// entityTypeName reverses the generated entity-id table (the selector's
// type= predicate is written by name).
var entityTypeNames = func() map[int]string {
	out := make(map[int]string, len(entityByName))
	for name, id := range entityByName {
		out[id] = name
	}
	return out
}()

func entityTypeName(etype int) string { return entityTypeNames[etype] }

func playerDist(t, from *tracked) float64 {
	dx, dy, dz := t.x-from.x, t.y-from.y, t.z-from.z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func mobDist(m *mob, from *tracked) float64 {
	dx, dy, dz := m.x-from.x, m.y-from.y, m.z-from.z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// ---- coordinate arguments ---------------------------------------------------

// parseCoord reads one coordinate: an absolute number, or `~`/`~offset`
// relative to base.
func parseCoord(arg string, base float64) (float64, bool) {
	if strings.HasPrefix(arg, "~") {
		if arg == "~" {
			return base, true
		}
		d, err := strconv.ParseFloat(arg[1:], 64)
		if err != nil {
			return 0, false
		}
		return base + d, true
	}
	v, err := strconv.ParseFloat(arg, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parsePosition reads an x y z triple. All three may be `~`-relative; all
// three may instead be `^`-local, which is measured along the way the caller
// is facing (left, up, forward).
func parsePosition(args []string, x, y, z float64, yaw, pitch float32) (float64, float64, float64, bool) {
	if len(args) < 3 {
		return 0, 0, 0, false
	}
	locals := 0
	for _, a := range args[:3] {
		if strings.HasPrefix(a, "^") {
			locals++
		}
	}
	if locals == 3 {
		var d [3]float64
		for i, a := range args[:3] {
			if a == "^" {
				continue
			}
			v, err := strconv.ParseFloat(a[1:], 64)
			if err != nil {
				return 0, 0, 0, false
			}
			d[i] = v
		}
		fx, fy, fz := lookVector(yaw, pitch)
		// The local basis: forward, up (forward pitched a quarter turn back)
		// and left (their cross product), exactly as vanilla builds it.
		ux, uy, uz := lookVector(yaw, pitch-90)
		lx, ly, lz := uy*fz-uz*fy, uz*fx-ux*fz, ux*fy-uy*fx
		return x + lx*d[0] + ux*d[1] + fx*d[2],
			y + ly*d[0] + uy*d[1] + fy*d[2],
			z + lz*d[0] + uz*d[1] + fz*d[2], true
	}
	if locals != 0 {
		return 0, 0, 0, false // vanilla refuses a mix of ^ and ~
	}
	nx, ok1 := parseCoord(args[0], x)
	ny, ok2 := parseCoord(args[1], y)
	nz, ok3 := parseCoord(args[2], z)
	return nx, ny, nz, ok1 && ok2 && ok3
}

// commandTargets resolves a command's target argument to players.
func (h *hub) commandTargets(players map[int32]*tracked, by int32, arg string) []*tracked {
	spec, ok := parseTargetSpec(arg)
	if !ok {
		return nil
	}
	return h.selectPlayers(players, players[by], spec)
}

// commandMobs resolves a command's target argument to mobs (only @e does).
func (h *hub) commandMobs(players map[int32]*tracked, by int32, arg string) []*mob {
	spec, ok := parseTargetSpec(arg)
	if !ok || !spec.selectsEntities() {
		return nil
	}
	return h.selectMobs(players[by], spec)
}

// evTeleportTo is /tp <player|selector>: the hub knows where everyone is.
type evTeleportTo struct {
	eid    int32
	target string
}

func (evTeleportTo) isHubEvent() {}

// onTeleportTo moves the caller to the first entity their selector picks.
func (h *hub) onTeleportTo(players map[int32]*tracked, e evTeleportTo) {
	me := players[e.eid]
	if me == nil {
		return
	}
	x, y, z, dim, ok := 0.0, 0.0, 0.0, 0, false
	if ts := h.commandTargets(players, e.eid, e.target); len(ts) > 0 && ts[0] != me {
		x, y, z, dim, ok = ts[0].x, ts[0].y, ts[0].z, ts[0].dim, true
	} else if ms := h.commandMobs(players, e.eid, e.target); len(ms) > 0 {
		x, y, z, dim, ok = ms[0].x, ms[0].y, ms[0].z, ms[0].dim, true
	}
	if !ok {
		me.p.tell("No entity was found.")
		return
	}
	if dim != me.dim {
		// TeleportCommand moves the caller into the entity's level.
		me.p.pendingFrom = dimPos{}
		me.p.pendingDest = blockPos{floorInt(x), floorInt(y), floorInt(z) - 1}
		me.p.pendingDestOK = true
		me.p.pendingDim.Store(int32(dim))
		h.cmdSuccess(players, me.p, "Teleported.", true)
		return
	}
	me.x, me.y, me.z = x, y, z
	me.p.x, me.p.y, me.p.z = x, y, z
	me.p.setHubPos(x, z)
	me.p.sendEv(teleportEv(x, y, z, me.yaw, me.pitch))
	h.cmdSuccess(players, me.p, "Teleported.", true)
}

// evTeleportTargets is /tp with targets: send them to an entity or a place.
type evTeleportTargets struct {
	by         int32
	targets    string
	dest       string // an entity selector, or "" for the position below
	x, y, z    float64
	rot        bool
	yaw, pitch float32
	// facing: look at a point (face) or at an entity (faceEntity, its eyes
	// or its feet) from the destination (TeleportCommand LookAt).
	face       bool
	fx, fy, fz float64
	faceEntity string
	faceEyes   bool
}

func (evTeleportTargets) isHubEvent() {}

func (h *hub) onTeleportTargets(players map[int32]*tracked, e evTeleportTargets) {
	me := players[e.by]
	if me == nil {
		return
	}
	ts := h.commandTargets(players, e.by, e.targets)
	ms := h.commandMobs(players, e.by, e.targets)
	if len(ts)+len(ms) == 0 {
		me.p.tell("No entity was found")
		return
	}
	x, y, z, dim, where := e.x, e.y, e.z, me.dim, ""
	if e.dest != "" {
		if d := h.commandTargets(players, e.by, e.dest); len(d) > 0 {
			x, y, z, dim, where = d[0].x, d[0].y, d[0].z, d[0].dim, d[0].p.name
		} else if d := h.commandMobs(players, e.by, e.dest); len(d) > 0 {
			x, y, z, dim, where = d[0].x, d[0].y, d[0].z, d[0].dim, mobDisplayName(d[0].etype)
		} else {
			me.p.tell("No entity was found")
			return
		}
	}
	if e.faceEntity != "" {
		fs := h.commandTargets(players, e.by, e.faceEntity)
		ms := h.commandMobs(players, e.by, e.faceEntity)
		switch {
		case len(fs) > 0:
			e.face, e.fx, e.fy, e.fz = true, fs[0].x, fs[0].y, fs[0].z
			if e.faceEyes {
				e.fy += fs[0].eyeHeight()
			}
		case len(ms) > 0:
			e.face, e.fx, e.fy, e.fz = true, ms[0].x, ms[0].y, ms[0].z
			if e.faceEyes {
				e.fy += ms[0].box().h * 0.85
			}
		default:
			me.p.tell("No entity was found")
			return
		}
	}
	moved, name := 0, ""
	for _, t := range ts {
		if e.rot {
			t.yaw, t.pitch = e.yaw, e.pitch
		}
		if e.face { // LookAt from the eyes at the destination
			yaw, pitch := lookAngles(x, y+t.eyeHeight(), z, e.fx, e.fy, e.fz)
			t.yaw, t.pitch = float32(yaw), float32(pitch)
		}
		if t.dim != dim {
			// TeleportCommand moves the target into the destination's level:
			// the connection's dimension switch lands them on the spot.
			t.p.pendingFrom = dimPos{}
			t.p.pendingDest = blockPos{floorInt(x), floorInt(y), floorInt(z) - 1}
			t.p.pendingDestOK = true
			t.p.pendingDim.Store(int32(dim))
			moved, name = moved+1, t.p.name
			continue
		}
		h.teleportPlayer(players, t, x, y, z)
		moved, name = moved+1, t.p.name
	}
	for _, m := range ms {
		if m.dim != dim {
			// TeleportCommand: the mob changes level, as a portal's
			// traveller does — gone from the old dimension's viewers, its
			// seat and its quarry left behind.
			if m.dying > 0 || m == h.dragon {
				continue
			}
			h.mobChangeDimension(players, m, dim, x, y, z)
			moved, name = moved+1, mobDisplayName(m.etype)
			continue
		}
		m.x, m.y, m.z = x, y, z
		if e.rot {
			m.yaw = e.yaw
		}
		if e.face {
			yaw, _ := lookAngles(x, y+m.box().h*0.85, z, e.fx, e.fy, e.fz)
			m.yaw = float32(yaw)
		}
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, false))
		moved, name = moved+1, mobDisplayName(m.etype)
	}
	if moved == 0 {
		me.p.tell("That destination is in another dimension")
		return
	}
	if moved > 1 {
		name = fmt.Sprintf("%d entities", moved)
	}
	if where != "" {
		h.cmdSuccess(players, me.p, fmt.Sprintf("Teleported %s to %s", name, where), true)
	} else {
		h.cmdSuccess(players, me.p, fmt.Sprintf("Teleported %s to %f, %f, %f", name, x, y, z), true)
	}
}
