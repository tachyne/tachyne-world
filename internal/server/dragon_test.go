package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func endHub(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(7))
	ew, _ := world.NewEnd(7, nil)
	h.end = ew
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.dim = 1 // will switch to 2
	players := map[int32]*tracked{1: pl}
	return h, pl, players
}

func TestDragonStagesOnFirstEndArrival(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	if h.dragon == nil {
		t.Fatal("the dragon should await the first End arrival")
	}
	if h.dragon.dim != 2 || h.dragon.health != dragonHealth {
		t.Fatalf("dragon misconfigured: dim=%d hp=%d", h.dragon.dim, h.dragon.health)
	}
	if len(h.crystals) != worldgen.EndPillars {
		t.Fatalf("want %d crystals, got %d", worldgen.EndPillars, len(h.crystals))
	}
	// Second arrival must not double-stage.
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	if len(h.crystals) != worldgen.EndPillars {
		t.Fatal("re-arrival duplicated the crystals")
	}
}

func TestCrystalsHealAndDie(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	h.dragon.health = 100
	h.tick.Store(20) // it finds its nearest crystal…
	h.updateDragon(players)
	h.tick.Store(40) // …and heals from it on the next beat
	h.updateDragon(players)
	if h.dragon.health <= 100 {
		t.Fatal("living crystals should heal the dragon")
	}
	// Pop every crystal; healing stops.
	for eid := range h.crystals {
		if !h.hitCrystal(players, eid, pl) {
			t.Fatal("crystal hit should register")
		}
	}
	if len(h.crystals) != 0 {
		t.Fatal("all crystals should be gone")
	}
	hp := h.dragon.health
	h.tick.Store(40)
	h.updateDragon(players)
	if h.dragon.health != hp {
		t.Fatal("no crystals — no healing")
	}
}

func TestDragonDeathOpensExitAndDropsElytra(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	m := h.dragon
	h.hurtByEID(m, 0) // a player hurt it just now
	h.killMob(players, m)
	m.dying = 1
	h.despawnMob(players, m)
	if h.dragon != nil || !h.rules.DragonDefeated {
		t.Fatal("defeat not recorded")
	}
	// Exit portal blocks exist near the origin.
	found := false
	for y := worldgen.EndSurfaceY - 2; y < worldgen.EndSurfaceY+10 && !found; y++ {
		if h.end.At(1, y, 0) == worldgen.EndPortalBlock {
			found = true
		}
	}
	if !found {
		t.Fatal("exit portal missing")
	}
	elytra := false
	for _, it := range h.items {
		if it.item == itemElytra && it.dim == 2 {
			elytra = true
		}
	}
	if !elytra {
		t.Fatal("the elytra should drop at the exit portal")
	}
	// A rejoin must not respawn the dragon.
	h.enterEnd(players, nil)
	if h.dragon != nil {
		t.Fatal("a defeated dragon stays dead")
	}
}

func TestDragonBossbarLifecycle(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	h.updateDragonBar(players)
	if !pl.bossBarOn {
		t.Fatal("End players should see the dragon bar")
	}
	// Leaving the End removes it.
	pl.dim = 0
	h.updateDragonBar(players)
	if pl.bossBarOn {
		t.Fatal("the bar must go when the player leaves the End")
	}
	// Dragon dead: no bar even in the End.
	pl.dim = 2
	h.dragon = nil
	h.updateDragonBar(players)
	if pl.bossBarOn {
		t.Fatal("no dragon — no bar")
	}
}

// The dragon's body deals ten and its wings five with a shove, as vanilla's
// hurt() and knockBack() do — and a perched one deals nothing.
func TestDragonContactDamage(t *testing.T) {
	if dragonContact != 10 || dragonWingDamage != 5 {
		t.Fatalf("vanilla deals 10 from the body and 5 from a wing, got %v and %v",
			dragonContact, dragonWingDamage)
	}
	if dragonWingReach <= dragonBodyReach {
		t.Error("the wings reach further than the body")
	}
}

// The dragon is eight boxes, and every one but the head takes a quarter of
// what lands on it: EnderDragon.hurt's one line is the shape of the fight.
func TestDragonPartsQuarterEverythingButTheHead(t *testing.T) {
	if got := dragonPartDamage("head", 8); got != 8 {
		t.Fatalf("the head takes a blow whole: %v", got)
	}
	// 8 → 8/4 + min(8,1) = 3
	if got := dragonPartDamage("tail", 8); got != 3 {
		t.Fatalf("the tail should take 3 of 8, got %v", got)
	}
	if got := dragonPartDamage("body", 0.5); got != 0.625 {
		t.Fatalf("a small blow keeps its own size as the floor: %v", got)
	}
}

// The parts sit where the dragon is looking: the head well ahead of it, the
// tail behind, the wings out to the sides.
func TestDragonPartsLieAlongItsFacing(t *testing.T) {
	m := &mob{etype: entityEnderDragon, x: 100, y: 80, z: 100, yaw: 0} // yaw 0 is south: +z
	head, ok := dragonPartAt(m, 100, 80, 106.5)
	if !ok || head != "head" {
		t.Fatalf("six and a half blocks ahead should be the head, got %q ok=%v", head, ok)
	}
	tail, ok := dragonPartAt(m, 100, 80, 94.5)
	if !ok || tail != "tail" {
		t.Fatalf("behind it should be tail, got %q ok=%v", tail, ok)
	}
	if _, ok := dragonPartAt(m, 100, 80, 130); ok {
		t.Fatal("thirty blocks away is not the dragon at all")
	}
	// A wing, out to the side and two up.
	if p, ok := dragonPartAt(m, 104.5, 82, 100); !ok || p != "wing" {
		t.Fatalf("out to the side should be a wing, got %q ok=%v", p, ok)
	}
}

// EnderDragon.onCrystalDestroyed: destroying the crystal the dragon is
// healing from costs it 10; any other crystal costs it nothing.
func TestDragonHurtWhenItsHealingCrystalBreaks(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	pl.x, pl.y, pl.z = 300, 60, 300 // far from every blast
	m := h.dragon
	var near, far *crystal
	for _, c := range h.crystals {
		if near == nil {
			near = c
		} else if far == nil || dist3sq(c.x, c.y, c.z, near.x, near.y, near.z) > dist3sq(far.x, far.y, far.z, near.x, near.y, near.z) {
			far = c
		}
	}
	m.x, m.y, m.z = near.x, near.y+20, near.z // in reach of it, out of its blast
	h.tick.Store(20)
	h.updateDragon(players)
	if h.dragonCrystal != near.eid {
		t.Fatalf("the dragon beside a crystal heals from %d, want %d", h.dragonCrystal, near.eid)
	}
	m.health = 100
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: far.eid})
	if m.health != 100 {
		t.Fatalf("breaking a crystal the dragon is not healing from cost it %d", 100-m.health)
	}
	h.tick.Add(40) // past the hurt cooldown
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: near.eid})
	if m.health != 100-dragonCrystalLoss {
		t.Fatalf("breaking its healing crystal left the dragon at %d, want %d", m.health, 100-dragonCrystalLoss)
	}
	if h.dragonCrystal != 0 {
		t.Fatal("the dragon still heals from a destroyed crystal")
	}
}
