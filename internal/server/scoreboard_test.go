package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
)

func drainSB(pl *tracked) (objs []attachproto.Objective, scores []attachproto.Score, teams []attachproto.Team, slots []attachproto.DisplaySlot) {
	for {
		select {
		case pkt := <-pl.p.out:
			switch v := pkt.ev.(type) {
			case attachproto.Objective:
				objs = append(objs, v)
			case attachproto.Score:
				scores = append(scores, v)
			case attachproto.Team:
				teams = append(teams, v)
			case attachproto.DisplaySlot:
				slots = append(slots, v)
			}
		default:
			return
		}
	}
}

// TestScoreboardCommandsAndCriteria drives the op-command surface and the
// automatic criteria end to end.
func TestScoreboardCommandsAndCriteria(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl

	h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: []string{"objectives", "add", "kills", "totalKillCount", "Kills"}})
	h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: []string{"objectives", "setdisplay", "sidebar", "kills"}})
	objs, _, _, slots := drainSB(pl)
	if len(objs) != 1 || objs[0].Name != "kills" || objs[0].Title != "Kills" || objs[0].Method != attachproto.ObjAdd {
		t.Fatalf("objective frames: %+v", objs)
	}
	if len(slots) != 1 || slots[0].Slot != attachproto.SlotSidebar || slots[0].Objective != "kills" {
		t.Fatalf("display frames: %+v", slots)
	}

	// the kill criteria feeds the objective
	h.sbCriteria(players, "totalKillCount", "tester", 1, false)
	h.sbCriteria(players, "totalKillCount", "tester", 1, false)
	_, scores, _, _ := drainSB(pl)
	if len(scores) != 2 || scores[1].Value != 2 {
		t.Fatalf("criteria scores: %+v", scores)
	}
	if h.sb.Scores["tester"]["kills"] != 2 {
		t.Fatal("state not updated")
	}

	// unchanged gauge values stay silent
	h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: []string{"objectives", "add", "hp", "health"}})
	drainSB(pl)
	h.sbCriteria(players, "health", "tester", 20, true)
	h.sbCriteria(players, "health", "tester", 20, true)
	if _, scores, _, _ := drainSB(pl); len(scores) != 1 {
		t.Fatalf("gauge dedup failed: %+v", scores)
	}

	// teams: create, join (also leaves other teams), color
	h.cmdTeam(players, evTeamCmd{p: pl.p, args: []string{"add", "red", "Red Team"}})
	h.cmdTeam(players, evTeamCmd{p: pl.p, args: []string{"join", "red", "tester"}})
	h.cmdTeam(players, evTeamCmd{p: pl.p, args: []string{"modify", "red", "color", "red"}})
	_, _, teams, _ := drainSB(pl)
	if len(teams) != 3 || teams[1].Method != attachproto.TeamAddPlayers || teams[1].Players[0] != "tester" {
		t.Fatalf("team frames: %+v", teams)
	}
	if teams[2].Color != 12 {
		t.Fatalf("color frame: %+v", teams[2])
	}

	h.cmdTeam(players, evTeamCmd{p: pl.p, args: []string{"add", "blue"}})
	h.cmdTeam(players, evTeamCmd{p: pl.p, args: []string{"join", "blue", "tester"}})
	if h.sb.Teams["red"].Members["tester"] || !h.sb.Teams["blue"].Members["tester"] {
		t.Fatal("join should switch teams")
	}

	// join sync replays the whole board
	fresh := testTracked()
	h.sbSendAll(fresh)
	objs, scores, teams, slots = drainSB(fresh)
	if len(objs) != 2 || len(slots) != 1 || len(teams) != 2 {
		t.Fatalf("join sync: %d objs %d slots %d teams", len(objs), len(slots), len(teams))
	}
	if len(scores) != 2 { // kills + hp for tester
		t.Fatalf("join scores: %+v", scores)
	}
}

// The sixteen team-colour sidebars are display slots 3-18 (DisplaySlot
// order), and a joining player is sent them with the rest.
func TestScoreboardTeamSidebarSlots(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: []string{"objectives", "add", "reds", "dummy"}})
	h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: []string{"objectives", "setdisplay", "sidebar.team.red", "reds"}})
	h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: []string{"objectives", "setdisplay", "sidebar.team.white", "reds"}})
	_, _, _, slots := drainSB(pl)
	if len(slots) != 2 || slots[0].Slot != 15 || slots[1].Slot != 18 {
		t.Fatalf("team sidebar frames: %+v, want slots 15 and 18", slots)
	}
	if h.sb.Display[15] != "reds" || h.sb.Display[18] != "reds" {
		t.Fatalf("stored display: %v", h.sb.Display)
	}
}

// /trigger through the dispatcher: any player may use it, but only on a
// trigger objective an operator enabled for them, and only once per enable.
func TestTriggerCommand(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice, carol := ps["alice"], ps["carol"]

	s.handleCommand(alice, "scoreboard objectives add bonus trigger Bonus")
	s.handleCommand(alice, "scoreboard objectives add kills dummy")
	s.handleCommand(carol, "trigger bonus")
	s.handleCommand(carol, "trigger kills")
	s.handleCommand(alice, "scoreboard players enable carol bonus")
	s.handleCommand(carol, "trigger bonus set 5")
	s.handleCommand(carol, "trigger bonus")
	s.handleCommand(alice, "scoreboard players enable carol bonus")
	s.handleCommand(carol, "trigger bonus add 2")
	settle(t, h, logs, "T1")
	c := linesBetween(logs["carol"], "", "T1")
	for _, want := range []string{
		"You cannot trigger this objective yet",
		"You can only trigger objectives that are 'trigger' type",
		"Triggered [Bonus] (set value to 5)",
		"Triggered [Bonus] (added 2 to value)",
	} {
		if !hasLine(c, want) {
			t.Errorf("missing %q in %q", want, c)
		}
	}
	n := 0
	for _, l := range c {
		if l == "You cannot trigger this objective yet" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("the trigger should refuse twice (before enable, and after use), refused %d times", n)
	}
	var score int32
	onHub(t, h, func() { score = h.sb.Scores["carol"]["bonus"] })
	if score != 7 {
		t.Errorf("carol's bonus is %d, want 7", score)
	}
}

// The gauge criteria follow the player (food, air, armor, xp, level), are
// read-only to /scoreboard players set, and the death criterion is vanilla's
// deathCount.
func TestScoreboardGaugeCriteria(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.food, pl.xpLevel = 13, 4
	players := map[int32]*tracked{1: pl}
	cmd := func(args ...string) { h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: args}) }
	cmd("objectives", "add", "hunger", "food")
	cmd("objectives", "add", "lvl", "level")
	cmd("objectives", "add", "d", "deaths")
	cmd("objectives", "add", "d", "deathCount")
	h.sbGauges(players)
	if got := h.sb.Scores[pl.p.name]["hunger"]; got != 13 {
		t.Errorf("food score %d, want 13", got)
	}
	if got := h.sb.Scores[pl.p.name]["lvl"]; got != 4 {
		t.Errorf("level score %d, want 4", got)
	}
	if o := h.sb.Objectives["d"]; o == nil || o.Criteria != "deathCount" {
		t.Errorf("the death objective is %+v, want deathCount (\"deaths\" is not vanilla)", o)
	}
	cmd("players", "set", pl.p.name, "hunger", "20")
	if got := h.sb.Scores[pl.p.name]["hunger"]; got != 13 {
		t.Errorf("a read-only gauge was set to %d", got)
	}
}

// deathMessageVisibility: hideForOtherTeams keeps a death to the team,
// hideForOwnTeam keeps it from them, never from everyone.
func TestTeamDeathMessageVisibility(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	cmd := func(args ...string) { h.cmdTeam(players, evTeamCmd{p: pl.p, args: args}) }
	cmd("add", "red")
	cmd("join", "red", "ann")
	cmd("join", "red", "ben")
	for _, c := range []struct {
		vis            string
		mate, outsider bool
	}{{"always", true, true}, {"hideForOtherTeams", true, false}, {"hideForOwnTeam", false, true}, {"never", false, false}} {
		cmd("modify", "red", "deathMessageVisibility", c.vis)
		if got := h.deathMessageReaches("ann", "ben"); got != c.mate {
			t.Errorf("%s: a teammate reads it %v, want %v", c.vis, got, c.mate)
		}
		if got := h.deathMessageReaches("ann", "cal"); got != c.outsider {
			t.Errorf("%s: an outsider reads it %v, want %v", c.vis, got, c.outsider)
		}
	}
	cmd("modify", "red", "seeFriendlyInvisibles", "true")
	if !h.sb.Teams["red"].SeeInvisible {
		t.Error("seeFriendlyInvisibles was not set")
	}
}
