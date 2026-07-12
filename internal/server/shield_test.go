package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestShieldBlocksFrontArc(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(100)
	pl := testTracked()
	pl.x, pl.z, pl.yaw = 0, 0, -90 // facing +x
	pl.blockingSince = 90          // raised 10 ticks ago (past the delay)
	if !h.shieldBlocks(pl, 5, 0) {
		t.Fatal("should block an attacker in front (+x)")
	}
	if h.shieldBlocks(pl, -5, 0) {
		t.Fatal("should NOT block an attacker behind (-x)")
	}
}

func TestShieldBlockDelay(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(100)
	pl := testTracked()
	pl.yaw = -90
	pl.blockingSince = 98 // only 2 ticks ago — under the 5-tick raise delay
	if h.shieldBlocks(pl, 5, 0) {
		t.Fatal("a shield under the raise delay should not block yet")
	}
}

func TestRaiseShieldRequiresShield(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(50)
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemShield)
	h.raiseShield(pl)
	if pl.blockingSince != 50 {
		t.Fatalf("holding a shield should raise it, blockingSince=%d", pl.blockingSince)
	}
	pl2 := testTracked()
	pl2.p.setHotbarSlot(0, itemBow)
	h.raiseShield(pl2)
	if pl2.blockingSince != 0 {
		t.Fatal("a non-shield item should not raise a block")
	}
}
