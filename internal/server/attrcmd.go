package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/attribute"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// /attribute <target> <attribute> get [<scale>] | base get [<scale>] |
// base set <value> | base reset | modifier add <id> <value> <operation> |
// modifier remove <id> | modifier value get <id> [<scale>]
// (AttributeCommand). It works on the same attribute map every mechanic
// reads, so a changed MAX_HEALTH or MOVEMENT_SPEED takes effect at once, and
// the once-a-second attribute sync carries it to the clients. A player's
// attribute changes last the session: player attributes are not saved.

type evAttributeCmd struct {
	by     int32
	target string
	id     attr.ID
	op     string // get, base get, base set, base reset, modifier add, modifier remove, modifier get
	value  float64
	scale  float64 // get's <scale>: it scales only the command's result value, never the feedback
	mod    string  // the modifier id
	modOp  attr.Op
}

func (evAttributeCmd) isHubEvent() {}

const attrCmdUsage = "Usage: /attribute <target> <attribute> get [<scale>] | base get [<scale>] | base set <value> | base reset | modifier add <id> <value> add_value|add_multiplied_base|add_multiplied_total | modifier remove <id> | modifier value get <id> [<scale>]"

func (s *Server) cmdAttribute(p *player, args []string) {
	if !s.isOp(p.name) { // AttributeCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 3 {
		p.tell(attrCmdUsage)
		return
	}
	e := evAttributeCmd{by: p.eid, target: args[0], id: attr.ID(nsID(args[1])), scale: 1}
	if _, ok := attr.Defs[e.id]; !ok {
		p.tell(fmt.Sprintf("Can't find element '%s' of type 'minecraft:attribute'", e.id))
		return
	}
	rest := args[2:]
	scaleAt := func(i int) bool { // an optional trailing <scale>
		if len(rest) == i {
			return true
		}
		if len(rest) != i+1 {
			return false
		}
		v, err := strconv.ParseFloat(rest[i], 64)
		if err != nil {
			return false
		}
		e.scale = v
		return true
	}
	ok := false
	switch {
	case rest[0] == "get":
		e.op, ok = "get", scaleAt(1)
	case rest[0] == "base" && len(rest) >= 2 && rest[1] == "get":
		e.op, ok = "base get", scaleAt(2)
	case rest[0] == "base" && len(rest) == 3 && rest[1] == "set":
		v, err := strconv.ParseFloat(rest[2], 64)
		e.op, e.value, ok = "base set", v, err == nil
	case rest[0] == "base" && len(rest) == 2 && rest[1] == "reset":
		e.op, ok = "base reset", true
	case rest[0] == "modifier" && len(rest) == 5 && rest[1] == "add":
		v, err := strconv.ParseFloat(rest[3], 64)
		op, opOK := modifierOps[rest[4]]
		e.op, e.mod, e.value, e.modOp, ok = "modifier add", nsID(rest[2]), v, op, err == nil && opOK
	case rest[0] == "modifier" && len(rest) == 3 && rest[1] == "remove":
		e.op, e.mod, ok = "modifier remove", nsID(rest[2]), true
	case rest[0] == "modifier" && len(rest) >= 4 && rest[1] == "value" && rest[2] == "get":
		e.op, e.mod, ok = "modifier get", nsID(rest[3]), scaleAt(4)
	}
	if !ok {
		p.tell(attrCmdUsage)
		return
	}
	s.hub.post(e)
}

// modifierOps is AttributeModifier.Operation by its serialized name.
var modifierOps = map[string]attr.Op{
	"add_value":            attr.AddValue,
	"add_multiplied_base":  attr.AddMultipliedBase,
	"add_multiplied_total": attr.AddMultipliedTotal,
}

// attrDisplayName is the attribute's English name (attribute.name.*), which
// is what the feedback prints. Most are the id in title case; these are not.
func attrDisplayName(id attr.ID) string {
	path := strings.TrimPrefix(string(id), "minecraft:")
	switch path {
	case "movement_speed":
		return "Speed"
	case "follow_range":
		return "Mob Follow Range"
	case "tempt_range":
		return "Mob Tempt Range"
	case "spawn_reinforcements":
		return "Zombie Reinforcements"
	case "below_name_distance":
		return "Name Tag Score Distance"
	case "nameplate_distance":
		return "Name Tag Distance"
	}
	words := strings.Split(path, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// attrTarget is the attribute map a command reaches and the factor between
// vanilla's units and the engine's: a mob keeps MOVEMENT_SPEED in per-step
// blocks (attrToStep × vanilla's), so a flat amount is scaled going in and
// coming out; proportional modifiers mean the same either way.
func attrTarget(en cmdEntity, id attr.ID) (a *attribute.Map, unit float64) {
	if en.t != nil {
		return en.t.playerAttrs(), 1
	}
	if id == attr.MovementSpeed {
		return en.m.mobAttrs(), attrToStep
	}
	return en.m.mobAttrs(), 1
}

// attrDefaultBase is the base a reset returns to: the player's or the
// species' own starting value (DefaultAttributes), in engine units.
func attrDefaultBase(en cmdEntity, id attr.ID) float64 {
	if en.t != nil {
		return newPlayerAttributes().Get(id).Base()
	}
	return newMobAttributes(en.m.etype).Get(id).Base()
}

// fromEngine converts an engine-unit value back to vanilla's. The division
// leaves binary noise (0.1035/0.45 is 0.22999999999999998), so a converted
// value is rounded to nine places — far finer than any attribute is set.
func fromEngine(v, unit float64) float64 {
	if unit == 1 {
		return v
	}
	return math.Round(v/unit*1e9) / 1e9
}

// modifierAmount is a modifier's amount in vanilla's units.
func modifierAmount(m attr.Modifier, unit float64) float64 {
	if m.Op == attr.AddValue {
		return fromEngine(m.Amount, unit)
	}
	return m.Amount
}

// applyAttributeCommand runs /attribute on the hub.
func (h *hub) applyAttributeCommand(players map[int32]*tracked, e evAttributeCmd) {
	tell := cmdTeller(players, e.by)
	targets := h.commandEntities(players, e.by, e.target)
	switch {
	case len(targets) == 0:
		tell("No entity was found")
		return
	case len(targets) > 1:
		tell("Only one entity is allowed, but the provided selector allows more than one")
		return
	}
	en := targets[0]
	a, unit := attrTarget(en, e.id)
	in := a.Get(e.id)
	name, who := attrDisplayName(e.id), en.name()
	switch e.op {
	case "get":
		tell(fmt.Sprintf("The value of attribute %s for entity %s is %s", name, who, jDouble(fromEngine(in.Value(), unit))))
	case "base get":
		tell(fmt.Sprintf("The base value of attribute %s for entity %s is %s", name, who, jDouble(fromEngine(in.Base(), unit))))
	case "base set":
		in.SetBase(e.value * unit)
		h.attrChanged(players, en, e.id)
		tell(fmt.Sprintf("The base value for attribute %s for entity %s set to %s", name, who, jDouble(e.value)))
	case "base reset":
		in.SetBase(attrDefaultBase(en, e.id))
		h.attrChanged(players, en, e.id)
		tell(fmt.Sprintf("The base value for attribute %s for entity %s reset to default %s", name, who, jDouble(fromEngine(in.Base(), unit))))
	case "modifier add":
		if in.HasModifier(e.mod) {
			tell(fmt.Sprintf("Modifier %s is already present on attribute %s for entity %s", e.mod, name, who))
			return
		}
		amount := e.value
		if e.modOp == attr.AddValue {
			amount *= unit
		}
		in.AddModifier(attr.Modifier{Source: e.mod, Amount: amount, Op: e.modOp})
		h.attrChanged(players, en, e.id)
		tell(fmt.Sprintf("Added modifier %s to attribute %s for entity %s", e.mod, name, who))
	case "modifier remove":
		if !in.HasModifier(e.mod) {
			tell(fmt.Sprintf("Attribute %s for entity %s has no modifier %s", name, who, e.mod))
			return
		}
		in.RemoveModifier(e.mod)
		h.attrChanged(players, en, e.id)
		tell(fmt.Sprintf("Removed modifier %s from attribute %s for entity %s", e.mod, name, who))
	case "modifier get":
		for _, m := range in.Modifiers() {
			if m.Source == e.mod {
				tell(fmt.Sprintf("The value of modifier %s on attribute %s for entity %s is %s", e.mod, name, who, jDouble(modifierAmount(m, unit))))
				return
			}
		}
		tell(fmt.Sprintf("Attribute %s for entity %s has no modifier %s", name, who, e.mod))
	}
}

// attrChanged is what a changed attribute sets off at once rather than on the
// next sync: health above a lowered MAX_HEALTH comes down to it
// (LivingEntity's onAttributeUpdated), and the frame goes out now so the
// client's hearts and pace follow the command immediately.
func (h *hub) attrChanged(players map[int32]*tracked, en cmdEntity, id attr.ID) {
	if en.t != nil {
		t := en.t
		if id == attr.MaxHealth && t.health > t.maxHP() {
			t.health = t.maxHP()
			h.sendHealth(t)
		}
		fr := playerAttrFrame(t)
		t.attrSent = attrFingerprint(fr)
		t.p.trySendEv(fr)
		h.toNearbyEv(players, t.dim, t.x, t.z, fr)
		return
	}
	m := en.m
	if id == attr.MaxHealth && m.health > m.maxHP() {
		m.health = m.maxHP()
	}
	fr := mobAttrFrame(m)
	m.attrSent = attrFingerprint(fr)
	if len(fr.Attrs) > 0 {
		h.toNearbyEv(players, m.dim, m.x, m.z, fr)
	}
}
