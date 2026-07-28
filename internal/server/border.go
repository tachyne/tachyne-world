package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The world border.
//
// Nothing implemented one at all: no wall, no damage, no warning, no command.
// The border is a square centred on (CenterX, CenterZ) whose SIZE is the full
// edge length, not the radius — vanilla's `size` is a diameter, and getting
// that backwards halves or doubles everyone's world.
//
// A border can be in motion. Rather than track an animation server-side, the
// engine stores where the border started, where it is going and when it set
// off, and computes the current size from the clock. The client is told the
// same three things once and animates it itself, so the two stay in step
// without a stream of updates.

const (
	borderDefaultSize   = 59999968.0 // vanilla's default: effectively no border
	borderDefaultDamage = 0.2        // damage per block per tick beyond the safe zone
	borderDefaultSafe   = 5.0        // blocks outside the border you may stand unharmed
	borderDefaultWarnB  = 5          // blocks from the edge the red vignette appears
	borderDefaultWarnT  = 15         // seconds of warning for an incoming border
	borderMaxSize       = 59999968.0
	borderMaxCoordinate = 29999984.0
)

// worldBorder is the persisted border state.
type worldBorder struct {
	CenterX float64 `json:"centerX"`
	CenterZ float64 `json:"centerZ"`
	Size    float64 `json:"size"`

	// A border in motion: it was OldSize at StartTick and reaches Size at
	// StartTick+LerpTicks. LerpTicks 0 means it is stationary.
	OldSize   float64 `json:"oldSize,omitempty"`
	StartTick uint64  `json:"startTick,omitempty"`
	LerpTicks uint64  `json:"lerpTicks,omitempty"`

	Damage     float64 `json:"damagePerBlock"`
	SafeZone   float64 `json:"safeZone"`
	WarnBlocks int32   `json:"warningBlocks"`
	WarnTime   int32   `json:"warningTime"`
}

func defaultBorder() worldBorder {
	return worldBorder{
		Size: borderDefaultSize, Damage: borderDefaultDamage, SafeZone: borderDefaultSafe,
		WarnBlocks: borderDefaultWarnB, WarnTime: borderDefaultWarnT,
	}
}

// sizeAt is the border's diameter at a given tick, interpolating a moving one.
func (b worldBorder) sizeAt(now uint64) float64 {
	if b.LerpTicks == 0 || now >= b.StartTick+b.LerpTicks {
		return b.Size
	}
	if now <= b.StartTick {
		return b.OldSize
	}
	frac := float64(now-b.StartTick) / float64(b.LerpTicks)
	return b.OldSize + (b.Size-b.OldSize)*frac
}

// distanceToBorder is the distance from (x,z) to the nearest edge: positive
// inside, NEGATIVE outside. Vanilla's sign convention, and the damage rule
// below depends on it.
func (b worldBorder) distanceToBorder(x, z, size float64) float64 {
	half := size / 2
	minX, maxX := b.CenterX-half, b.CenterX+half
	minZ, maxZ := b.CenterZ-half, b.CenterZ+half
	return math.Min(math.Min(x-minX, maxX-x), math.Min(z-minZ, maxZ-z))
}

// borderFrame renders the current state as the attach frame.
func (h *hub) borderFrame() attachproto.WorldBorder {
	b := h.border
	now := h.tick.Load()
	f := attachproto.WorldBorder{
		CenterX: b.CenterX, CenterZ: b.CenterZ,
		Size:       b.sizeAt(now),
		WarnBlocks: b.WarnBlocks, WarnTime: b.WarnTime,
	}
	if b.LerpTicks > 0 && now < b.StartTick+b.LerpTicks {
		f.Target = b.Size
		// Ticks left, in milliseconds — the client animates the remainder, so
		// a player joining mid-move sees the tail of it rather than a jump.
		f.LerpMs = int64(b.StartTick+b.LerpTicks-now) * 50
	}
	return f
}

// sendBorder tells one player the border state.
func (h *hub) sendBorder(t *tracked) { t.p.trySendEv(h.borderFrame()) }

// broadcastBorder tells everyone (any change, and the tail of a move).
func (h *hub) broadcastBorder(players map[int32]*tracked) {
	f := h.borderFrame()
	for _, t := range players {
		t.p.trySendEv(f)
	}
}

// borderDamage hurts players standing outside. Vanilla applies this to PLAYERS
// only — mobs wander out freely — every tick, scaled by how far out they are,
// and always at least half a heart.
func (h *hub) borderDamage(players map[int32]*tracked) {
	b := h.border
	if b.Damage <= 0 {
		return
	}
	size := b.sizeAt(h.tick.Load())
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead {
			continue
		}
		d := b.distanceToBorder(t.x, t.z, size) + b.SafeZone
		if d >= 0 {
			continue
		}
		dmg := math.Max(1, math.Floor(-d*b.Damage))
		h.hurtBy(players, t, float32(dmg), dtOutsideBorder, deathCause{key: causeBorder})
	}
}

// evBorderCmd carries /worldborder onto the hub goroutine, which owns the
// border state like every other piece of world state.
type evBorderCmd struct {
	p    *player
	args []string
}

func (evBorderCmd) isHubEvent() {}

// cmdWorldBorder implements /worldborder.
func (h *hub) cmdWorldBorder(players map[int32]*tracked, t *tracked, args []string) string {
	b := &h.border
	now := h.tick.Load()
	if len(args) == 0 {
		return fmt.Sprintf("World border is currently %.0f blocks wide, centred on %.1f, %.1f",
			b.sizeAt(now), b.CenterX, b.CenterZ)
	}
	num := func(i int) (float64, bool) {
		if i >= len(args) {
			return 0, false
		}
		v, err := strconv.ParseFloat(args[i], 64)
		return v, err == nil
	}
	switch strings.ToLower(args[0]) {
	case "get":
		return fmt.Sprintf("World border is currently %.0f blocks wide", b.sizeAt(now))

	case "set", "add":
		v, ok := num(1)
		if !ok {
			return "Usage: /worldborder set|add <blocks> [seconds]"
		}
		target := v
		if strings.EqualFold(args[0], "add") {
			target = b.sizeAt(now) + v
		}
		if target < 1 || target > borderMaxSize {
			return fmt.Sprintf("Border size must be between 1 and %.0f", borderMaxSize)
		}
		secs, hasSecs := num(2)
		b.OldSize = b.sizeAt(now)
		b.Size = target
		if hasSecs && secs > 0 {
			b.StartTick, b.LerpTicks = now, uint64(secs*20)
		} else {
			b.StartTick, b.LerpTicks = 0, 0
		}
		h.saveBorder()
		h.broadcastBorder(players)
		if hasSecs && secs > 0 {
			return fmt.Sprintf("Set world border to %.0f blocks wide over %.0f seconds", target, secs)
		}
		return fmt.Sprintf("Set world border to %.0f blocks wide", target)

	case "center", "centre":
		x, okX := num(1)
		z, okZ := num(2)
		if !okX || !okZ {
			return "Usage: /worldborder center <x> <z>"
		}
		if math.Abs(x) > borderMaxCoordinate || math.Abs(z) > borderMaxCoordinate {
			return "Border centre is out of range"
		}
		b.CenterX, b.CenterZ = x, z
		h.saveBorder()
		h.broadcastBorder(players)
		return fmt.Sprintf("Set world border centre to %.1f, %.1f", x, z)

	case "damage":
		if len(args) < 2 {
			return "Usage: /worldborder damage amount|buffer <value>"
		}
		v, ok := num(2)
		if !ok || v < 0 {
			return "Usage: /worldborder damage amount|buffer <value>"
		}
		switch strings.ToLower(args[1]) {
		case "amount":
			b.Damage = v
			h.saveBorder()
			return fmt.Sprintf("Set world border damage to %.2f per block", v)
		case "buffer":
			b.SafeZone = v
			h.saveBorder()
			return fmt.Sprintf("Set world border damage buffer to %.0f blocks", v)
		}
		return "Usage: /worldborder damage amount|buffer <value>"

	case "warning":
		if len(args) < 3 {
			return "Usage: /worldborder warning distance|time <value>"
		}
		v, ok := num(2)
		if !ok || v < 0 {
			return "Usage: /worldborder warning distance|time <value>"
		}
		switch strings.ToLower(args[1]) {
		case "distance":
			b.WarnBlocks = int32(v)
		case "time":
			b.WarnTime = int32(v)
		default:
			return "Usage: /worldborder warning distance|time <value>"
		}
		h.saveBorder()
		h.broadcastBorder(players)
		return fmt.Sprintf("Set world border warning %s to %.0f", strings.ToLower(args[1]), v)
	}
	return "Usage: /worldborder [get|set|add|center|damage|warning]"
}
