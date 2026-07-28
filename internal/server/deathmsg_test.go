package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The message says what happened, and who did it when there is a who.
func TestDeathMessagesNameTheCause(t *testing.T) {
	cases := []struct {
		cause deathCause
		want  string
	}{
		{deathCause{}, "Wesley died"},
		{deathCause{key: causePlayer, by: "EdgeZA"}, "Wesley was slain by EdgeZA"},
		{deathCause{key: causeMob, by: "Cave Spider"}, "Wesley was slain by Cave Spider"},
		{deathCause{key: causeArrow, by: "EdgeZA"}, "Wesley was shot by EdgeZA"},
		{deathCause{key: causeArrow}, "Wesley was shot"},
		{deathCause{key: causeFall}, "Wesley fell from a high place"},
		{deathCause{key: causeStalagmite}, "Wesley was impaled on a stalagmite"},
		{deathCause{key: causeLava}, "Wesley tried to swim in lava"},
		{deathCause{key: causeDrown}, "Wesley drowned"},
		{deathCause{key: causeCactus}, "Wesley was pricked to death"},
		{deathCause{key: causeExplosion}, "Wesley blew up"},
		{deathCause{key: causeLightning}, "Wesley was struck by lightning"},
	}
	for _, c := range cases {
		if got := deathMessage("Wesley", c.cause); got != c.want {
			t.Errorf("%+v → %q, want %q", c.cause, got, c.want)
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

	h.hurtBy(players, pl, 3, dtLava, deathCause{key: causeLava})
	if pl.lastCause.key != causeLava {
		t.Fatal("the cause did not stick to the player")
	}
	// The LAST hit wins, as in vanilla.
	h.hurtBy(players, pl, 3, dtCactus, deathCause{key: causeCactus})
	if pl.lastCause.key != causeCactus {
		t.Error("an older cause outlived a newer one")
	}
	h.hurtBy(players, pl, 100, dtPlayerAttack, deathCause{key: causePlayer, by: "EdgeZA"})
	if !pl.dead {
		t.Fatal("the killing blow did not land")
	}
	if got := deathMessage(pl.p.name, pl.lastCause); got != pl.p.name+" was slain by EdgeZA" {
		t.Errorf("death message %q", got)
	}
	h.respawn(pl)
	if pl.lastCause.key != causeGeneric {
		t.Error("a respawned player still carries their last death's cause")
	}
}
