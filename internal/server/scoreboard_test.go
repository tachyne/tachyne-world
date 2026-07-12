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
