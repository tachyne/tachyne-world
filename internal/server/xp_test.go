package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestXPCurveMatchesVanilla(t *testing.T) {
	// Spot checks against the wiki's leveling table.
	for _, c := range [][2]int{{0, 7}, {15, 37}, {16, 42}, {30, 112}, {31, 121}, {40, 202}} {
		if got := xpToNext(c[0]); got != c[1] {
			t.Fatalf("xpToNext(%d) = %d, want %d", c[0], got, c[1])
		}
	}
	for _, c := range [][2]int{{16, 352}, {31, 1507}} {
		if got := totalXP(c[0], 0); got != c[1] {
			t.Fatalf("totalXP(%d) = %d, want %d", c[0], got, c[1])
		}
	}
}

func TestAddXPRollsLevels(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.addXP(pl, 7) // exactly level 0's cost
	if pl.xpLevel != 1 || pl.xpPoints != 0 {
		t.Fatalf("after 7 points: level=%d points=%d", pl.xpLevel, pl.xpPoints)
	}
	h.addXP(pl, 8) // level 1 needs 9
	if pl.xpLevel != 1 || pl.xpPoints != 8 {
		t.Fatalf("after +8: level=%d points=%d", pl.xpLevel, pl.xpPoints)
	}
	h.addXP(pl, 1)
	if pl.xpLevel != 2 || pl.xpPoints != 0 {
		t.Fatalf("after +1: level=%d points=%d", pl.xpLevel, pl.xpPoints)
	}
}

func TestOrbPickupAndDeathScatter(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	pl.y = float64(h.world.SurfaceFeet(0, 0))
	players := map[int32]*tracked{1: pl}

	// An award is paid in ladder denominations, so ten points is a 7 and a 3.
	h.spawnXPOrb(players, 10, pl.x, pl.y, pl.z)
	if len(h.orbs) != 2 {
		t.Fatalf("ten points should split into two orbs, got %d", len(h.orbs))
	}
	for i := 0; i < 20 && len(h.orbs) > 0; i++ {
		h.updateOrbs(players) // one orb every other tick (takeXpDelay)
	}
	if len(h.orbs) != 0 || pl.xpLevel != 1 || pl.xpPoints != 3 {
		t.Fatalf("orb pickup: orbs=%d level=%d points=%d", len(h.orbs), pl.xpLevel, pl.xpPoints)
	}

	// Death scatters 7×level (capped) at the spot and zeroes the bar.
	pl.xpLevel, pl.xpPoints = 10, 4
	h.damageOf(players, pl, 1000, dtGeneric)
	if !pl.dead || pl.xpLevel != 0 || pl.xpPoints != 0 {
		t.Fatalf("death must zero XP: dead=%v level=%d", pl.dead, pl.xpLevel)
	}
	total := 0
	for _, o := range h.orbs {
		total += o.value * o.count
	}
	if total != 70 {
		t.Fatalf("death must drop 7×level=70 across its orbs, got %d", total)
	}
	// A dead player can't hoover the orbs back up.
	was := len(h.orbs)
	h.updateOrbs(players)
	if len(h.orbs) != was {
		t.Fatal("a dead player must not pick up orbs")
	}
}

func TestMobXPOnlyForPlayerKills(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	burned := h.spawnHostile(players, entityZombie, 30, 30)
	h.despawnMob(players, burned) // died to daylight — nobody earned anything
	if len(h.orbs) != 0 {
		t.Fatal("environment deaths must not pay XP")
	}

	pl.x, pl.z = 40.5, 40.5
	fought := h.spawnHostile(players, entityZombie, 41, 40)
	fought.y = pl.y
	h.attackMob(players, 1, fought.eid)
	h.despawnMob(players, fought)
	// A zombie is worth 5, which the ladder pays as a 3 and two 1s.
	total := 0
	for _, o := range h.orbs {
		total += o.value * o.count
	}
	if total != 5 {
		t.Fatalf("a player-hit mob's death must drop its 5 XP, got %d", total)
	}
}

func TestOreXPGatedToSurvivalMiner(t *testing.T) {
	h := newHub(world.New(1))
	if xpForBlock(worldgen.CoalOre, func(int) int { return 1 }) != 1 {
		t.Fatal("coal ore must pay XP")
	}
	if xpForBlock(worldgen.DiamondOre, func(int) int { return 0 }) != 3 {
		t.Fatal("diamond ore must pay at least 3")
	}
	if xpForBlock(worldgen.IronOre, func(int) int { return 1 }) != 0 {
		t.Fatal("iron pays at the furnace, not the pick (vanilla)")
	}
	if xpForBlock(worldgen.Stone, func(int) int { return 1 }) != 0 {
		t.Fatal("plain stone pays nothing")
	}
	_ = h
}

// An award is paid out in the ladder denominations, largest first, the way
// ExperienceOrb.awardWithDirection walks getExperienceValue down.
func TestOrbAwardSplitsDownTheLadder(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.spawnXPOrb(players, 100, 0.5, float64(h.world.SurfaceFeet(0, 0)), 0.5)
	got, total := map[int]int{}, 0
	for _, o := range h.orbs {
		got[o.value] += o.count
		total += o.value * o.count
	}
	if total != 100 {
		t.Fatalf("the split must preserve the award, got %d of 100", total)
	}
	// 100 = 73 + 17 + 7 + 3.
	for _, want := range []int{73, 17, 7, 3} {
		if got[want] == 0 {
			t.Fatalf("no orb of value %d in %v", want, got)
		}
	}
	if got[1] != 0 {
		t.Fatalf("the ladder should not need any 1s for 100: %v", got)
	}
}

// Orbs of the same value lying in the same spot collapse into one entity that
// stands for several, so a mob farm does not fill the world with orbs.
func TestOrbsMergeInPlace(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := float64(h.world.SurfaceFeet(0, 0))
	// Two orbs of equal value in the same group merge; force the group by
	// giving the second one the id the first's modulus asks for.
	h.spawnXPOrb(players, 7, 0.5, y, 0.5)
	var first *xpOrb
	for _, o := range h.orbs {
		first = o
	}
	second := &xpOrb{eid: first.eid + orbMergeGroups, dim: first.dim,
		x: first.x, y: first.y, z: first.z, value: first.value, count: 1, born: h.tick.Load()}
	h.orbs[second.eid] = second
	h.scanForOrbMerges(players, first)
	if len(h.orbs) != 1 {
		t.Fatalf("the two orbs should have merged, %d left", len(h.orbs))
	}
	if first.count != 2 {
		t.Fatalf("the survivor should stand for two orbs, got %d", first.count)
	}
	// It pays out both, one touch at a time.
	pl := testTracked()
	pl.x, pl.y, pl.z = first.x, first.y, first.z
	players[1] = pl
	for i := 0; i < 10 && len(h.orbs) > 0; i++ {
		h.updateOrbs(players)
	}
	if got := totalXP(pl.xpLevel, pl.xpPoints); got != 14 {
		t.Fatalf("a merged pair of 7s is worth 14, got %d", got)
	}
}

// A mob that dies wearing or holding something is worth more: vanilla adds
// 1-3 per equipped piece on top of the species reward, which is why a
// skeleton in full iron is worth several times a bare one.
func TestEquippedMobsPayMoreXP(t *testing.T) {
	always := func(n int) int { return 0 } // the low end of every 1+rand(3)

	bare := &mob{etype: entityZombie, hostile: true}
	if got := xpForMob(bare, always); got != hostileXP {
		t.Fatalf("a bare zombie is worth %d, want %d", got, hostileXP)
	}

	armed := &mob{etype: entityZombie, hostile: true, held: int32(itemByName["iron_sword"])}
	if got := xpForMob(armed, always); got != hostileXP+1 {
		t.Fatalf("a zombie with a sword is worth %d, want %d", got, hostileXP+1)
	}

	full := &mob{etype: entityZombie, hostile: true, held: int32(itemByName["iron_sword"])}
	for i := range full.gear {
		full.gear[i] = invStack{item: int32(itemByName["iron_helmet"]), count: 1}
	}
	if got := xpForMob(full, always); got != hostileXP+5 { // hand + four pieces
		t.Fatalf("a zombie in full iron is worth %d, want %d", got, hostileXP+5)
	}

	// Something worth nothing to begin with stays worth nothing: vanilla's
	// loop only runs when the base reward is above zero.
	golem := &mob{etype: entityIronGolem, held: int32(itemByName["iron_sword"])}
	if got := xpForMob(golem, always); got != 0 {
		t.Fatalf("an iron golem pays nothing whatever it holds, got %d", got)
	}
}

// /xp through the dispatcher: points by default, levels on request, set with
// the per-level cap, query, the /experience alias, and a lost point that
// drops a level.
func TestXPCommand(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice := ps["alice"]
	s.handleCommand(alice, "xp add bob 3 levels")
	s.handleCommand(alice, "xp add bob 5")
	s.handleCommand(alice, "experience query bob levels")
	s.handleCommand(alice, "xp query bob points")
	s.handleCommand(alice, "xp set bob 100 points")
	s.handleCommand(alice, "xp add bob -6")
	s.handleCommand(alice, "xp query bob levels")
	settle(t, h, logs, "X1")
	a := linesBetween(logs["alice"], "", "X1")
	for _, want := range []string{
		"Gave 3 experience levels to bob",
		"Gave 5 experience points to bob",
		"bob has 3 experience levels",
		"bob has 5 experience points",
		"Cannot set experience points above the maximum points for the player's current level",
		"Gave -6 experience points to bob",
		"bob has 2 experience levels",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
}

// /difficulty with no argument reports it; setting the same one says so.
func TestDifficultyQuery(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice := ps["alice"]
	s.handleCommand(alice, "difficulty hard")
	settle(t, h, logs, "D0")
	s.handleCommand(alice, "difficulty")
	s.handleCommand(alice, "difficulty hard")
	settle(t, h, logs, "D1")
	a := linesBetween(logs["alice"], "", "D1")
	for _, want := range []string{"The difficulty has been set to Hard", "The difficulty is Hard",
		"The difficulty did not change; it is already set to Hard"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
}

// /spawnpoint: operators only, for a named target, at a given position.
func TestSpawnpointCommand(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	onHub(t, h, func() { h.spawns = newSpawnStore(t.TempDir() + "/spawns.json") })
	alice, carol := ps["alice"], ps["carol"]
	s.handleCommand(carol, "spawnpoint")
	s.handleCommand(alice, "spawnpoint bob 10 70 -5")
	settle(t, h, logs, "S1")
	if !hasLine(linesBetween(logs["carol"], "", "S1"), "You don't have permission.") {
		t.Error("a non-operator set a spawn point")
	}
	if !hasLine(linesBetween(logs["alice"], "", "S1"), "Set spawn point to 10, 70, -5 [0.0, 0.0] in minecraft:overworld for bob") {
		t.Errorf("alice's reply: %q", linesBetween(logs["alice"], "", "S1"))
	}
	var pos blockPos
	var ok bool
	onHub(t, h, func() { pos, _, ok = h.spawns.get(h.playersRef[ps["bob"].eid].p.key()) })
	if !ok || pos != (blockPos{10, 70, -5}) {
		t.Fatalf("bob's spawn is %v (ok %v)", pos, ok)
	}
}

// /clear with an item and a maximum: count only at 0, take up to the
// maximum, then everything of that item; other items stay.
func TestClearItemAndMax(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	stone, dirt := int32(itemByName["stone"]), int32(itemByName["dirt"])
	pl.inv.slots[0] = invStack{item: stone, count: 10}
	pl.inv.slots[1] = invStack{item: stone, count: 5}
	pl.inv.slots[2] = invStack{item: dirt, count: 7}
	count := func(id int32) (n int) {
		for _, s := range pl.inv.slots {
			if s.item == id {
				n += s.count
			}
		}
		return n
	}
	h.onClearInv(players, evClearInv{by: pl.p, name: pl.p.name, item: stone, max: 0})
	if count(stone) != 15 {
		t.Fatal("a count-only clear removed stone")
	}
	h.onClearInv(players, evClearInv{by: pl.p, name: pl.p.name, item: stone, max: 12})
	if count(stone) != 3 || count(dirt) != 7 {
		t.Fatalf("after clearing 12 stone: stone %d dirt %d, want 3 and 7", count(stone), count(dirt))
	}
	h.onClearInv(players, evClearInv{by: pl.p, name: pl.p.name, item: stone, max: -1})
	if count(stone) != 0 || count(dirt) != 7 {
		t.Fatalf("clearing all stone left stone %d dirt %d", count(stone), count(dirt))
	}
}

// /playsound in vanilla's shape: the source picks the volume slider, a far
// target hears nothing unless minVolume carries it, and the replies match.
func TestPlaysoundCommand(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice := ps["alice"]
	s.handleCommand(alice, "playsound minecraft:block.note_block.harp block bob")
	s.handleCommand(alice, "playsound minecraft:block.note_block.harp master bob 5000 70 5000")
	settle(t, h, logs, "P1")
	a := linesBetween(logs["alice"], "", "P1")
	for _, want := range []string{"Played sound minecraft:block.note_block.harp to bob", "The sound is too far away to be heard"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
}
