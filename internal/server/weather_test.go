package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// TestWeatherCycleFlips: the vanilla two-timer cycle — a timer reaching zero
// flips its flag and the next tick re-rolls it as a duration (flag on) or a
// delay (flag off), each in the vanilla uniform range.
func TestWeatherCycleFlips(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{1: testTracked()}
	if h.raining || h.rainFlag {
		t.Fatal("worlds start clear")
	}
	h.rainTime, h.thunderTime = 1, 1<<20 // isolate the rain timer
	h.updateWeather(players)
	if !h.rainFlag {
		t.Fatal("rain delay ending must start a rain spell")
	}
	h.updateWeather(players) // re-roll happens on the next tick (rainTime==0)
	if h.rainTime < rainDurationMin-2 || h.rainTime > rainDurationMax {
		t.Fatalf("rain duration out of vanilla range: %d", h.rainTime)
	}
	// levels ramp at ±0.01/tick; the gameplay boolean trips past 0.2
	for i := 0; i < 25; i++ {
		h.updateWeather(players)
	}
	if !h.raining || h.rainLevel < 0.2 {
		t.Fatalf("rain level must ramp past the 0.2 threshold: level=%v raining=%v", h.rainLevel, h.raining)
	}
	// end the spell: flag off, level fades, raining clears below 0.2
	h.rainTime = 1
	h.updateWeather(players)
	if h.rainFlag {
		t.Fatal("rain duration ending must stop the spell")
	}
	for i := 0; i < 120; i++ {
		h.updateWeather(players)
	}
	if h.raining || h.rainLevel != 0 {
		t.Fatalf("rain must fade fully clear: level=%v", h.rainLevel)
	}
}

// TestClearWindowSuppressesSpells: an active /weather clear window forces both
// flags off and parks the timers (vanilla advanceWeatherCycle's first branch).
func TestClearWindowSuppressesSpells(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.clearTime, h.rainFlag, h.thunderFlag = 5, true, true
	h.updateWeather(players)
	if h.rainFlag || h.thunderFlag || h.clearTime != 4 {
		t.Fatalf("clear window must suppress spells: rain=%v thunder=%v clear=%d",
			h.rainFlag, h.thunderFlag, h.clearTime)
	}
	if h.rainTime != 0 || h.thunderTime != 0 {
		t.Fatalf("active spells park at 0 in a clear window: %d/%d", h.rainTime, h.thunderTime)
	}
}

// TestWeatherCommandSemantics: /weather ports setWeatherParameters — thunder
// sets BOTH flags with one shared duration; a missing duration samples the
// vanilla distribution; clear rolls a fresh delay window.
func TestWeatherCommandSemantics(t *testing.T) {
	h := newHub(world.New(1))
	h.applyWeatherCommand(evSetWeather{kind: "thunder", duration: -1})
	if !h.rainFlag || !h.thunderFlag || h.clearTime != 0 {
		t.Fatal("/weather thunder must raise both flags")
	}
	if h.rainTime != h.thunderTime || h.thunderTime < thunderDurationMin || h.thunderTime > thunderDurationMax {
		t.Fatalf("thunder duration: rain=%d thunder=%d", h.rainTime, h.thunderTime)
	}
	h.applyWeatherCommand(evSetWeather{kind: "rain", duration: 100})
	if !h.rainFlag || h.thunderFlag || h.rainTime != 100 {
		t.Fatalf("/weather rain 100: flag=%v thunder=%v time=%d", h.rainFlag, h.thunderFlag, h.rainTime)
	}
	h.applyWeatherCommand(evSetWeather{kind: "clear", duration: -1})
	if h.rainFlag || h.thunderFlag || h.clearTime < rainDelayMin || h.clearTime > rainDelayMax {
		t.Fatalf("/weather clear: flags=%v/%v window=%d", h.rainFlag, h.thunderFlag, h.clearTime)
	}
	if d, err := parseTimeArg("15s"); err != nil || d != 300 {
		t.Fatalf("15s = 300 ticks, got %d (%v)", d, err)
	}
	if d, err := parseTimeArg("1d"); err != nil || d != 24000 {
		t.Fatalf("1d = 24000 ticks, got %d (%v)", d, err)
	}
}

// TestPrecipitationTable: the vanilla climate gates — dry biomes never rain,
// cold biomes snow, and altitude pushes temperate biomes past the snow line.
func TestPrecipitationTable(t *testing.T) {
	cases := []struct {
		biome string
		y     int
		want  int
	}{
		{"minecraft:desert", 70, worldgen.PrecipNone},
		{"minecraft:savanna", 70, worldgen.PrecipNone},
		{"minecraft:badlands", 70, worldgen.PrecipNone},
		{"minecraft:plains", 70, worldgen.PrecipRain},
		{"minecraft:snowy_plains", 70, worldgen.PrecipSnow},
		{"minecraft:jagged_peaks", 200, worldgen.PrecipSnow},
		{"minecraft:taiga", 70, worldgen.PrecipRain},
		{"minecraft:taiga", 200, worldgen.PrecipSnow}, // above the snow line
		{"minecraft:plains", 200, worldgen.PrecipRain},
	}
	for _, c := range cases {
		if got := worldgen.PrecipitationAt(c.biome, c.y); got != c.want {
			t.Errorf("%s@y%d = %d, want %d", c.biome, c.y, got, c.want)
		}
	}
}

// TestLightningPrefersRods: a lightning rod crowning its column redirects a
// strike within the vanilla 128-block search (the strike lands on its tip).
func TestLightningPrefersRods(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	surf := h.world.SurfaceFeet(10, 10)
	rod := worldgen.BlockID("waxed_lightning_rod") // the whole family attracts
	h.world.SetBlock(10, surf, 10, rod)
	h.rodIndexOnBlockChange(10, surf, 10, rod)
	x, y, z, onRod := h.findLightningTarget(players, 40, 40)
	if !onRod || x != 10 || z != 10 || y != surf+1 {
		t.Fatalf("strike must land on the rod tip: (%d,%d,%d) onRod=%v (rod at 10,%d,10)", x, y, z, onRod, surf)
	}
	// breaking the rod drops it from the index
	h.rodIndexOnBlockChange(10, surf, 10, worldgen.Air)
	if _, _, _, onRod := h.findLightningTarget(players, 40, 40); onRod {
		t.Fatal("a broken rod must stop attracting")
	}
	// end rods are not lightning rods
	if isLightningRodState(worldgen.BlockID("end_rod")) {
		t.Fatal("end rods must not attract lightning")
	}
}

// TestRainShieldsTheUndead — unchanged behavior: h.raining keeps its meaning.
func TestRainShieldsTheUndead(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	h.dayTime.Store(6000) // noon — burn time
	z := h.spawnHostile(players, entityZombie, 5, 5)
	z.burnDelay = 0
	// Seat it on a dry, open-sky pillar (the generated column at 5,5 is ocean;
	// a submerged zombie is correctly doused and wouldn't burn).
	z.x, z.y, z.z = 6.5, 100, 6.5
	h.world.SetBlock(6, 99, 6, worldgen.Stone)
	h.world.SetBlock(6, 100, 6, worldgen.Air)
	h.world.SetBlock(6, 101, 6, worldgen.Air)
	h.raining = true
	hp := z.health
	h.updateHostiles(players)
	h.mobEnvironment(players)
	if z.health != hp || z.burning {
		t.Fatalf("zombies must not burn in the rain: health=%d burning=%v", z.health, z.burning)
	}
	h.raining = false
	h.updateHostiles(players)
	h.mobEnvironment(players)
	if z.health == hp && !z.burning {
		t.Fatal("clear noon sky must burn the zombie")
	}
}

func TestLightningStrikeDamages(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityCow, 1.5, 64, 1.5)
	hp := pl.health
	h.strikeLightning(players, 0.5, 64, 0.5, false)
	if pl.health >= hp {
		t.Fatal("a direct strike must hurt the player")
	}
	if m.health >= cowHealth {
		// cow at 1.5,1.5 is within the 3-block strike box
	} else if m.health != cowHealth-lightningDamage {
		t.Fatalf("cow health = %d", m.health)
	}
	if len(h.bolts) != 1 {
		t.Fatal("the bolt entity should be alive")
	}
	h.tick.Store(h.tick.Load() + boltLifeTicks + 1)
	h.updateBolts(players)
	if len(h.bolts) != 0 {
		t.Fatal("the bolt must despawn after its flash")
	}
	// a skeleton-trap bolt is visual-only: flash and thunder, no damage
	hp = pl.health
	h.strikeLightning(players, 0.5, 64, 0.5, true)
	if pl.health != hp {
		t.Fatal("a visual-only bolt must not hurt anyone")
	}
}

func TestSleepClearsWeather(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	h.dayTime.Store(14000)
	h.rainFlag, h.thunderFlag = true, true
	h.rainLevel, h.thunderLevel = 1, 1
	h.raining, h.thundering = true, true
	pl.sleeping, pl.sleepingAt = true, 0
	h.tick.Store(sleepSkipTicks + 1)
	h.updateSleep(players)
	if h.rainFlag || h.thunderFlag || h.rainTime != 0 || h.thunderTime != 0 {
		t.Fatal("sleeping through the night must reset the weather cycle")
	}
	for i := 0; i < 120; i++ { // fresh delays roll; the sky fades clear
		h.updateWeather(players)
	}
	if h.raining || h.thundering {
		t.Fatal("the storm must fade out after the sleep reset")
	}
	if h.rainTime < rainDelayMin-121 || h.thunderTime < thunderDelayMin-121 {
		t.Fatalf("fresh vanilla delays must roll: rain=%d thunder=%d", h.rainTime, h.thunderTime)
	}
}

// TestWeatherPersistence: the five vanilla WeatherData fields survive a
// restart via settings.json, and a restored storm resumes at full level.
func TestWeatherPersistence(t *testing.T) {
	path := t.TempDir() + "/settings.json"
	h := newHub(world.New(1))
	h.rulesPath = path
	h.clearTime, h.rainTime, h.thunderTime = 0, 777, 555
	h.rainFlag, h.thunderFlag = true, true
	h.saveRules()

	h2 := newHub(world.New(1))
	h2.rulesPath = path
	h2.loadRules()
	if h2.rainTime != 777 || h2.thunderTime != 555 || !h2.rainFlag || !h2.thunderFlag {
		t.Fatalf("weather state lost across restart: %+v", h2.rules.Weather)
	}
	if h2.rainLevel != 1 || h2.thunderLevel != 1 || !h2.raining || !h2.thundering {
		t.Fatal("a restored storm must resume at full level (vanilla prepareWeather)")
	}
}

// TestRainGameEventIDs pins the begin/end rain ids to the client's vanilla
// ClientboundGameEventPacket.Type table (START_RAINING=1, STOP_RAINING=2 in
// BOTH 1.21.5 and 26.2). They were inverted for the engine's whole life —
// clients rendered clear skies during rain (while the engine, correctly,
// shielded the undead from the daylight burn) and rain after it ended.
func TestRainGameEventIDs(t *testing.T) {
	if gameEventBeginRain != 1 || gameEventEndRain != 2 {
		t.Fatalf("rain game events drifted from the client enum: begin=%d end=%d (want 1/2)",
			gameEventBeginRain, gameEventEndRain)
	}
}
