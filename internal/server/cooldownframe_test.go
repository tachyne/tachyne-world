package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A cooldown the world starts reaches the client as the cooldown frame
// vanilla's ServerItemCooldowns sends — keyed by the item's id, for the
// ticks it lasts — so the client draws the sweep over the disabled shield.
func TestCooldownFrame(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	drainEvents(pl)

	h.disableShield(players, pl, axeDisableSeconds)

	var got []attachproto.ItemCooldown
	for done := false; !done; {
		select {
		case pkt := <-pl.p.out:
			if c, ok := pkt.ev.(attachproto.ItemCooldown); ok {
				got = append(got, c)
			}
		default:
			done = true
		}
	}
	if len(got) != 1 || got[0].Group != "minecraft:shield" || got[0].Ticks != 100 {
		t.Fatalf("cooldown frames %+v, want one minecraft:shield for 100 ticks", got)
	}
}
