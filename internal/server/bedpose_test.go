package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Clicking either end of a bed lies you down on the HEAD half. Anchoring at
// the foot is what left a sleeper's legs hanging off the end of the bed.
func TestSleepingAnchorsToTheBedHead(t *testing.T) {
	for _, clickHead := range []bool{false, true} {
		h, players, pl := bedSetup(t)
		foot, head := blockPos{4, 70, 4}, tBedHead
		h.dayTime.Store(sleepStart + 100) // night
		pl.x, pl.y, pl.z = float64(foot.x)+0.5, 70, float64(foot.z)+0.5

		click := foot
		if clickHead {
			click = head
		}
		h.handleUseBed(players, pl, click)

		if !pl.sleeping {
			t.Fatalf("clicking the %v end did not put the player to sleep", click)
		}
		if pl.sleepPos != head {
			t.Errorf("clicked %v: anchored at %v, want the head %v", click, pl.sleepPos, head)
		}
		if pl.z != float64(head.z)+0.5 {
			t.Errorf("clicked %v: lying at z=%v, want the head's %v", click, pl.z, float64(head.z)+0.5)
		}
	}
}

// LivingEntity.setPosToBed lifts the body clear of the mattress.
func TestSleeperSitsAtVanillaHeight(t *testing.T) {
	h, players, pl := bedSetup(t)
	head := tBedHead
	h.dayTime.Store(sleepStart + 100)
	h.handleUseBed(players, pl, head)

	want := float64(head.y) + 0.6875
	if math.Abs(pl.y-want) > 1e-9 {
		t.Errorf("sleeper at y=%v, want %v (setPosToBed)", pl.y, want)
	}
}

// The bed shows that someone is in it, and stops showing it when they get up.
func TestBedOccupiedTracksTheSleeper(t *testing.T) {
	h, players, pl := bedSetup(t)
	head := tBedHead
	// Somewhere to stand up into.
	stone, _ := worldgen.BlockRange("stone")
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			h.world.SetBlock(head.x+dx, head.y-1, head.z+dz, stone)
		}
	}
	h.dayTime.Store(sleepStart + 100)
	h.handleUseBed(players, pl, head)

	occupied := func() string {
		st := h.world.At(head.x, head.y, head.z)
		info, _ := worldgen.InfoForState(st)
		return worldgen.GetProperty(info, st, "occupied")
	}
	if occupied() != "true" {
		t.Error("the bed does not show as occupied while someone sleeps in it")
	}
	h.wakePlayer(players, pl)
	if occupied() != "false" {
		t.Error("the bed still shows as occupied after the sleeper got up")
	}
}

// Waking puts you BESIDE the bed, not in it.
func TestWakingStandsYouUpBesideTheBed(t *testing.T) {
	h, players, pl := bedSetup(t)
	head := tBedHead
	stone, _ := worldgen.BlockRange("stone")
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			h.world.SetBlock(head.x+dx, head.y-1, head.z+dz, stone)
		}
	}
	h.dayTime.Store(sleepStart + 100)
	h.handleUseBed(players, pl, head)
	h.wakePlayer(players, pl)

	bx, bz := float64(head.x)+0.5, float64(head.z)+0.5
	if pl.x == bx && pl.z == bz {
		t.Error("the woken player is still standing in the bed's own cell")
	}
	if math.Hypot(pl.x-bx, pl.z-bz) > 2.5 {
		t.Errorf("stood up at (%v,%v), too far from the bed at (%v,%v)", pl.x, pl.z, bx, bz)
	}
	if math.Abs(pl.y-float64(head.y)) > 1e-9 {
		t.Errorf("stood up at y=%v, want the floor beside the bed at %v", pl.y, head.y)
	}
}

// Half a bed is not something you can lie in.
func TestHalfABedCannotBeSleptIn(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.world.SetBlock(tBedHead.x, tBedHead.y, tBedHead.z, worldgen.Air) // break off the head
	h.dayTime.Store(sleepStart + 100)
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping {
		t.Error("a bed with no head half should not be sleepable")
	}
	if _, _, ok := h.spawns.get(pl.p.name); ok {
		t.Error("a half bed claimed a respawn point")
	}
}

// The monster check is vanilla's box: ±8 across, ±5 up and down. A zombie on
// the floor above used to keep you awake.
func TestMonsterCheckUsesTheVanillaBox(t *testing.T) {
	h, players, pl := bedSetup(t)
	head := tBedHead
	h.dayTime.Store(sleepStart + 100)

	m := h.spawnMob(players, entityZombie, float64(head.x)+0.5, float64(head.y)+7, float64(head.z)+0.5)
	if m == nil {
		t.Fatal("could not place the test zombie")
	}
	m.hostile = true
	h.handleUseBed(players, pl, head)
	if !pl.sleeping {
		t.Error("a monster 7 blocks overhead is outside vanilla's ±5 box and must not block sleep")
	}
}
