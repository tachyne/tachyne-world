package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The message is vanilla's own text for the damage TYPE, with the killer's
// name where vanilla puts one — including the three that used to read "died"
// because no hand-written cause was ever passed for them.
func TestDeathMessagesNameTheCause(t *testing.T) {
	cases := []struct {
		cause deathCause
		want  string
	}{
		{deathCause{dt: dtGeneric}, "Wesley died"},
		{deathCause{dt: dtPlayerAttack, by: "EdgeZA"}, "Wesley was slain by EdgeZA"},
		{deathCause{dt: dtMobAttack, by: "Cave Spider"}, "Wesley was slain by Cave Spider"},
		{deathCause{dt: dtArrow, by: "EdgeZA"}, "Wesley was shot by EdgeZA"},
		{deathCause{dt: dtFall}, "Wesley hit the ground too hard"},
		{deathCause{dt: dtStalagmite}, "Wesley was impaled on a stalagmite"},
		{deathCause{dt: dtFallingStalactite}, "Wesley was skewered by a falling stalactite"},
		{deathCause{dt: dtLava}, "Wesley tried to swim in lava"},
		{deathCause{dt: dtDrown}, "Wesley drowned"},
		{deathCause{dt: dtCactus}, "Wesley was pricked to death"},
		{deathCause{dt: dtExplosion}, "Wesley blew up"},
		// An explosion something is to blame for is its own vanilla type.
		{deathCause{dt: dtPlayerExplosion, by: "Creeper"}, "Wesley was blown up by Creeper"},
		{deathCause{dt: dtLightningBolt}, "Wesley was struck by lightning"},
		// The three that were dead code: no cause was ever passed for them.
		{deathCause{dt: dtStarve}, "Wesley starved to death"},
		{deathCause{dt: dtOutOfWorld}, "Wesley fell out of the world"},
		{deathCause{dt: dtWither}, "Wesley withered away"},
		// And the families the hand-written switch never had at all.
		{deathCause{dt: dtFallingAnvil}, "Wesley was squashed by a falling anvil"},
		{deathCause{dt: dtFireworks}, "Wesley went off with a bang"},
		{deathCause{dt: dtCramming}, "Wesley was squished too much"},
		{deathCause{dt: dtInWall}, "Wesley suffocated in a wall"},
		{deathCause{dt: dtFlyIntoWall}, "Wesley experienced kinetic energy"},
		{deathCause{dt: dtSonicBoom, by: "Warden"}, "Wesley was obliterated by a sonically-charged shriek"},
		{deathCause{dt: dtBadRespawnPoint}, "Wesley was killed by [Intentional Game Design]"},
		// A named weapon is named, and a death with nobody to blame still
		// credits whoever the victim was fighting.
		{deathCause{dt: dtPlayerAttack, by: "EdgeZA", weapon: "Excalibur"},
			"Wesley was slain by EdgeZA using Excalibur"},
		{deathCause{dt: dtCactus, credit: "Zombie"},
			"Wesley walked into a cactus while trying to escape Zombie"},
		{deathCause{dt: dtDrown, credit: "Drowned"},
			"Wesley drowned while trying to escape Drowned"},
	}
	for _, c := range cases {
		if got := deathMessage("Wesley", c.cause); got != c.want {
			t.Errorf("%+v → %q, want %q", c.cause, got, c.want)
		}
	}
}

// Every damage type must render something other than the fallback, or a
// player dies to it and learns nothing.
func TestEveryDamageTypeHasAMessage(t *testing.T) {
	for dt := range dmgTypeMsgID {
		if dmgTypeMsgID[dt] == "" {
			t.Errorf("%s has no message id", dmgTypeNames[dt])
			continue
		}
		if got := deathMessage("Wesley", deathCause{dt: dmgType(dt)}); got == "Wesley died" &&
			dmgTypeNames[dt] != "generic" && dmgTypeNames[dt] != "generic_kill" {
			t.Errorf("%s falls back to the generic message", dmgTypeNames[dt])
		}
	}
}

// Registry names become readable ones.
func TestMobDisplayNames(t *testing.T) {
	if got := mobDisplayName(entityCaveSpider); got != "Cave Spider" {
		t.Errorf("cave spider reads %q", got)
	}
	if got := mobDisplayName(entityZombie); got != "Zombie" {
		t.Errorf("zombie reads %q", got)
	}
}

// The cause rides with the damage, survives to the death, and resets on
// respawn — the three things that make attribution work at all.
func TestDeathCauseRidesWithTheDamage(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}

	h.hurtBy(players, pl, 3, dtLava, deathCause{})
	if pl.lastCause.dt != dtLava {
		t.Fatal("the cause did not stick to the player")
	}
	// The LAST hit wins, as in vanilla.
	h.hurtBy(players, pl, 3, dtCactus, deathCause{})
	if pl.lastCause.dt != dtCactus {
		t.Error("an older cause outlived a newer one")
	}
	h.hurtBy(players, pl, 100, dtPlayerAttack, deathCause{by: "EdgeZA"})
	if !pl.dead {
		t.Fatal("the killing blow did not land")
	}
	if got := deathMessage(pl.p.name, pl.lastCause); got != pl.p.name+" was slain by EdgeZA" {
		t.Errorf("death message %q", got)
	}
	h.respawn(pl)
	if pl.lastCause != (deathCause{}) {
		t.Errorf("a respawned player still carries their last death's cause: %+v", pl.lastCause)
	}
}
