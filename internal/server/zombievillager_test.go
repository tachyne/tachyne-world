package server

import (
	"tachyne/internal/world"
	"testing"
)

// Zombie villagers reach players via the night-spawn variant roll
// (hostilePick: 1-in-20 zombies). Vanilla burns them in daylight like their
// base zombie — verify the whole path: spawn, flag, ignition, death.
func TestZombieVillagerBurnsInDaylight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	players[1] = pl
	lx, lz := h.findLand(0, 0)
	m := h.spawnHostileY(players, entityZombieVillager, float64(lx), float64(h.world.SurfaceFeet(lx, lz)), float64(lz))
	if !m.burns {
		t.Fatal("zombie villager must carry the daylight-burn flag (vanilla parity)")
	}
	m.burnDelay = 0
	m.health = burnDamagePerSec
	h.dayTime.Store(3000) // 09:00 — the report
	h.updateHostiles(players)
	h.mobEnvironment(players)
	if !m.burning || m.dying == 0 {
		t.Fatalf("sky-exposed zombie villager at 09:00: burning=%v dying=%d", m.burning, m.dying)
	}
}
