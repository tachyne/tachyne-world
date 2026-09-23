package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Eating from the offhand: letting go once the eat is done feeds the player
// and must not take the hub down. The offhand is slot 40, and stopEating read
// it straight out of the 36-slot main array.
func TestOffhandEatReleaseFeedsAndDoesNotPanic(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	tr := testTracked()
	initSurvival(tr)
	tr.gamemode = gmSurvival
	tr.food = 10
	tr.offhand = invStack{item: itemByName["bread"], count: 3}
	h.tick.Store(1000)
	tr.eatingSlot, tr.eatingAt = offhandSlot, 1000-40
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("releasing an offhand eat panicked: %v", r)
		}
	}()
	h.stopEating(players, tr)
	if tr.food <= 10 {
		t.Errorf("the offhand bread did not feed: food %d", tr.food)
	}
	if tr.offhand.count != 2 {
		t.Errorf("one bread should be eaten from the offhand, %d left", tr.offhand.count)
	}
}
