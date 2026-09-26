package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDryThunderIsNoStorm: the thunder and rain timers run apart, so the
// thunder flag is often on under a clear sky. Level.getThunderLevel scales
// thunder by rain, so that dry spell neither darkens the sky nor counts as
// isThundering. The engine used the raw thunder level: for the length of a
// dry spell every undead in the world stood unburnt in the morning sun, and
// the thunderstorm spawn rule let monsters spawn in open daylight. This is
// the snowy mountain from the reports: a zombie on a snow layer over a snow
// block and packed ice, at mid-morning.
func TestDryThunderIsNoStorm(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 181, 4.5 // close by, so nothing despawns
	players := map[int32]*tracked{1: pl}
	h.world.ForceLoad(0, 0, 2)
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			for y := 176; y <= 179; y++ {
				h.world.SetBlock(x, y, z, packedIceBlock)
			}
			h.world.SetBlock(x, 180, z, worldgen.BlockBase("snow_block"))
			h.world.SetBlock(x, 181, z, snowLayer1)
			for y := 182; y < 200; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	h.rules.DoWeather = false // hold the flags: thunder on, rain off
	h.rainFlag, h.thunderFlag = false, true
	for i := 0; i < 120; i++ { // the levels ramp at 0.01 a tick
		h.updateWeather(players)
	}
	if h.thunderLevel != 1 || h.rainLevel != 0 {
		t.Fatalf("levels thunder=%v rain=%v, want 1 and 0", h.thunderLevel, h.rainLevel)
	}
	if h.thundering {
		t.Fatal("thunder without rain must not count as isThundering")
	}
	h.dayTime.Store(3600)
	if d := h.skyDarken(); d != 0 {
		t.Fatalf("a dry thunder spell darkened the morning sky by %d", d)
	}
	for i := 0; i < 200; i++ {
		if h.darkEnoughToSpawn(15, 0) {
			t.Fatal("open daylight passed the monster spawn light check in a dry thunder spell")
		}
	}

	z := h.spawnHostileY(players, entityZombie, 0.5, 181, 0.5)
	z.health = 100
	z.gear[0] = invStack{} // bare-headed, as in the reports
	for i := 0; i < 30 && z.fireSecs == 0; i++ {
		h.updateHostiles(players)
	}
	if z.fireSecs == 0 {
		t.Fatalf("a zombie on the snow in the morning sun did not burn (magic %.3f, sky open %v)",
			h.lightMagic(z), h.skyExposedAt(0, floorInt(z.y+mobEyeHeight(z)), 0))
	}
}
