package server

// Timed chunk tickets (TicketType.PORTAL and ENDER_PEARL). A ticket at a
// chunk with radius r sits at level 33-r (TicketStorage.addTicketWithRadius)
// and spreads one level a chunk, so the chunks within r of it are loaded,
// those within r-1 block tick and those within r-2 entity tick. Both types
// simulate (FLAG_SIMULATION) and time out:
//
//   - PORTAL, radius 3, 300 ticks: placed where an entity comes out of a
//     nether or end portal (TeleportTransition.PLACE_PORTAL_TICKET), so a
//     mob or an item sent through keeps moving on the far side with nobody
//     there, for fifteen seconds.
//   - ENDER_PEARL, radius 2, 40 ticks: placed at a player's pearl in
//     flight, again whenever it crosses into another chunk or the last one
//     is about to lapse (ThrownEnderpearl.tick).
//
// Re-adding a ticket that is already held resets its time (Ticket.
// resetTicksLeft). The loaded chunks are pinned against the chunk cache's
// eviction (world.SetTicketed) and warmed off the hub goroutine, as forced
// chunks are; the ticking ones join random ticks and the entity-chunk set,
// and a held ticket keeps the mob pass running with nobody online.

type ticketKind uint8

const (
	ticketPortal ticketKind = iota
	ticketEnderPearl
	numTicketKinds
)

// ticketTimeout and ticketRadius are TicketType's timeout and the radius
// each caller passes to addTicketWithRadius.
var (
	ticketTimeout = [numTicketKinds]uint64{ticketPortal: 300, ticketEnderPearl: 40}
	ticketRadius  = [numTicketKinds]int{ticketPortal: 3, ticketEnderPearl: 2}
)

// chunkTicket is one held ticket: its type, dimension and chunk.
type chunkTicket struct {
	kind   ticketKind
	dim    int
	cx, cz int
}

// addChunkTicket holds a ticket at the chunk holding (x, z) in dim. It
// returns the ticket's timeout (what placeEnderPearlTicket hands back).
func (h *hub) addChunkTicket(kind ticketKind, dim int, x, z float64) uint64 {
	if h.worldFor(dim) == nil {
		return 0
	}
	if h.tickets == nil {
		h.tickets = map[chunkTicket]uint64{}
	}
	k := chunkTicket{kind: kind, dim: dim, cx: chunkFloor(x), cz: chunkFloor(z)}
	_, had := h.tickets[k]
	// Ticket.isTimedOut: ticksLeft < 0, after one decrement a tick — so the
	// ticket is gone on the (timeout+1)th tick after this one.
	h.tickets[k] = h.tick.Load() + ticketTimeout[kind] + 1
	if !had {
		h.syncTicketPins()
	}
	return ticketTimeout[kind]
}

// expireTickets is TicketStorage.purgeStaleTickets: tickets past their
// time go, and the chunks they alone held are released.
func (h *hub) expireTickets() {
	if len(h.tickets) == 0 {
		return
	}
	now := h.tick.Load()
	changed := false
	for k, until := range h.tickets {
		if now >= until {
			delete(h.tickets, k)
			changed = true
		}
	}
	if changed {
		h.syncTicketPins()
	}
}

// syncTicketPins hands each dimension's world the chunks its tickets hold
// loaded, and warms the ones newly held.
func (h *hub) syncTicketPins() {
	sets := map[int]map[[2]int32]bool{}
	for k := range h.tickets {
		r := ticketRadius[k.kind]
		set := sets[k.dim]
		if set == nil {
			set = map[[2]int32]bool{}
			sets[k.dim] = set
		}
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				set[[2]int32{int32(k.cx + dx), int32(k.cz + dz)}] = true
			}
		}
	}
	for dim, w := range h.allDims() {
		warmForced(w, w.SetTicketed(sets[dim]))
	}
}

// forEachTicketChunk calls fn for every chunk a ticket makes block ticking
// (slack 1: within r-1) or entity ticking (slack 2: within r-2). A chunk
// two tickets cover is visited twice; callers dedupe.
func (h *hub) forEachTicketChunk(slack int, fn func(dim, cx, cz int)) {
	for k := range h.tickets {
		r := ticketRadius[k.kind] - slack
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				fn(k.dim, k.cx+dx, k.cz+dz)
			}
		}
	}
}

// pearlTickets is ThrownEnderpearl.tick's ticket upkeep for every player's
// pearl in flight: a fresh ticket when the last is about to lapse or the
// pearl has crossed into another chunk. Only a pearl whose thrower is a
// player here holds one.
func (h *hub) pearlTickets(players map[int32]*tracked) {
	for _, a := range h.arrows {
		if !a.pearl || a.stuck || players[a.shooter] == nil {
			continue
		}
		c := [3]int{a.dim, chunkFloor(a.x), chunkFloor(a.z)}
		a.pearlTicketTimer--
		if a.pearlTicketTimer <= 0 || !a.pearlTicketed || c != a.pearlChunk {
			// registerAndUpdateEnderPearlTicket: the timeout, less one.
			a.pearlTicketTimer = int64(h.addChunkTicket(ticketEnderPearl, a.dim, a.x, a.z)) - 1
			a.pearlChunk, a.pearlTicketed = c, true
		}
	}
}
