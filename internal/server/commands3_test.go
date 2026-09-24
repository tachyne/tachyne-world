package server

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Tests for the third command batch (advcmd.go, attrcmd.go, recipecmd.go,
// tagcmd.go, ridecmd.go, damagecmd.go, spreadcmd.go, forcecmd.go,
// spawncmd.go, utilcmds.go): each drives the hub-side handler and checks the
// state it changed, not only the feedback.

// cmdChats drains a player's queue and returns the chat lines in it.
func cmdChats(p *player) []string {
	var out []string
	for _, ev := range drainEvs(p) {
		if c, ok := ev.(attachproto.Chat); ok {
			out = append(out, c.Text)
		}
	}
	return out
}

// wantChat fails unless one of the lines is want.
func wantChat(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, l := range lines {
		if l == want {
			return
		}
	}
	t.Errorf("no %q among %q", want, lines)
}

// cmdHub is a hub with one survival player (eid 1, "tester") at y=180.
func cmdHub() (*hub, *tracked, map[int32]*tracked) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.adv = advState{}
	pl.rbKnown, pl.rbHighlight = map[int32]bool{}, map[int32]bool{}
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	return h, pl, players
}

// a second player for the commands that count targets.
func cmdSecondPlayer(players map[int32]*tracked, eid int32, name string) *tracked {
	t := &tracked{p: newPlayer(eid, name, [16]byte{}), gamemode: gmSurvival}
	initSurvival(t)
	t.x, t.y, t.z = 3.5, 180, 0.5
	t.adv = advState{}
	t.rbKnown, t.rbHighlight = map[int32]bool{}, map[int32]bool{}
	players[eid] = t
	return t
}

func TestAdvancementCommand(t *testing.T) {
	h, pl, players := cmdHub()
	stone := advByID["minecraft:story/mine_stone"]
	h.applyAdvancementCommand(players, evAdvancementCmd{by: 1, grant: true, target: "@s", mode: "only", adv: stone.id})
	if !pl.adv.done(stone) {
		t.Fatal("grant only left Stone Age undone")
	}
	evs := drainEvs(pl.p)
	sawProgress := false
	for _, ev := range evs {
		if p, ok := ev.(attachproto.AdvProgress); ok {
			for _, e := range p.Entries {
				if e.ID == stone.id {
					sawProgress = true
				}
			}
		}
	}
	if !sawProgress {
		t.Error("the grant sent the client no progress for it")
	}
	// A second grant changes nothing and says so.
	h.applyAdvancementCommand(players, evAdvancementCmd{by: 1, grant: true, target: "@s", mode: "only", adv: stone.id})
	wantChat(t, cmdChats(pl.p), "Couldn't grant advancement [Stone Age] to tester as they already have it")

	// through: the ancestors, the node and its subtree.
	h.applyAdvancementCommand(players, evAdvancementCmd{by: 1, grant: true, target: "@s", mode: "through", adv: stone.id})
	set := advCommandSet("through", stone.id)
	if set[0].id != "minecraft:story/root" || len(set) < 3 {
		t.Fatalf("through set %d long, starting %s", len(set), set[0].id)
	}
	for _, n := range set {
		if !pl.adv.done(n) {
			t.Errorf("through left %s undone", n.id)
		}
	}
	// The count is the set's size, as vanilla counts it, not how many changed.
	wantChat(t, cmdChats(pl.p), "Granted "+strconv.Itoa(len(set))+" advancements to tester")

	// Revoke takes it back and re-sends the tree (a reset snapshot).
	h.applyAdvancementCommand(players, evAdvancementCmd{by: 1, target: "@s", mode: "only", adv: stone.id})
	if pl.adv.done(stone) || len(pl.adv[stone.id]) != 0 {
		t.Fatal("revoke left progress behind")
	}
	reset := false
	for _, ev := range drainEvs(pl.p) {
		if p, ok := ev.(attachproto.AdvProgress); ok && p.Reset {
			reset = true
		}
	}
	if !reset {
		t.Error("a revoke should re-send the whole tree")
	}

	// A single criterion.
	h.applyAdvancementCommand(players, evAdvancementCmd{by: 1, grant: true, target: "@s", mode: "only", adv: stone.id, crit: "get_stone"})
	if !pl.adv.done(stone) {
		t.Error("granting the only criterion should complete it")
	}
	wantChat(t, cmdChats(pl.p), "Granted criterion 'get_stone' of advancement [Stone Age] to tester")

	// everything counts every advancement.
	h.applyAdvancementCommand(players, evAdvancementCmd{by: 1, target: "@s", mode: "everything"})
	if len(pl.adv) != 0 {
		t.Errorf("revoke everything left %d advancements", len(pl.adv))
	}
}

func TestAttributeCommand(t *testing.T) {
	h, pl, players := cmdHub()
	id := attr.MaxHealth
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: id, op: "base set", value: 10})
	if pl.maxHP() != 10 || pl.health != 10 {
		t.Fatalf("max health %v, health %v: want both 10", pl.maxHP(), pl.health)
	}
	lines := cmdChats(pl.p)
	wantChat(t, lines, "The base value for attribute Max Health for entity tester set to 10.0")
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: id, op: "modifier add", mod: "minecraft:bonus", value: 4, modOp: attr.AddValue})
	if pl.maxHP() != 14 {
		t.Fatalf("a +4 modifier gave max health %v", pl.maxHP())
	}
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: id, op: "modifier add", mod: "minecraft:bonus", value: 4, modOp: attr.AddValue})
	wantChat(t, cmdChats(pl.p), "Modifier minecraft:bonus is already present on attribute Max Health for entity tester")
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: id, op: "get"})
	wantChat(t, cmdChats(pl.p), "The value of attribute Max Health for entity tester is 14.0")
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: id, op: "modifier remove", mod: "minecraft:bonus"})
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@s", id: id, op: "base reset"})
	if pl.maxHP() != maxHealth {
		t.Errorf("reset left max health %v", pl.maxHP())
	}
	// The change went to the client at once.
	sawFrame := false
	for _, ev := range drainEvs(pl.p) {
		if _, ok := ev.(attachproto.EntityAttributes); ok {
			sawFrame = true
		}
	}
	if !sawFrame {
		t.Error("no attribute frame followed the change")
	}

	// A mob's MOVEMENT_SPEED reads back in vanilla's units, not per-step.
	m := h.spawnMob(players, entityZombie, 4.5, 180, 4.5)
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=zombie]", id: attr.MovementSpeed, op: "base get"})
	wantChat(t, cmdChats(pl.p), "The base value of attribute Speed for entity Zombie is "+jDouble(fromEngine(speedFor(entityZombie), attrToStep)))
	h.spawnMob(players, entityCow, 8.5, 180, 8.5)
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=cow]", id: attr.MovementSpeed, op: "base get"})
	wantChat(t, cmdChats(pl.p), "The base value of attribute Speed for entity Cow is 0.2") // 0.09 per step
	h.applyAttributeCommand(players, evAttributeCmd{by: 1, target: "@e[type=zombie]", id: attr.MovementSpeed, op: "base set", value: 0.46})
	if got := m.mobAttrs().Get(attr.MovementSpeed).Base(); math.Abs(got-0.46*attrToStep) > 1e-9 {
		t.Errorf("zombie speed base %v in engine units, want %v", got, 0.46*attrToStep)
	}
}

func TestRecipeCommand(t *testing.T) {
	h, pl, players := cmdHub()
	id := recipeIDByName["acacia_fence"]
	h.applyRecipeCommand(players, evRecipeCmd{by: 1, give: true, target: "@s", ids: []int32{id}})
	if !pl.rbKnown[id] || !pl.rbHighlight[id] {
		t.Fatal("give did not unlock the recipe")
	}
	wantChat(t, cmdChats(pl.p), "Unlocked 1 recipe(s) for tester")
	h.applyRecipeCommand(players, evRecipeCmd{by: 1, give: true, target: "@s", ids: []int32{id}})
	wantChat(t, cmdChats(pl.p), "No new recipes were learned")

	h.applyRecipeCommand(players, evRecipeCmd{by: 1, target: "@s", ids: []int32{id}})
	if pl.rbKnown[id] {
		t.Fatal("take left the recipe known")
	}
	replaced := false
	for _, ev := range drainEvs(pl.p) {
		if rb, ok := ev.(attachproto.RecipeBook); ok && rb.Replace {
			replaced = true
		}
	}
	if !replaced {
		t.Error("take should re-send the book whole")
	}
	// Holding the ingredients again does not bring a taken recipe back.
	var ings []int32
	for item, ids := range rbIngredientIndex {
		for _, r := range ids {
			if r == id {
				ings = append(ings, item)
			}
		}
	}
	h.recipeUnlocks(pl, ings)
	if pl.rbKnown[id] {
		t.Error("the ingredient poll handed back a taken recipe")
	}
	// It persists.
	store := newRecipeBookStore(filepath.Join(t.TempDir(), "rb.json"))
	store.save("tester", pl)
	back := testTracked()
	newRecipeBookStore(store.path).loadInto(back, "tester")
	if !back.rbTaken[id] {
		t.Error("the taken set did not survive a save")
	}
}

func TestTagCommandAndSelector(t *testing.T) {
	h, pl, players := cmdHub()
	other := cmdSecondPlayer(players, 2, "other")
	h.applyTagCommand(players, evTagCmd{by: 1, target: "@s", op: "add", name: "red"})
	if !pl.tags["red"] || other.tags["red"] {
		t.Fatal("add tagged the wrong players")
	}
	wantChat(t, cmdChats(pl.p), "Added tag 'red' to tester")
	h.applyTagCommand(players, evTagCmd{by: 1, target: "@s", op: "add", name: "red"})
	wantChat(t, cmdChats(pl.p), "Target either already has the tag or has too many tags")

	// The selector reads the tags.
	if got := h.commandTargets(players, 1, "@a[tag=red]"); len(got) != 1 || got[0] != pl {
		t.Errorf("@a[tag=red] picked %d", len(got))
	}
	if got := h.commandTargets(players, 1, "@a[tag=!red]"); len(got) != 1 || got[0] != other {
		t.Errorf("@a[tag=!red] picked %d", len(got))
	}
	if got := h.commandTargets(players, 1, "@a[tag=]"); len(got) != 1 || got[0] != other {
		t.Errorf("@a[tag=] picked %d", len(got))
	}
	m := h.spawnMob(players, entityPig, 4.5, 180, 4.5)
	h.applyTagCommand(players, evTagCmd{by: 1, target: "@e[type=pig]", op: "add", name: "pet"})
	if got := h.commandMobs(players, 1, "@e[tag=pet]"); len(got) != 1 || got[0] != m {
		t.Errorf("@e[tag=pet] picked %d mobs", len(got))
	}
	h.applyTagCommand(players, evTagCmd{by: 1, target: "@a", op: "list"})
	wantChat(t, cmdChats(pl.p), "tester has 1 tag(s): red")

	// Tags persist: a player's in the inventory store, a mob's with the mob.
	inv := newInvStore(filepath.Join(t.TempDir(), "inv.json"))
	inv.save("tester", pl)
	back := testTracked()
	newInvStore(inv.path).loadInto(back, "tester")
	if !back.tags["red"] {
		t.Error("the player's tag did not survive a save")
	}
	sm := toSavedMob(m)
	if len(sm.Tags) != 1 || sm.Tags[0] != "pet" {
		t.Errorf("saved mob tags %v", sm.Tags)
	}
	if r := h.reloadMob(players, &sm); r == nil || !r.tags["pet"] {
		t.Error("the mob's tag did not come back on reload")
	}

	h.applyTagCommand(players, evTagCmd{by: 1, target: "@s", op: "remove", name: "red"})
	if pl.tags["red"] {
		t.Error("remove kept the tag")
	}
}

func TestRideCommand(t *testing.T) {
	h, pl, players := cmdHub()
	pig := h.spawnMob(players, entityPig, 20.5, 180, 0.5)
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@s", vehicle: "@e[type=pig]"})
	if pl.ridingEID != pig.eid || pig.rider != pl.p.eid {
		t.Fatalf("player not seated: riding %d, pig rider %d", pl.ridingEID, pig.rider)
	}
	wantChat(t, cmdChats(pl.p), "tester started riding Pig")
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@s", vehicle: "@e[type=pig]"})
	wantChat(t, cmdChats(pl.p), "tester is already riding Pig")
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@s"})
	if pl.ridingEID != 0 || pig.rider != 0 {
		t.Fatal("dismount left the player aboard")
	}
	wantChat(t, cmdChats(pl.p), "tester stopped riding Pig")

	// Mob on mob, and the loop guard.
	z := h.spawnMob(players, entityZombie, 6.5, 180, 0.5)
	c := h.spawnMob(players, entityChicken, 8.5, 180, 0.5)
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@e[type=zombie]", vehicle: "@e[type=chicken]"})
	if z.mount != c.eid || c.mobRider != z.eid {
		t.Fatalf("zombie not on the chicken: mount %d, rider %d", z.mount, c.mobRider)
	}
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@e[type=chicken]", vehicle: "@e[type=zombie]"})
	wantChat(t, cmdChats(pl.p), "Can't mount entity on itself or any of its passengers")
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@e[type=pig]", vehicle: "@s"})
	wantChat(t, cmdChats(pl.p), "Players can't be ridden")
	h.applyRideCommand(players, evRideCmd{by: 1, target: "@e[type=zombie]"})
	if z.mount != 0 || c.mobRider != 0 {
		t.Error("the zombie did not get down")
	}
}

func TestDamageCommand(t *testing.T) {
	h, pl, players := cmdHub()
	pl.health = 20
	h.applyDamageCommand(players, evDamageCmd{by: 1, target: "@s", amount: 5, dt: dtGeneric})
	if pl.health != 15 {
		t.Fatalf("health %v after 5 generic, want 15", pl.health)
	}
	wantChat(t, cmdChats(pl.p), "Applied 5.0 damage to tester")
	pl.gamemode = gmCreative
	h.applyDamageCommand(players, evDamageCmd{by: 1, target: "@s", amount: 5, dt: dtGeneric})
	wantChat(t, cmdChats(pl.p), "Target is invulnerable to the given damage type")
	if pl.health != 15 {
		t.Error("a creative player took damage")
	}
	m := h.spawnMob(players, entityPig, 4.5, 180, 4.5)
	before := m.health
	h.applyDamageCommand(players, evDamageCmd{by: 1, target: "@e[type=pig]", amount: 3, dt: dmgTypeByName["fall"]})
	if m.health != before-3 {
		t.Errorf("pig health %d → %d, want 3 less", before, m.health)
	}
	if dmgTypeByName["out_of_world"] != dtOutOfWorld {
		t.Error("damage types resolve by the wrong name")
	}
}

func TestSpreadPlayersCommand(t *testing.T) {
	h, pl, players := cmdHub()
	other := cmdSecondPlayer(players, 2, "other")
	h.world.ForceLoad(0, 0, 3)
	h.applySpreadCommand(players, evSpreadCmd{by: 1, cx: 0.5, cz: 0.5, spread: 6, maxR: 24, target: "@a"})
	lines := cmdChats(pl.p)
	if len(lines) == 0 || !strings.HasPrefix(lines[len(lines)-1], "Spread 2 entity/entities around 0.5, 0.5 with an average distance of ") {
		t.Fatalf("feedback %q", lines)
	}
	for _, p := range []*tracked{pl, other} {
		if math.Abs(p.x-0.5) > 25 || math.Abs(p.z-0.5) > 25 {
			t.Errorf("%s landed outside the range at %.1f,%.1f", p.p.name, p.x, p.z)
		}
		if p.x != math.Floor(p.x)+0.5 || p.z != math.Floor(p.z)+0.5 {
			t.Errorf("%s not centred on a block: %.2f,%.2f", p.p.name, p.x, p.z)
		}
		below := h.world.At(floorInt(p.x), int(p.y)-1, floorInt(p.z))
		if !worldgen.IsSolid(below) || h.world.At(floorInt(p.x), int(p.y), floorInt(p.z)) != worldgen.Air {
			t.Errorf("%s stands on %d at y=%v", p.p.name, below, p.y)
		}
	}
	if d := math.Hypot(pl.x-other.x, pl.z-other.z); d < 5 {
		t.Errorf("the two landed %.1f apart, closer than the spread", d)
	}
}

func TestForceLoadCommand(t *testing.T) {
	h, pl, players := cmdHub()
	h.applyForceLoadCommand(players, evForceLoadCmd{by: 1, op: "add", x0: 0, z0: 0, x1: 20, z1: 5})
	if !h.world.Forced(0, 0) || !h.world.Forced(1, 0) || h.world.Forced(2, 0) {
		t.Fatal("add forced the wrong chunks")
	}
	wantChat(t, cmdChats(pl.p), "Marked 2 chunks in minecraft:overworld from [0, 0] to [1, 0] to be force loaded")
	if len(h.rules.Forced) != 2 {
		t.Errorf("settings hold %d forced chunks", len(h.rules.Forced))
	}
	h.applyForceLoadCommand(players, evForceLoadCmd{by: 1, op: "query", x0: 17, z0: 3})
	wantChat(t, cmdChats(pl.p), "Chunk at [1, 0] in minecraft:overworld is marked for force loading")
	h.applyForceLoadCommand(players, evForceLoadCmd{by: 1, op: "list"})
	wantChat(t, cmdChats(pl.p), "2 force loaded chunks were found in minecraft:overworld at: [0, 0], [1, 0]")
	h.applyForceLoadCommand(players, evForceLoadCmd{by: 1, op: "add", x0: 0, z0: 0, x1: 300, z1: 300})
	wantChat(t, cmdChats(pl.p), "Too many chunks in the specified area (maximum 256, but specified 361)")

	// A restart re-pins what was saved.
	h2 := newHub(world.New(1))
	h2.rules.Forced = h.rules.Forced
	h2.restoreForced()
	if !h2.world.Forced(1, 0) {
		t.Error("the saved forced chunks were not restored")
	}
	h.applyForceLoadCommand(players, evForceLoadCmd{by: 1, op: "remove", x0: 0, z0: 0})
	if h.world.Forced(0, 0) || !h.world.Forced(1, 0) {
		t.Error("remove took the wrong chunk")
	}
	wantChat(t, cmdChats(pl.p), "Unmarked chunk [0, 0] in minecraft:overworld for force loading")
	h.applyForceLoadCommand(players, evForceLoadCmd{by: 1, op: "remove all"})
	if h.world.ForcedCount() != 0 || len(h.rules.Forced) != 0 {
		t.Error("remove all left chunks forced")
	}
}

// /setworldspawn and /defaultgamemode survive a restart: the settings file
// they write is read back, and the saved values beat the boot flags.
func TestWorldSpawnAndDefaultGamemodePersist(t *testing.T) {
	h, pl, players := cmdHub()
	h.rulesPath = filepath.Join(t.TempDir(), "settings.json")
	modes := newModeStore("", gmSurvival)
	modes.pin("tester") // an existing player
	h.applySetWorldSpawn(players, evSetWorldSpawn{by: 1, x: 100, y: 70, z: -40, yaw: 90})
	if !h.hasWorldSpawn || h.worldSpawnX != 100.5 || h.worldSpawnY != 70 || h.worldSpawnZ != -39.5 {
		t.Fatalf("world spawn %v,%v,%v", h.worldSpawnX, h.worldSpawnY, h.worldSpawnZ)
	}
	evs := drainEvs(pl.p)
	sawSpawn := false
	for _, ev := range evs {
		if d, ok := ev.(attachproto.DefaultSpawn); ok && d.X == 100 && d.Z == -40 && d.Angle == 90 {
			sawSpawn = true
		}
	}
	if !sawSpawn {
		t.Error("clients were not told the new spawn")
	}
	h.applyDefaultGamemode(players, evDefaultGamemode{by: 1, mode: gmCreative, modes: modes})
	wantChat(t, cmdChats(pl.p), "The default game mode is now Creative Mode")
	if modes.get("newcomer") != gmCreative || modes.get("tester") != gmSurvival {
		t.Error("the default should move new players only")
	}

	// Boot: a fresh server with a -spawn flag and -gamemode survival.
	s := New()
	s.world = world.New(1)
	s.hub = newHub(s.world)
	s.hub.rulesPath = h.rulesPath
	s.modes = newModeStore("", gmSurvival)
	s.SpawnSet, s.SpawnX, s.SpawnY, s.SpawnZ = true, 5, 64, 5
	s.hub.loadRules()
	if !s.restoreWorldSpawn() {
		t.Fatal("no saved spawn")
	}
	if x, y, z := s.joinSpawn(); x != 100.5 || y != 70 || z != -39.5 {
		t.Errorf("a joining player arrives at %v,%v,%v", x, y, z)
	}
	if gm := s.hub.rules.DefaultGamemode; gm == nil || *gm != gmCreative {
		t.Error("the default game mode was not saved")
	}
	h.applySetWorldSpawn(map[int32]*tracked{}, evSetWorldSpawn{dim: dimNether})
	if h.worldSpawnX != 100.5 {
		t.Error("a Nether /setworldspawn moved the overworld spawn")
	}
}

func TestRandomSwingTeamMsg(t *testing.T) {
	h, pl, players := cmdHub()
	other := cmdSecondPlayer(players, 2, "other")
	for i := 0; i < 20; i++ {
		if v := h.applyRandomCommand(players, evRandomCmd{by: 1, name: "tester", min: 1, max: 6}); v < 1 || v > 6 {
			t.Fatalf("rolled %d outside 1..6", v)
		}
	}
	if lines := cmdChats(other.p); len(lines) != 0 {
		t.Errorf("value spoke to others: %q", lines)
	}
	cmdChats(pl.p)
	v := h.applyRandomCommand(players, evRandomCmd{by: 1, name: "tester", min: 1, max: 6, announce: true})
	wantChat(t, cmdChats(other.p), "tester rolled "+strconv.Itoa(v)+" (from 1 to 6)")
	if _, _, msg := parseIntRange("3..1"); msg != "Min cannot be bigger than max" {
		t.Errorf("swapped range: %q", msg)
	}
	if lo, hi, msg := parseIntRange("..6"); msg != "" || lo != math.MinInt32 || hi != 6 {
		t.Errorf("..6 parsed %d..%d %q", lo, hi, msg)
	}

	// /swing: the swinger's own client sees it too.
	h.applySwingCommand(players, evSwingCmd{by: 1, target: "@s", hand: 1})
	swung := false
	for _, ev := range drainEvs(pl.p) {
		if s, ok := ev.(attachproto.Swing); ok && s.EID == pl.p.eid && s.Hand == 1 {
			swung = true
		}
	}
	if !swung {
		t.Error("no off-hand swing reached the swinger")
	}

	// /teammsg goes to the team only.
	mate := cmdSecondPlayer(players, 3, "mate")
	h.sb.Teams["red"] = &sbTeam{Title: "Reds", Members: map[string]bool{"tester": true, "mate": true}}
	cmdChats(other.p)
	cmdChats(mate.p)
	h.applyTeamMsg(players, evTeamMsg{from: 1, text: "hello"})
	wantChat(t, cmdChats(mate.p), "[Reds] [tester] hello")
	wantChat(t, cmdChats(pl.p), "-> [Reds] [tester] hello")
	if lines := cmdChats(other.p); len(lines) != 0 {
		t.Errorf("a player off the team heard it: %q", lines)
	}
	h.applyTeamMsg(players, evTeamMsg{from: 2, text: "hi"})
	wantChat(t, cmdChats(other.p), "You must be on a team to message your team")
}

func TestJDouble(t *testing.T) {
	for v, want := range map[float64]string{20: "20.0", 0.1: "0.1", 0.0001: "1.0E-4", 1e7: "1.0E7", -2.5: "-2.5"} {
		if got := jDouble(v); got != want {
			t.Errorf("jDouble(%v) = %q, want %q", v, got, want)
		}
	}
}
