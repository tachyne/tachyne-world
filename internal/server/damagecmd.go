package server

import (
	"fmt"
	"strconv"
	"strings"
)

// /damage <target> <amount> [<damageType>] (DamageCommand): the hit goes
// down the engine's own damage paths — hurtBy for a player, hurtMobOf for a
// mob — so the type decides what armour, Resistance and the protection
// enchantments do about it, exactly as the same damage from the world would.
// The attributed forms (at <location>, by <entity> [from <cause>]) are not
// taken: the paths below have no attacker to credit.

type evDamageCmd struct {
	by     int32
	target string
	amount float32
	dt     dmgType
}

func (evDamageCmd) isHubEvent() {}

// dmgTypeByName is the damage_type registry by name.
var dmgTypeByName = func() map[string]dmgType {
	m := make(map[string]dmgType, len(dmgTypeNames))
	for dt, name := range dmgTypeNames {
		m[name] = dmgType(dt)
	}
	return m
}()

func (s *Server) cmdDamage(p *player, args []string) {
	if !s.isOp(p.name) { // DamageCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 2 || len(args) > 3 {
		p.tell("Usage: /damage <target> <amount> [<damageType>]")
		return
	}
	amount, err := strconv.ParseFloat(args[1], 32)
	if err != nil {
		p.tell(fmt.Sprintf("Invalid float '%s'", args[1]))
		return
	}
	if amount < 0 {
		p.tell(fmt.Sprintf("Float must not be less than 0.0: found %s", jFloat(float32(amount))))
		return
	}
	dt := dtGeneric
	if len(args) == 3 {
		name := strings.TrimPrefix(args[2], "minecraft:")
		d, ok := dmgTypeByName[name]
		if !ok {
			p.tell(fmt.Sprintf("Can't find element 'minecraft:%s' of type 'minecraft:damage_type'", name))
			return
		}
		dt = d
	}
	s.hub.post(evDamageCmd{by: p.eid, target: args[0], amount: float32(amount), dt: dt})
}

// applyDamageCommand runs /damage on the hub.
func (h *hub) applyDamageCommand(players map[int32]*tracked, e evDamageCmd) {
	tell := cmdTeller(players, e.by)
	en, ok := h.singleEntity(players, e.by, e.target, tell)
	if !ok {
		return
	}
	name := en.name() // before the blow: a killed mob is gone after it
	if !h.commandHurt(players, en, e.amount, e.dt) {
		tell("Target is invulnerable to the given damage type")
		return
	}
	tell(fmt.Sprintf("Applied %s damage to %s", jFloat(e.amount), name))
}

// commandHurt deals the damage and reports whether it landed
// (Entity.hurtServer's result).
func (h *hub) commandHurt(players map[int32]*tracked, en cmdEntity, amount float32, dt dmgType) bool {
	if t := en.t; t != nil {
		return h.hurtBy(players, t, amount, dt, deathCause{})
	}
	m := en.m
	if m.dying != 0 || m.spawnInvuln > 0 {
		return false
	}
	h.hurtMobOf(players, m, float64(amount), dt)
	return true
}
