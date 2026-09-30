package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A PORTAL ticket (radius 3, 300 ticks) holds the chunks within 3 of it
// loaded, block-ticks those within 2 and entity-ticks those within 1, and
// lapses on the 301st tick; re-adding it resets the time.
func TestPortalTicketRangesAndTimeout(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	h.tick.Store(1000)
	h.addChunkTicket(ticketPortal, dimOverworld, 100, 100) // chunk (6,6)
	if !w.Ticketed(9, 6) || !w.Ticketed(3, 3) || w.Ticketed(10, 6) {
		t.Error("a portal ticket should hold exactly the chunks within 3 of it")
	}
	count := func(slack int) int {
		n := 0
		h.forEachTicketChunk(slack, func(dim, cx, cz int) { n++ })
		return n
	}
	if b, e := count(1), count(2); b != 25 || e != 9 {
		t.Errorf("portal ticket: %d block-ticking, %d entity-ticking chunks; want 25 and 9", b, e)
	}
	h.tick.Store(1300)
	h.expireTickets()
	if len(h.tickets) != 1 {
		t.Fatal("the portal ticket lapsed before its 300 ticks were up")
	}
	h.tick.Store(1301)
	h.expireTickets()
	if len(h.tickets) != 0 || w.Ticketed(6, 6) {
		t.Fatal("the portal ticket outlived its time, or its chunks stayed held")
	}

	h.tick.Store(2000)
	h.addChunkTicket(ticketEnderPearl, dimOverworld, 100, 100)
	if b, e := count(1), count(2); b != 9 || e != 1 {
		t.Errorf("pearl ticket: %d block-ticking, %d entity-ticking chunks; want 9 and 1", b, e)
	}
	h.tick.Store(2030)
	h.addChunkTicket(ticketEnderPearl, dimOverworld, 100, 100) // renewed
	h.tick.Store(2041)
	h.expireTickets()
	if len(h.tickets) != 1 {
		t.Error("a renewed pearl ticket lapsed on its first time")
	}
}

// A player's pearl in flight holds an ENDER_PEARL ticket where it is, and
// a fresh one each time it crosses into another chunk; a pearl nobody
// here threw holds none.
func TestPearlInFlightHoldsItsChunk(t *testing.T) {
	h := newTestHub(world.New(1))
	if h.arrows == nil {
		h.arrows = map[int32]*arrowEntity{}
	}
	players := map[int32]*tracked{7: {}}
	a := &arrowEntity{eid: 90, etype: entityPearlProj, pearl: true, shooter: 7, x: 5, y: 100, z: 5}
	h.arrows[a.eid] = a
	h.pearlTickets(players)
	if _, ok := h.tickets[chunkTicket{kind: ticketEnderPearl, dim: dimOverworld, cx: 0, cz: 0}]; !ok {
		t.Fatal("a pearl in flight holds no ticket")
	}
	a.x = 21 // into chunk 1
	h.pearlTickets(players)
	if _, ok := h.tickets[chunkTicket{kind: ticketEnderPearl, dim: dimOverworld, cx: 1, cz: 0}]; !ok {
		t.Error("the pearl crossed a chunk border without a fresh ticket")
	}
	stray := &arrowEntity{eid: 91, etype: entityPearlProj, pearl: true, shooter: 8, x: 500, y: 100, z: 500}
	h.arrows[stray.eid] = stray
	h.pearlTickets(players)
	if _, ok := h.tickets[chunkTicket{kind: ticketEnderPearl, dim: dimOverworld, cx: 31, cz: 31}]; ok {
		t.Error("a pearl with no player thrower held a ticket")
	}
}

// With nobody online and nothing forced, a portal ticket alone keeps the
// mob pass running: a zombie in the ticket's entity-ticking chunks moves.
func TestPortalTicketTicksMobsWithNobodyOnline(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	w.ForceLoad(8, 8, 3)
	startHub(t, h)
	var z *mob
	top := float64(w.SurfaceY(8, 8)) + 20
	onHub(t, h, func() {
		h.addChunkTicket(ticketPortal, dimOverworld, 8, 8)
		z = h.spawnMob(h.playersRef, entityZombie, 8.5, top, 8.5)
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		var y float64
		var gone bool
		onHub(t, h, func() { y, gone = z.y, h.mobs[z.eid] == nil })
		if gone {
			t.Fatal("the zombie under the portal ticket was removed with nobody online")
		}
		if y < top-1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the zombie under the portal ticket never moved (y %.1f)", y)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
