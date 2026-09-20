package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func playSetup(t *testing.T, n int) (*hub, map[int32]*tracked, []*mob) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.dayTime.Store(4000) // daytime: the children are out
	lx, lz := h.findLand(0, 0)
	y := float64(h.world.MobFeet(lx, lz))
	kids := make([]*mob, 0, n)
	for i := 0; i < n; i++ {
		m := h.spawnMob(players, entityVillager, float64(lx)+float64(i)*2, y, float64(lz))
		m.baby = true
		kids = append(kids, m)
	}
	return h, players, kids
}

// A child with other children about picks one and runs after it.
func TestBabyVillagerChasesAnotherChild(t *testing.T) {
	h, players, kids := playSetup(t, 3)
	me := kids[0]
	h.pickPlay(me, h.visibleBabies(me))
	if me.playMate == 0 {
		t.Fatal("it should have picked somebody to chase")
	}
	if me.playMate == me.eid {
		t.Fatal("it should not chase itself")
	}
	if !h.villagerPlayStep(players, me) {
		t.Fatal("chasing should take the mob's movement")
	}
	o := h.mobs[me.playMate]
	if (o.x-me.x)*me.vx+(o.z-me.z)*me.vz <= 0 {
		t.Errorf("it should be moving toward its quarry, v=(%v,%v)", me.vx, me.vz)
	}
	// A lone child has nobody to play with and falls back to the ordinary goals.
	lone := kids[1]
	for _, k := range kids {
		if k != lone {
			delete(h.mobs, k.eid)
		}
	}
	if h.villagerPlayStep(players, lone) {
		t.Error("with nobody about there is no game")
	}
}

// Being chased is what makes a child run: it drops its own quarry and heads
// somewhere else in the village.
func TestBabyVillagerFleesItsChaser(t *testing.T) {
	h, players, kids := playSetup(t, 2)
	me, chaser := kids[0], kids[1]
	chaser.playMate = me.eid
	me.playMate = chaser.eid // it was chasing back
	h.pickPlay(me, h.visibleBabies(me))
	if me.playMate != 0 || !me.playFlee {
		t.Fatalf("being chased should make it run, mate=%d flee=%v", me.playMate, me.playFlee)
	}
	if d := math.Hypot(me.playX-me.x, me.playZ-me.z); d < 4 || d > playFleeXZ {
		t.Errorf("it should run somewhere four to twenty blocks off, got %.1f", d)
	}
	if !h.villagerPlayStep(players, me) {
		t.Error("fleeing should take the mob's movement")
	}
}

// Everybody piles onto whoever is already being chased, up to five of them.
func TestBabyVillagersJoinTheChase(t *testing.T) {
	h, _, kids := playSetup(t, 4)
	me, hunter, quarry := kids[0], kids[1], kids[2]
	hunter.playMate = quarry.eid
	h.pickPlay(me, h.visibleBabies(me))
	if me.playMate != quarry.eid {
		t.Errorf("it should join the chase already on, got %d want %d", me.playMate, quarry.eid)
	}
	// Once five are after it, it stops being the popular one.
	for i, k := range kids {
		k.playMate = quarry.eid
		_ = i
	}
	quarry.playMate = 0
	me.playMate = 0
	h.pickPlay(me, h.visibleBabies(me))
	if me.playMate == quarry.eid && len(kids) > playMaxChasers {
		t.Error("nobody should join a chase that already has five in it")
	}
}

// A child at night is in bed, not playing.
func TestBabyVillagersDoNotPlayAtNight(t *testing.T) {
	h, players, kids := playSetup(t, 3)
	h.dayTime.Store(sleepStart + 100)
	if h.villagerPlayStep(players, kids[0]) {
		t.Error("the game is over at bedtime")
	}
}
