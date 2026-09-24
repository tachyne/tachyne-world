package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /spreadplayers <x> <z> <spreadDistance> <maxRange> [under <maxHeight>]
// <respectTeams> <targets> (SpreadPlayersCommand): random columns inside the
// square of maxRange around the centre, pushed apart until no two are closer
// than spreadDistance, each on a safe surface — the top block that has two
// air blocks above it, is no fluid and blocks motion. With respectTeams a
// team shares one spot. The entities are the caller's dimension's; a target
// elsewhere is left where it is.

type evSpreadCmd struct {
	by           int32
	cx, cz       float64
	spread, maxR float64
	maxHeight    int
	hasMaxHeight bool
	teams        bool
	target       string
	dim          int
}

func (evSpreadCmd) isHubEvent() {}

const spreadUsage = "Usage: /spreadplayers <x> <z> <spreadDistance> <maxRange> [under <maxHeight>] <respectTeams> <targets>"

// parseVec2Coord is one Vec2Argument coordinate: `~` relative to the caller,
// and a whole absolute number centred on its block (+0.5), as vanilla's
// centreCorrect does.
func parseVec2Coord(arg string, base float64) (float64, bool) {
	v, ok := parseCoord(arg, base)
	if ok && !strings.HasPrefix(arg, "~") && !strings.Contains(arg, ".") {
		v += 0.5
	}
	return v, ok
}

func (s *Server) cmdSpreadPlayers(p *player, args []string) {
	if !s.isOp(p.name) { // SpreadPlayersCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 6 && len(args) != 8 {
		p.tell(spreadUsage)
		return
	}
	e := evSpreadCmd{by: p.eid, dim: p.dim}
	var ok1, ok2 bool
	e.cx, ok1 = parseVec2Coord(args[0], p.x)
	e.cz, ok2 = parseVec2Coord(args[1], p.z)
	spread, err1 := strconv.ParseFloat(args[2], 32)
	maxR, err2 := strconv.ParseFloat(args[3], 32)
	if !ok1 || !ok2 || err1 != nil || err2 != nil {
		p.tell(spreadUsage)
		return
	}
	if spread < 0 {
		p.tell(fmt.Sprintf("Float must not be less than 0.0: found %s", jFloat(float32(spread))))
		return
	}
	if maxR < spread+1 { // maxRange is floatArg(spreadDistance + 1)
		p.tell(fmt.Sprintf("Float must not be less than %s: found %s", jFloat(float32(spread+1)), jFloat(float32(maxR))))
		return
	}
	e.spread, e.maxR = spread, maxR
	rest := args[4:]
	if len(rest) == 4 {
		if rest[0] != "under" {
			p.tell(spreadUsage)
			return
		}
		n, err := strconv.Atoi(rest[1])
		if err != nil {
			p.tell(fmt.Sprintf("Invalid integer '%s'", rest[1]))
			return
		}
		e.maxHeight, e.hasMaxHeight = n, true
		rest = rest[2:]
	}
	if rest[0] != "true" && rest[0] != "false" {
		p.tell(spreadUsage)
		return
	}
	e.teams, e.target = rest[0] == "true", rest[1]
	s.hub.post(e)
}

// spreadPos is SpreadPlayersCommand.Position.
type spreadPos struct{ x, z float64 }

func (p spreadPos) dist(o spreadPos) float64 { return math.Hypot(p.x-o.x, p.z-o.z) }

func (p *spreadPos) randomize(h *hub, minX, minZ, maxX, maxZ float64) {
	p.x = minX + h.rng.Float64()*(maxX-minX)
	p.z = minZ + h.rng.Float64()*(maxZ-minZ)
}

func (p *spreadPos) clamp(minX, minZ, maxX, maxZ float64) bool {
	changed := false
	switch {
	case p.x < minX:
		p.x, changed = minX, true
	case p.x > maxX:
		p.x, changed = maxX, true
	}
	switch {
	case p.z < minZ:
		p.z, changed = minZ, true
	case p.z > maxZ:
		p.z, changed = maxZ, true
	}
	return changed
}

// spreadSpawnY is Position.getSpawnY: down from maxHeight+1, the first block that is
// not air with two air blocks above it stands the entity one higher.
func (h *hub) spreadSpawnY(dim int, p spreadPos, maxHeight int) int {
	w := h.worldFor(dim)
	x, z := floorInt(p.x), floorInt(p.z)
	y := maxHeight + 1
	air2 := w.At(x, y, z) == worldgen.Air
	y--
	air1 := w.At(x, y, z) == worldgen.Air
	for y > worldgen.MinY {
		y--
		cur := w.At(x, y, z) == worldgen.Air
		if !cur && air1 && air2 {
			return y + 1
		}
		air2, air1 = air1, cur
	}
	return maxHeight + 1
}

// spreadSafe is Position.isSafe: below the spawn height, not a fluid, and a
// block that stops movement (#entities_can_teleport_to is #blocks_motion,
// which the engine answers with its solid-state table).
func (h *hub) spreadSafe(dim int, p spreadPos, maxHeight int) bool {
	y := h.spreadSpawnY(dim, p, maxHeight) - 1
	st := h.worldFor(dim).At(floorInt(p.x), y, floorInt(p.z))
	return y < maxHeight && !worldgen.IsFluid(st) && worldgen.IsSolid(st)
}

// teamOf is the scoreboard team a player is on, "" for none.
func (h *hub) teamOf(name string) string {
	if h.sb == nil {
		return ""
	}
	for tn, t := range h.sb.Teams {
		if t.Members[name] {
			return tn
		}
	}
	return ""
}

// applySpreadCommand runs /spreadplayers on the hub.
func (h *hub) applySpreadCommand(players map[int32]*tracked, e evSpreadCmd) {
	tell := cmdTeller(players, e.by)
	var ents []cmdEntity
	for _, en := range h.commandEntities(players, e.by, e.target) {
		if en.dim() == e.dim {
			ents = append(ents, en)
		}
	}
	if len(ents) == 0 {
		tell("No entity was found")
		return
	}
	maxHeight := h.worldFor(e.dim).Ceiling() - 1
	if e.hasMaxHeight {
		if e.maxHeight < worldgen.MinY {
			tell(fmt.Sprintf("Invalid maxHeight %d; expected higher than world minimum %d", e.maxHeight, worldgen.MinY))
			return
		}
		maxHeight = e.maxHeight
	}
	// A team shares a spot; a mob, like a player on no team, is its own.
	groupOf := func(en cmdEntity) string {
		if en.t != nil {
			return h.teamOf(en.t.p.name)
		}
		return ""
	}
	n := len(ents)
	if e.teams {
		seen := map[string]bool{}
		for _, en := range ents {
			seen[groupOf(en)] = true
		}
		n = len(seen)
	}
	minX, minZ, maxX, maxZ := e.cx-e.maxR, e.cz-e.maxR, e.cx+e.maxR, e.cz+e.maxR
	pos := make([]spreadPos, n)
	for i := range pos {
		pos[i].randomize(h, minX, minZ, maxX, maxZ)
	}
	what := "entity/entities"
	if e.teams {
		what = "team(s)"
	}
	if minDist, ok := h.spreadPositions(e.dim, pos, e.spread, minX, minZ, maxX, maxZ, maxHeight); !ok {
		tell(fmt.Sprintf("Could not spread %d %s around %s, %s (too many entities for space - try using spread of at most %.2f)",
			n, what, jFloat(float32(e.cx)), jFloat(float32(e.cz)), minDist))
		return
	}
	// setPlayerPositions: each entity (or team) takes the next spot.
	avg, next := 0.0, 0
	byTeam := map[string]int{}
	for _, en := range ents {
		i := next
		if e.teams {
			g := groupOf(en)
			if j, ok := byTeam[g]; ok {
				i = j
			} else {
				byTeam[g] = next
				next++
			}
		} else {
			next++
		}
		p := pos[i]
		x, z := float64(floorInt(p.x))+0.5, float64(floorInt(p.z))+0.5
		y := float64(h.spreadSpawnY(e.dim, p, maxHeight))
		if t := en.t; t != nil {
			h.teleportPlayer(players, t, x, y, z)
			t.p.setHubPos(x, z)
		} else {
			m := en.m
			m.x, m.y, m.z = x, y, z
			m.sx, m.sy, m.sz = x, y, z
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, x, y, z, m.yaw, 0, true))
		}
		closest := math.MaxFloat64
		for j := range pos {
			if j != i {
				closest = math.Min(closest, p.dist(pos[j]))
			}
		}
		avg += closest
	}
	if len(ents) < 2 {
		avg = 0
	} else {
		avg /= float64(len(ents))
	}
	tell(fmt.Sprintf("Spread %d %s around %s, %s with an average distance of %.2f block(s) apart",
		n, what, jFloat(float32(e.cx)), jFloat(float32(e.cz)), avg))
}

// spreadPositions is the command's spreadPositions: push crowded spots apart, clamp them
// into the square, and re-roll unsafe ones, for up to 10000 rounds. Reports
// the closest pair's distance and whether it settled.
func (h *hub) spreadPositions(dim int, pos []spreadPos, spread, minX, minZ, maxX, maxZ float64, maxHeight int) (float64, bool) {
	collisions := true
	minDist := math.MaxFloat32
	iter := 0
	for ; iter < 10000 && collisions; iter++ {
		collisions = false
		minDist = math.MaxFloat32
		for i := range pos {
			p := &pos[i]
			near := 0
			var avg spreadPos
			for j := range pos {
				if i == j {
					continue
				}
				d := p.dist(pos[j])
				minDist = math.Min(d, minDist)
				if d < spread {
					near++
					avg.x += pos[j].x - p.x
					avg.z += pos[j].z - p.z
				}
			}
			if near > 0 {
				avg.x /= float64(near)
				avg.z /= float64(near)
				if l := math.Hypot(avg.x, avg.z); l > 0 {
					p.x -= avg.x / l
					p.z -= avg.z / l
				} else {
					p.randomize(h, minX, minZ, maxX, maxZ)
				}
				collisions = true
			}
			if p.clamp(minX, minZ, maxX, maxZ) {
				collisions = true
			}
		}
		if !collisions {
			for i := range pos {
				if !h.spreadSafe(dim, pos[i], maxHeight) {
					pos[i].randomize(h, minX, minZ, maxX, maxZ)
					collisions = true
				}
			}
		}
	}
	if minDist == math.MaxFloat32 {
		minDist = 0
	}
	return minDist, iter < 10000
}
