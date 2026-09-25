package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
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
	h.damageOf(players, pl, 10, dtGeneric)
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
	h.damageOf(players, pl, 3, dtGeneric) // fully soaked
	if pl.health != 20 || pl.absorption != 1 {
		t.Fatalf("absorption soak: health=%v absorption=%v, want 20/1", pl.health, pl.absorption)
	}
	h.tick.Add(20)                        // past the first blow's damage cooldown
	h.damageOf(players, pl, 5, dtGeneric) // 1 soaked, 4 to health
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

// A player's fall reads SAFE_FALL_DISTANCE and FALL_DAMAGE_MULTIPLIER, so
// /attribute (or a plugin) moves both: an 8-block drop is 5 damage, 10 at a
// multiplier of 2, and nothing with a safe distance of 10.
func TestPlayerFallReadsAttributes(t *testing.T) {
	for _, c := range []struct {
		safe, mult float64
		want       float32
	}{{3, 1, 15}, {3, 2, 10}, {10, 1, 20}} {
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 1)
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		a := pl.playerAttrs()
		a.SetBase(attr.SafeFallDistance, c.safe)
		a.SetBase(attr.FallDamageMultiplier, c.mult)
		pl.health, pl.airborne, pl.peakY = 20, true, 188
		h.world.SetBlock(0, 179, 0, worldgen.BlockBase("stone"))
		h.onFallAndExhaust(players, pl, evMove{x: 0.5, y: 180, z: 0.5, onGround: true})
		if pl.health != c.want {
			t.Errorf("safe %v ×%v: health %v, want %v", c.safe, c.mult, pl.health, c.want)
		}
	}
}
