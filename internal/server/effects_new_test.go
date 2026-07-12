package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func survPlayer(h *hub) *tracked {
	t := testTracked()
	t.gamemode = gmSurvival
	initSurvival(t)
	return t
}

func TestResistanceReducesDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.applyEffect(players, pl, effResistance, 1, 30) // Resistance II = -40%
	pl.health = 20
	h.damage(players, pl, 10)
	if pl.health != 14 { // 10 * (25-2*5)/25 = 10*0.6 = 6 taken
		t.Fatalf("Resistance II: health=%v, want 14 (6 taken)", pl.health)
	}
}

func TestAbsorptionSoaksDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.applyEffect(players, pl, effAbsorption, 0, 120) // 4 HP buffer
	pl.health = 20
	h.damage(players, pl, 3) // fully soaked
	if pl.health != 20 || pl.absorption != 1 {
		t.Fatalf("absorption soak: health=%v absorption=%v, want 20/1", pl.health, pl.absorption)
	}
	h.damage(players, pl, 5) // 1 soaked, 4 to health
	if pl.health != 16 || pl.absorption != 0 {
		t.Fatalf("absorption overflow: health=%v absorption=%v, want 16/0", pl.health, pl.absorption)
	}
}

func TestSlowFallingNoFallDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.applyEffect(players, pl, effSlowFalling, 0, 30)
	pl.health, pl.airborne, pl.peakY = 20, true, 90
	h.onFallAndExhaust(players, pl, evMove{x: pl.x, y: 60, z: pl.z, onGround: true}) // 30-block drop
	if pl.health != 20 {
		t.Fatalf("Slow Falling should negate fall damage, health=%v", pl.health)
	}
}
