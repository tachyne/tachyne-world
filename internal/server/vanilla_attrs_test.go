package server

import "testing"

// TestSpeciesMatchVanillaAttributes pins mob stats to the vanilla 1.21.11
// createAttributes() values (MAX_HEALTH, and MOVEMENT_SPEED which our `speed`
// field mirrors 1:1). A representative cross-section plus the three the audit
// corrected — a regression trips if a value drifts from vanilla. Flyers/fish use
// an engine-tuned `step` (custom move controllers), so only walking `speed` is
// checked here.
func TestSpeciesMatchVanillaAttributes(t *testing.T) {
	type want struct {
		health int
		speed  float64 // vanilla MOVEMENT_SPEED (0 = uses step / not checked)
	}
	// Values straight from vanilla CopperGolem/HappyGhast/ZombieHorse/… .
	cases := map[int]want{
		entityCopperGolem: {12, 0.20}, // audit fix: was 0.25
		entityZombieHorse: {25, 0.20}, // audit fix: was 15
		entityHappyGhast:  {20, 0},    // step-based flyer; MOVEMENT_SPEED 0.05
		entityCamel:       {32, 0.09},
		entityArmadillo:   {12, 0.14},
		entitySniffer:     {14, 0.10},
		entityGoat:        {10, 0.20},
		entityWolf:        {8, 0.30},
		entityRavager:     {100, 0.30},
		entityWarden:      {500, 0.30},
		entityHoglin:      {40, 0.30},
		entityPiglinBrute: {50, 0.35},
		entityPolarBear:   {30, 0.25},
		entityTurtle:      {30, 0.25},
		entityWither:      {300, 0}, // flyer step
	}
	for etype, w := range cases {
		d := speciesOf(etype)
		if d == nil {
			t.Errorf("%d: not in speciesTable", etype)
			continue
		}
		if d.health != w.health {
			t.Errorf("%s: health %d, vanilla MAX_HEALTH %d", d.name, d.health, w.health)
		}
		if w.speed != 0 && d.speed != w.speed {
			t.Errorf("%s: speed %v, vanilla MOVEMENT_SPEED %v", d.name, d.speed, w.speed)
		}
	}
}
