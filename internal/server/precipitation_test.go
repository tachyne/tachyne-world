package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The temperature noises against values taken from vanilla's own
// SimplexNoise/NoiseStack for the same seeds and positions.
func TestPrecipitationNoisesMatchVanilla(t *testing.T) {
	for _, c := range []struct {
		x, z                   int
		temp, frozen, bi2, bi9 float32
	}{
		{100, -37, -0.25938663, 0.19876762, -0.27399886, 0.598317},
		{-1234, 5678, 0.18867333, 0.33556572, -0.26489127, -0.4013407},
		{31, 17, 0.62783754, 0.18541148, -0.65324706, -0.6054377},
		{-8, -900, -0.3162076, 0.23808631, 0.33333477, 0.6763378},
		{4000, 4000, -0.12789407, 0.46382257, -0.8525361, 0.11329108},
	} {
		x, z := float64(c.x), float64(c.z)
		got := [4]float32{
			temperatureNoise.get(float64(float32(c.x)/8), float64(float32(c.z)/8)),
			frozenTemperatureNoise(x*0.05, z*0.05),
			biomeInfoNoise.get(x*0.2, z*0.2),
			biomeInfoNoise.get(x*0.09, z*0.09),
		}
		want := [4]float32{c.temp, c.frozen, c.bi2, c.bi9}
		if got != want {
			t.Errorf("at %d,%d: %v, want %v", c.x, c.z, got, want)
		}
	}
}

// The snow line wobbles with the noise, and a frozen ocean has patches of
// rain.
func TestPrecipitationRules(t *testing.T) {
	if p := precipitationAt("minecraft:desert", 0, 70, 0); p != worldgen.PrecipNone {
		t.Errorf("desert: %d", p)
	}
	if p := precipitationAt("minecraft:snowy_plains", 0, 70, 0); p != worldgen.PrecipSnow {
		t.Errorf("snowy plains: %d", p)
	}
	rain, snow := 0, 0
	for x := 0; x < 400; x += 7 {
		for z := 0; z < 400; z += 7 {
			if precipitationAt("minecraft:frozen_ocean", x, 63, z) == worldgen.PrecipRain {
				rain++
			} else {
				snow++
			}
		}
	}
	if rain == 0 || snow == 0 {
		t.Errorf("frozen ocean: %d rain, %d snow cells; want both", rain, snow)
	}
	// Plains (0.8) turns to snow near y = 80 + (0.65×40/0.05) = 600, ±8.
	lo, hi := 0, 0
	for x := 0; x < 64; x++ {
		if precipitationAt("minecraft:plains", x*13, 594, x*7) == worldgen.PrecipSnow {
			lo++
		}
		if precipitationAt("minecraft:plains", x*13, 606, x*7) == worldgen.PrecipSnow {
			hi++
		}
	}
	if lo == 64 || hi == 0 || lo >= hi {
		t.Errorf("the snow line does not wobble: %d of 64 snow at 594, %d at 606", lo, hi)
	}
}
