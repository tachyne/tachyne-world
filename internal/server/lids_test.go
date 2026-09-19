package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Opening a chest sends its lid block event (action 1, one opener) to the
// players nearby — the opener and a bystander — and closing sends zero.
func TestChestLidEvents(t *testing.T) {
	h := newHub(world.New(1))
	opener := survPlayer(h)
	watcher := survPlayer(h)
	watcher.p.eid = 2
	players := map[int32]*tracked{opener.p.eid: opener, watcher.p.eid: watcher}
	h.playersRef = players
	x, y, z := 1600, 180, 1600
	clearAirBox(h.world, x, y, z, 2)
	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	h.world.SetBlock(x, y, z, worldgen.BlockID("chest"))
	for _, pl := range players {
		pl.x, pl.y, pl.z = float64(x)+2, float64(y), float64(z)
	}
	drainEvents(opener)
	drainEvents(watcher)
	h.openChest(opener, x, y, z)
	lid := func(tr *tracked) (attachproto.BlockEvent, bool) {
		for {
			select {
			case pkt := <-tr.p.out:
				if ev, ok := pkt.ev.(attachproto.BlockEvent); ok {
					return ev, true
				}
			default:
				return attachproto.BlockEvent{}, false
			}
		}
	}
	ev, ok := lid(watcher)
	if !ok || ev.Action != 1 || ev.Param != 1 || ev.X != int32(x) {
		t.Fatalf("the bystander should see the lid rise (action 1, one opener): %+v ok=%v", ev, ok)
	}
	if _, ok := lid(opener); !ok {
		t.Fatal("the opener sees the lid too")
	}
	h.closeWindow(players, opener)
	ev, ok = lid(watcher)
	if !ok || ev.Param != 0 {
		t.Fatalf("closing drops the lid (zero openers): %+v ok=%v", ev, ok)
	}
}
