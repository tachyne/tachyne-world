package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestChestSoundsOnlyOnFirstAndLastViewer: ContainerOpenersCounter plays
// the open sound when the first viewer arrives and the close sound when the
// last one leaves; viewers in between are silent.
func TestChestSoundsOnlyOnFirstAndLastViewer(t *testing.T) {
	h := newHub(world.New(1))
	a, b := survPlayer(h), survPlayer(h)
	b.p = newPlayer(2, "second", [16]byte{9})
	players := map[int32]*tracked{a.p.eid: a, b.p.eid: b}
	h.playersRef = players
	x, y, z := 3, 180, 3
	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	h.world.SetBlock(x, y, z, worldgen.BlockBase("chest"))
	h.world.SetBlock(x, y+1, z, worldgen.Air)
	a.x, a.y, a.z = float64(x)+1.5, float64(y), float64(z)
	b.x, b.y, b.z = a.x, a.y, a.z
	count := func(name string) int {
		n := 0
		for {
			select {
			case pkt := <-a.p.out:
				if ev, ok := pkt.ev.(attachproto.Sound); ok && ev.Name == name {
					n++
				}
			default:
				return n
			}
		}
	}
	drainOut(a.p)
	h.openChest(a, x, y, z)
	h.openChest(b, x, y, z)
	if n := count("minecraft:block.chest.open"); n != 1 {
		t.Errorf("two viewers opening: %d open sounds, want 1", n)
	}
	h.closeWindow(players, a)
	if n := count("minecraft:block.chest.close"); n != 0 {
		t.Errorf("a viewer leaving while another stays: %d close sounds, want 0", n)
	}
	h.closeWindow(players, b)
	if n := count("minecraft:block.chest.close"); n != 1 {
		t.Errorf("the last viewer leaving: %d close sounds, want 1", n)
	}
}
