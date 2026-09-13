package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestCamelDash: the rider's jump command sets the dash flag and a 55-tick
// cooldown, the flag drops once the cooldown is under fifty, and a second
// dash waits for the cooldown.
func TestCamelDash(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	c := h.spawnAnimal(players, entityCamel, 3, 3)
	c.saddled = true
	pl.ridingEID = c.eid
	h.camelDashStart(players, pl)
	if !c.dashing || c.dashCD != camelDashCooldown {
		t.Fatalf("dash: flag %v cd %d", c.dashing, c.dashCD)
	}
	for i := 0; i < 3 && c.dashing; i++ {
		h.camelDashTick(players, c)
	}
	if c.dashing || c.dashCD >= camelDashFlagOff {
		t.Fatalf("the flag drops under fifty: flag %v cd %d", c.dashing, c.dashCD)
	}
	h.camelDashStart(players, pl)
	if c.dashing {
		t.Fatal("no dash while the cooldown runs")
	}
	for c.dashCD > 0 {
		h.camelDashTick(players, c)
	}
	h.camelDashStart(players, pl)
	if !c.dashing {
		t.Fatal("after the cooldown it dashes again")
	}
	// An unsaddled camel, or a horse, ignores the command.
	c2 := h.spawnAnimal(players, entityCamel, 6, 3)
	pl.ridingEID = c2.eid
	h.camelDashStart(players, pl)
	if c2.dashing {
		t.Fatal("an unsaddled camel does not dash")
	}
}
