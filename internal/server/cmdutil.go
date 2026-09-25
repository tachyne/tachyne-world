package server

import (
	"math"
	"strconv"
	"strings"
)

// Shared pieces of the command batch that came after the first two
// (advancement, attribute, recipe, tag, ride, damage, spreadplayers,
// forceload, setworldspawn, defaultgamemode, random, swing, teammsg): the
// response tally vanilla's newer commands report through, a resolved entity
// that may be a player or a mob, and Java's number formatting for the
// feedback lines.

// cmdTally is CommandResponseTracker: each target is tracked with how much the
// command did to it, and the feedback names the target when exactly one was
// touched ("…to Steve") and counts them otherwise ("…to 3 players").
type cmdTally struct {
	total, count, nonZero int
	only, onlyNonZero     string
}

func (c *cmdTally) track(name string, v int) {
	c.total += v
	if c.count++; c.count == 1 {
		c.only = name
	} else {
		c.only = ""
	}
	if v != 0 {
		if c.nonZero++; c.nonZero == 1 {
			c.onlyNonZero = name
		} else {
			c.onlyNonZero = ""
		}
	}
}

// single is the one target the message names, "" when it counts instead.
// nonZero picks vanilla's NON_ZERO element type (success lines) over ANY
// (failure lines).
func (c *cmdTally) single(nonZero bool) string {
	if nonZero {
		return c.onlyNonZero
	}
	return c.only
}

// n is how many targets a counting message counts.
func (c *cmdTally) n(nonZero bool) int {
	if nonZero {
		return c.nonZero
	}
	return c.count
}

// cmdEntity is one entity a command resolved: a player or a mob, never both.
type cmdEntity struct {
	t *tracked
	m *mob
}

// name is the entity's display name as feedback prints it.
func (e cmdEntity) name() string {
	if e.t != nil {
		return e.t.p.name
	}
	if e.m.customName != "" {
		return e.m.customName
	}
	return mobDisplayName(e.m.etype)
}

func (e cmdEntity) dim() int {
	if e.t != nil {
		return e.t.dim
	}
	return e.m.dim
}

func (e cmdEntity) pos() (float64, float64, float64) {
	if e.t != nil {
		return e.t.x, e.t.y, e.t.z
	}
	return e.m.x, e.m.y, e.m.z
}

func (e cmdEntity) eid() int32 {
	if e.t != nil {
		return e.t.p.eid
	}
	return e.m.eid
}

// living is the entity's LivingEntity half — tags, attributes, effects.
func (e cmdEntity) living() *living {
	if e.t != nil {
		return &e.t.living
	}
	return &e.m.living
}

// commandEntities resolves an EntityArgument: the players the selector picks,
// then (for @e) the mobs.
func (h *hub) commandEntities(players map[int32]*tracked, by int32, arg string) []cmdEntity {
	spec, ok := parseTargetSpec(arg)
	if !ok {
		return nil
	}
	// One selection over players and mobs together, so a sort and a limit
	// apply across both (@e[limit=1,sort=nearest] is one entity).
	return h.selectEntities(players, players[by], spec, true, spec.selectsEntities())
}

// jDouble formats a double the way Java's Double.toString does, which is how
// vanilla's feedback prints one: always a decimal point ("20.0"), and
// scientific notation outside [10^-3, 10^7).
func jDouble(v float64) string {
	a := math.Abs(v)
	if a != 0 && (a < 1e-3 || a >= 1e7) {
		s := strconv.FormatFloat(v, 'E', -1, 64) // 1.0E-4 is Java's spelling
		mant, exp, _ := strings.Cut(s, "E")
		if !strings.Contains(mant, ".") {
			mant += ".0"
		}
		e, _ := strconv.Atoi(exp) // "E+07" → 7
		return mant + "E" + strconv.Itoa(e)
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// jFloat is jDouble for a float argument (Float.toString).
func jFloat(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', -1, 32)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// nsID puts a bare identifier in the minecraft namespace, as vanilla's
// Identifier argument does.
func nsID(s string) string {
	if strings.Contains(s, ":") {
		return s
	}
	return "minecraft:" + s
}

// cmdTeller returns a function that tells the command's caller something.
func cmdTeller(players map[int32]*tracked, by int32) func(string) {
	caller := players[by]
	return func(msg string) {
		if caller != nil {
			caller.p.trySendEv(chatEv(msg))
		}
	}
}
