package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Inside a fortress the nether spawn pass rolls the fortress's own table on
// the fortress floors, and the corridor chests fill from nether_bridge.
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
	spawned := 0
	for _, p := range pieces {
		x, z := (p.X0+p.X1)/2, (p.Z0+p.Z1)/2
		if m := h.spawnFortressMob(players, x, z); m != nil {
			if m.dim != dimNether {
				t.Fatalf("fortress mob in dim %d", m.dim)
			}
			switch m.etype {
			case entityBlaze, entityZombifiedPiglin, entityWitherSkeleton, entitySkeleton, entityMagmaCube:
			default:
				t.Fatalf("fortress rolled an off-table mob %d", m.etype)
			}
			spawned++
		}
		if spawned >= 5 {
			break
		}
	}
	if spawned == 0 {
		t.Error("no fortress piece offered a floor to spawn on")
	}
	if h.spawnFortressMob(players, f.X+5000, f.Z+5000) != nil {
		t.Error("outside every fortress the branch must decline")
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
