package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// phantomPlayer puts a survival player out under open sky, above sea level.
func phantomPlayer(h *hub) (*tracked, map[int32]*tracked) {
	pl := survPlayer(h)
	pl.x, pl.z = 0.5, 0.5
	pl.y = float64(worldSurfaceForTest(h, 0, 0))
	return pl, map[int32]*tracked{pl.p.eid: pl}
}

func worldSurfaceForTest(h *hub, x, z int) int {
	for y := 200; y > 0; y-- {
		if h.world.At(x, y, z) != 0 {
			return y + 1
		}
	}
	return 70
}

// The whole point of the mob: sleeping buys them off, and not sleeping does
// not. A player who has just risen must never be harried.
func TestPhantomsNeedInsomnia(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := phantomPlayer(h)
	h.rules.Difficulty = diffHard
	h.dayTime.Store(15000)

	countPhantoms := func() (n int) {
		for _, m := range h.mobs {
			if m.etype == entityPhantom {
				n++
			}
		}
		return
	}

	// Freshly rested: no clock, so the roll can never reach three days.
	h.resetCustom(pl, "time_since_rest")
	for i := 0; i < 500; i++ {
		h.phantomNextAt = 0 // let the cadence fire every pass
		h.phantomSpawner(players)
	}
	if n := countPhantoms(); n != 0 {
		t.Fatalf("a rested player was harried by %d phantoms", n)
	}

	// Days without sleep: now they come.
	h.incCustom(pl, "time_since_rest", 24000*8)
	for i := 0; i < 500 && countPhantoms() == 0; i++ {
		h.phantomNextAt = 0
		h.phantomSpawner(players)
	}
	if countPhantoms() == 0 {
		t.Error("a player who had not slept for eight days saw no phantoms")
	}
}

// Climbing into a bed is what stops the clock — vanilla resets on getting IN,
// so being woken early still counts.
func TestSleepingResetsTheInsomniaClock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.incCustom(pl, "time_since_rest", 24000*5)
	if customStat(pl, "time_since_rest") == 0 {
		t.Fatal("the clock never started — this test would prove nothing")
	}
	h.setSleeping(players, pl, blockPos{0, 70, 0})
	if got := customStat(pl, "time_since_rest"); got != 0 {
		t.Errorf("the insomnia clock read %d after going to bed, want 0", got)
	}
	// Waking does not restart it from anywhere but zero.
	h.wakePlayer(players, pl)
	if got := customStat(pl, "time_since_rest"); got != 0 {
		t.Errorf("waking put %d back on the clock", got)
	}
}

// The gamerule still switches them off wholesale.
func TestSpawnPhantomsRuleOff(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := phantomPlayer(h)
	h.rules.Difficulty = diffHard
	h.rules.SpawnPhantoms = false
	h.incCustom(pl, "time_since_rest", 24000*8)

	for i := 0; i < 300; i++ {
		h.phantomNextAt = 0
		h.phantomSpawner(players)
	}
	for _, m := range h.mobs {
		if m.etype == entityPhantom {
			t.Fatal("phantoms spawned with the gamerule off")
		}
	}
}
