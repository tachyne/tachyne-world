package server

// The scoreboard's automatic criteria beyond the built-in ones: the stat
// family (ObjectiveCriteria for a Stat — "minecraft.custom:minecraft.jump",
// "minecraft.mined:minecraft.stone", …), which every awardStat adds to, and
// the per-colour team kill pair (teamkill.<colour>, killedByTeam.<colour>)
// ServerPlayer.handleTeamKill feeds. Also the team collision rule
// (EntitySelector.pushableBy), enforced where mobs push each other.

import (
	"sort"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// statTypeNames are the stat_type registry by id (attach Stat* order).
var statTypeNames = map[string]int32{
	"mined": attachproto.StatMined, "crafted": attachproto.StatCrafted, "used": attachproto.StatUsed,
	"broken": attachproto.StatBroken, "picked_up": attachproto.StatPickedUp, "dropped": attachproto.StatDropped,
	"killed": attachproto.StatKilled, "killed_by": attachproto.StatKilledBy, "custom": attachproto.StatCustom,
}

// mcDotted is Identifier.bySeparator(s, '.') for the minecraft namespace:
// "minecraft.jump" and "jump" are both minecraft:jump.
func mcDotted(s string) (string, bool) {
	ns, path, dotted := strings.Cut(s, ".")
	if !dotted {
		return s, s != ""
	}
	if ns != "minecraft" {
		return "", false
	}
	return path, path != ""
}

// parseStatCriteria is ObjectiveCriteria.byName's stat branch: "<type>:<value>",
// each an identifier written with '.' for ':'. It returns the stat and the
// criteria's canonical name.
func parseStatCriteria(c string) (statKey, string, bool) {
	typ, val, ok := strings.Cut(c, ":")
	if !ok {
		return statKey{}, "", false
	}
	tp, ok1 := mcDotted(typ)
	vp, ok2 := mcDotted(val)
	if !ok1 || !ok2 {
		return statKey{}, "", false
	}
	t, ok := statTypeNames[tp]
	if !ok {
		return statKey{}, "", false
	}
	var key int32
	switch t {
	case attachproto.StatCustom:
		id, ok := customStatID[vp]
		if !ok {
			return statKey{}, "", false
		}
		key = id
	case attachproto.StatMined:
		lo, _, ok := worldgen.BlockRangeOK(vp)
		if !ok {
			return statKey{}, "", false
		}
		reg, ok := statBlockReg(lo)
		if !ok {
			return statKey{}, "", false
		}
		key = reg
	case attachproto.StatKilled, attachproto.StatKilledBy:
		id, ok := entityByName[vp]
		if !ok {
			return statKey{}, "", false
		}
		key = int32(id)
	default: // crafted, used, broken, picked_up, dropped: an item
		id, ok := itemByName[vp]
		if !ok {
			return statKey{}, "", false
		}
		key = id
	}
	return statKey{t, key}, "minecraft." + tp + ":minecraft." + vp, true
}

// teamColorByID are TeamColor's serialized names by ChatFormatting ordinal.
var teamColorByID = func() map[int32]string {
	m := map[int32]string{}
	for name, c := range sbTeamColors {
		if c >= 0 {
			m[c] = name
		}
	}
	return m
}()

// teamKillCriteria reports whether c is a teamkill.<colour> or
// killedByTeam.<colour> criteria.
func teamKillCriteria(c string) bool {
	for _, prefix := range []string{"teamkill.", "killedByTeam."} {
		if col, ok := strings.CutPrefix(c, prefix); ok {
			v, known := sbTeamColors[col]
			return known && v >= 0
		}
	}
	return false
}

// sbCriteriaName validates an objective criteria and returns its canonical
// name: a built-in one, a team kill one, or a stat.
func sbCriteriaName(c string) (string, bool) {
	if sbValidCriteria[c] || teamKillCriteria(c) {
		return c, true
	}
	if _, canon, ok := parseStatCriteria(c); ok {
		return canon, true
	}
	return "", false
}

// sbStat is the scoreboard half of ServerPlayer.awardStat: every objective
// on the stat adds the same amount to the player's score.
func (h *hub) sbStat(t *tracked, k statKey, n int32) {
	if h.sb == nil || len(h.sb.Objectives) == 0 || t == nil || t.p == nil {
		return
	}
	for _, name := range h.sbStatObjectives(k) {
		h.sbSetScore(h.playersRef, t.p.name, name, h.sb.Scores[t.p.name][name]+n)
	}
}

// sbStatReset is resetStat's: the stat's objectives go back to 0.
func (h *hub) sbStatReset(t *tracked, k statKey) {
	if h.sb == nil || len(h.sb.Objectives) == 0 || t == nil || t.p == nil {
		return
	}
	for _, name := range h.sbStatObjectives(k) {
		h.sbSetScore(h.playersRef, t.p.name, name, 0)
	}
}

// sbStatObjectives names the objectives whose criteria is the stat, in
// name order.
func (h *hub) sbStatObjectives(k statKey) []string {
	var out []string
	for name, o := range h.sb.Objectives {
		if sk, ok := o.statKey(); ok && sk == k {
			out = append(out, name)
		}
	}
	if len(out) > 1 {
		sort.Strings(out)
	}
	return out
}

// statKey is the stat an objective's criteria names, parsed once (the
// criteria never changes after the objective is made).
func (o *sbObjective) statKey() (statKey, bool) {
	if o.statState == 0 {
		o.statState = -1
		if strings.Contains(o.Criteria, ":") {
			if sk, _, ok := parseStatCriteria(o.Criteria); ok {
				o.stat, o.statState = sk, 1
			}
		}
	}
	return o.stat, o.statState == 1
}

// sbTeamKill is ServerPlayer.handleTeamKill, both halves: the killer's
// teamkill.<victim's team colour> and the victim's killedByTeam.<killer's
// team colour>, each only when that team has a colour.
func (h *hub) sbTeamKill(players map[int32]*tracked, killer, victim string) {
	if h.sb == nil || len(h.sb.Objectives) == 0 || len(h.sb.Teams) == 0 {
		return
	}
	colour := func(name string) (string, bool) {
		tn := h.teamOf(name)
		if tn == "" {
			return "", false
		}
		c, ok := teamColorByID[h.sb.Teams[tn].Color]
		return c, ok
	}
	if c, ok := colour(victim); ok {
		h.sbCriteria(players, "teamkill."+c, killer, 1, false)
	}
	if c, ok := colour(killer); ok {
		h.sbCriteria(players, "killedByTeam."+c, victim, 1, false)
	}
}

// Team.CollisionRule, by its stored id.
const (
	collAlways         = 0
	collNever          = 1
	collPushOtherTeams = 2
	collPushOwnTeam    = 3
)

// teamPushAllowed is EntitySelector.pushableBy's team test: may an entity
// on team own push one on team their ("" = no team)?
func (h *hub) teamPushAllowed(own, their string) bool {
	rule := func(tn string) int32 {
		if tn == "" || h.sb == nil {
			return collAlways
		}
		if t := h.sb.Teams[tn]; t != nil {
			return t.Collision
		}
		return collAlways
	}
	or, tr := rule(own), rule(their)
	if or == collNever || tr == collNever {
		return false
	}
	same := own != "" && own == their
	if (or == collPushOwnTeam || tr == collPushOwnTeam) && same {
		return false
	}
	return or != collPushOtherTeams && tr != collPushOtherTeams || same
}

// sbMemberTeams indexes team membership (member → team) for a pass that
// asks many times; nil when no team has members.
func (h *hub) sbMemberTeams() map[string]string {
	if h.sb == nil {
		return nil
	}
	var out map[string]string
	for tn, t := range h.sb.Teams {
		for m := range t.Members {
			if out == nil {
				out = map[string]string{}
			}
			out[m] = tn
		}
	}
	return out
}
