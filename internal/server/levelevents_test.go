package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// drainFX collects the level events queued for a player.
func drainFX(pl *tracked) []attachproto.WorldFX {
	var out []attachproto.WorldFX
	for {
		select {
		case pkt := <-pl.p.out:
			if fx, ok := pkt.ev.(attachproto.WorldFX); ok {
				out = append(out, fx)
			}
		default:
			return out
		}
	}
}

// Bone meal on a crop fires vanilla's level event 1505 at the block — the
// client draws the green burst and plays the sound — instead of a hand-made
// particle burst.
func TestBoneMealLevelEvent(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 5.5, 70, 0.5
	pl.inv.slots[0] = invStack{item: itemBoneMeal, count: 3}
	h.world.SetBlock(5, 69, 0, worldgen.BlockBase("farmland"))
	h.world.SetBlock(5, 70, 0, worldgen.BlockBase("wheat"))
	drainEvents(pl)
	h.onBoneMeal(players, evBoneMeal{eid: pl.p.eid, x: 5, y: 70, z: 0, slot: 0})
	got := drainFX(pl)
	if len(got) != 1 || got[0].Event != worldEventBoneMeal || got[0].X != 5 || got[0].Y != 70 || got[0].Data != 15 {
		t.Fatalf("level events %+v, want one 1505 at the crop with 15 particles", got)
	}
	if dir3D(0, -1, 0) != 0 || dir3D(0, 1, 0) != 1 || dir3D(0, 0, -1) != 2 || dir3D(0, 0, 1) != 3 || dir3D(-1, 0, 0) != 4 || dir3D(1, 0, 0) != 5 {
		t.Fatal("dir3D is not Direction.get3DDataValue order")
	}
}
