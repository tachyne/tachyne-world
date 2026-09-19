package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Inside a fortress the natural spawner's monster pool is the fortress's own
// garrison table (the structure's spawn override), and the corridor chests
// fill from nether_bridge.
func TestFortressSpawnsAndLoots(t *testing.T) {
	h := newHub(world.New(9))
	players := map[int32]*tracked{}
	h.playersRef = players
	nw, _ := world.NewNether(9, nil)
	h.nether = nw
	g := nw.Gen()
	var f worldgen.Fortress
	for cx := -6; cx < 6 && !f.Exists; cx++ {
		for cz := -6; cz < 6 && !f.Exists; cz++ {
			f = g.FortressIn(cx*400+8, cz*400+8)
		}
	}
	if !f.Exists {
		t.Fatal("no fortress found")
	}
	pieces := g.FortressPieces(f)
	if len(pieces) == 0 {
		t.Fatal("fortress has no pieces")
	}
	inside := 0
	for _, p := range pieces {
		x, z := (p.X0+p.X1)/2, (p.Z0+p.Z1)/2
		if !h.inFortressPiece(x, z) {
			t.Fatalf("the middle of a fortress piece at (%d,%d) is inside the fortress", x, z)
		}
		pool := h.spawnPool(dimNether, catMonster, x, (p.Y0+p.Y1)/2, z)
		if len(pool) != len(fortressPool) {
			t.Fatalf("inside a fortress the monster pool is the garrison's, got %d entries", len(pool))
		}
		for _, e := range pool {
			switch e.etype {
			case entityBlaze, entityZombifiedPiglin, entityWitherSkeleton, entitySkeleton, entityMagmaCube:
			default:
				t.Fatalf("an off-table species %d in the garrison pool", e.etype)
			}
		}
		if inside++; inside >= 5 {
			break
		}
	}
	if h.inFortressPiece(f.X+5000, f.Z+5000) {
		t.Error("far outside every fortress the biome's pool applies")
	}
	if pool := h.spawnPool(dimNether, catCreature, pieces[0].X0, pieces[0].Y0, pieces[0].Z0); len(pool) == 0 || pool[0].etype != entityStrider {
		t.Error("the creature pool inside a fortress is still the biome's (striders)")
	}
	chests := g.FortressChests(f)
	if len(chests) == 0 {
		t.Skip("this fortress rolled no chests")
	}
	c := &chest{}
	h.fillStructureChestIn(dimNether, blockPos{chests[0][0], chests[0][1], chests[0][2]}, c)
	filled := 0
	for _, s := range c.slots {
		if s.count > 0 {
			filled++
		}
	}
	if filled == 0 {
		t.Errorf("fortress chest at %v should fill from nether_bridge", chests[0])
	}
}
