package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// universal_anger: a provoked neutral mob holds no grudge against one
// player in particular (it hunts whoever is nearest), and a death does not
// buy forgiveness.
func TestUniversalAnger(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	h.applyRule(players, evSetRule{rule: "universal_anger", on: true})
	wolf := h.spawnMob(players, entityWolf, 3.5, 80, 0.5)
	h.provoke(wolf, pl)
	if !wolf.hostile || wolf.targetEID != 0 {
		t.Fatalf("universal anger: hostile=%v target=%d, want hostile with no pinned target", wolf.hostile, wolf.targetEID)
	}
	h.deathForgiveness(players, pl)
	if !wolf.hostile {
		t.Fatal("universal anger holds the grudge past a death")
	}
	h.applyRule(players, evSetRule{rule: "universal_anger", on: false})
	h.provoke(wolf, pl)
	if wolf.targetEID != pl.p.eid {
		t.Fatal("without universal anger the attacker is the target")
	}
}

// Stepping into water is a SPLASH vibration (2) once, on entry; a sneaking
// entry is silent.
func TestSplashVibrationOnEnteringWater(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(x, y, z, sensor)
	pos := blockPos{x, y, z}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.playersRef = players
	w.SetBlock(x+3, y, z, worldgen.WaterBase)
	pl.x, pl.y, pl.z = float64(x)+3.5, float64(y)+1, float64(z)+0.5
	move := func(px, py, pz float64) {
		h.onFallAndExhaust(players, pl, evMove{eid: pl.p.eid, x: px, y: py, z: pz, onGround: false})
		pl.x, pl.y, pl.z = px, py, pz
	}
	move(float64(x)+3.5, float64(y), float64(z)+0.5) // into the water
	if v, ok := h.sculkVib[pos]; !ok || v.freq != freqSplash {
		t.Fatalf("entering water should be heard at 2: %+v ok=%v", v, ok)
	}
	delete(h.sculkVib, pos)
	move(float64(x)+3.5, float64(y), float64(z)+0.6) // still in it
	if _, again := h.sculkVib[pos]; again {
		t.Fatal("splash fires on entry only")
	}
	move(float64(x)+3.5, float64(y)+1, float64(z)+0.5) // out
	pl.p.sneaking = true
	move(float64(x)+3.5, float64(y), float64(z)+0.5)
	if _, heard := h.sculkVib[pos]; heard {
		t.Fatal("a sneaking entry is silent")
	}
}

// The mount inventory closes when the mount dies or is left behind.
func TestMountMenuClosesWhenInvalid(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	horse := h.spawnMob(players, entityHorse, 1.5, 80, 0.5)
	horse.tamed = true
	h.openHorseScreen(players, pl, horse)
	if pl.winKind != winHorse {
		t.Fatalf("no mount window: kind %d", pl.winKind)
	}
	drainEvents(pl)
	h.validateWindows(players)
	if pl.winKind != winHorse {
		t.Fatal("a valid mount menu closed")
	}
	pl.x = 12.5
	h.validateWindows(players)
	if pl.winKind != winPlayer || len(closeFrames(pl)) != 1 {
		t.Fatal("walking off should close the mount menu")
	}
}
