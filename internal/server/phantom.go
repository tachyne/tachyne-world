package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Phantoms, and the insomnia that summons them.
//
// The old driver was a flat 1-in-30 roll for a phantom over some player, and
// its own comment admitted the deviation. That misses the entire point of the
// mob: a phantom is the punishment for not sleeping, so a player who beds down
// every night should never see one and a player who has not slept in a week
// should be hounded. As built, sleeping changed nothing.
//
// This is vanilla's PhantomSpawner: a 60-119 second timer, then a per-player
// check gated on the insomnia clock (`time_since_rest`, reset by climbing into
// a bed) and on the player being under open sky at or above sea level.

const (
	// The chance ramps with the clock: nextInt(timeSinceRest) must reach three
	// days' worth of ticks, so it cannot fire at all below 72000 and grows
	// steadily likelier after that.
	phantomInsomniaTicks = 72000
	phantomSpawnMinY     = worldgen.SeaLevel
)

// phantomNextDelay is the spawner's own cadence: 60-119 seconds.
func (h *hub) phantomNextDelay() uint64 { return uint64(60+h.rng.Intn(60)) * 20 }

// phantomSpawner runs vanilla's CustomSpawner tick. Called once per hub tick
// while it is dark; it rate-limits itself.
func (h *hub) phantomSpawner(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful {
		return
	}
	if !h.rules.SpawnPhantoms {
		return // gamerule spawn_phantoms
	}
	now := h.tick.Load()
	if now < h.phantomNextAt {
		return
	}
	h.phantomNextAt = now + h.phantomNextDelay()
	for _, t := range players {
		if t.dim != 0 || t.dead || t.gamemode != gmSurvival {
			continue
		}
		// Vanilla requires open sky at or above sea level: phantoms do not
		// find you underground, which is the other half of how you avoid them.
		if t.y < phantomSpawnMinY || !h.skyExposedAt(int(math.Floor(t.x)), int(math.Floor(t.y)), int(math.Floor(t.z))) {
			continue
		}
		// Harder difficulties clear this more often (vanilla isHarderThan).
		if float64(h.rules.Difficulty) <= h.rng.Float64()*3 {
			continue
		}
		// The insomnia roll. Below three days the clock cannot reach the
		// threshold at all, so a player who sleeps is simply never harried.
		since := customStat(t, "time_since_rest")
		if since < 1 {
			since = 1
		}
		if int32(h.rng.Intn(int(since))) < phantomInsomniaTicks {
			continue
		}
		x := t.x + float64(h.rng.Intn(21)-10)
		z := t.z + float64(h.rng.Intn(21)-10)
		y := t.y + float64(20+h.rng.Intn(15))
		// A flight of them, bigger on harder difficulties.
		n := 1 + h.rng.Intn(h.rules.Difficulty+1)
		for i := 0; i < n; i++ {
			m := h.spawnSpecies(players, entityPhantom, t.dim, x, y, z)
			if m == nil {
				continue
			}
			m.hasTarget, m.tx, m.tz, m.ty = true, t.x, t.z, t.y
		}
	}
}
