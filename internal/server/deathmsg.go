package server

import (
	"encoding/json"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

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

// mobMeleeCause is a mob's blow: its name, and — getLocalizedDeathMessage
// reading the attacker's main hand, as for a player — a named item it holds,
// for the "… using Excalibur" form.
func mobMeleeCause(m *mob) deathCause {
	return deathCause{by: mobDisplayName(m.etype), weapon: m.heldStack().name}
}

// killCreditTicks is how long vanilla remembers who a victim was last
// fighting (LivingEntity's lastHurtByMob timeout). Inside that window a death
// with nobody directly to blame still reads "while trying to escape X".
const killCreditTicks = 100

// deathMessage renders the message for a death the way vanilla's
// DamageSource.getLocalizedDeathMessage does, as English text; deathMessageOf
// is the same message as vanilla's translatable component.
func deathMessage(victim string, c deathCause) string { return deathMessageOf(victim, c).english() }

// deathMessageOf is DamageSource.getLocalizedDeathMessage: the damage type
// names a death.attack.<id> family, and which member of it depends on
// whether something is to blame.
//
//   - Something dealt it: death.attack.<id> with the killer as the second
//     argument, or the .item form when they were holding something named.
//   - Nothing dealt it, but the victim was fighting recently: the .player
//     form, which is the "while trying to escape X" wording.
//   - Nothing at all: the plain form.
func deathMessageOf(victim string, c deathCause) deathMsg {
	v := textArg(victim)
	id := ""
	if int(c.dt) < len(dmgTypeMsgID) {
		id = dmgTypeMsgID[c.dt]
	}
	if id == "" {
		return deathMsg{key: "death.attack.generic", args: []deathArg{v}}
	}
	base := "death.attack." + id
	kind := deathMsgDefault
	if int(c.dt) < len(dmgTypeDeathKind) {
		kind = dmgTypeDeathKind[c.dt]
	}
	if kind == deathMsgIntentional {
		// The killer is a link the client shows in square brackets: "was
		// killed by [Intentional Game Design]".
		link := deathArg{plain: "[" + deathMsgText[base+".link"] + "]",
			comp: attachproto.Text{Translate: "chat.square_brackets", With: []attachproto.Text{{Translate: base + ".link"}}}}
		return deathMsg{key: base + ".message", args: []deathArg{v, link}}
	}
	// The fall family's "doomed to fall by X" wording needs vanilla's combat
	// tracker, which remembers what put the victim in the air; without it a
	// fall reads as the plain fall message, which is also what vanilla falls
	// back to when nothing pushed them.
	by := strings.TrimSpace(c.by)
	if by != "" {
		if weapon := strings.TrimSpace(c.weapon); weapon != "" {
			if _, ok := deathMsgText[base+".item"]; ok {
				return deathMsg{key: base + ".item", args: []deathArg{v, nameArg(by), weaponArg(weapon)}}
			}
		}
		return deathMsg{key: base, args: []deathArg{v, nameArg(by)}}
	}
	if credit := strings.TrimSpace(c.credit); credit != "" {
		if _, ok := deathMsgText[base+".player"]; ok {
			return deathMsg{key: base + ".player", args: []deathArg{v, nameArg(credit)}}
		}
	}
	return deathMsg{key: base, args: []deathArg{v}}
}

// deathMsg is a death message as vanilla builds it: a translation key and
// its arguments (victim, killer or credit, weapon).
type deathMsg struct {
	key  string
	args []deathArg
}

// deathArg is one argument: the English it reads as, and its component.
type deathArg struct {
	plain string
	comp  attachproto.Text
}

// textArg is a literal argument — the victim's name, a player's.
func textArg(s string) deathArg { return deathArg{plain: s, comp: attachproto.Text{Text: s}} }

// nameArg is a killer's or credit's name: a mob's (the English type name the
// causes carry) is its type's translatable name, entity.minecraft.<id>, as
// Entity.getDisplayName gives an unnamed mob; anyone else's is literal.
func nameArg(s string) deathArg {
	if key, ok := mobNameKeys[s]; ok {
		return deathArg{plain: s, comp: attachproto.Text{Translate: key, Fallback: s}}
	}
	return textArg(s)
}

// weaponArg is a named weapon: ItemStack.getDisplayName, its name in square
// brackets.
func weaponArg(s string) deathArg {
	return deathArg{plain: s, comp: attachproto.Text{Translate: "chat.square_brackets", With: []attachproto.Text{{Text: s}}}}
}

// english is the message in vanilla's English.
func (d deathMsg) english() string {
	var a [3]string
	for i := 0; i < len(d.args) && i < len(a); i++ {
		a[i] = d.args[i].plain
	}
	return format(deathMsgText[d.key], a[0], a[1], a[2])
}

// component is the message as a translatable component, with the English
// as its fallback.
func (d deathMsg) component() attachproto.Text {
	t := attachproto.Text{Translate: d.key, Fallback: d.english()}
	for _, a := range d.args {
		t.With = append(t.With, a.comp)
	}
	return t
}

// chat is the message as a chat line: the component, with the English as
// the plain text renderers without components show.
func (d deathMsg) chat() attachproto.Chat {
	c := chatEv(d.english())
	if raw, err := json.Marshal(d.component()); err == nil {
		c.Component = raw
	}
	return c
}

// mobNameKeys maps the English type name a death cause carries for a mob
// ("Cave Spider") to its translation key (entity.minecraft.cave_spider).
var mobNameKeys = func() map[string]string {
	m := map[string]string{}
	for et, n := range advEntityName {
		if name := strings.TrimPrefix(n, "minecraft:"); name != "" {
			m[mobDisplayName(et)] = "entity.minecraft." + name
		}
	}
	return m
}()

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
