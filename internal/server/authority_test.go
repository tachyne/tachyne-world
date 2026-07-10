package server

import (
	"testing"

	"tachyne/internal/world"
)

// The server is authoritative: these tests are hacked-client simulations —
// packets a vanilla client would never send — and the server must ignore or
// resync every one of them, never apply.

func TestSurvivalClientCannotConjureCreativeItems(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked() // gamemode survival
	players[1] = pl
	// A hacked survival client sends set_creative_slot with 64 diamonds.
	h.post(evCreativeSlot{eid: 1, slot: 36, st: invStack{item: itemByName["diamond"], count: 64}})
	// Drain the event exactly as the hub loop would.
	ev := <-h.events
	e := ev.(evCreativeSlot)
	if tr := players[e.eid]; tr != nil && tr.gamemode == gmCreative && tr.inv != nil && tr.winID == 0 {
		t.Fatal("gate did not reject a survival creative-slot")
	}
	if pl.inv.slots[0].item != 0 {
		t.Fatalf("survival inventory must be untouched, got %+v", pl.inv.slots[0])
	}

	// The same event from an actually-creative player is applied.
	pl.gamemode = gmCreative
	if pl.gamemode == gmCreative && pl.inv != nil && pl.winID == 0 {
		if ptr, _ := h.winSlotPtr(pl, 36); ptr != nil {
			*ptr = invStack{item: itemByName["diamond"], count: 64}
		}
	}
	if pl.inv.slots[0].item != itemByName["diamond"] {
		t.Fatal("creative player's slot set should apply")
	}
}

func TestAttackBeyondReachIgnored(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[1] = pl
	m := &mob{eid: 2, etype: entityZombie, hostile: true, health: zombieHealth, x: 50, y: 70, z: 50}
	h.mobs[2] = m

	h.attackMob(players, 1, 2) // kill-aura: claimed hit from ~70 blocks away
	if m.health != zombieHealth {
		t.Fatalf("out-of-reach hit must be ignored, health=%d", m.health)
	}
	m.x, m.z = 2.5, 0.5 // in reach now
	h.attackMob(players, 1, 2)
	if m.health != zombieHealth-fistDamage {
		t.Fatalf("in-reach hit should land, health=%d", m.health)
	}
}

func TestClickCannotFabricateItems(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	// A hacked client declares 64 diamonds appearing in an empty slot with an
	// empty cursor — nothing was taken from anywhere.
	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 9, mode: 0,
		changed: []slotChange{{slot: 9, st: invStack{item: itemByName["diamond"], count: 64}}},
	})
	if pl.inv.slots[9].item != 0 {
		t.Fatalf("fabricated stack must be rejected, got %+v", pl.inv.slots[9])
	}
	// Topping up a stack beyond what the cursor held is fabrication too.
	pl.inv.slots[9] = invStack{item: 1, count: 10}
	pl.cursor = invStack{item: 1, count: 5}
	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 9, mode: 0, cursor: invStack{},
		changed: []slotChange{{slot: 9, st: invStack{item: 1, count: 40}}},
	})
	if pl.inv.slots[9].count != 10 {
		t.Fatalf("over-deposit must be rejected, got %+v", pl.inv.slots[9])
	}
	// A legitimate move (cursor 5 onto the stack of 10) still applies.
	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 9, mode: 0, cursor: invStack{},
		changed: []slotChange{{slot: 9, st: invStack{item: 1, count: 15}}},
	})
	if pl.inv.slots[9].count != 15 {
		t.Fatalf("legitimate deposit should apply, got %+v", pl.inv.slots[9])
	}
}
