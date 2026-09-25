package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// strawBedSetup swaps bedSetup's white bed for a straw one in the same cells.
func strawBedSetup(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	h, players, pl := bedSetup(t)
	base := worldgen.BlockBase("straw_bed")
	info, ok := worldgen.InfoForState(base)
	if !ok {
		t.Fatal("straw_bed has no state layout")
	}
	foot := worldgen.SetProperty(info, base, "facing", "north")
	foot = worldgen.SetProperty(info, foot, "occupied", "false")
	foot = worldgen.SetProperty(info, foot, "part", "foot")
	h.world.SetBlock(4, 70, 4, foot)
	h.world.SetBlock(4, 70, 3, worldgen.SetProperty(info, foot, "part", "head"))
	return h, players, pl
}

// A straw bed lets you sleep in the overworld but never claims your spawn,
// counts its own stat, and is used up when you get out (StrawBedBlock with
// BedRule.DESTROY_ON_LEAVE).
func TestStrawBedIsUsedUpByANight(t *testing.T) {
	h, players, pl := strawBedSetup(t)
	h.dayTime.Store(13000)
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if !pl.sleeping {
		t.Fatal("a straw bed should be slept in at night")
	}
	if _, _, ok := h.spawns.get("tester"); ok {
		t.Fatal("a straw bed must not set the respawn point")
	}
	if customStat(pl, "sleep_in_straw_bed") != 1 || customStat(pl, "sleep_in_bed") != 0 {
		t.Fatalf("stats: straw %d bed %d", customStat(pl, "sleep_in_straw_bed"), customStat(pl, "sleep_in_bed"))
	}
	h.wakePlayer(players, pl)
	if pl.sleeping {
		t.Fatal("should be awake")
	}
	for _, c := range []blockPos{{4, 70, 4}, tBedHead} {
		if s := h.world.Block(c.x, c.y, c.z); s != worldgen.Air {
			t.Fatalf("straw bed cell %v should be gone after waking, got %d", c, s)
		}
	}
	if pl.y != 70 {
		t.Fatalf("sleeper should stand up beside the bed, got y=%v", pl.y)
	}
}

// Outside the overworld (DESTROY_ON_USE) a straw bed breaks when used: no
// explosion, no sleep, no spawn.
func TestStrawBedBreaksInTheNether(t *testing.T) {
	h, players, pl := strawBedSetup(t)
	pl.dim = dimNether // worldFor falls back to the one test world
	h.dayTime.Store(13000)
	hp := pl.health
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping || pl.health != hp {
		t.Fatalf("sleeping=%v health %v→%v: a straw bed must neither sleep nor explode", pl.sleeping, hp, pl.health)
	}
	for _, c := range []blockPos{{4, 70, 4}, tBedHead} {
		if s := h.world.Block(c.x, c.y, c.z); s != worldgen.Air {
			t.Fatalf("straw bed cell %v should be broken, got %d", c, s)
		}
	}
	if h.world.Block(4, 69, 4) != worldgen.Stone {
		t.Fatal("the floor under the bed should be untouched — no blast")
	}
}
