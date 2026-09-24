package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The combat tracker (CombatTracker), for the fall family of death
// messages. Every hit a player takes is logged with how far they had fallen
// at the time and what they were last climbing; when a fall kills them, the
// most significant fall in the log decides the wording. A plain fall off a
// ladder reads "fell off a ladder". A fall after someone's blow reads "was
// doomed to fall by X", or "fell too far and was finished by X" when the
// same one finished them. The log clears after five seconds without damage,
// or fifteen once a fight has started.

const (
	combatResetTicks  = 100 // RESET_DAMAGE_STATUS_TIME
	combatFightTicks  = 300 // RESET_COMBAT_STATUS_TIME
	combatSignificant = 5.0 // a fall or a hit past five counts
)

// combatEntry is one CombatEntry.
type combatEntry struct {
	cause    deathCause
	damage   float32
	fallLoc  string  // FallLocation's id ("ladder", "water"…), "" for none
	fallDist float64 // fallDistance when the hit landed
}

// combatLog is a player's CombatTracker state.
type combatLog struct {
	entries  []combatEntry
	lastHit  uint64
	inCombat bool
	climb    blockPos // lastClimbablePos
	hasClimb bool
}

// trackClimbable is LivingEntity.onClimbable's bookkeeping: the last
// climbable block the player was in, forgotten on landing.
func (h *hub) trackClimbable(t *tracked, x, y, z float64, onGround bool) {
	if p := (blockPos{floorInt(x), floorInt(y), floorInt(z)}); isClimbable(h.worldFor(t.dim).At(p.x, p.y, p.z)) {
		t.combat.climb, t.combat.hasClimb = p, true
	} else if onGround {
		t.combat.hasClimb = false
	}
}

// fallLocation is FallLocation.getCurrentFallLocation.
func (h *hub) fallLocation(t *tracked) string {
	if t.combat.hasClimb {
		name, _ := worldgen.StateName(h.worldFor(t.dim).At(t.combat.climb.x, t.combat.climb.y, t.combat.climb.z))
		switch name {
		case "ladder":
			return "ladder"
		case "vine":
			return "vines"
		case "weeping_vines", "weeping_vines_plant":
			return "weeping_vines"
		case "twisting_vines", "twisting_vines_plant":
			return "twisting_vines"
		case "scaffolding":
			return "scaffolding"
		default:
			return "other_climbable"
		}
	}
	if h.inWater(t.dim, t.x, t.y, t.z) {
		return "water"
	}
	return ""
}

// recordCombat is CombatTracker.recordDamage. fall is the landing's fall
// distance for the fall damage itself; otherwise the drop so far counts.
func (h *hub) recordCombat(t *tracked, cause deathCause, damage float32, fall float64) {
	now := h.tick.Load()
	h.recheckCombat(t, now)
	if fall == 0 && t.airborne && t.peakY > t.y {
		fall = t.peakY - t.y
	}
	t.combat.entries = append(t.combat.entries, combatEntry{cause: cause, damage: damage, fallLoc: h.fallLocation(t), fallDist: fall})
	if len(t.combat.entries) > 64 {
		t.combat.entries = t.combat.entries[len(t.combat.entries)-64:]
	}
	t.combat.lastHit = now
	if cause.by != "" { // shouldEnterCombat: an attacker
		t.combat.inCombat = true
	}
}

// recheckCombat is CombatTracker.recheckStatus.
func (h *hub) recheckCombat(t *tracked, now uint64) {
	reset := uint64(combatResetTicks)
	if t.combat.inCombat {
		reset = combatFightTicks
	}
	if len(t.combat.entries) > 0 && now-t.combat.lastHit > reset {
		t.combat.entries, t.combat.inCombat = nil, false
	}
}

// mostSignificantFall is CombatTracker.getMostSignificantFall: the entry
// that set up the longest fall (the hit before it, if there was one), else
// the hardest hit taken on a climbable or in water.
func mostSignificantFall(es []combatEntry) *combatEntry {
	var result, alt *combatEntry
	var bestFall float64
	var altDamage float32
	for i := range es {
		e := &es[i]
		fake := e.cause.dt.has(tagAlwaysMostSignificantFall)
		fd := e.fallDist
		if fake {
			fd = math.MaxFloat64
		}
		if (e.cause.dt.has(tagIsFall) || fake) && fd > 0 && (result == nil || fd > bestFall) {
			result = e
			if i > 0 {
				result = &es[i-1]
			}
			bestFall = fd
		}
		if e.fallLoc != "" && (alt == nil || e.damage > altDamage) {
			alt, altDamage = e, e.damage
		}
	}
	if bestFall > combatSignificant && result != nil {
		return result
	}
	if altDamage > combatSignificant && alt != nil {
		return alt
	}
	return nil
}

// combatDeathMessage is CombatTracker.getDeathMessage.
func (h *hub) combatDeathMessage(t *tracked) string {
	c := t.lastCause
	if int(c.dt) < len(dmgTypeDeathKind) && dmgTypeDeathKind[c.dt] == deathMsgFallVariants {
		if k := mostSignificantFall(t.combat.entries); k != nil {
			return fallMessage(t.p.name, *k, c)
		}
	}
	return deathMessage(t.p.name, c)
}

// fallMessage is CombatTracker.getFallMessage.
func fallMessage(victim string, knockOff combatEntry, killing deathCause) string {
	src := knockOff.cause
	if !src.dt.has(tagIsFall) && !src.dt.has(tagAlwaysMostSignificantFall) {
		switch {
		case src.by != "" && src.by != killing.by:
			return assistedFall(victim, src, "death.fell.assist")
		case killing.by != "":
			return assistedFall(victim, killing, "death.fell.finish")
		}
		return format(deathMsgText["death.fell.killer"], victim, "", "")
	}
	loc := knockOff.fallLoc
	if loc == "" {
		loc = "generic"
	}
	if text, ok := deathMsgText["death.fell.accident."+loc]; ok {
		return format(text, victim, "", "")
	}
	return format(deathMsgText["death.fell.accident.generic"], victim, "", "")
}

// assistedFall is getMessageForAssistedFall: the .item form when the
// attacker held something named.
func assistedFall(victim string, by deathCause, key string) string {
	if w := by.weapon; w != "" {
		if text, ok := deathMsgText[key+".item"]; ok {
			return format(text, victim, by.by, w)
		}
	}
	return format(deathMsgText[key], victim, by.by, "")
}
