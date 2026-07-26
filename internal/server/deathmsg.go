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

// deathCause is what last hurt a player: a message key, and the name of
// whoever or whatever dealt it (empty for the impersonal ones).
type deathCause struct {
	key string
	by  string
}

// The message keys. These mirror vanilla's death.attack.* translation keys,
// with the same split between the plain form and the "_player" form used when
// something is to blame.
const (
	causeGeneric    = ""
	causePlayer     = "player"     // another player's melee
	causeMob        = "mob"        // a mob's melee
	causeArrow      = "arrow"      // shot
	causeFall       = "fall"       // fell from a high place
	causeStalagmite = "stalagmite" // impaled
	causeLava       = "lava"       // tried to swim in lava
	causeFire       = "fire"       // went up in flames
	causeDrown      = "drown"      // drowned
	causeStarve     = "starve"     // starved to death
	causeCactus     = "cactus"     // pricked to death
	causeExplosion  = "explosion"  // blew up
	causeMagic      = "magic"      // killed by magic
	causeLightning  = "lightning"  // struck by lightning
	causeSweetBerry = "sweetBerry" // poked to death by a sweet berry bush
	causeDragon     = "dragon"     // the fight went badly
	causeVoid       = "void"       // fell out of the world
	causeWither     = "wither"     // withered away
	causeThorns     = "thorns"     // killed by the armour of whoever they attacked
)

// deathMessage renders the message for a death. The "by" form is used when
// there is something to name; otherwise the impersonal one.
func deathMessage(victim string, c deathCause) string {
	by := strings.TrimSpace(c.by)
	switch c.key {
	case causePlayer:
		if by != "" {
			return victim + " was slain by " + by
		}
		return victim + " was slain"
	case causeMob:
		if by != "" {
			return victim + " was slain by " + by
		}
		return victim + " was slain"
	case causeArrow:
		if by != "" {
			return victim + " was shot by " + by
		}
		return victim + " was shot"
	case causeFall:
		return victim + " fell from a high place"
	case causeStalagmite:
		return victim + " was impaled on a stalagmite"
	case causeLava:
		return victim + " tried to swim in lava"
	case causeFire:
		if by != "" {
			return victim + " was burnt to a crisp whilst fighting " + by
		}
		return victim + " went up in flames"
	case causeDrown:
		return victim + " drowned"
	case causeStarve:
		return victim + " starved to death"
	case causeCactus:
		return victim + " was pricked to death"
	case causeSweetBerry:
		return victim + " was poked to death by a sweet berry bush"
	case causeExplosion:
		if by != "" {
			return victim + " was blown up by " + by
		}
		return victim + " blew up"
	case causeMagic:
		if by != "" {
			return victim + " was killed by " + by + " using magic"
		}
		return victim + " was killed by magic"
	case causeLightning:
		return victim + " was struck by lightning"
	case causeDragon:
		if by != "" {
			return victim + " was slain by " + by
		}
		return victim + " was killed by the dragon"
	case causeThorns:
		if by != "" {
			return victim + " was killed whilst trying to hurt " + by
		}
		return victim + " was killed"
	case causeVoid:
		return victim + " fell out of the world"
	case causeWither:
		return victim + " withered away"
	}
	return victim + " died"
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
