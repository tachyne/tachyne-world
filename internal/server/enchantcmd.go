package server

import (
	"fmt"
	"strconv"
	"strings"
)

// /enchant <targets> <enchantment> [<level>] (EnchantCommand): the item in
// each target's main hand takes the enchantment if the item supports it and
// nothing already on it conflicts. The level may not exceed the
// enchantment's maximum.

type evEnchantCmd struct {
	by     int32
	target string
	ench   int8
	lvl    int8
}

func (evEnchantCmd) isHubEvent() {}

func (s *Server) cmdEnchant(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 2 || len(args) > 3 {
		p.tell("Usage: /enchant <targets> <enchantment> [<level>]")
		return
	}
	id, ok := enchByName[strings.TrimPrefix(args[1], "minecraft:")]
	if !ok {
		p.tell("Unknown enchantment: " + args[1])
		return
	}
	lvl := 1
	if len(args) == 3 {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 {
			p.tell("Usage: /enchant <targets> <enchantment> [<level>]")
			return
		}
		lvl = n
	}
	if max := enchDefs[id].maxLevel; lvl > max {
		p.tell(fmt.Sprintf("%d is higher than the maximum level of %d supported by that enchantment", lvl, max))
		return
	}
	s.hub.post(evEnchantCmd{by: p.eid, target: args[0], ench: id, lvl: int8(lvl)})
}

// applyEnchantCommand runs /enchant on the hub. The targets are
// EntityArgument.entities(): a living one — a player or a mob — has the
// item in its main hand enchanted; anything else is not a valid target.
// With a single target each refusal is an error; with several, the ones
// that cannot take it are passed over.
func (h *hub) applyEnchantCommand(players map[int32]*tracked, e evEnchantCmd) {
	caller := players[e.by]
	tell := func(msg string) {
		if caller != nil {
			caller.p.trySendEv(chatEv(msg))
		}
	}
	targets := h.commandEntitiesAll(players, e.by, e.target)
	if len(targets) == 0 {
		tell("No entity was found")
		return
	}
	single := len(targets) == 1
	done, doneName := 0, ""
	for _, en := range targets {
		var st invStack
		switch {
		case en.t != nil:
			st = en.t.inv.slots[en.t.p.heldSlot()]
		case en.m != nil:
			st = en.m.heldStack()
		default:
			if single {
				tell(en.name() + " is not a valid entity for this command")
				return
			}
			continue
		}
		switch {
		case st.item == 0:
			if single {
				tell(en.name() + " is not holding any item")
				return
			}
		case !enchIsSupported(e.ench, st.item) || !enchCompatibleWith(e.ench, st.ench):
			if single {
				tell(itemNameOf[st.item] + " cannot support that enchantment")
				return
			}
		default:
			st.ench = enchSetLevel(st.ench, e.ench, e.lvl)
			if t := en.t; t != nil {
				slot := t.p.heldSlot()
				t.inv.slots[slot] = st
				h.sendSlot(t, slot)
			} else {
				m := en.m
				m.setHeld(st)
				h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
			}
			done++
			doneName = en.name()
		}
	}
	name := enchDefs[e.ench].name
	switch {
	case done == 0:
		tell("Nothing changed. Targets either have no item in their hands or the enchantment could not be applied")
	case done == 1:
		h.cmdOK(players, e.by)(fmt.Sprintf("Applied enchantment %s to %s's item", name, doneName))
	default:
		h.cmdOK(players, e.by)(fmt.Sprintf("Applied enchantment %s to %d entities", name, done))
	}
}
