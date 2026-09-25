package server

import "strings"

// Death messages.
//
// Every death read "<name> died", whatever killed them — which loses the one
// piece of information a death message exists to carry. Vanilla builds the
// message from the damage TYPE plus, where there is one, the thing that dealt
// it: "Wesley was slain by Zombie", "Wesley fell from a high place", "Wesley
// was shot by EdgeZA".
//
// The cause rides with the damage rather than being guessed at the death,
// because by the time health reaches zero the source is long gone. It is
// stored on the player as the LAST thing that hurt them, which is also how
// vanilla does it — walk into lava, climb out, and die of the burns, and lava
// still gets the credit.

// deathCause is what last hurt a player: the damage type (which decides the
// message) and the name of whoever or whatever dealt it, empty for the
// impersonal ones. hurtFrom stamps the type on, so a call site only ever has
// to name the killer.
type deathCause struct {
	dt dmgType
	by string
	// weapon is the killer's weapon when it carries a name of its own, which
	// is the only case vanilla names it ("… using Excalibur"). credit is who
	// the victim was last fighting, filled in by hurtFrom, and is what turns
	// an impersonal death into "while trying to escape X".
	weapon string
	credit string
	// byEID is the player to blame, when a player is (resolvePlayerResponsibleForDamage):
	// the victim remembers them for 100 ticks, and a death inside that is
	// their kill.
	byEID int32
}

// namedMainhand is the name of what a player holds in the main hand when it
// has one of its own (CUSTOM_NAME) — the only case a death message names
// the weapon.
func namedMainhand(t *tracked) string {
	if held := t.mainHand(); held.count > 0 {
		return held.name
	}
	return ""
}

// playerCause is a death caused by player t: their name, their kill, and —
// as getLocalizedDeathMessage reads the causing entity's main hand when the
// message is made — a named item they are holding, whatever the blow was
// dealt with (an arrow, a thorn, a blast they lit).
func playerCause(t *tracked) deathCause {
	return deathCause{by: t.p.name, byEID: t.p.eid, weapon: namedMainhand(t)}
}

// killCreditTicks is how long vanilla remembers who a victim was last
// fighting (LivingEntity's lastHurtByMob timeout). Inside that window a death
// with nobody directly to blame still reads "while trying to escape X".
const killCreditTicks = 100

// deathMessage renders the message for a death the way vanilla's
// DamageSource.getLocalizedDeathMessage does: the damage type names a
// death.attack.<id> family, and which member of it depends on whether
// something is to blame.
//
//   - Something dealt it: death.attack.<id> with the killer as the second
//     argument, or the .item form when they were holding something named.
//   - Nothing dealt it, but the victim was fighting recently: the .player
//     form, which is the "while trying to escape X" wording.
//   - Nothing at all: the plain form.
func deathMessage(victim string, c deathCause) string {
	id := ""
	if int(c.dt) < len(dmgTypeMsgID) {
		id = dmgTypeMsgID[c.dt]
	}
	if id == "" {
		return victim + " died"
	}
	kind := deathMsgDefault
	if int(c.dt) < len(dmgTypeDeathKind) {
		kind = dmgTypeDeathKind[c.dt]
	}
	if kind == deathMsgIntentional {
		// The killer is a link component whose text the client shows in square
		// brackets: "was killed by [Intentional Game Design]".
		return format(deathMsgText["death.attack."+id+".message"],
			victim, "["+deathMsgText["death.attack."+id+".link"]+"]", "")
	}
	// The fall family's "doomed to fall by X" wording needs vanilla's combat
	// tracker, which remembers what put the victim in the air; without it a
	// fall reads as the plain fall message, which is also what vanilla falls
	// back to when nothing pushed them.
	by := strings.TrimSpace(c.by)
	if by != "" {
		if weapon := strings.TrimSpace(c.weapon); weapon != "" {
			if text, ok := deathMsgText["death.attack."+id+".item"]; ok {
				return format(text, victim, by, weapon)
			}
		}
		return format(deathMsgText["death.attack."+id], victim, by, "")
	}
	if credit := strings.TrimSpace(c.credit); credit != "" {
		if text, ok := deathMsgText["death.attack."+id+".player"]; ok {
			return format(text, victim, credit, "")
		}
	}
	return format(deathMsgText["death.attack."+id], victim, "", "")
}

// format fills vanilla's positional placeholders. A message that names fewer
// arguments than it is given simply ignores the rest, as translation does.
func format(text, victim, killer, weapon string) string {
	if text == "" {
		return victim + " died"
	}
	r := strings.NewReplacer("%1$s", victim, "%2$s", killer, "%3$s", weapon)
	return r.Replace(text)
}

// mobDisplayName turns an entity type into the name a death message shows —
// "Zombie", "Cave Spider" — from the registry name.
func mobDisplayName(etype int) string {
	name := strings.TrimPrefix(advEntityName[etype], "minecraft:")
	if name == "" {
		return "a mob"
	}
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
