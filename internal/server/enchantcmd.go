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

// applyEnchantCommand runs /enchant on the hub.
func (h *hub) applyEnchantCommand(players map[int32]*tracked, e evEnchantCmd) {
	caller := players[e.by]
	tell := func(msg string) {
		if caller != nil {
			caller.p.trySendEv(chatEv(msg))
		}
	}
	targets := h.commandTargets(players, e.by, e.target)
	done := 0
	for _, t := range targets {
		slot := t.p.heldSlot()
		st := t.inv.slots[slot]
		switch {
		case st.item == 0:
			if len(targets) == 1 {
				tell(t.p.name + " is not holding any item")
				return
			}
		case !enchIsSupported(e.ench, st.item) || !enchCompatibleWith(e.ench, st.ench):
			if len(targets) == 1 {
				tell(itemNameOf[st.item] + " cannot support that enchantment")
				return
			}
		default:
			st.ench = enchSetLevel(st.ench, e.ench, e.lvl)
			t.inv.slots[slot] = st
			h.sendSlot(t, slot)
			done++
		}
	}
	name := enchDefs[e.ench].name
	switch {
	case done == 0:
		tell("Nothing changed. Targets either have no item in their hands or the enchantment could not be applied")
	case len(targets) == 1:
		tell(fmt.Sprintf("Applied enchantment %s to %s's item", name, targets[0].p.name))
	default:
		tell(fmt.Sprintf("Applied enchantment %s to %d entities", name, done))
	}
}
