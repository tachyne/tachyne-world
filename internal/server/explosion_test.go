package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Vanilla's blast damage: (impact² + impact)/2 × 7 × 2r + 1 for what the
// blast can see — a power-1 blast a block off does about 5.5 in the open,
// a stone wall between leaves only the flat 1, and out of reach is out of
// reach. The shove comes as a velocity from the eyes.
func TestExplosionDamageModel(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	open, walled, far := testTracked(), testTracked(), testTracked()
	walled.p.eid, far.p.eid = 2, 3
	open.x, open.y, open.z = 1.5, 180, 0.5        // a block east of the blast
	walled.x, walled.y, walled.z = -1.4, 180, 0.5 // just within reach to the west, behind a wall
	far.x, far.y, far.z = 0.5, 180, 4.5           // out of a power-1 blast's reach
	for y := 179; y <= 183; y++ {
		w.SetBlock(-1, y, 0, worldgen.Stone) // the wall (the player stands in cell -2)
	}
	players := map[int32]*tracked{1: open, 2: walled, 3: far}
	h.playersRef = players
	for _, pl := range players {
		pl.health = 20
		drainEvents(pl)
	}
	h.explodeIn(players, 0, 0.5, 180.5, 0.5, 0, 1, blastMob)
	want := explosionDamage(1, explosionImpact(1, 0.5, 180.5, 0.5, 1.5, 180, 0.5, 1)) // ≈5.45
	if d := 20 - open.health; math.Abs(float64(d)-want) > 0.05 {
		t.Fatalf("in the open a block off: took %v, want %.2f", d, want)
	}
	if d := 20 - walled.health; math.Abs(float64(d)-1) > 0.01 {
		t.Fatalf("behind a wall: took %v, want the flat 1", d)
	}
	if far.health != 20 {
		t.Fatalf("out of reach took %v", 20-far.health)
	}
	// A Java client's shove rides the explode packet, which it ADDS to its
	// motion (ClientboundExplodePacket.playerKnockback).
	var ex []attachproto.Explode
	for done := false; !done; {
		select {
		case pkt := <-open.p.out:
			if v, ok := pkt.ev.(attachproto.Explode); ok {
				ex = append(ex, v)
			}
			if _, ok := pkt.ev.(attachproto.Velocity); ok {
				t.Fatal("a Java player's shove must not replace their motion")
			}
		default:
			done = true
		}
	}
	if len(ex) != 1 || ex[0].Knockback == nil || ex[0].Knockback[0] <= 0.2 || ex[0].Knockback[1] <= 0 {
		t.Fatalf("the shove: %+v, want one explode, eastward and upward", ex)
	}
	if got := explosionDamage(4, 1); math.Abs(got-57) > 1e-9 {
		t.Fatalf("TNT at the feet does %v, want 57", got)
	}
}

// A mob in a blast takes the same damage and is shoved off.
func TestExplosionHurtsMobs(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityCow, 1.5, 180, 0.5)
	hp := m.health
	h.explodeIn(players, 0, 0.5, 180.5, 0.5, 0, 1, blastMob)
	if d := hp - m.health; d < 5 || d > 6 {
		t.Fatalf("the cow took %v, want ~5.5", d)
	}
	if m.vx <= 0 || m.kb == 0 {
		t.Fatalf("the cow was not shoved: vx %v kb %d", m.vx, m.kb)
	}
}
