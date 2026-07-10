package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

var tWhiteBed = worldgen.BlockBase("white_bed") + 3 // foot, facing north

func bedSetup(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	h := newHub(world.New(1))
	h.spawns = newSpawnStore(t.TempDir() + "/spawns.json")
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	h.world.SetBlock(4, 70, 4, tWhiteBed)
	pl.x, pl.y, pl.z = 4.5, 70, 4.5
	return h, players, pl
}

func TestBedClaimsRespawnPoint(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.dayTime.Store(1000) // daytime: no sleep, but the claim still lands
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	if pl.sleeping {
		t.Fatal("must not sleep during the day")
	}
	if pos, ok := h.spawns.get("tester"); !ok || pos != (blockPos{4, 70, 4}) {
		t.Fatalf("respawn point should be claimed, got %v %v", pos, ok)
	}
	// Death now returns to the bed, not world spawn.
	h.damage(players, pl, 25)
	h.respawn(pl)
	if pl.x != 4.5 || pl.z != 4.5 {
		t.Fatalf("respawn should return to the bed, got (%v,%v)", pl.x, pl.z)
	}
}

func TestRespawnFallsBackWhenBedGone(t *testing.T) {
	h, players, pl := bedSetup(t)
	h.handleUseBed(players, pl, blockPos{4, 70, 4})
	h.world.SetBlock(4, 70, 4, worldgen.Air) // bed destroyed
	h.damage(players, pl, 25)
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
	h.handleUseBed(players, p2, blockPos{4, 70, 4})
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
	// The sleeper is moved onto the bed surface (vanilla anchors them there).
	if pl.x != 4.5 || pl.y != 70+bedSurface || pl.z != 4.5 {
		t.Fatalf("sleeper should lie at the bed centre, got (%v,%v,%v)", pl.x, pl.y, pl.z)
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
