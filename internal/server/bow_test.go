package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func bowSetup() (*hub, *tracked, map[int32]*tracked) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5 // high in the air: clear flight path
	pl.p.setHotbarSlot(0, itemBow)
	pl.inv.slots[0] = invStack{item: itemBow, count: 1}
	pl.inv.slots[1] = invStack{item: itemArrowAmmo, count: 5}
	h.tick.Store(100)
	return h, pl, map[int32]*tracked{1: pl}
}

func TestBowChargeScalesTheShot(t *testing.T) {
	h, pl, players := bowSetup()
	h.startDraw(pl)
	if pl.drawingAt == 0 {
		t.Fatal("draw should start with a bow + ammo in hand")
	}
	h.tick.Add(bowFullDraw) // full power
	h.releaseDraw(players, pl)
	if len(h.arrows) != 1 {
		t.Fatalf("full draw must loose an arrow, got %d", len(h.arrows))
	}
	for _, a := range h.arrows {
		if !a.playerShot || a.dmg < 6 {
			t.Fatalf("full-charge arrow should be player-shot at ≥6 dmg: %+v", a)
		}
		if sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz); sp < bowMaxSpeed*0.95 {
			t.Fatalf("full-charge speed = %.2f, want ~%v", sp, bowMaxSpeed)
		}
	}
	if pl.inv.slots[1].count != 4 {
		t.Fatalf("one arrow should be consumed, left %d", pl.inv.slots[1].count)
	}
}

func TestBowFizzlesAndGates(t *testing.T) {
	h, pl, players := bowSetup()
	h.startDraw(pl)
	h.tick.Add(1) // a twitch, not a draw
	h.releaseDraw(players, pl)
	if len(h.arrows) != 0 {
		t.Fatal("a 1-tick pull must fizzle")
	}
	pl.inv.slots[1] = invStack{} // out of ammo
	h.startDraw(pl)
	if pl.drawingAt != 0 {
		t.Fatal("no ammo → no draw (survival)")
	}
	// A hotbar switch lowers the bow without firing.
	pl.inv.slots[1] = invStack{item: itemArrowAmmo, count: 1}
	h.startDraw(pl)
	h.tick.Add(bowFullDraw)
	pl.drawingAt = 0 // what the evStopEat{fire:false} path does
	h.releaseDraw(players, pl)
	if len(h.arrows) != 0 {
		t.Fatal("a lowered bow must not fire")
	}
}

func TestPlayerArrowKillsMobAndPaysXP(t *testing.T) {
	h, pl, players := bowSetup()
	m := &mob{eid: 9, etype: entityZombie, hostile: true, health: 2, x: 0.5, y: 80, z: 6.5}
	h.mobs[9] = m
	pl.yaw, pl.pitch = 0, 0 // facing +z, level
	h.startDraw(pl)
	h.tick.Add(bowFullDraw)
	h.releaseDraw(players, pl)
	for i := 0; i < 20 && len(h.arrows) > 0; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if _, alive := h.mobs[9]; alive && m.dying == 0 {
		t.Fatalf("arrow should have killed the 2 HP zombie: health=%d", m.health)
	}
	if !m.hitByPlayer {
		t.Fatal("bow kills must count as player kills (XP)")
	}
}

func TestSnowballThrowsAndShatters(t *testing.T) {
	h, pl, players := bowSetup()
	pl.inv.slots[2] = invStack{item: itemSnowball, count: 3}
	h.throwProjectile(players, pl, itemSnowball)
	if pl.inv.slots[2].count != 2 || len(h.arrows) != 1 {
		t.Fatalf("throw should consume one snowball and fly: count=%d arrows=%d", pl.inv.slots[2].count, len(h.arrows))
	}
	for _, a := range h.arrows {
		if !a.breaks || a.dmg != 0 {
			t.Fatalf("snowballs shatter and deal nothing: %+v", a)
		}
	}
}
