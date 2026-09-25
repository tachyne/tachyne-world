package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A mob standing about eventually watches a nearby player (LookAtPlayerGoal),
// turning its head without turning its body, and lets go when the look runs
// out or the player leaves.
func TestIdleLookWatchesAPlayer(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 3, 70, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	m := &mob{eid: 9, etype: entityCow, x: 0, y: 70, z: 0, yaw: 180}

	looked := false
	for i := 0; i < 2000 && !looked; i++ {
		h.idleLook(players, m)
		looked = m.lookTicks > 0 && m.lookEID == pl.p.eid
	}
	if !looked {
		t.Fatal("a cow beside a player should start watching them")
	}
	h.idleLook(players, m)
	if math.Abs(float64(m.headYaw-(-90))) > 1 {
		t.Fatalf("the head should turn toward the player to the east (yaw -90), got %v", m.headYaw)
	}
	if m.yaw != 180 {
		t.Fatalf("the body must not turn to look: yaw %v", m.yaw)
	}
	// Out of range: the look ends and the head returns to the body.
	pl.x = 100
	m.lookTicks = 4
	h.idleLook(players, m)
	h.idleLook(players, m)
	if m.lookTicks > 0 && m.lookEID != 0 {
		t.Fatal("a player out of reach should end the look")
	}
}

// Look ranges come from the vanilla goals, and the species with no look
// goals at all never turn their heads.
func TestLookRanges(t *testing.T) {
	cases := map[int]float64{
		entityCow: 6, entityZombie: 8, entityCat: 10, entityPillager: 15,
		entitySquid: 0, entityCod: 0, entityCreeper: 8,
		entityID("giant"): 0, // no goals at all
		entityPanda:       6, entitySniffer: 6, entityTadpole: 6,
	}
	for et, want := range cases {
		if got := lookRange(&mob{etype: et}); got != want {
			t.Errorf("%s look range %v, want %v", entityNameByID[et], got, want)
		}
	}
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	sq := &mob{eid: 3, etype: entitySquid, yaw: 45}
	for i := 0; i < 500; i++ {
		h.idleLook(players, sq)
	}
	if sq.lookTicks != 0 || sq.headYaw != 45 {
		t.Fatalf("a squid has no look goals: ticks=%d head=%v", sq.lookTicks, sq.headYaw)
	}
}

// A blow on one of a kind rouses its neighbours within follow range —
// HurtByTargetGoal.setAlertOthers — while a species without the goal, or a
// neighbour already fighting, is left alone.
func TestAlertKinRousesNeighbours(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0, 70, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	hit := h.spawnMob(players, entityZombie, 20, 70, 20)
	near := h.spawnMob(players, entityZombie, 22, 70, 21)
	husk := h.spawnMob(players, entityHusk, 24, 70, 20)
	piglin := h.spawnMob(players, entityZombifiedPiglin, 22, 70, 22)
	far := h.spawnMob(players, entityZombie, 20, 70, 300)
	busy := h.spawnMob(players, entityZombie, 21, 70, 21)
	busy.targetEID = 777

	h.alertKin(hit, pl)
	if near.targetEID != pl.p.eid {
		t.Errorf("a zombie beside the struck one should join in, target=%d", near.targetEID)
	}
	if husk.targetEID != pl.p.eid {
		t.Errorf("a husk answers a zombie's cry (vanilla alerts by class), target=%d", husk.targetEID)
	}
	if piglin.targetEID != 0 {
		t.Errorf("a zombified piglin is the one the goal excludes, target=%d", piglin.targetEID)
	}
	if far.targetEID != 0 {
		t.Errorf("a zombie far out of follow range should not hear it, target=%d", far.targetEID)
	}
	if busy.targetEID != 777 {
		t.Errorf("a zombie already fighting keeps its target, got %d", busy.targetEID)
	}
	// A husk's class is Husk: its cry does not reach a plain zombie.
	hz := h.spawnMob(players, entityZombie, 60, 70, 60)
	hh := h.spawnMob(players, entityHusk, 61, 70, 60)
	h.alertKin(hh, pl)
	if hz.targetEID != 0 {
		t.Errorf("a husk alerts husks only, zombie target=%d", hz.targetEID)
	}
	// The search is a box r out and 10 below / 11 above, not a sphere: a
	// zombie off the corner at (r, r) and one 10.5 above both hear it.
	box := h.spawnMob(players, entityZombie, 100, 70, 100)
	r := box.followRange()
	corner := h.spawnMob(players, entityZombie, 100+r-0.5, 70, 100+r-0.5)
	above := h.spawnMob(players, entityZombie, 100, 80.5, 102)
	h.alertKin(box, pl)
	if corner.targetEID != pl.p.eid || above.targetEID != pl.p.eid {
		t.Errorf("the alert box reaches its corners and 11 up: corner=%d above=%d", corner.targetEID, above.targetEID)
	}
	// A species without the goal rouses nobody.
	sk := h.spawnMob(players, entitySkeleton, 40, 70, 40)
	sk2 := h.spawnMob(players, entitySkeleton, 41, 70, 41)
	h.alertKin(sk, pl)
	if sk2.targetEID != 0 {
		t.Errorf("skeletons have no setAlertOthers, target=%d", sk2.targetEID)
	}
}
