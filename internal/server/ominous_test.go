package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The trial-chamber effects are LivingEntity-wide: a mob that dies oozing
// splits slimes, one that dies weaving leaves cobwebs, and an infested one
// bursts silverfish when it is hurt.
func TestOminousEffectsReachMobs(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.rules.MobGriefing = true
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 69, z, worldgen.Stone)
			h.world.SetBlock(x, 70, z, worldgen.Air)
			h.world.SetBlock(x, 71, z, worldgen.Air)
		}
	}
	m := h.spawnMob(players, entityZombie, 0.5, 70, 0.5)
	m.effects = map[int32]*activeEffect{effOozing: {amp: 0, left: 200}, effWeaving: {amp: 0, left: 200}}
	h.killMob(players, m)

	slimes := 0
	for _, o := range h.mobs {
		if o.etype == entitySlime {
			slimes++
		}
	}
	if slimes == 0 {
		t.Error("an oozing mob should split slimes when it dies")
	}
	webs := 0
	for x := -2; x <= 2; x++ {
		for y := 69; y <= 71; y++ {
			for z := -2; z <= 2; z++ {
				if wb, _ := worldgen.BlockRange("cobweb"); wb != 0 && h.world.At(x, y, z) == wb {
					webs++
				}
			}
		}
	}
	if webs == 0 {
		t.Error("a weaving mob should leave cobwebs")
	}

	// Infested bursts on a hit, not on death.
	inf := h.spawnMob(players, entityZombie, 0.5, 70, 0.5)
	inf.effects = map[int32]*activeEffect{effInfested: {amp: 0, left: 200}}
	inf.health = 200
	fish := 0
	for i := 0; i < 200 && fish == 0; i++ {
		h.infestOnMobHurt(players, inf)
		for _, o := range h.mobs {
			if o.etype == entitySilverfish {
				fish++
			}
		}
	}
	if fish == 0 {
		t.Error("an infested mob should burst silverfish when it is hurt")
	}
}
