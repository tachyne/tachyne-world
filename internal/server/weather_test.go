package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func TestWeatherCycleFlips(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{1: testTracked()}
	if h.raining {
		t.Fatal("worlds start clear")
	}
	h.weatherLeft = 1
	h.updateWeather(players)
	if !h.raining || h.weatherLeft < rainMinTicks-survivalTickN {
		t.Fatalf("clear spell ending must start rain: raining=%v left=%d", h.raining, h.weatherLeft)
	}
	h.weatherLeft = 1
	h.updateWeather(players)
	if h.raining || h.thundering {
		t.Fatal("rain spell ending must clear the sky")
	}
}

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
	h.strikeLightning(players, 0.5, 64, 0.5)
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
}

func TestSleepClearsWeather(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	h.dayTime.Store(14000)
	h.raining, h.thundering = true, true
	pl.sleeping, pl.sleepingAt = true, 0
	h.tick.Store(sleepSkipTicks + 1)
	h.updateSleep(players)
	if h.raining || h.thundering {
		t.Fatal("sleeping through the night must clear the storm")
	}
}
