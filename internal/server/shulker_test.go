package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestShulkerShell: a shulker starts closed with twenty armour, opens wide
// and fires at a player within twenty, closes again when they leave, and
// teleports onto a floor within eight when hurt below half.
func TestShulkerShell(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -10; x <= 10; x++ {
		for z := -10; z <= 10; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 190; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = 40, 180, 0.5
	s := h.spawnMob(players, entityShulker, 0.5, 180, 0.5)
	h.shulkerTick(players, s)
	if !s.shulkerClosed() || s.armorValue() < 20 {
		t.Fatalf("closed with twenty armour: peek %d armour %.0f", s.shPeek, s.armorValue())
	}
	pl.x = 8.5
	before := len(h.arrows)
	h.shulkerTick(players, s)
	if s.shPeek != shulkerPeekOpen || s.armorValue() >= 20 || len(h.arrows) != before+1 {
		t.Fatalf("open and firing at a target: peek %d armour %.0f bullets %d", s.shPeek, s.armorValue(), len(h.arrows)-before)
	}
	pl.x = 40
	h.shulkerTick(players, s)
	if !s.shulkerClosed() {
		t.Fatal("closed again once the target is gone")
	}
	s.health = 10
	moved := false
	for i := 0; i < 200 && !moved; i++ {
		s.shHurt = true
		h.shulkerTick(players, s)
		moved = s.x != 0.5 || s.z != 0.5 || s.y != 180
	}
	if !moved || w.At(int(s.x), int(s.y)-1, int(s.z)) != worldgen.Stone {
		t.Fatalf("hurt below half it teleports onto a floor: %.1f,%.1f,%.1f", s.x, s.y, s.z)
	}
}
