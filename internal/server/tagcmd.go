package server

import (
	"fmt"
	"sort"
	"strings"
)

// /tag <targets> add|remove <name> | list (TagCommand): an entity's
// scoreboard tags, which the selector's tag= predicate then reads. A player's
// tags persist with the rest of their data (inventories.json), a mob's with
// the mob (mobs.json).

// maxEntityTags is Entity.MAX_ENTITY_TAG_COUNT.
const maxEntityTags = 1024

type evTagCmd struct {
	by     int32
	target string
	op     string // add, remove, list
	name   string
}

func (evTagCmd) isHubEvent() {}

const tagCmdUsage = "Usage: /tag <targets> add|remove <name> | /tag <targets> list"

func (s *Server) cmdTag(p *player, args []string) {
	if !s.isOp(p.name) { // TagCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	switch {
	case len(args) == 2 && args[1] == "list":
	case len(args) == 3 && (args[1] == "add" || args[1] == "remove"):
		if !validTagName(args[2]) {
			p.tell("Invalid tag name: " + args[2])
			return
		}
	default:
		p.tell(tagCmdUsage)
		return
	}
	e := evTagCmd{by: p.eid, target: args[0], op: args[1]}
	if len(args) == 3 {
		e.name = args[2]
	}
	s.hub.post(e)
}

// validTagName is StringArgumentType.word: the unquoted-string characters.
func validTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '_', r == '-', r == '.', r == '+':
		default:
			return false
		}
	}
	return true
}

// addTag is Entity.addTag: false when the tag is already there or the entity
// already carries the maximum.
func (l *living) addTag(name string) bool {
	if l.tags[name] || len(l.tags) >= maxEntityTags {
		return false
	}
	if l.tags == nil {
		l.tags = map[string]bool{}
	}
	l.tags[name] = true
	return true
}

// removeTag is Entity.removeTag.
func (l *living) removeTag(name string) bool {
	if !l.tags[name] {
		return false
	}
	delete(l.tags, name)
	return true
}

// sortedTags is a tag set as a stable list (the saved form).
func sortedTags(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// tagSet is the saved list back as a set (nil for none).
func tagSet(list []string) map[string]bool {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]bool, len(list))
	for _, t := range list {
		out[t] = true
	}
	return out
}

// applyTagCommand runs /tag on the hub.
func (h *hub) applyTagCommand(players map[int32]*tracked, e evTagCmd) {
	tell := cmdTeller(players, e.by)
	targets := h.commandEntities(players, e.by, e.target)
	if len(targets) == 0 {
		tell("No entity was found")
		return
	}
	var tally cmdTally
	switch e.op {
	case "add", "remove":
		for _, en := range targets {
			l := en.living()
			ok := false
			if e.op == "add" {
				ok = l.addTag(e.name)
			} else {
				ok = l.removeTag(e.name)
			}
			tally.track(en.name(), b2i(ok))
		}
		switch who := tally.single(true); {
		case tally.nonZero == 0 && e.op == "add":
			tell("Target either already has the tag or has too many tags")
		case tally.nonZero == 0:
			tell("Target does not have this tag")
		case who != "" && e.op == "add":
			tell(fmt.Sprintf("Added tag '%s' to %s", e.name, who))
		case who != "":
			tell(fmt.Sprintf("Removed tag '%s' from %s", e.name, who))
		case e.op == "add":
			tell(fmt.Sprintf("Added tag '%s' to %d entities", e.name, tally.nonZero))
		default:
			tell(fmt.Sprintf("Removed tag '%s' from %d entities", e.name, tally.nonZero))
		}
	case "list":
		all := map[string]bool{}
		for _, en := range targets {
			l := en.living()
			for t := range l.tags {
				all[t] = true
			}
			tally.track(en.name(), len(l.tags))
		}
		if len(all) == 0 { // the no-tags line names what was asked about
			if who := tally.single(false); who != "" {
				tell(who + " has no tags")
			} else {
				tell(fmt.Sprintf("There are no tags on the %d entities", tally.count))
			}
			return
		}
		list := strings.Join(sortedTags(all), ", ")
		if who := tally.single(true); who != "" {
			tell(fmt.Sprintf("%s has %d tag(s): %s", who, len(all), list))
		} else {
			tell(fmt.Sprintf("The %d entities have %d total tag(s): %s", tally.nonZero, len(all), list))
		}
	}
}

// tagsMatch is the selector's tag= predicates against an entity's tags:
// tag=x needs x, tag=!x forbids it, a bare tag= wants no tags at all and
// tag=! wants at least one (EntitySelectorOptions "tag").
func (spec targetSpec) tagsMatch(tags map[string]bool) bool {
	for _, t := range spec.tags {
		if t == "" {
			if len(tags) != 0 {
				return false
			}
		} else if !tags[t] {
			return false
		}
	}
	for _, t := range spec.notTags {
		if t == "" {
			if len(tags) == 0 {
				return false
			}
		} else if tags[t] {
			return false
		}
	}
	return true
}
