package server

import (
	"encoding/binary"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDragonRespawnCeremony: four crystals around the open exit portal
// close it and run the ceremony — sky beams, the pillar sweep, the
// detonation — after which the dragon is back with every pillar crowned.
func TestDragonRespawnCeremony(t *testing.T) {
	h := newTestHub(world.New(1))
	ew, _ := world.NewEnd(7, nil)
	h.dims.set(dimEnd, ew)
	pl := survPlayer(h)
	pl.dim = 2
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.rules.DragonDefeated = true
	_, cy, _ := h.worldFor(dimEnd).Gen().EndExitPortal()
	h.worldFor(dimEnd).SetBlock(1, cy, 0, worldgen.EndPortalBlock)  // an open portal cell
	for _, d := range [4][2]int{{3, 0}, {-3, 0}, {0, 3}, {0, -3}} { // on the podium's rim
		c := &crystal{eid: h.allocEID(), dim: dimEnd, x: float64(d[0]) + 0.5, y: float64(cy + 1), z: float64(d[1]) + 0.5}
		binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
		h.crystals[c.eid] = c
	}
	h.tryRespawnDragon(players)
	if h.dragonRespawn == nil || h.dragon != nil {
		t.Fatal("the ceremony should be under way, the dragon not yet back")
	}
	if h.worldFor(dimEnd).At(1, cy, 0) != worldgen.Air {
		t.Fatal("the portal closes for the ceremony (the inactive podium: air inside the rim)")
	}
	if len(h.crystals) != 4 {
		t.Fatalf("the four crystals stay through the ceremony: %d", len(h.crystals))
	}
	for i := 0; i < 2000 && h.dragonRespawn != nil; i++ {
		h.tickDragonRespawn(players)
	}
	if h.dragonRespawn != nil || h.dragon == nil || h.rules.DragonDefeated {
		t.Fatalf("the dragon should be back: respawn %v dragon %v", h.dragonRespawn != nil, h.dragon != nil)
	}
	if len(h.crystals) != worldgen.EndPillars {
		t.Fatalf("a crystal on every pillar, the four spent: %d", len(h.crystals))
	}
}
