package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
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

// EndDragonFight.setDragonKilled / EnderDragon.tickDeath: the first kill
// leaves the egg and 12000 XP, a later one 500 XP and no new egg; no kill
// drops an elytra (that is the End ships' loot).
func TestDragonDeathOpensExitEggAndXP(t *testing.T) {
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
	// Exit portal blocks exist near the origin, the egg on top.
	portalY, egg := 0, false
	for y := worldgen.EndSurfaceY - 2; y < worldgen.EndSurfaceY+10; y++ {
		if h.end.At(1, y, 0) == worldgen.EndPortalBlock && portalY == 0 {
			portalY = y
		}
		if h.end.At(0, y, 0) == worldgen.DragonEgg {
			egg = true
		}
	}
	if portalY == 0 || !egg {
		t.Fatalf("exit portal at %d, egg %v", portalY, egg)
	}
	orbXP := func() int {
		n := 0
		for id, o := range h.orbs {
			n += o.value * max(o.count, 1)
			delete(h.orbs, id)
		}
		return n
	}
	if got := orbXP(); got != 12000 {
		t.Fatalf("the first kill gave %d XP, want 12000", got)
	}
	for _, it := range h.items {
		if it.item == itemElytra {
			t.Fatal("the dragon dropped an elytra")
		}
	}
	// A second fight: 500 XP, and the egg is not set again.
	for y := worldgen.EndSurfaceY - 2; y < worldgen.EndSurfaceY+10; y++ {
		if h.end.At(0, y, 0) == worldgen.DragonEgg {
			h.end.SetBlock(0, y, 0, worldgen.Air) // someone took it
		}
	}
	h.dragonDefeated(players)
	if got := orbXP(); got != 500 {
		t.Fatalf("a second kill gave %d XP, want 500", got)
	}
	for y := worldgen.EndSurfaceY - 2; y < worldgen.EndSurfaceY+10; y++ {
		if h.end.At(0, y, 0) == worldgen.DragonEgg {
			t.Fatal("a second kill set another egg")
		}
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

// EnderDragon.tickDeath: the death is a global level event (1028) — a
// player across the End hears it.
func TestDragonDeathSoundIsGlobal(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	if h.dragon == nil {
		t.Skip("no dragon in this End")
	}
	pl.x, pl.z = 900, 900 // far across the End
	drainOut(pl.p)
	h.dragonDefeated(players)
	for len(pl.p.out) > 0 {
		if ev, ok := (<-pl.p.out).ev.(attachproto.Sound); ok && ev.Name == "minecraft:entity.ender_dragon.death" {
			return
		}
	}
	t.Fatal("a player across the End did not hear the dragon die")
}

// A client strikes the dragon's parts, not the dragon (it is not pickable):
// part ids follow the dragon's, head first. A blow on the head lands whole,
// on a wing a quarter plus one; the ids are the dragon's alone.
func TestDragonMeleeLandsOnThePartStruck(t *testing.T) {
	h, pl, players := endHub(t)
	h.enterEnd(players, nil)
	d := h.dragon
	if d == nil {
		t.Fatal("no dragon")
	}
	if next := h.allocEID(); next <= d.eid+int32(len(dragonPartNames)) {
		t.Fatalf("the part ids must be reserved: next eid %d, dragon %d", next, d.eid)
	}
	pl.dim = 2
	d.yaw = 0
	sword := itemByName["diamond_sword"]
	pl.p.setHotbarSlot(0, sword)
	pl.inv.slots[0] = invStack{item: sword, count: 1}
	head := dragonPartOf(d, 0)
	pl.x, pl.y, pl.z = head.x, head.y, head.z+1
	d.health = 200
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: d.eid + 1})
	full := 200 - d.health
	if full <= 0 {
		t.Fatal("a blow on the head part should land")
	}
	d.health, d.invulnTicks, pl.lastAttack = 200, 0, 0
	wing := dragonPartOf(d, 6)
	pl.x, pl.y, pl.z = wing.x, wing.y, wing.z+1
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: d.eid + 7})
	if got := 200 - d.health; got >= full {
		t.Fatalf("a wing takes a quarter plus one: head %d, wing %d", full, got)
	}
}

// DragonFlightHistory: the tail follows where the dragon was heading; after
// a turn it still trails along the old line.
func TestDragonTailLagsThroughTheTurn(t *testing.T) {
	m := &mob{etype: entityEnderDragon, x: 0, y: 80, z: 0, yaw: 0}
	for i := 0; i < dragonHistLen; i++ {
		m.recordDragonFlight()
	}
	straight := dragonPartOf(m, 5) // the last tail segment
	m.yaw = 90
	m.recordDragonFlight()
	bent := dragonPartOf(m, 5)
	body := dragonPartOf(m, 2)
	// Turned to yaw 90 the body lies along −x; the lagging tail has not yet
	// swung all the way round.
	if bent.x == straight.x && bent.z == straight.z {
		t.Fatal("the tail should move when the dragon turns")
	}
	if dz := bent.z - body.z; dz >= 0 {
		t.Fatalf("the tail should still trail along the old heading (−z), got dz %.2f", dz)
	}
}
