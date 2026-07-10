package server

import (
	"encoding/binary"
	"fmt"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
)

// Weather: the hub owns a rain/thunder cycle on vanilla-ish durations,
// announced via Game Event packets (1 end rain / 2 begin rain / 7 rain level /
// 8 thunder level — the same table change-game-mode=3 sits in). During a
// thunderstorm, lightning strikes near players: a brief lightning_bolt entity,
// the thunder crack, and damage at the strike point. Rain also keeps the
// undead from burning at dawn (updateHostiles checks h.raining).

const (
	// Values are the client's ClientboundGameEventPacket.Type table (decompiled
	// 1.21.5 AND 26.2 — identical): START_RAINING=1, STOP_RAINING=2. These were
	// INVERTED here for the engine's whole life (end=1/begin=2, wiki-derived):
	// clients rendered clear skies during rain — while the engine, correctly,
	// shielded the undead from daylight burn — and rendered rain once it ended.
	// Found live: "zombies walking around at 09:00" under an invisibly-rainy sky.
	gameEventBeginRain    = 1 // START_RAINING
	gameEventEndRain      = 2 // STOP_RAINING
	gameEventRainLevel    = 7
	gameEventThunderLevel = 8

	clearMinTicks   = 12000 // vanilla: clear spells 12000-180000 ticks
	clearMaxTicks   = 180000
	rainMinTicks    = 12000 // vanilla: rain 12000-24000 ticks
	rainMaxTicks    = 24000
	thunderChance   = 30 // % of rains that are thunderstorms
	lightningPeriod = 8  // seconds: average gap between strikes per player
	lightningRange  = 32 // strikes land within this many blocks of a player
	lightningDamage = 5  // vanilla
	boltLifeTicks   = 10 // the bolt entity flashes briefly, then despawns
)

var (
	entityLightning = entityID("lightning_bolt") // minecraft:entity_type "lightning_bolt" (1.21.5)
)

// startWeather begins (or ends) rain, broadcasting the transition.
func (h *hub) startWeather(players map[int32]*tracked, rain, thunder bool) {
	h.raining, h.thundering = rain, thunder
	if rain {
		h.weatherLeft = rainMinTicks + h.rng.Intn(rainMaxTicks-rainMinTicks)
	} else {
		h.weatherLeft = clearMinTicks + h.rng.Intn(clearMaxTicks-clearMinTicks)
	}
	for _, t := range players {
		h.sendWeather(t)
	}
}

// sendWeather pushes the current weather state to one player (join + change).
func (h *hub) sendWeather(t *tracked) {
	if h.raining {
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventBeginRain})
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventRainLevel, Value: 1})
		lvl := float32(0)
		if h.thundering {
			lvl = 1
		}
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventThunderLevel, Value: lvl})
	} else {
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventEndRain})
	}
}

// updateWeather runs at 1 Hz: advance the cycle and roll thunderstorm strikes.
func (h *hub) updateWeather(players map[int32]*tracked) {
	if h.weatherLeft -= survivalTickN; h.weatherLeft <= 0 {
		if h.raining {
			h.startWeather(players, false, false)
		} else {
			h.startWeather(players, true, h.rng.Intn(100) < thunderChance)
		}
	}
	if !h.thundering || len(players) == 0 {
		return
	}
	for _, t := range players {
		if h.rng.Intn(lightningPeriod) != 0 {
			continue
		}
		x := int(t.x) + h.rng.Intn(2*lightningRange) - lightningRange
		z := int(t.z) + h.rng.Intn(2*lightningRange) - lightningRange
		if !h.skyExposedColumn(x, z) {
			continue // lightning only hits the open sky
		}
		h.strikeLightning(players, float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
	}
}

// strikeLightning spawns the bolt flash, cracks the thunder, and hurts
// everything at the strike point.
func (h *hub) strikeLightning(players map[int32]*tracked, x, y, z float64) {
	eid := h.allocEID()
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(eid))
	h.toNearbyEv(players, 0, x, z, entAdd(eid, entityLightning, uuid, x, y, z, 0, 0))
	h.bolts = append(h.bolts, bolt{eid: eid, x: x, z: z, dieAt: h.tick.Load() + boltLifeTicks})
	// Thunder is heard far beyond the chunk-tracking radius in vanilla; the
	// crack at volume 10 carries like the real thing.
	h.playSound(players, "minecraft:entity.lightning_bolt.thunder", sndBlock, x, y, z, 10, 0.8+h.rng.Float32()*0.4)
	h.playSound(players, "minecraft:entity.lightning_bolt.impact", sndBlock, x, y, z, 2, 1)

	for _, t := range players {
		if math.Abs(t.x-x) <= 3 && math.Abs(t.z-z) <= 3 && math.Abs(t.y-y) <= 6 {
			h.damage(players, t, lightningDamage)
		}
	}
	for _, m := range h.mobs {
		if m.dying == 0 && math.Abs(m.x-x) <= 3 && math.Abs(m.z-z) <= 3 && math.Abs(m.y-y) <= 6 {
			m.hurt(float64(lightningDamage)) // lightning_bolt doesn't bypass armor
			if m.health <= 0 {
				h.killMob(players, m)
			}
		}
	}
	h.bus.publish("lightning", map[string]any{"x": x, "y": y, "z": z})
}

// bolt is a transient lightning entity awaiting despawn.
type bolt struct {
	eid   int32
	x, z  float64
	dieAt uint64
}

// updateBolts despawns finished lightning flashes (runs every tick).
func (h *hub) updateBolts(players map[int32]*tracked) {
	if len(h.bolts) == 0 {
		return
	}
	now := h.tick.Load()
	kept := h.bolts[:0]
	for _, b := range h.bolts {
		if now >= b.dieAt {
			h.toNearbyEv(players, 0, b.x, b.z, entGone(b.eid))
		} else {
			kept = append(kept, b)
		}
	}
	h.bolts = kept
}

// cmdWeather is the op command: /weather <clear|rain|thunder>.
func (s *Server) cmdWeather(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to change the weather.")
		return
	}
	if len(args) != 1 {
		p.tell("Usage: /weather <clear|rain|thunder>")
		return
	}
	var rain, thunder bool
	switch args[0] {
	case "clear":
	case "rain":
		rain = true
	case "thunder":
		rain, thunder = true, true
	default:
		p.tell("Usage: /weather <clear|rain|thunder>")
		return
	}
	s.hub.post(evSetWeather{rain: rain, thunder: thunder})
	p.tell(fmt.Sprintf("Weather set to %s", args[0]))
}

type evSetWeather struct{ rain, thunder bool }

func (evSetWeather) isHubEvent() {}
