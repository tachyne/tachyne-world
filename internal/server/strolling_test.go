package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Stroll speeds are the vanilla goal's own multipliers.
func TestStrollSpeeds(t *testing.T) {
	cases := map[int]float64{
		entityRavager: 0.4, entityHorse: 0.7, entityCreeper: 0.8, entitySpider: 0.8,
		entityRabbit: 0.6, entityPillager: 0.6, entityCow: 1, entityZombie: 1,
		entityZoglin: 0.4, entityCreaking: 0.3,
	}
	for et, want := range cases {
		if got := strollSpeed(&mob{etype: et}); got != want {
			t.Errorf("%s stroll speed %v, want %v", entityNameByID[et], got, want)
		}
	}
}

// A monster picking a fresh heading prefers the dark: with a lit strip to
// one side and night on the other, its draws land away from the light. A
// passive mob has no such preference.
func TestMonsterStrollPrefersTheDark(t *testing.T) {
	h := newHub(world.New(1))
	h.dayTime.Store(18000) // midnight: the sky is dark everywhere
	x, y, z := 600, 80, 600
	for dx := -12; dx <= 12; dx++ {
		for dz := -12; dz <= 12; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 3; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	// A row of torches to the east.
	for dz := -4; dz <= 4; dz++ {
		h.world.SetBlock(x+6, y, z+dz, worldgen.BlockID("torch"))
	}
	m := &mob{etype: entityZombie, hostile: true, x: float64(x) + 0.5, y: float64(y), z: float64(z) + 0.5}
	east := 0
	for i := 0; i < 400; i++ {
		if a := h.strollAngle(m); math.Cos(a) > 0.7 {
			east++
		}
	}
	if east > 40 {
		t.Errorf("a monster should shy away from the lit side: %d of 400 draws headed into it", east)
	}
	// A cow draws without preference — roughly a sixth of the circle each way.
	c := &mob{etype: entityCow, x: m.x, y: m.y, z: m.z}
	cowEast := 0
	for i := 0; i < 400; i++ {
		if a := h.strollAngle(c); math.Cos(a) > 0.7 {
			cowEast++
		}
	}
	if cowEast < 40 {
		t.Errorf("a cow has no light preference, yet only %d of 400 draws went east", cowEast)
	}
}
