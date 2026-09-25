package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestFoxTrust: a cub born of foxes fed by a player trusts them, a
// trusted player is not fled, and what hurts a trusted player becomes the
// fox's quarry.
func TestFoxTrust(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	f := h.spawnMob(players, entityFox, 0.5, 180, 0.5)
	if avoidPlayerExempt(f, pl) {
		t.Fatal("a wild fox trusts nobody")
	}
	foxAddTrusted(f, pl.p.name)
	if !avoidPlayerExempt(f, pl) || !foxTrusts(f, pl) {
		t.Fatal("a fox that trusts a player does not run from them")
	}
	h.avoidScan(players, f)
	if f.avoidLeft != 0 {
		t.Fatal("no flight from a trusted player")
	}
	z := h.spawnMob(players, entityZombie, 9.5, 180, 0.5)
	pl.lastHurtByMob = z.eid
	if d := h.foxDefendTarget(players, f); d != z {
		t.Fatalf("what hurt a trusted player is the fox's quarry: %v", d != nil)
	}
	// Breeding: the cub trusts the breeder.
	a := h.spawnMob(players, entityFox, 0.5, 180, 3.5)
	b := h.spawnMob(players, entityFox, 1.5, 180, 3.5)
	a.lovedBy, b.lovedBy = pl.p.eid, pl.p.eid
	a.loveTicks, b.loveTicks = 100, 100
	a.baby, b.baby = false, false
	h.gridDirty()
	courtBreeding(h, players) // BreedGoal: sixty ticks together first
	cub := (*mob)(nil)
	for _, m := range h.mobs {
		if m.etype == entityFox && m.baby {
			cub = m
		}
	}
	if cub == nil || !foxTrusts(cub, pl) {
		t.Fatalf("the cub trusts whoever fed its parents: cub %v", cub != nil)
	}
}

// A fox going for whatever hurt a player it trusts is defending: it holds
// its ground against other players and does not panic
// (AvoidEntityGoal's and FoxPanicGoal's !isDefending), and a defending cub
// does not trail after its parent (FoxFollowParentGoal).
func TestFoxDefendingHoldsItsGround(t *testing.T) {
	h := newHub(world.New(1))
	friend := survPlayer(h)
	u, _ := parseUUIDString(offlineUUIDString("stranger"))
	stranger := &tracked{p: newPlayer(2, "stranger", u), gamemode: gmSurvival}
	initSurvival(stranger)
	players := map[int32]*tracked{friend.p.eid: friend, stranger.p.eid: stranger}
	h.playersRef = players
	y := foxArena(h, 14)
	h.dayTime.Store(15000)
	friend.x, friend.y, friend.z = -3.5, y, 0.5
	stranger.x, stranger.y, stranger.z = 5.5, y, 0.5
	fox := h.spawnMob(players, entityFox, 0.5, y, 0.5)
	foxAddTrusted(fox, friend.p.name)
	z := h.spawnMob(players, entityZombie, -8.5, y, 4.5)
	z.frozen = true
	friend.lastHurtByMob = z.eid
	fox.panic = 20
	h.gridDirty()
	h.updateMobs(players)
	if fox.foxFlags&foxFlagDefending == 0 || fox.foxPrey != z.eid {
		t.Fatalf("the fox goes for the zombie, defending: flags=%#x prey=%d", fox.foxFlags, fox.foxPrey)
	}
	if fox.avoidLeft != 0 {
		t.Error("a defending fox does not run from the stranger")
	}
	if fox.panic != 0 || fox.panicHasT {
		t.Error("a defending fox does not panic")
	}
	// The cub.
	cub := h.spawnMob(players, entityFox, 0.5, y, -9.5)
	cub.baby = true
	adult := h.spawnMob(players, entityFox, 6.5, y, -9.5)
	h.gridDirty()
	cub.foxFlags |= foxFlagDefending
	if h.followParentStep(cub) {
		t.Error("a defending cub does not follow its parent")
	}
	cub.foxFlags &^= foxFlagDefending
	if !h.followParentStep(cub) || cub.parent != adult.eid {
		t.Error("otherwise it trails the adult")
	}
	// The zombie gone, the defence is over.
	h.despawnMob(players, z)
	h.gridDirty()
	h.updateMobs(players)
	if fox.foxFlags&foxFlagDefending != 0 {
		t.Error("with its quarry gone the fox stands down")
	}
}
