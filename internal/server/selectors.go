package server

import (
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
			spec.name = strings.Trim(v, `"`)
		case "limit", "c":
			n, err := strconv.Atoi(v)
			if err != nil {
				return targetSpec{}, false
			}
			spec.limit, spec.nearest = n, true
		case "sort":
			spec.nearest = v == "nearest"
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
	switch spec.kind {
	case 0:
		for _, t := range players {
			if t.p.name == spec.name {
				out = append(out, t)
			}
		}
		return out
	case 's':
		if from != nil && spec.matchesPlayer(from, from) {
			out = append(out, from)
		}
		return out
	}
	if spec.kind == 'e' && spec.etype != "" && spec.etype != "player" {
		return nil // @e[type=zombie] selects no players
	}
	for _, t := range players {
		if spec.matchesPlayer(t, from) {
			out = append(out, t)
		}
	}
	if from != nil && (spec.nearest || spec.kind == 'p') {
		sort.Slice(out, func(i, j int) bool {
			return playerDist(out[i], from) < playerDist(out[j], from)
		})
	} else {
		sort.Slice(out, func(i, j int) bool { return out[i].p.name < out[j].p.name })
	}
	if spec.kind == 'r' && len(out) > 1 {
		i := h.rng.Intn(len(out))
		out[0], out[i] = out[i], out[0]
	}
	if spec.limit > 0 && len(out) > spec.limit {
		out = out[:spec.limit]
	}
	return out
}

// matchesPlayer applies the predicates that apply to a player.
func (spec targetSpec) matchesPlayer(t, from *tracked) bool {
	if t.dead {
		return false
	}
	if spec.name != "" && t.p.name != spec.name {
		return false
	}
	if spec.notEtype == "player" {
		return false
	}
	if spec.hasDist {
		if from == nil || t.dim != from.dim {
			return false
		}
		d := playerDist(t, from)
		if d < spec.minDist || (spec.maxDist > 0 && d > spec.maxDist) {
			return false
		}
	}
	return true
}

// selectMobs resolves a spec against the loaded mobs (only @e does).
func (h *hub) selectMobs(from *tracked, spec targetSpec) []*mob {
	if spec.kind != 'e' {
		return nil
	}
	var out []*mob
	for _, m := range h.mobs {
		if m.dying != 0 {
			continue
		}
		name := entityTypeName(m.etype)
		if spec.etype != "" && name != spec.etype {
			continue
		}
		if spec.notEtype != "" && name == spec.notEtype {
			continue
		}
		if spec.hasDist {
			if from == nil || m.dim != from.dim {
				continue
			}
			d := math.Sqrt((m.x-from.x)*(m.x-from.x) + (m.y-from.y)*(m.y-from.y) + (m.z-from.z)*(m.z-from.z))
			if d < spec.minDist || (spec.maxDist > 0 && d > spec.maxDist) {
				continue
			}
		}
		out = append(out, m)
	}
	if from != nil && spec.nearest {
		sort.Slice(out, func(i, j int) bool { return mobDist(out[i], from) < mobDist(out[j], from) })
	} else {
		sort.Slice(out, func(i, j int) bool { return out[i].eid < out[j].eid })
	}
	if spec.limit > 0 && len(out) > spec.limit {
		out = out[:spec.limit]
	}
	return out
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
		me.p.tell("That entity is in another dimension.")
		return
	}
	me.x, me.y, me.z = x, y, z
	me.p.x, me.p.y, me.p.z = x, y, z
	me.p.setHubPos(x, z)
	me.p.sendEv(teleportEv(x, y, z, me.yaw, me.pitch))
	me.p.tell("Teleported.")
}
