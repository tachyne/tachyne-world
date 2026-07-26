package server

// The spyglass. The zoom itself is the client's — it narrows its own field of
// view while the item is held up — so the server's half is the use state and
// the two sounds that bracket it, which is also what tells everyone else that
// the player has a spyglass to their eye.

var itemSpyglass = itemByName["spyglass"]

const spyglassUseTicks = 1200 // vanilla USE_DURATION: it holds until released

// A scope is tracked by the tick it EXPIRES rather than the tick it started:
// "started at tick 0" is indistinguishable from "not scoping" when the sentinel
// is zero, and a fresh world starts at tick 0.

type evSpyglass struct{ eid int32 }

func (evSpyglass) isHubEvent() {}

// raiseSpyglass starts a scope.
func (h *hub) raiseSpyglass(players map[int32]*tracked, t *tracked) {
	if t.dead || t.p.heldItem() != itemSpyglass {
		return
	}
	t.scopeUntil = h.tick.Load() + spyglassUseTicks
	h.playSound(players, "minecraft:item.spyglass.use", sndPlayer, t.x, t.y, t.z, 1, 1)
}

// lowerSpyglass ends one — on release, on a hotbar switch, or when the
// twenty-second hold runs out.
func (h *hub) lowerSpyglass(players map[int32]*tracked, t *tracked) {
	if t.scopeUntil == 0 {
		return
	}
	t.scopeUntil = 0
	h.playSound(players, "minecraft:item.spyglass.stop_using", sndPlayer, t.x, t.y, t.z, 1, 1)
}

// expireSpyglass drops a scope that has run its full duration.
func (h *hub) expireSpyglass(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, t := range players {
		if t.scopeUntil != 0 && now >= t.scopeUntil {
			h.lowerSpyglass(players, t)
		}
	}
}
