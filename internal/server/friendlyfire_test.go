package server

import (
	"encoding/json"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Player.canHarmPlayer through Player.hurtServer: a teammate's blow does
// nothing while the team has friendly fire off, lands once it is on, and a
// player on another team (or none) is always hurt.
func TestFriendlyFireGatesTeammates(t *testing.T) {
	h := newTestHub(world.New(1))
	h.tick.Store(1000) // past every hurt cooldown the fixtures start with
	if h.sb == nil {
		h.sb, _ = newScoreboard("")
	}
	victim, mate, stranger := survPlayer(h), survPlayer(h), survPlayer(h)
	victim.p.name, mate.p.name, stranger.p.name = "victim", "mate", "stranger"
	mate.p.eid, stranger.p.eid = 2, 3
	players := map[int32]*tracked{victim.p.eid: victim, mate.p.eid: mate, stranger.p.eid: stranger}
	team := &sbTeam{Title: "Reds", Color: -1, Members: map[string]bool{"victim": true, "mate": true}}
	h.sb.Teams["red"] = team

	hit := func(by *tracked) bool {
		victim.health, victim.hurtAt, victim.lastHurt = 20, 0, 0
		return h.hurtFrom(players, victim, 2, dtPlayerAttack, deathCause{by: by.p.name, byEID: by.p.eid}, from(0, 0))
	}
	team.FriendlyFire = false
	if hit(mate) {
		t.Error("a teammate hurt the victim with friendly fire off")
	}
	if !hit(stranger) {
		t.Error("a player off the team could not hurt the victim")
	}
	team.FriendlyFire = true
	if !hit(mate) {
		t.Error("a teammate could not hurt the victim with friendly fire on")
	}
}

// Teams save their friendly-fire and see-invisible options inverted: a
// team saved before (old "ff"/"seeinvis" keys, absent when false) comes
// back with vanilla's defaults, and an option turned off survives a save.
func TestTeamOptionsSaveAsVanillaDefaults(t *testing.T) {
	var old sbTeam
	if err := json.Unmarshal([]byte(`{"title":"Old","color":-1,"members":{"a":true}}`), &old); err != nil {
		t.Fatal(err)
	}
	if !old.FriendlyFire || !old.SeeInvisible || old.Title != "Old" || !old.Members["a"] {
		t.Errorf("an old team loaded as %+v, want vanilla's defaults on", old)
	}
	off := sbTeam{Title: "Off", Color: -1, FriendlyFire: false, SeeInvisible: true}
	b, err := json.Marshal(off)
	if err != nil {
		t.Fatal(err)
	}
	var back sbTeam
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.FriendlyFire || !back.SeeInvisible || back.Title != "Off" {
		t.Errorf("round trip %s gave %+v", b, back)
	}
}
