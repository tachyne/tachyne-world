package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// An idle horse rears now and then (RandomStandGoal), for 20 ticks, and the
// STANDING flag goes out and comes back down; a llama never rears; an
// angered horse rears at once (makeMad).
func TestHorseRears(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	horse := h.spawnMob(players, entityHorse, 0.5, 180, 0.5)
	llama := h.spawnMob(players, entityLlama, 4.5, 180, 0.5)
	reared := false
	for i := 0; i < 20000/mobMoveInterval && !reared; i++ {
		h.tick.Add(mobMoveInterval)
		h.horseStandTick(players, horse)
		h.horseStandTick(players, llama)
		reared = horse.standLeft > 0
	}
	if !reared {
		t.Fatal("a horse never reared in 1000 seconds")
	}
	if llama.standLeft != 0 {
		t.Fatal("a llama reared")
	}
	if meta := horseFlagsMeta(horse); meta[len(meta)-2]&horseFlagStanding == 0 {
		t.Fatal("a rearing horse's flags lack STANDING")
	}
	for i := 0; i < horseStandTicks/mobMoveInterval; i++ {
		h.horseStandTick(players, horse)
	}
	if horse.standLeft != 0 {
		t.Fatalf("the rear lasted past 20 ticks: %d left", horse.standLeft)
	}
	h.horseMakeMad(players, horse)
	if horse.standLeft != horseStandTicks {
		t.Fatal("an angered horse did not rear")
	}
}
