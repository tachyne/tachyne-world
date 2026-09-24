package server

import (
	"fmt"
	"strings"
)

// /advancement grant|revoke <targets> only|from|through|until <advancement>
// [<criterion>] | everything (AdvancementCommands). A grant goes through the
// same award path a trigger does — the toast, the chat announcement and the
// experience reward all follow — and a revoke takes criteria back, which can
// hide nodes again: the client is re-sent its whole tree then, since the
// attach vocabulary has additions but no removals.

type evAdvancementCmd struct {
	by     int32
	grant  bool
	target string
	mode   string // only, from, through, until, everything
	adv    string // full id; "" for everything
	crit   string // only: a single criterion
}

func (evAdvancementCmd) isHubEvent() {}

const advCmdUsage = "Usage: /advancement grant|revoke <targets> only <advancement> [<criterion>] | from|through|until <advancement> | everything"

func (s *Server) cmdAdvancement(p *player, args []string) {
	if !s.isOp(p.name) { // AdvancementCommands: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 3 || (args[0] != "grant" && args[0] != "revoke") {
		p.tell(advCmdUsage)
		return
	}
	e := evAdvancementCmd{by: p.eid, grant: args[0] == "grant", target: args[1], mode: args[2]}
	switch e.mode {
	case "everything":
		if len(args) != 3 {
			p.tell(advCmdUsage)
			return
		}
	case "only", "from", "through", "until":
		if len(args) < 4 || len(args) > 5 || (len(args) == 5 && e.mode != "only") {
			p.tell(advCmdUsage)
			return
		}
		e.adv = nsID(args[3])
		n := advByID[e.adv]
		if n == nil {
			p.tell("Unknown advancement: " + e.adv)
			return
		}
		if len(args) == 5 {
			e.crit = args[4]
			if !containsStr(advCriteriaNames(n), e.crit) {
				p.tell(fmt.Sprintf("The advancement %s does not contain the criterion '%s'", advChatName(n), e.crit))
				return
			}
		}
	default:
		p.tell(advCmdUsage)
		return
	}
	s.hub.post(e)
}

// advChatName is Advancement.name: the title in brackets, or the bare id for
// a node with no display.
func advChatName(n *advNode) string {
	if n.display != nil {
		return "[" + n.display.titleEN + "]"
	}
	return n.id
}

// advCriteriaNames is every criterion the advancement has: the requirement
// names in order, then any criterion the requirements leave out.
func advCriteriaNames(n *advNode) []string {
	var out []string
	for _, group := range n.reqs {
		for _, c := range group {
			if !containsStr(out, c) {
				out = append(out, c)
			}
		}
	}
	for _, c := range n.criteria {
		if !containsStr(out, c.name) {
			out = append(out, c.name)
		}
	}
	return out
}

// advCommandSet is getAdvancements: the named node, plus its ancestors
// (until, through) and its whole subtree (from, through). everything is every
// root taken through.
func advCommandSet(mode, id string) []*advNode {
	var out []*advNode
	var subtree func(n *advNode)
	subtree = func(n *advNode) {
		for _, c := range advChildren[n.id] {
			out = append(out, c)
			subtree(c)
		}
	}
	if mode == "everything" {
		for _, r := range advRoots {
			out = append(out, r)
			subtree(r)
		}
		return out
	}
	n := advByID[id]
	if n == nil {
		return nil
	}
	if mode == "until" || mode == "through" {
		for p := advByID[n.parent]; p != nil; p = advByID[p.parent] {
			out = append(out, p)
		}
	}
	out = append(out, n)
	if mode == "from" || mode == "through" {
		subtree(n)
	}
	return out
}

// revoke takes one criterion back. Reports whether the player had it.
func (s advState) revoke(n *advNode, crit string) bool {
	m := s[n.id]
	if _, ok := m[crit]; !ok {
		return false
	}
	delete(m, crit)
	if len(m) == 0 {
		delete(s, n.id)
	}
	return true
}

// applyAdvancementCommand runs /advancement on the hub.
func (h *hub) applyAdvancementCommand(players map[int32]*tracked, e evAdvancementCmd) {
	tell := cmdTeller(players, e.by)
	okTell := h.cmdOK(players, e.by) // sendSuccess(…, true)
	targets := h.commandTargets(players, e.by, e.target)
	if len(targets) == 0 {
		tell("No player was found")
		return
	}
	verb, prep := "revoke", "from"
	if e.grant {
		verb, prep = "grant", "to"
	}
	var tally cmdTally
	if e.crit != "" {
		n := advByID[e.adv]
		for _, t := range targets {
			var b advCommandBatch
			tally.track(t.p.name, b2i(b.apply(t, e.grant, n, []string{e.crit})))
			b.sync(h, players, t, e.grant)
		}
		name := advChatName(n)
		if tally.total == 0 {
			if who := tally.single(false); who != "" {
				tell(fmt.Sprintf("Couldn't %s criterion '%s' of advancement %s %s %s as they %s", verb, e.crit, name, prep, who, advAlready(e.grant)))
			} else {
				tell(fmt.Sprintf("Couldn't %s criterion '%s' of advancement %s %s %d players as they %s", verb, e.crit, name, prep, tally.n(false), advAlready(e.grant)))
			}
			return
		}
		Verb := advPastTense(e.grant)
		if who := tally.single(true); who != "" {
			okTell(fmt.Sprintf("%s criterion '%s' of advancement %s %s %s", Verb, e.crit, name, prep, who))
		} else {
			okTell(fmt.Sprintf("%s criterion '%s' of advancement %s %s %d players", Verb, e.crit, name, prep, tally.n(true)))
		}
		return
	}
	set := advCommandSet(e.mode, e.adv)
	for _, t := range targets {
		var b advCommandBatch
		for _, n := range set {
			b.apply(t, e.grant, n, nil)
		}
		b.sync(h, players, t, e.grant)
		tally.track(t.p.name, len(b.changed))
	}
	Verb := advPastTense(e.grant)
	if len(set) == 1 {
		name := advChatName(set[0])
		switch who := tally.single(tally.total != 0); {
		case tally.total == 0 && who != "":
			tell(fmt.Sprintf("Couldn't %s advancement %s %s %s as they %s", verb, name, prep, who, advAlready(e.grant)))
		case tally.total == 0:
			tell(fmt.Sprintf("Couldn't %s advancement %s %s %d players as they %s", verb, name, prep, tally.n(false), advAlready(e.grant)))
		case who != "":
			okTell(fmt.Sprintf("%s the advancement %s %s %s", Verb, name, prep, who))
		default:
			okTell(fmt.Sprintf("%s the advancement %s %s %d players", Verb, name, prep, tally.n(true)))
		}
		return
	}
	them := strings.Replace(advAlready(e.grant), " it", " them", 1)
	switch who := tally.single(tally.total != 0); {
	case tally.total == 0 && who != "":
		tell(fmt.Sprintf("Couldn't %s %d advancements %s %s as they %s", verb, len(set), prep, who, them))
	case tally.total == 0:
		tell(fmt.Sprintf("Couldn't %s %d advancements %s %d players as they %s", verb, len(set), prep, tally.n(false), them))
	case who != "":
		okTell(fmt.Sprintf("%s %d advancements %s %s", Verb, len(set), prep, who))
	default:
		okTell(fmt.Sprintf("%s %d advancements %s %d players", Verb, len(set), prep, tally.n(true)))
	}
}

func advPastTense(grant bool) string {
	if grant {
		return "Granted"
	}
	return "Revoked"
}

func advAlready(grant bool) string {
	if grant {
		return "already have it"
	}
	return "don't have it"
}

// advCommandOne grants or revokes one advancement for one player: the named
// criteria, or (crits nil) all of them — a grant only when it is not already
// done, a revoke only when there is progress to take (Action.perform). It
// changes state only; advCommandBatch.sync tells the client. Reports whether
// anything changed, and whether a grant completed the advancement.
func advCommandOne(t *tracked, grant bool, n *advNode, crits []string) (changed, completed bool) {
	if t.adv == nil {
		t.adv = advState{}
	}
	if crits == nil {
		if grant && t.adv.done(n) || !grant && len(t.adv[n.id]) == 0 {
			return false, false
		}
		crits = advCriteriaNames(n)
	}
	for _, c := range crits {
		if grant {
			fresh, done := t.adv.grant(n, c)
			changed, completed = changed || fresh, completed || done
		} else if t.adv.revoke(n, c) {
			changed = true
		}
	}
	return changed, completed
}

// advCommandBatch is one player's share of an /advancement run: the
// advancements it changed and completed, so the client hears of them once.
type advCommandBatch struct {
	changed, completed []*advNode
}

func (b *advCommandBatch) apply(t *tracked, grant bool, n *advNode, crits []string) bool {
	changed, completed := advCommandOne(t, grant, n, crits)
	if changed {
		b.changed = append(b.changed, n)
	}
	if completed {
		b.completed = append(b.completed, n)
	}
	return changed
}

// sync tells the client: a grant reveals, streams and announces as a trigger
// would; a revoke may have to take nodes away again, so the whole tree is
// re-sent.
func (b *advCommandBatch) sync(h *hub, players map[int32]*tracked, t *tracked, grant bool) {
	switch {
	case len(b.changed) == 0:
	case grant:
		h.advDeliver(players, t, b.changed, b.completed)
	default:
		h.advSendAll(t)
	}
}
