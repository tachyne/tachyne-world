package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Leads: tie a mob to yourself, walk it, tie it to a fence, cut it loose.

func leashWorld(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	airBox(t, h.world, -8, 70, -8, 40, 90, 40)
	return h, players
}

// leashPlayer registers a survival player at a spot, with an inventory.
func leashPlayer(t *testing.T, h *hub, players map[int32]*tracked, x, y, z float64) *tracked {
	t.Helper()
	eid := h.allocEID()
	tr := &tracked{p: newPlayer(eid, "tester", [16]byte{}), gamemode: gmSurvival, x: x, y: y, z: z}
	initSurvival(tr)
	players[eid] = tr
	return tr
}

// giveHeld puts a stack in the player's selected hotbar slot.
func giveHeld(t *testing.T, tr *tracked, item int32, count int) {
	t.Helper()
	tr.inv.slots[tr.p.heldSlot()] = invStack{item: item, count: count}
}

// leashCow puts a cow at a spot with its AI quiet.
func leashCow(t *testing.T, h *hub, players map[int32]*tracked, x, y, z float64) *mob {
	t.Helper()
	m := h.spawnMob(players, entityCow, x, y, z)
	if m == nil {
		t.Fatal("no cow")
	}
	m.x, m.y, m.z, m.rest = x, y, z, 1<<20
	return m
}

func TestLeadTiesAndUntiesAMob(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)

	giveHeld(t, tr, itemLead, 1)
	if !h.tryLeash(players, tr, m) {
		t.Fatal("a lead on a cow did not tie it")
	}
	if m.leash != tr.p.eid {
		t.Errorf("leash holder %d, want the player %d", m.leash, tr.p.eid)
	}
	if got := heldStack(tr).count; got != 0 {
		t.Errorf("%d leads left in hand, want the one to be spent", got)
	}
	// Clicking again unties, and the lead comes back as an item.
	before := len(h.items)
	if !h.tryLeash(players, tr, m) {
		t.Fatal("clicking a mob you are holding did not untie it")
	}
	if m.leash != 0 {
		t.Errorf("still leashed to %d", m.leash)
	}
	if len(h.items) != before+1 {
		t.Error("untying did not drop the lead back")
	}
}

// Mob.canBeLeashed: hostiles refuse, and so do the water animals vanilla
// excludes — but not the ones that override back to true.
func TestWhatWillAndWillNotTakeALead(t *testing.T) {
	cases := []struct {
		name  string
		etype int
		want  bool
	}{
		{"a cow", entityCow, true},
		{"a wolf", entityWolf, true},
		{"a dolphin", entityDolphin, true},  // overrides canBeLeashed to true
		{"an axolotl", entityAxolotl, true}, // likewise
		{"a zombie", entityZombie, false},   // Enemy
		{"a squid", entitySquid, false},     // WaterAnimal
		{"a turtle", entityTurtle, false},   // overrides to false
		{"a cod", entityCod, false},         // WaterAnimal
		{"a pufferfish", entityPufferfish, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, players := leashWorld(t)
			m := h.spawnMob(players, c.etype, 0.5, 70, 0.5)
			if m == nil {
				t.Skip("species not spawnable here")
			}
			if got := canBeLeashed(m); got != c.want {
				t.Errorf("canBeLeashed = %v, want %v", got, c.want)
			}
		})
	}
}

// Leashable.tickLeash: past the elastic distance the mob is pulled after its
// holder; inside it the lead is slack and the mob is left alone.
func TestALeadPullsOnlyOnceItIsTaut(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 3.5, 70, 0.5) // 3 blocks: slack
	h.setLeash(players, m, tr.p.eid)

	m.vx, m.vz = 0, 0
	h.updateLeashes(players)
	if m.vx != 0 || m.vz != 0 {
		t.Errorf("a slack lead pulled the cow (%.4f, %.4f)", m.vx, m.vz)
	}

	m.x = 9.5 // 9 blocks: past the 6-block elastic distance, inside the 12 snap
	h.updateLeashes(players)
	if m.vx >= 0 {
		t.Errorf("taut lead pulled x by %.4f; the holder is at -x so it should be negative", m.vx)
	}
	if m.leash == 0 {
		t.Error("the lead snapped at 9 blocks; vanilla snaps at 12")
	}
}

// LEASH_TOO_FAR_DIST: past 12 the lead breaks and drops.
func TestALeadSnapsWhenStretchedTooFar(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 3.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)

	m.x = 0.5 + leashSnapDist + 1
	before := len(h.items)
	h.updateLeashes(players)
	if m.leash != 0 {
		t.Errorf("still leashed %.0f blocks away", leashSnapDist+1)
	}
	if len(h.items) != before+1 {
		t.Error("a snapped lead did not drop as an item")
	}
}

// The holder logging out drops the lead rather than leaving a mob tethered to
// a ghost — vanilla's canInteractWithLevel check.
func TestALeadDropsWhenItsHolderLeaves(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)

	delete(players, tr.p.eid) // logged out
	h.updateLeashes(players)
	if m.leash != 0 {
		t.Error("the cow is still tied to a player who left")
	}
}

// LeadItem.bindPlayerMobs: clicking a fence moves everything you are towing
// onto a knot there, and the knot is a real entity the client can draw to.
func TestTyingToAFenceMakesAKnot(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	a := leashCow(t, h, players, 1.5, 70, 0.5)
	b := leashCow(t, h, players, 1.5, 70, 1.5)
	h.setLeash(players, a, tr.p.eid)
	h.setLeash(players, b, tr.p.eid)

	fence := blockPos{2, 70, 2}
	lo, _ := worldgen.BlockRange("oak_fence")
	h.world.SetBlock(fence.x, fence.y, fence.z, lo)

	if !h.leashToFence(players, tr, fence) {
		t.Fatal("clicking a fence while towing did not tie anything")
	}
	if len(h.knots) != 1 {
		t.Fatalf("%d knots, want 1", len(h.knots))
	}
	var kid int32
	for id := range h.knots {
		kid = id
	}
	if a.leash != kid || b.leash != kid {
		t.Errorf("cows tied to %d and %d, want the knot %d", a.leash, b.leash, kid)
	}
	if a.leashPos != fence {
		t.Errorf("recorded knot block %v, want %v", a.leashPos, fence)
	}
}

// Break the fence and the knot goes with it, dropping what it held.
func TestBreakingTheFenceCutsTheLeashes(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)

	fence := blockPos{2, 70, 2}
	lo, _ := worldgen.BlockRange("oak_fence")
	h.world.SetBlock(fence.x, fence.y, fence.z, lo)
	h.leashToFence(players, tr, fence)

	h.setBlockAt(players, 0, fence, worldgen.Air)
	if m.leash != 0 {
		t.Error("the cow is still tied to a fence that is gone")
	}
	if len(h.knots) != 0 {
		t.Errorf("%d knots left behind", len(h.knots))
	}
}

// A knot with nothing on it is discarded, as vanilla discards one whose last
// leash goes.
func TestTheKnotGoesWhenTheLastLeadDoes(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	a := leashCow(t, h, players, 1.5, 70, 0.5)
	b := leashCow(t, h, players, 1.5, 70, 1.5)
	h.setLeash(players, a, tr.p.eid)
	h.setLeash(players, b, tr.p.eid)
	fence := blockPos{2, 70, 2}
	lo, _ := worldgen.BlockRange("oak_fence")
	h.world.SetBlock(fence.x, fence.y, fence.z, lo)
	h.leashToFence(players, tr, fence)

	h.dropLeash(players, a, true)
	if len(h.knots) != 1 {
		t.Error("the knot went while it was still holding the other cow")
	}
	h.dropLeash(players, b, true)
	if len(h.knots) != 0 {
		t.Error("an empty knot was left behind")
	}
}

// Only #minecraft:fences takes a lead — not walls, not fence gates.
func TestOnlyRealFencesTakeALead(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"oak_fence", true},
		{"nether_brick_fence", true},
		{"bamboo_fence", true},
		{"pale_oak_fence", true},
		{"oak_fence_gate", false},
		{"cobblestone_wall", false},
	}
	for _, c := range cases {
		lo, hi, ok := worldgen.BlockRangeOK(c.name)
		if !ok {
			t.Errorf("%s is not a known block", c.name)
			continue
		}
		if got := isFence(lo); got != c.want {
			t.Errorf("isFence(%s) = %v, want %v", c.name, got, c.want)
		}
		if got := isFence(hi); got != c.want {
			t.Errorf("isFence(%s, last state) = %v, want %v", c.name, got, c.want)
		}
	}
}

// A lead tied to a fence survives a restart; the knot is rebuilt from the mob
// that names it.
func TestAFenceLeashSurvivesARestart(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)
	fence := blockPos{2, 70, 2}
	lo, _ := worldgen.BlockRange("oak_fence")
	h.world.SetBlock(fence.x, fence.y, fence.z, lo)
	h.leashToFence(players, tr, fence)

	saved := toSavedMob(m)
	if saved.LeashPos == nil {
		t.Fatal("a fence leash was not persisted")
	}
	if *saved.LeashPos != [3]int{fence.x, fence.y, fence.z} {
		t.Errorf("persisted %v, want %v", *saved.LeashPos, fence)
	}

	// A fresh world with the same fence: the mob comes back tied to a rebuilt knot.
	h2, players2 := leashWorld(t)
	h2.world.SetBlock(fence.x, fence.y, fence.z, lo)
	m2 := leashCow(t, h2, players2, 1.5, 70, 0.5)
	h2.restoreLeash(players2, m2, saved.LeashPos)
	if m2.leash == 0 {
		t.Fatal("the leash did not come back")
	}
	if len(h2.knots) != 1 {
		t.Errorf("%d knots rebuilt, want 1", len(h2.knots))
	}
}

// …but not if the fence is gone by the time the world loads.
func TestARestoredLeashNeedsItsFenceStillThere(t *testing.T) {
	h, players := leashWorld(t)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.restoreLeash(players, m, &[3]int{2, 70, 2}) // nothing there
	if m.leash != 0 {
		t.Error("a leash was restored to a fence that no longer exists")
	}
	if len(h.knots) != 0 {
		t.Error("a knot was made in mid-air")
	}
}

// A player-held lead is deliberately not persisted: it is already dropped by
// the time the world saves, because the holder logging out cuts it.
func TestAPlayerHeldLeashIsNotPersisted(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)
	if got := toSavedMob(m); got.LeashPos != nil {
		t.Errorf("persisted %v for a player-held lead, want nothing", *got.LeashPos)
	}
}

// You cannot take a mob off another player's lead, but you can take one off a
// fence (vanilla only guards the player case).
func TestYouCannotStealAMobOffAnotherPlayersLead(t *testing.T) {
	h, players := leashWorld(t)
	owner := leashPlayer(t, h, players, 0.5, 70, 0.5)
	thief := leashPlayer(t, h, players, 1.0, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, owner.p.eid)

	giveHeld(t, thief, itemLead, 1)
	if h.tryLeash(players, thief, m) {
		t.Error("a mob was taken off another player's lead")
	}
	if m.leash != owner.p.eid {
		t.Errorf("holder is now %d, want the original owner %d", m.leash, owner.p.eid)
	}
}

// LeashFenceKnotEntity.interact: clicking a knot with nothing in tow takes its
// mobs back onto your own lead — how a fence full of animals gets collected.
func TestClickingAKnotTakesTheMobsBack(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)
	fence := blockPos{2, 70, 2}
	lo, _ := worldgen.BlockRange("oak_fence")
	h.world.SetBlock(fence.x, fence.y, fence.z, lo)
	h.leashToFence(players, tr, fence)

	var k *leashKnot
	for _, kk := range h.knots {
		k = kk
	}
	if !h.interactKnot(players, tr, k, false) {
		t.Fatal("clicking the knot did nothing")
	}
	if m.leash != tr.p.eid {
		t.Errorf("holder %d, want the player %d back on the lead", m.leash, tr.p.eid)
	}
	if len(h.knots) != 0 {
		t.Error("the emptied knot was left behind")
	}
}

// …and sneaking suppresses that, so you can walk past without collecting.
func TestSneakingDoesNotCollectFromAKnot(t *testing.T) {
	h, players := leashWorld(t)
	tr := leashPlayer(t, h, players, 0.5, 70, 0.5)
	m := leashCow(t, h, players, 1.5, 70, 0.5)
	h.setLeash(players, m, tr.p.eid)
	fence := blockPos{2, 70, 2}
	lo, _ := worldgen.BlockRange("oak_fence")
	h.world.SetBlock(fence.x, fence.y, fence.z, lo)
	h.leashToFence(players, tr, fence)

	var k *leashKnot
	for _, kk := range h.knots {
		k = kk
	}
	kid := k.eid
	h.interactKnot(players, tr, k, true)
	if m.leash != kid {
		t.Error("sneaking still collected the cow off the fence")
	}
}

// A hostile species refuses a lead no matter how it was spawned — the flag
// that says "this mob is hunting you" is set by the spawn path, so leashing
// must be decided by the species itself.
func TestHostilesRefuseALeadHoweverTheyWereSpawned(t *testing.T) {
	h, players := leashWorld(t)
	m := h.spawnMob(players, entityZombie, 0.5, 70, 0.5) // bare spawn: no hostile flag
	if m == nil {
		t.Fatal("no zombie")
	}
	if m.hostile {
		t.Fatal("this test needs a zombie whose runtime flag is unset")
	}
	if canBeLeashed(m) {
		t.Error("a zombie took a lead because its hostile flag happened to be false")
	}
}
