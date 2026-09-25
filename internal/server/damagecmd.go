package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// /damage <target> <amount> [<damageType>] (DamageCommand): the hit goes
// down the engine's own damage paths — hurtBy for a player, hurtMobOf for a
// mob — so the type decides what armour, Resistance and the protection
// enchantments do about it, exactly as the same damage from the world would.
// The attributed forms follow the type: `at <location>` gives the blow a
// source position (a shield faces it, the knockback runs away from it), and
// `by <entity> [from <cause>]` names the direct entity and the one to blame,
// which is who the death message, the kill credit and the grudge go to.

type evDamageCmd struct {
	by     int32
	target string
	amount float32
	dt     dmgType
	// at is the `at` source position (atOK set); direct and cause are the
	// `by` entity and the `from` cause selectors ("" when not given).
	atX, atY, atZ float64
	atOK          bool
	direct, cause string
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
	usage := func() {
		p.tell("Usage: /damage <target> <amount> [<damageType> [at <location> | by <entity> [from <cause>]]]")
	}
	if len(args) < 2 {
		usage()
		return
	}
	ev := evDamageCmd{by: p.eid, target: args[0]}
	if len(args) > 3 {
		switch rest := args[3:]; {
		case rest[0] == "at" && len(rest) == 4:
			x, y, z, ok := parsePosition(rest[1:], p.x, p.y, p.z, p.yaw, p.pitch)
			if !ok {
				p.tell("Invalid position")
				return
			}
			ev.atX, ev.atY, ev.atZ, ev.atOK = x, y, z, true
		case rest[0] == "by" && len(rest) == 2:
			ev.direct = rest[1]
		case rest[0] == "by" && len(rest) == 4 && rest[2] == "from":
			ev.direct, ev.cause = rest[1], rest[3]
		default:
			usage()
			return
		}
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
	if len(args) >= 3 {
		name := strings.TrimPrefix(args[2], "minecraft:")
		d, ok := dmgTypeByName[name]
		if !ok {
			p.tell(fmt.Sprintf("Can't find element 'minecraft:%s' of type 'minecraft:damage_type'", name))
			return
		}
		dt = d
	}
	ev.amount, ev.dt = float32(amount), dt
	s.hub.post(ev)
}

// applyDamageCommand runs /damage on the hub.
func (h *hub) applyDamageCommand(players map[int32]*tracked, e evDamageCmd) {
	tell := cmdTeller(players, e.by)
	okTell := h.cmdOK(players, e.by) // sendSuccess(…, true)
	en, ok := h.singleEntity(players, e.by, e.target, tell)
	if !ok {
		return
	}
	src := cmdDamageSource{}
	if e.atOK {
		src.pos, src.x, src.z = true, e.atX, e.atZ
	}
	if e.direct != "" {
		d, ok := h.singleEntity(players, e.by, e.direct, tell)
		if !ok {
			return
		}
		src.direct, src.cause = &d, &d
		if e.cause != "" {
			c, ok := h.singleEntity(players, e.by, e.cause, tell)
			if !ok {
				return
			}
			src.cause = &c
		}
		// DamageSource.getSourcePosition: the direct entity's position.
		src.pos = true
		src.x, _, src.z = d.pos()
	}
	name := en.name() // before the blow: a killed mob is gone after it
	if !h.commandHurt(players, en, e.amount, e.dt, src) {
		tell("Target is invulnerable to the given damage type")
		return
	}
	okTell(fmt.Sprintf("Applied %s damage to %s", jFloat(e.amount), name))
}

// commandHurt deals the damage and reports whether it landed
// (Entity.hurtServer's result).
func (h *hub) commandHurt(players map[int32]*tracked, en cmdEntity, amount float32, dt dmgType, src cmdDamageSource) bool {
	// LivingEntity.dealDefaultKnockback: 0.4 away from the source position,
	// or in a random direction when the blow has none — unless the type is
	// #no_knockback.
	fx, fz := src.x, src.z
	if !src.pos {
		a := h.rng.Float64() * 2 * math.Pi
		ex, _, ez := en.pos()
		fx, fz = ex+math.Cos(a), ez+math.Sin(a)
	}
	knock := !dt.has(tagNoKnockback)
	if t := en.t; t != nil {
		cause := deathCause{}
		var from dmgFrom
		if src.pos {
			from = dmgFrom{x: src.x, z: src.z, ok: true}
		}
		if c := src.cause; c != nil {
			cause.by = c.name()
			if c.t != nil {
				cause.byEID = c.t.p.eid
			} else {
				from.byMob = true // a living non-player to blame: the difficulty scales it
			}
		}
		if !h.hurtFrom(players, t, amount, dt, cause, from) {
			return false
		}
		if knock && !t.dead {
			h.knockback(t, fx, fz)
		}
		return true
	}
	m := en.m
	if m.dying != 0 || m.spawnInvuln > 0 {
		return false
	}
	if c := src.cause; c != nil && c.t != nil {
		// A player to blame: the blow is answered as theirs — the kill
		// credit (resolvePlayerResponsibleForDamage), the grudge or the
		// panic (HurtByTargetGoal, PanicGoal), the flash and the death.
		if dt.has(tagIsFire) && m.resistsFire() {
			return false
		}
		h.vibAt(m.dim, freqEntityDamage, m.x, m.y, m.z, m.eid)
		h.hurtByPlayerOn(m, c.t)
		m.hurtKind(float64(amount), dt)
		h.mobStruck(players, m, c.t, dt)
	} else {
		h.hurtMobOf(players, m, float64(amount), dt)
	}
	if knock && m.dying == 0 && m.health > 0 && m.kbScale() > 0 {
		h.mobKnockFrom(players, m, fx, fz)
	}
	return true
}

// cmdDamageSource is the DamageSource /damage builds: an optional source
// position, the direct entity and the causing entity.
type cmdDamageSource struct {
	pos           bool
	x, z          float64
	direct, cause *cmdEntity
}
