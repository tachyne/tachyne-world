package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A stranded dolphin heads for the water; a strider off the lava heads back
// to it; neither moves when it is already in its element.
func TestStrandedAnimalsHeadHome(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			h.world.SetBlock(x, 69, z, worldgen.Stone)
			h.world.SetBlock(x, 70, z, worldgen.Air)
		}
	}
	for z := 4; z <= 6; z++ {
		h.world.SetBlock(0, 70, z, worldgen.WaterBase)
		h.world.SetBlock(0, 70, -z, worldgen.LavaBase)
	}

	d := h.spawnMob(players, entityDolphin, 0.5, 70, 0.5)
	d.setMoveSpeed(0.1)
	if !h.findWaterStep(d) {
		t.Fatal("a dolphin on land should head for the water")
	}
	if d.vz <= 0 {
		t.Errorf("the water is at +z: vz=%v", d.vz)
	}
	// Already in the water: the goal does nothing.
	d.x, d.z, d.vx, d.vz = 0.5, 5.5, 0, 0
	if h.findWaterStep(d) {
		t.Error("a dolphin in the water has nowhere to go")
	}

	s := h.spawnMob(players, entityStrider, 0.5, 70, 0.5)
	s.setMoveSpeed(0.1)
	if !h.findWaterStep(s) {
		t.Fatal("a strider on solid ground should head for the lava")
	}
	if s.vz >= 0 {
		t.Errorf("the lava is at -z: vz=%v", s.vz)
	}
	s.z, s.vx, s.vz = -5.5, 0, 0
	if h.findWaterStep(s) {
		t.Error("a strider in the lava stays put")
	}
}
