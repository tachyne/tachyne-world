package server

import (
	"math"
	"testing"
)

func angleDeg(ax, ay, az, bx, by, bz float64) float64 {
	d := (ax*bx + ay*by + az*bz) / (math.Sqrt(ax*ax+ay*ay+az*az) * math.Sqrt(bx*bx+by*by+bz*bz))
	return math.Acos(math.Max(-1, math.Min(1, d))) * 180 / math.Pi
}

// CrossbowItem.performShooting shoots with uncertainty 1: bolts from the
// same aim scatter a little, around 3.15 a tick.
func TestCrossbowShotsScatter(t *testing.T) {
	h, pl, players := xbowSetup()
	pl.pitch = -20
	lx, ly, lz := lookVector(pl.yaw, pl.pitch)
	var first [3]float64
	spread := 0.0
	for i := 0; i < 20; i++ {
		h.arrows = map[int32]*arrowEntity{}
		pl.inv.slots[0].load = xbowLoad{item: itemArrowAmmo, n: 1}
		h.useXbow(players, pl)
		a := onlyProjectile(t, h)
		if sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz); math.Abs(sp-xbowSpeed) > 0.1 {
			t.Fatalf("bolt speed %.3f, want about %.2f", sp, xbowSpeed)
		}
		if ang := angleDeg(a.vx, a.vy, a.vz, lx, ly, lz); ang > 2 {
			t.Fatalf("a bolt left %.2f° off the aim", ang)
		}
		if i == 0 {
			first = [3]float64{a.vx, a.vy, a.vz}
		}
		spread = math.Max(spread, angleDeg(a.vx, a.vy, a.vz, first[0], first[1], first[2]))
	}
	if spread < 0.05 {
		t.Fatal("twenty crossbow bolts from one aim flew exactly alike: no scatter")
	}
}

// Multishot fans its side bolts 10° either side of the aim about the
// player's own up axis, so the fan stays 10° wide however steeply they aim.
func TestCrossbowMultishotFanFollowsPitch(t *testing.T) {
	h, pl, players := xbowSetup()
	pl.pitch = -50
	lx, ly, lz := lookVector(pl.yaw, pl.pitch)
	sum, n := 0.0, 0
	for i := 0; i < 40; i++ {
		h.arrows = map[int32]*arrowEntity{}
		pl.inv.slots[0].load = xbowLoad{item: itemArrowAmmo, n: 3}
		h.useXbow(players, pl)
		if len(h.arrows) != 3 {
			t.Fatalf("multishot loosed %d bolts", len(h.arrows))
		}
		for _, a := range h.arrows {
			if a.noPickup { // a side bolt
				sum += angleDeg(a.vx, a.vy, a.vz, lx, ly, lz)
				n++
			}
		}
	}
	if avg := sum / float64(n); math.Abs(avg-10) > 0.5 {
		t.Fatalf("side bolts leave on average %.2f° off the aim, want 10°", avg)
	}
}
