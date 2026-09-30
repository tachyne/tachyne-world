package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Stat criteria through the dispatcher: an objective on a stat follows every
// awardStat, and one naming a stat that does not exist is refused.
func TestScoreboardStatCriteria(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"scoreboard objectives add jumps minecraft.custom:minecraft.jump",
		"scoreboard objectives add stone minecraft.mined:minecraft.stone",
		"scoreboard objectives add pigs killed:pig",
		"scoreboard objectives add bogus minecraft.custom:minecraft.no_such_stat",
		"scoreboard objectives add red teamkill.red",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "ST1")
	if !hasLine(linesBetween(logs["alice"], "", "ST1"), "Unknown criterion 'minecraft.custom:minecraft.no_such_stat'") {
		t.Errorf("a bogus stat was not refused: %q", linesBetween(logs["alice"], "", "ST1"))
	}
	onHub(t, h, func() {
		if o := h.sb.Objectives["pigs"]; o == nil || o.Criteria != "minecraft.killed:minecraft.pig" {
			t.Errorf("killed:pig stored as %+v, want the canonical name", o)
		}
		if h.sb.Objectives["bogus"] != nil || h.sb.Objectives["red"] == nil {
			t.Error("criteria validation")
		}
		var at *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "alice" {
				at = tr
			}
		}
		if at == nil {
			t.Error("no alice")
			return
		}
		h.incCustom(at, "jump", 2)
		reg, _ := statBlockReg(worldgen.BlockBase("stone"))
		h.incStat(at, attachproto.StatMined, reg, 1)
		h.incStat(at, attachproto.StatKilled, int32(entityByName["pig"]), 3)
		if got := h.sb.Scores["alice"]; got["jumps"] != 2 || got["stone"] != 1 || got["pigs"] != 3 {
			t.Errorf("stat scores %v", got)
		}
	})
}

// handleTeamKill: the killer's teamkill.<victim's colour>, the victim's
// killedByTeam.<killer's colour>; a team without a colour counts for neither.
func TestScoreboardTeamKillCriteria(t *testing.T) {
	h := newTestHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	sb := func(args ...string) { h.cmdScoreboard(players, evScoreboardCmd{p: pl.p, args: args}) }
	tm := func(args ...string) { h.cmdTeam(players, evTeamCmd{p: pl.p, args: args}) }
	sb("objectives", "add", "tkred", "teamkill.red")
	sb("objectives", "add", "kbblue", "killedByTeam.blue")
	tm("add", "red")
	tm("add", "blue")
	tm("add", "plain")
	tm("modify", "red", "color", "red")
	tm("modify", "blue", "color", "blue")
	tm("join", "red", "victim")
	tm("join", "blue", "killer")
	h.sbTeamKill(players, "killer", "victim")
	if h.sb.Scores["killer"]["tkred"] != 1 || h.sb.Scores["victim"]["kbblue"] != 1 {
		t.Errorf("team kill scores %v", h.sb.Scores)
	}
	tm("join", "plain", "victim")
	h.sbTeamKill(players, "killer", "victim")
	if h.sb.Scores["killer"]["tkred"] != 1 {
		t.Error("a colourless team counted a team kill")
	}
}

// players get / operation / display, objectives modify.
func TestScoreboardPlayersOperationGetDisplay(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"scoreboard objectives add a dummy",
		"scoreboard objectives add b dummy",
		"scoreboard players set alice a 7",
		"scoreboard players set bob b 3",
		"scoreboard players operation alice a %= bob b",            // 1
		"scoreboard players operation alice a += bob b",            // 4
		"scoreboard players operation alice a >< bob b",            // alice 3, bob 4
		"scoreboard players operation @a[name=alice] a *= alice a", // 9: one score on both sides
		"scoreboard players operation alice a /= carol b",          // carol's b is 0: refused
		"scoreboard players operation alice a ** bob b",
		"scoreboard players get alice a",
		"scoreboard players get carol a",
		`scoreboard players display name alice a "Ally"`,
		"scoreboard players display numberformat alice a fixed \"hi\"",
		`scoreboard objectives modify a numberformat styled {"color":"red","bold":true}`,
		`scoreboard objectives modify a displayname "Alpha"`,
		"scoreboard objectives modify a rendertype hearts",
		"scoreboard objectives modify a displayautoupdate true",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "OP1")
	lines := linesBetween(logs["alice"], "", "OP1")
	for _, want := range []string{
		"Cannot divide by zero",
		"Invalid operation",
		"alice has 9 [a]",
		"Can't get value of a for carol; none is set",
		"Changed the display name of a to [Alpha]",
	} {
		if !hasLine(lines, want) {
			t.Errorf("missing %q in %q", want, lines)
		}
	}
	onHub(t, h, func() {
		if h.sb.Scores["alice"]["a"] != 9 || h.sb.Scores["bob"]["b"] != 4 {
			t.Errorf("scores after the operations: %v", h.sb.Scores)
		}
		if _, made := h.sb.Scores["carol"]["b"]; !made {
			t.Error("an operation's source score is created (getOrCreatePlayerScore)")
		}
		x := h.sb.Extras["alice"]["a"]
		if x == nil || x.Display != "Ally" || x.NumberFormat == nil || x.NumberFormat.Kind != "fixed" || x.NumberFormat.Fixed != "hi" {
			t.Errorf("score extras %+v", x)
		}
		o := h.sb.Objectives["a"]
		if o.Title != "Alpha" || !o.Hearts || !o.AutoUpdate {
			t.Errorf("objective after modify: %+v", o)
		}
		if f := o.NumberFormat; f == nil || f.Kind != "styled" || f.Color != "red" || !f.Bold {
			t.Errorf("objective number format %+v", f)
		} else if fr := o.frame("a", attachproto.ObjUpdate); fr.Format == nil || fr.Format.Color != "red" {
			t.Errorf("the objective frame does not carry the format: %+v", fr)
		}
		if sc := h.sbScoreFrame("alice", "a", 9); sc.Format == nil || sc.Format.Fixed != "hi" {
			t.Errorf("the score frame does not carry its format: %+v", sc)
		}
	})
}

// OperationArgument's arithmetic: floorDiv and floorMod, not truncation.
func TestScoreboardOperationArithmetic(t *testing.T) {
	for _, c := range []struct {
		op         string
		a, b, want int32
	}{
		{"/=", -7, 2, -4}, {"/=", 7, 2, 3}, {"%=", -7, 3, 2}, {"%=", 7, -3, -2}, {"%=", 6, 3, 0},
		{"<", 4, 9, 4}, {">", 4, 9, 9}, {"=", 4, 9, 9}, {"-=", 4, 9, -5},
	} {
		if got, _, bad := sbOperation(c.op, c.a, c.b); bad != "" || got != c.want {
			t.Errorf("%d %s %d = %d (%q), want %d", c.a, c.op, c.b, got, bad, c.want)
		}
	}
	if a, b, _ := sbOperation("><", 1, 2); a != 2 || b != 1 {
		t.Error("swap")
	}
}

// /team empty, leave, a quoted display name, and PlayerTeam's defaults.
func TestTeamEmptyLeaveAndDefaults(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		`team add red "Red Team"`,
		"team join red @a",
		"team empty red",
		"team empty red",
		"team add blue",
		"team join blue bob",
		"team leave bob",
		"team leave bob",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "TE1")
	lines := linesBetween(logs["alice"], "", "TE1")
	for _, want := range []string{
		"Created team [Red Team]",
		"Added 3 members to team [Red Team]",
		"Removed 3 member(s) from team [Red Team]",
		"Nothing changed. That team is already empty",
		"Removed bob from any team",
		"Removed 0 members from any team",
	} {
		if !hasLine(lines, want) {
			t.Errorf("missing %q in %q", want, lines)
		}
	}
	onHub(t, h, func() {
		red := h.sb.Teams["red"]
		if red == nil || red.Title != "Red Team" || len(red.Members) != 0 || !red.FriendlyFire || !red.SeeInvisible {
			t.Errorf("red team %+v", red)
		}
	})
}

// A team's collision rule keeps its mobs from shoving each other (and
// from counting in each other's crowd), as EntitySelector.pushableBy does.
func TestTeamCollisionRuleStopsPushing(t *testing.T) {
	h, players := pushWorld(t)
	a := putMob(t, h, players, entityCow, 100.5, 70, 100.5)
	b := putMob(t, h, players, entityCow, 100.7, 70, 100.5)
	pl := testTracked()
	tm := func(args ...string) { h.cmdTeam(players, evTeamCmd{p: pl.p, args: args}) }
	tm("add", "herd")
	tm("join", "herd", uuidString(a.uuid))
	tm("join", "herd", uuidString(b.uuid))
	tm("modify", "herd", "collisionRule", "pushOwnTeam")
	h.pushMobs(players)
	if a.pushX != 0 || b.pushX != 0 {
		t.Errorf("teammates under pushOwnTeam were shoved: %.4f %.4f", a.pushX, b.pushX)
	}
	tm("modify", "herd", "collisionRule", "always")
	h.pushMobs(players)
	if a.pushX == 0 && b.pushX == 0 {
		t.Error("under always the pair should shove")
	}
	for _, c := range []struct {
		own, their string
		rule       string
		want       bool
	}{
		{"herd", "", "never", false}, {"herd", "herd", "pushOtherTeams", true},
		{"herd", "", "pushOtherTeams", false}, {"herd", "", "pushOwnTeam", true},
	} {
		h.sb.Teams["herd"].Collision = sbCollisionIDs[c.rule]
		if got := h.teamPushAllowed(c.own, c.their); got != c.want {
			t.Errorf("%s: %q pushing %q = %v, want %v", c.rule, c.own, c.their, got, c.want)
		}
	}
}
