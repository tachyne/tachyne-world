package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

var tWhiteBed = worldgen.BlockBase("white_bed") + 3 // foot, facing north

func bedSetup(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	h := newHub(world.New(1))
	h.spawns = newSpawnStore(t.TempDir() + "/spawns.json")
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	// A WHOLE bed: foot at 4,70,4 facing north, head one block north of it.
	// Half a bed cannot be slept in — see TestHalfABedCannotBeSleptIn.
	info, _ := worldgen.InfoForState(tWhiteBed)
	for x := 1; x <= 7; x++ { // a room around it: a floor, and air to stand up in
		for z := 0; z <= 7; z++ {
			h.world.SetBlock(x, 69, z, worldgen.Stone)
			h.world.SetBlock(x, 70, z, worldgen.Air)
			h.world.SetBlock(x, 71, z, worldgen.Air)
		}
	}
	h.world.SetBlock(4, 70, 4, tWhiteBed)
	h.world.SetBlock(4, 70, 3, worldgen.SetProperty(info, tWhiteBed, "part", "head"))
	pl.x, pl.y, pl.z = 4.5, 70, 4.5
	return h, players, pl
}

// tBedHead is where bedSetup's sleeper actually lies.
var tBedHead = blockPos{4, 70, 3}

func TestBedClaimsRespawnPoint(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.dayTime.Store(1000) // daytime: no sleep, but the claim still lands
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping {
		t.Fatal("must not sleep during the day")
	}
	if pos, dim, ok := h.spawns.get("tester"); !ok || dim != dimOverworld || pos != tBedHead {
		t.Fatalf("respawn point should be claimed, got %v %v", pos, ok)
	}
	// Death now returns to the bed, not world spawn.
	h.damageOf(players, pl, 25, dtGeneric)
	h.respawn(pl)
	// …standing up beside it (BedBlock.findStandUpPosition), not on it.
	if math.Abs(pl.x-4.5) > 2 || math.Abs(pl.z-float64(tBedHead.z)-0.5) > 2.5 || pl.y != 70 {
		t.Fatalf("respawn should stand up beside the bed, got (%v,%v,%v)", pl.x, pl.y, pl.z)
	}
	if st := h.world.Block(int(math.Floor(pl.x)), 70, int(math.Floor(pl.z))); isBedBlock(st) {
		t.Fatalf("respawned standing in the bed at (%v,%v)", pl.x, pl.z)
	}
}

func TestRespawnFallsBackWhenBedGone(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	h.world.SetBlock(4, 70, 4, worldgen.Air) // bed destroyed
	h.world.SetBlock(tBedHead.x, tBedHead.y, tBedHead.z, worldgen.Air)
	h.damageOf(players, pl, 25, dtGeneric)
	h.rules.RespawnRadius = 0 // the exact spawn (the default fuzzes it by up to ten)
	h.respawn(pl)
	if pl.x != 0.5 || pl.z != 0.5 {
		t.Fatalf("missing bed should fall back to world spawn, got (%v,%v)", pl.x, pl.z)
	}
}

func TestSleepSkipsNightWhenAllInBed(t *testing.T) {
	h, players, pl := bedSetup(t)
	p2 := &tracked{p: newPlayer(2, "second", [16]byte{}), gamemode: gmSurvival, x: 4.5, y: 70, z: 4.5}
	initSurvival(p2)
	players[2] = p2
	h.dayTime.Store(13000) // night

	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if !pl.sleeping {
		t.Fatal("first sleeper should be sleeping")
	}
	h.tick.Add(sleepSkipTicks * 2)
	h.updateSleep(players)
	if got := h.dayTime.Load(); got != 13000 {
		t.Fatalf("night must not skip while someone is awake, dayTime=%d", got)
	}
	// The first bed is occupied now (BedBlock refuses a second sleeper), so
	// the second player takes the bed beside it.
	info, _ := worldgen.InfoForState(tWhiteBed)
	h.world.SetBlock(6, 70, 4, tWhiteBed)
	h.world.SetBlock(6, 70, 3, worldgen.SetProperty(info, tWhiteBed, "part", "head"))
	p2.x = 6.5
	h.handleUseBed(players, p2, blockPos{6, 70, 4})
	h.updateSleep(players)
	if got := h.dayTime.Load(); got != 13000 {
		t.Fatalf("skip must wait out the ~5s fade, dayTime=%d", got)
	}
	h.tick.Add(sleepSkipTicks) // everyone's been in bed long enough now
	h.updateSleep(players)
	if got := h.dayTime.Load(); got != dayLengthTicks {
		t.Fatalf("everyone asleep should jump to sunrise (24000), dayTime=%d", got)
	}
	if pl.sleeping || p2.sleeping {
		t.Fatal("sunrise should wake everyone")
	}
}

func TestSpectatorDoesNotBlockSleep(t *testing.T) {
	h, players, pl := bedSetup(t)
	spec := &tracked{p: newPlayer(3, "ghost", [16]byte{}), gamemode: gmSpectator}
	initSurvival(spec)
	players[3] = spec
	h.dayTime.Store(13000)
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	h.tick.Add(sleepSkipTicks)
	h.updateSleep(players)
	if got := h.dayTime.Load(); got != dayLengthTicks {
		t.Fatalf("a spectator must not hold the night, dayTime=%d", got)
	}
}

func TestMonstersPreventSleep(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.dayTime.Store(13000)
	h.mobs[99] = &mob{eid: 99, hostile: true, x: 7, y: 70, z: 4} // 3 blocks away
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping {
		t.Fatal("monsters nearby must prevent sleep")
	}
	h.mobs[99].x = 40 // far away now
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if !pl.sleeping && h.dayTime.Load() == 13000 {
		t.Fatal("sleep should work once the monster is gone")
	}
}

func TestSleepingLiesOnBed(t *testing.T) {
	h, players, pl := bedSetup(t)
	p2 := &tracked{p: newPlayer(2, "second", [16]byte{}), gamemode: gmSurvival}
	initSurvival(p2)
	players[2] = p2 // an awake player keeps the night from skipping instantly
	h.dayTime.Store(13000)
	pl.x, pl.z = 6.2, 6.9 // player stands a step away from the bed
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if !pl.sleeping {
		t.Fatal("should be sleeping")
	}
	// The sleeper is moved onto the bed's HEAD half at vanilla's height.
	if pl.x != 4.5 || pl.y != float64(tBedHead.y)+bedSleepY || pl.z != float64(tBedHead.z)+0.5 {
		t.Fatalf("sleeper should lie at the bed head, got (%v,%v,%v)", pl.x, pl.y, pl.z)
	}
	// Leave Bed (player_command STOP_SLEEPING → evStopSleep) stands them up.
	h.wakePlayer(players, pl)
	if pl.sleeping {
		t.Fatal("Leave Bed should wake the sleeper")
	}
}

func TestSleepMetadataShape(t *testing.T) {
	b := sleepMetadata(7, blockPos{4, 70, 4})
	// eid 7, [index 6, type 21(pose), value 2(SLEEPING)],
	// [index 14, type 11(opt block pos), true, packed pos], 0xff.
	want := []byte{7, 6, 21, 2, 14, 11, 1}
	for i, w := range want {
		if b[i] != w {
			t.Fatalf("sleepMetadata[%d] = %d, want %d (%v)", i, b[i], w, b)
		}
	}
	if b[len(b)-1] != 0xff {
		t.Fatalf("metadata must end with the terminator, got %v", b)
	}
	w := wakeMetadata(7)
	wantWake := []byte{7, 6, 21, 0, 14, 11, 0, 0xff}
	if len(w) != len(wantWake) {
		t.Fatalf("wakeMetadata = %v, want %v", w, wantWake)
	}
	for i := range wantWake {
		if w[i] != wantWake[i] {
			t.Fatalf("wakeMetadata[%d] = %d, want %d", i, w[i], wantWake[i])
		}
	}
}

func TestWalkingAwayWakes(t *testing.T) {
	h, players, pl := bedSetup(t)
	p2 := &tracked{p: newPlayer(2, "second", [16]byte{}), gamemode: gmSurvival}
	initSurvival(p2)
	players[2] = p2 // keeps the night from skipping instantly
	h.dayTime.Store(13000)
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if !pl.sleeping {
		t.Fatal("should be sleeping")
	}
	pl.x, pl.z = 12, 12
	h.wakeIfAway(players, pl)
	if pl.sleeping {
		t.Fatal("walking away should wake the sleeper")
	}
}

// Level.isDay is not a clock reading: it is skyDarken < 4, which the weather
// moves. That is why a thunderstorm lets you sleep at noon and ordinary rain
// does not — rain darkens the sky, but not far enough.
func TestSleepWindowFollowsTheSky(t *testing.T) {
	h := newHub(world.New(79))

	// With a clear sky the window is exactly the pair of constants vanilla's
	// formula produces, which is what the engine used to hard-code.
	h.rainLevel, h.thunderLevel = 0, 0
	for _, tc := range []struct {
		tick int
		day  bool
	}{
		{6000, true},          // noon
		{12000, true},         // still afternoon
		{sleepStart, false},   // dusk: the window is open
		{18000, false},        // midnight
		{sleepEnd, false},     // the last sleepable tick
		{sleepEnd + 60, true}, // dawn: it has closed
	} {
		h.dayTime.Store(uint64(tc.tick))
		if got := h.isDaylight(); got != tc.day {
			t.Errorf("t=%d isDaylight=%v, want %v (skyDarken %d)",
				tc.tick, got, tc.day, canonicalSkyDarken(uint64(tc.tick), h.rainLevel, h.thunderLevel))
		}
	}

	// The crossings sit on the constants the engine hard-coded, give or take
	// the one tick that truncating the curve to an int can move them. That
	// agreement is the check that the formula is vanilla's and not a guess.
	cross := func(from, to int) int {
		for tk := from; tk != to; tk++ {
			if (canonicalSkyDarken(uint64(tk), 0, 0) < 4) != (canonicalSkyDarken(uint64(tk+1), 0, 0) < 4) {
				return tk + 1
			}
		}
		return -1
	}
	if got := cross(11000, 14000); got < sleepStart-1 || got > sleepStart+1 {
		t.Errorf("the window opens at %d, want within a tick of %d", got, sleepStart)
	}
	if got := cross(22000, 23999); got < sleepEnd || got > sleepEnd+2 {
		t.Errorf("the window closes at %d, want just past %d", got, sleepEnd)
	}

	// At noon: clear and rainy are both too bright, a thunderstorm is not.
	h.dayTime.Store(6000)
	for _, tc := range []struct {
		name          string
		rain, thunder float32
		day           bool
	}{
		{"clear", 0, 0, true},
		{"raining", 1, 0, true},
		{"thundering", 1, 1, false},
	} {
		h.rainLevel, h.thunderLevel = tc.rain, tc.thunder
		if got := h.isDaylight(); got != tc.day {
			t.Errorf("noon %s: isDaylight=%v, want %v (skyDarken %d)",
				tc.name, got, tc.day, canonicalSkyDarken(h.dayTime.Load(), tc.rain, tc.thunder))
		}
	}
}

// TestBedRefusalsFollowVanilla: BedBlock refuses an occupied bed, and
// startSleepInBed a bed more than 3 blocks away or with a solid block over
// it — each an overlay message, not chat — and a creative player may sleep
// with monsters about.
func TestBedRefusalsFollowVanilla(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.dayTime.Store(13000)
	pl.x, pl.z = 12.5, 12.5 // far from the bed
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping {
		t.Fatal("a bed out of reach should not be slept in")
	}
	pl.x, pl.z = 4.5, 4.5
	h.world.SetBlock(4, 71, 3, worldgen.Stone) // a block over the head
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping {
		t.Fatal("an obstructed bed should not be slept in")
	}
	h.world.SetBlock(4, 71, 3, worldgen.Air)
	zombie := h.spawnMob(players, entityZombie, 6.5, 70, 4.5)
	zombie.hostile = true
	pl.gamemode = gmCreative
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if !pl.sleeping {
		t.Fatal("a creative player sleeps with a monster nearby")
	}
}

// A bed walled in on every side is obstructed: the respawn goes to the world
// spawn, as ServerPlayer.findRespawnAndUseSpawnBlock finds no stand-up spot.
func TestWalledInBedIsObstructed(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.spawns.set(pl.p.key(), tBedHead, dimOverworld)
	for x := 2; x <= 6; x++ {
		for z := 1; z <= 6; z++ {
			for y := 70; y <= 71; y++ {
				if st := h.world.Block(x, y, z); !isBedBlock(st) {
					h.world.SetBlock(x, y, z, worldgen.Stone)
				}
			}
		}
	}
	x, _, z, _ := h.respawnPointCharging(players, pl, false)
	if math.Abs(x-4.5) < 3 && math.Abs(z-3.5) < 3 {
		t.Fatalf("respawned at %v,%v beside a walled-in bed", x, z)
	}
}
