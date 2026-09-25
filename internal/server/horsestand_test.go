package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
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

// OPEN_INVENTORY (E while riding) opens a tamed mount's screen; an
// untamed one keeps it shut (AbstractHorse.openCustomInventoryScreen).
func TestOpenInventoryWhileRiding(t *testing.T) {
	for _, tamed := range []bool{true, false} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		pl.x, pl.y, pl.z = 0.5, 80, 0.5
		horse := h.spawnMob(players, entityHorse, 0.5, 80, 0.5)
		horse.tamed = tamed
		pl.ridingEID, horse.rider = horse.eid, pl.p.eid
		r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
		r.Action(attachproto.PlayerAction{Action: 7})
		for len(h.events) > 0 {
			if e, ok := (<-h.events).(evOpenMountInv); ok {
				h.openMountInventory(players, players[e.eid])
			}
		}
		if got := pl.winKind == winHorse; got != tamed {
			t.Fatalf("tamed=%v: mount window open %v", tamed, got)
		}
	}
}
