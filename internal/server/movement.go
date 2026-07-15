package server

import (
	"fmt"
	"log"
	"math"

	"github.com/tachyne/tachyne-common/shard"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Movement authority: the client streams Set Player Position packets and the
// hub validates every one before adopting it (AUTHORITY: the standing rule is
// that a client can never override the server). Three checks, all tuned to
// never trip on legitimate vanilla play:
//
//   - Budgeted speed: movement spends a per-tick allowance (horizontal distance
//     plus upward gain; falling is free). The budget accrues while idle up to a
//     burst cap, so jumps, knockback and latency bunching never trip it, but a
//     sustained speed hack outruns it within a second.
//   - Teleport cap: a single event moving farther than any packet gap could
//     legitimately cover is discarded outright.
//   - Floating: a survival/adventure player who spends floatLimit ticks neither
//     descending nor near ANY block (the vanilla no-blocks-around test — water,
//     ladders, jumps and wall-hugs all reset it) is a fly hack; they are snapped
//     down to the local floor.
//
// A rejected move is not applied: the client is rubber-banded back to the last
// accepted position with a rotation-relative teleport (the camera stays put).
// Server-initiated teleports (/tp) post evMove with teleport=true and bypass
// validation; respawn/bed set the tracked position directly, so a stale
// in-flight client position after those simply rubber-bands to the right spot.
const (
	walkPerTick = 0.6 // survival/adventure allowance: 12 blocks/s — sustained sprint-jumping
	//                   UPHILL legitimately reaches ~0.51/tick diagonal (7.1 m/s
	//                   forward + step-ups); hacks run 2-10× sprint and still trip
	flyPerTick = 1.5 // creative allowance: 30 blocks/s. Vanilla creative SPRINT-fly
	//                      is ~21.9 m/s (1.09 blocks/tick) — double the base fly speed —
	//                      and vertical ascent (~0.38/tick) co-occurs, so sustained
	//                      diagonal sprint-fly costs ~1.15/tick. 1.0 (base-fly only)
	//                      slowly drained the bank and hitched every few seconds; 1.5
	//                      clears sprint-fly + vertical + latency bunching with margin.
	spinPerTick    = 4.0  // riptide auto-spin-attack: brief high-speed travel (trident.go)
	budgetCapTicks = 30   // burst headroom: up to 1.5 s of allowance banked while idle
	teleportCap    = 12.0 // single-event displacement no legitimate packet gap can cover
	floatLimit     = 80   // ticks floating before the snap-down (vanilla's kick threshold)
	floatDescend   = -0.03
	rubberCooldown = 20 // min ticks between correction teleports (drains packet storms)
)

// validateMove vets a client movement event against the tracked (authoritative)
// position, reporting whether it may be applied. Rejections rubber-band.
func (h *hub) validateMove(t *tracked, e evMove) bool {
	if e.teleport || t.dead || t.gamemode == gmSpectator {
		return true // server-initiated, or a mode where anything goes
	}
	now := h.tick.Load()
	// Non-finite floats poison every later computation (and NaN slips past all
	// numeric comparisons below), so malformed packets are rejected first.
	if !finite(e.x) || !finite(e.y) || !finite(e.z) ||
		!finite(float64(e.yaw)) || !finite(float64(e.pitch)) {
		h.rubberBand2(t, now, e, fmt.Sprintf("nan yaw=%v pitch=%v", e.yaw, e.pitch))
		return false
	}
	// The WORLD-EDGE clamp: past the last owned region there is nothing — no
	// chunks are served, and a client left drifting in that void stalls and
	// times out (found by flying off the map on a real phone). Bounce at the
	// boundary instead. checkSeamCrossing has always relied on this clamp for
	// the void case; now it actually exists.
	if h.shardOf != nil && h.shardAt(e.x, e.z) == shard.Unowned {
		h.rubberBand2(t, now, e, "world edge")
		return false
	}
	dt := now - t.lastMoveTick
	t.lastMoveTick = now

	perTick := walkPerTick
	if !t.onGround && t.armor[1].item == itemElytra {
		perTick = 3.0 // elytra glide: the client's fall-flying physics is fast
	}
	if t.gamemode == gmCreative {
		perTick = flyPerTick
	}
	if now < t.spinUntil && perTick < spinPerTick {
		perTick = spinPerTick // riptide launch: fast travel until the spin-attack ends
	}
	if lvl := t.hasEffect(effSpeed); lvl > 0 {
		perTick *= 1 + 0.2*float64(lvl) // vanilla: +20% speed per level
	}
	t.moveBudget = math.Min(t.moveBudget+float64(dt)*perTick, budgetCapTicks*perTick)

	dx, dy, dz := e.x-t.x, e.y-t.y, e.z-t.z
	if dx*dx+dy*dy+dz*dz > teleportCap*teleportCap {
		h.rubberBand2(t, now, e, "teleport-cap")
		return false
	}
	// Cost is the EUCLIDEAN length of the motion with the downward component
	// zeroed (falling is free). Summing axes instead would over-charge diagonal
	// motion — sprint-jumping uphill (forward AND up at once) drained the
	// budget ~40% faster than the same speed on the flat and rubber-banded
	// legitimate climbing.
	up := math.Max(0, dy)
	cost := math.Sqrt(dx*dx + dz*dz + up*up)
	if cost > t.moveBudget {
		h.rubberBand2(t, now, e, "budget")
		return false
	}
	t.moveBudget -= cost

	// Noclip: the destination may not put the hitbox inside solid blocks —
	// unless the player is ALREADY inside one (sand fell on them; they must
	// always be able to dig/walk their way out).
	if t.gamemode != gmSpectator && h.insideSolid(t.dim, e.x, e.y, e.z) && !h.insideSolid(t.dim, t.x, t.y, t.z) {
		w := h.worldFor(t.dim)
		fx, fz := int(math.Floor(e.x)), int(math.Floor(e.z))
		h.rubberBand2(t, now, e, fmt.Sprintf("noclip dim=%d feet=%d head=%d",
			t.dim, w.At(fx, int(math.Floor(e.y+0.1)), fz), w.At(fx, int(math.Floor(e.y+1.6)), fz)))
		return false
	}
	t.p.setHubPos(e.x, e.z) // the connection streams chunks only near HERE

	if t.gamemode != gmSurvival && t.gamemode != gmAdventure {
		t.floatTicks = 0
		return true
	}
	if dy < floatDescend || h.nearAnyBlock(t.dim, e.x, e.y, e.z) {
		t.floatTicks = 0
		return true
	}
	if dt > 20 {
		dt = 20 // a long event gap (join, lag spike) counts as at most one second
	}
	if t.floatTicks += int(dt); t.floatTicks < floatLimit {
		return true
	}
	// Hovering with nothing to hold them up: put them on the local floor. The
	// normal fall-damage bookkeeping applies from here, like any other fall.
	t.floatTicks = 0
	t.x, t.z = e.x, e.z
	t.y = float64(h.worldFor(t.dim).DropY(int(math.Floor(e.x)), int(math.Floor(e.y)), int(math.Floor(e.z))))
	log.Printf("movement: %q floated %d ticks with no support — grounding at (%.1f, %.1f, %.1f)",
		t.p.name, floatLimit, t.x, t.y, t.z)
	t.lastRubber = now
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	return false
}

// nearAnyBlock reports whether any non-air block sits in the 2×2 columns the
// player's hitbox overlaps, from one below the feet through head height. This
// is what makes the floating check unable to false-positive: standing, jumping
// near ground, ladders, vines, water, scaffolding towers and wall-hugs all
// touch a block. Only genuine mid-air hovering is ever "unsupported".
func (h *hub) nearAnyBlock(dim int, x, y, z float64) bool {
	fy := int(math.Floor(y))
	for xi := int(math.Floor(x - 0.4)); xi <= int(math.Floor(x+0.4)); xi++ {
		for zi := int(math.Floor(z - 0.4)); zi <= int(math.Floor(z+0.4)); zi++ {
			for yi := fy - 1; yi <= fy+1; yi++ {
				if h.worldFor(dim).At(xi, yi, zi) != worldgen.Air {
					return true
				}
			}
		}
	}
	return false
}

// rubberBand snaps a client back to the last accepted position, at most once
// per rubberCooldown (a hacked client can fire hundreds of bad moves a second;
// one correction covers all of them until it lands).
func (h *hub) rubberBand(t *tracked, now uint64) {
	h.rubberBand2(t, now, evMove{x: t.x, y: t.y, z: t.z}, "other")
}

// rubberBand2 corrects the client — but an authority that rejects EVERY move
// forever is a deadlock, not security (a client that lost sync, e.g. across a
// dimension switch, can never recover: it falls, we snap, it falls). After
// ~5s of continuous rejection we yield once: adopt the client's position,
// log loudly, and start fresh.
func (h *hub) rubberBand2(t *tracked, now uint64, e evMove, why string) {
	// Rolling window: a fall-snap CYCLE interleaves accepted falling moves with
	// rejections, so "reset on accept" never accumulates. Count rejections that
	// happen within 5s of each other, whatever comes in between.
	if now-t.lastRejectTick > 100 {
		t.rejectStreak = 0
	}
	t.lastRejectTick = now
	t.rejectStreak++
	if t.rejectStreak >= 40 {
		log.Printf("movement: %q rejected %d in a row (%s) — YIELDING to client at (%.1f, %.1f, %.1f)",
			t.p.name, t.rejectStreak, why, e.x, e.y, e.z)
		t.rejectStreak = 0
		t.x, t.y, t.z = e.x, e.y, e.z
		t.p.setHubPos(e.x, e.z) // the yield must also reopen the chunk-stream gate
		t.moveBudget = budgetCapTicks * walkPerTick
		t.peakY = t.y
		return
	}
	if now-t.lastRubber < rubberCooldown {
		return
	}
	t.lastRubber = now
	log.Printf("movement: rejected %q (%s): client at (%.1f,%.1f,%.1f), server at (%.1f,%.1f,%.1f), budget %.1f",
		t.p.name, why, e.x, e.y, e.z, t.x, t.y, t.z, t.moveBudget)
	log.Printf("movement: rejected impossible move from %q — snapping back to (%.1f, %.1f, %.1f)",
		t.p.name, t.x, t.y, t.z)
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
