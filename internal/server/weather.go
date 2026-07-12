package server

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
)

// Weather: a port of the vanilla server's weather cycle (ServerLevel
// advanceWeatherCycle). Rain and thunder run as two INDEPENDENT timers, each
// alternating a delay (clear spell) and a duration; a visible thunderstorm is
// the overlap where both flags are on at once — which is why storms are rare
// (the old single-timer + 30%-thunder model stormed several times too often).
// Client rendering is driven by rain/thunder LEVELS that ramp 0→1 at
// ±0.01/tick (a five-second fade) and are announced via Game Event packets;
// the booleans the rest of the engine reads (h.raining / h.thundering) are
// the vanilla level thresholds (rain > 0.2, thunder > 0.9). During a storm
// lightning strikes the loaded area around players at vanilla odds,
// preferring lightning rods within 128 blocks, then sky-exposed creatures.

const (
	// Values are the client's ClientboundGameEventPacket.Type table (vanilla
	// 1.21.5 AND 26.2 — identical): START_RAINING=1, STOP_RAINING=2. These were
	// INVERTED here for the engine's whole life (end=1/begin=2, wiki-derived):
	// clients rendered clear skies during rain — while the engine, correctly,
	// shielded the undead from daylight burn — and rendered rain once it ended.
	// Found live: "zombies walking around at 09:00" under an invisibly-rainy sky.
	gameEventBeginRain    = 1 // START_RAINING
	gameEventEndRain      = 2 // STOP_RAINING
	gameEventRainLevel    = 7
	gameEventThunderLevel = 8

	// The vanilla timer distributions (ServerLevel RAIN_DELAY/RAIN_DURATION/
	// THUNDER_DELAY/THUNDER_DURATION), all uniform inclusive, in ticks.
	rainDelayMin       = 12000
	rainDelayMax       = 180000
	rainDurationMin    = 12000
	rainDurationMax    = 24000
	thunderDelayMin    = 12000
	thunderDelayMax    = 180000
	thunderDurationMin = 3600
	thunderDurationMax = 15600

	// Lightning: vanilla tickThunder rolls nextInt(100000)==0 per loaded chunk
	// per tick during a storm. We roll once per player per tick, scaled by the
	// number of chunks their tracked window covers.
	lightningChunkOdds = 100000
	rodAttractRange    = 128 // vanilla POI search radius for lightning rods
	lightningDamage    = 5   // vanilla
	boltLifeTicks      = 10  // the bolt entity flashes briefly, then despawns
)

var (
	entityLightning = entityID("lightning_bolt")
)

// weatherSave is the persisted cycle state — the same five fields as vanilla's
// WeatherData saved data, stored inside settings.json.
type weatherSave struct {
	ClearTime   int  `json:"clear_weather_time"`
	RainTime    int  `json:"rain_time"`
	ThunderTime int  `json:"thunder_time"`
	Raining     bool `json:"raining"`
	Thundering  bool `json:"thundering"`
}

// uniformTicks samples vanilla's UniformInt.of(lo, hi) (inclusive).
func (h *hub) uniformTicks(lo, hi int) int { return lo + h.rng.Intn(hi-lo+1) }

// updateWeather runs EVERY tick: advance the two vanilla timers, ramp the
// client levels toward the flags, broadcast transitions, and roll lightning.
func (h *hub) updateWeather(players map[int32]*tracked) {
	wasRaining := h.raining
	prevRainFlag, prevThunderFlag := h.rainFlag, h.thunderFlag

	if h.rules.DoWeather {
		// vanilla advanceWeatherCycle, verbatim: an active /weather clear
		// window suppresses both spells; otherwise each timer counts down and
		// flips its flag at zero, re-rolling as duration (flag on) or delay.
		if h.clearTime > 0 {
			h.clearTime--
			h.thunderTime = boolTicks(h.thunderFlag)
			h.rainTime = boolTicks(h.rainFlag)
			h.setThunderFlag(false)
			h.setRainFlag(false)
		} else {
			if h.thunderTime > 0 {
				// A plugin-cancelled flip leaves the timer at 0, so the
				// else-branches roll a fresh spell/delay next tick.
				if h.thunderTime--; h.thunderTime == 0 {
					h.setThunderFlag(!h.thunderFlag)
				}
			} else if h.thunderFlag {
				h.thunderTime = h.uniformTicks(thunderDurationMin, thunderDurationMax)
			} else {
				h.thunderTime = h.uniformTicks(thunderDelayMin, thunderDelayMax)
			}
			if h.rainTime > 0 {
				if h.rainTime--; h.rainTime == 0 {
					h.setRainFlag(!h.rainFlag)
				}
			} else if h.rainFlag {
				h.rainTime = h.uniformTicks(rainDurationMin, rainDurationMax)
			} else {
				h.rainTime = h.uniformTicks(rainDelayMin, rainDelayMax)
			}
		}
	}

	// Ramp the client levels toward the flags (vanilla ±0.01/tick) and derive
	// the gameplay booleans at the vanilla thresholds.
	oldRain, oldThunder := h.rainLevel, h.thunderLevel
	h.thunderLevel = rampLevel(h.thunderLevel, h.thunderFlag)
	h.rainLevel = rampLevel(h.rainLevel, h.rainFlag)
	h.raining = h.rainLevel > 0.2       // vanilla Level.isRaining
	h.thundering = h.thunderLevel > 0.9 // vanilla Level.isThundering

	if h.rainLevel != oldRain {
		h.broadcastEv(players, attachproto.GameEvent{Event: gameEventRainLevel, Value: h.rainLevel})
	}
	if h.thunderLevel != oldThunder {
		h.broadcastEv(players, attachproto.GameEvent{Event: gameEventThunderLevel, Value: h.thunderLevel})
	}
	if wasRaining != h.raining {
		ev := gameEventEndRain
		if h.raining {
			ev = gameEventBeginRain
		}
		h.broadcastEv(players, attachproto.GameEvent{Event: int32(ev)})
		// vanilla re-sends both levels on the edge so late-window ramps align
		h.broadcastEv(players, attachproto.GameEvent{Event: gameEventRainLevel, Value: h.rainLevel})
		h.broadcastEv(players, attachproto.GameEvent{Event: gameEventThunderLevel, Value: h.thunderLevel})
	}
	if prevRainFlag != h.rainFlag || prevThunderFlag != h.thunderFlag {
		h.saveRules() // spell edges are rare; exact times also ride the autosave
	}

	h.tickLightning(players)
}

// boolTicks ports vanilla's clear-window timer freeze: an active spell's
// timer parks at 0, an inactive one at 1.
func boolTicks(active bool) int {
	if active {
		return 0
	}
	return 1
}

// rampLevel steps a weather level one vanilla tick toward its flag.
func rampLevel(v float32, up bool) float32 {
	if up {
		v += 0.01
	} else {
		v -= 0.01
	}
	return min(max(v, 0), 1)
}

// broadcastEv sends one event to every player.
func (h *hub) broadcastEv(players map[int32]*tracked, ev attachproto.GameEvent) {
	for _, t := range players {
		t.p.trySendEv(ev)
	}
}

// sendWeather pushes the current weather state to one late joiner — vanilla
// sendLevelInfo: a begin-rain event plus both levels.
func (h *hub) sendWeather(t *tracked) {
	if h.rainLevel > 0 {
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventBeginRain})
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventRainLevel, Value: h.rainLevel})
		t.p.trySendEv(attachproto.GameEvent{Event: gameEventThunderLevel, Value: h.thunderLevel})
	}
}

// resetWeatherCycle ports vanilla: sleeping through the night zeroes both
// timers with the flags off, so the storm ends and fresh delays roll next
// tick (the sky fades clear over the ramp's five seconds).
func (h *hub) resetWeatherCycle() {
	h.setRainFlag(false)
	h.setThunderFlag(false)
	h.rainTime, h.thunderTime = 0, 0
	h.saveRules()
}

// setRainFlag proposes a rain-state flip to plugin handlers and reports
// whether it was applied (an unchanged value is a silent no-op). Hub
// goroutine only.
func (h *hub) setRainFlag(v bool) bool {
	if v == h.rainFlag {
		return true
	}
	if !h.plugins.Fire(&plugin.WeatherChangeEvent{Raining: v}) {
		return false
	}
	h.rainFlag = v
	return true
}

// setThunderFlag is setRainFlag's thunder twin.
func (h *hub) setThunderFlag(v bool) bool {
	if v == h.thunderFlag {
		return true
	}
	if !h.plugins.Fire(&plugin.ThunderChangeEvent{Thundering: v}) {
		return false
	}
	h.thunderFlag = v
	return true
}

// setWeatherParameters ports MinecraftServer.setWeatherParameters — the
// /weather command's direct write of the cycle state.
func (h *hub) setWeatherParameters(clearTime, weatherTime int, rain, thunder bool) {
	h.clearTime = clearTime
	h.rainTime, h.thunderTime = weatherTime, weatherTime
	h.setRainFlag(rain)
	h.setThunderFlag(thunder)
	h.saveRules()
}

// applyWeatherCommand resolves a /weather with vanilla defaults: no duration
// means sampling the matching distribution (clear draws a fresh rain delay).
func (h *hub) applyWeatherCommand(e evSetWeather) {
	d := e.duration
	switch e.kind {
	case "clear":
		if d < 0 {
			d = h.uniformTicks(rainDelayMin, rainDelayMax)
		}
		h.setWeatherParameters(d, 0, false, false)
	case "rain":
		if d < 0 {
			d = h.uniformTicks(rainDurationMin, rainDurationMax)
		}
		h.setWeatherParameters(0, d, true, false)
	case "thunder":
		if d < 0 {
			d = h.uniformTicks(thunderDurationMin, thunderDurationMax)
		}
		h.setWeatherParameters(0, d, true, true)
	}
}

// --- lightning --------------------------------------------------------------

// isLightningRodState: the whole 8-variant oxidation/waxing family attracts
// (vanilla's lightning-rod POI covers the WeatheringCopperCollection); end
// rods are rods to placement but not to storms.
func isLightningRodState(state uint32) bool {
	return isRodState(state) && !isEndRod(state)
}

// rodIndexOnBlockChange keeps the lightning-rod set current as blocks change
// (the engine's stand-in for vanilla's POI index; overworld only).
func (h *hub) rodIndexOnBlockChange(x, y, z int, state uint32) {
	pos := blockPos{x, y, z}
	if isLightningRodState(state) {
		h.rods[pos] = struct{}{}
	} else {
		delete(h.rods, pos)
	}
}

// tickLightning rolls vanilla's per-chunk strike odds over each player's
// tracked window during a storm (raining AND thundering, both level-derived).
func (h *hub) tickLightning(players map[int32]*tracked) {
	if !h.raining || !h.thundering || len(players) == 0 {
		return
	}
	const area = (2*viewRadius + 1) * (2*viewRadius + 1)
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		if h.rng.Float64() >= float64(area)/lightningChunkOdds {
			continue
		}
		// vanilla: a random position inside a random loaded chunk
		x := (chunkFloor(t.x)+h.rng.Intn(2*viewRadius+1)-viewRadius)*16 + h.rng.Intn(16)
		z := (chunkFloor(t.z)+h.rng.Intn(2*viewRadius+1)-viewRadius)*16 + h.rng.Intn(16)
		sx, sy, sz, onRod := h.findLightningTarget(players, x, z)
		if !h.isRainingAt(sx, sy, sz) {
			continue // dry and snowy biomes see the storm but never the bolt
		}
		// vanilla skeleton trap: difficulty-scaled chance, never on a rod; the
		// trap's bolt is visual-only (no damage, no fire). The full four-
		// horsemen ambush on approach is future work — today it's the horse.
		visualOnly := false
		if h.rules.DoMobSpawning && !onRod &&
			h.rng.Float64() < float64(h.effectiveDifficulty())*0.01 {
			h.spawnMob(players, entitySkeletonHorse, float64(sx)+0.5, float64(sy), float64(sz)+0.5)
			visualOnly = true
		}
		h.strikeLightning(players, float64(sx)+0.5, float64(sy), float64(sz)+0.5, visualOnly)
	}
}

// findLightningTarget ports vanilla findLightningTargetAround: a lightning
// rod crowning its column within 128 blocks wins (the strike lands on its
// tip), then a random sky-exposed living entity near the column, then the
// surface itself. Reports whether a rod took the strike.
func (h *hub) findLightningTarget(players map[int32]*tracked, x, z int) (int, int, int, bool) {
	surf := h.world.SurfaceFeet(x, z)
	bestD := math.MaxFloat64
	var best blockPos
	found := false
	for pos := range h.rods {
		d := sq(float64(pos.x-x)) + sq(float64(pos.z-z))
		if d > rodAttractRange*rodAttractRange || d >= bestD {
			continue
		}
		if pos.y != h.world.SurfaceFeet(pos.x, pos.z)-1 {
			continue // vanilla: only a rod that is the top block of its column
		}
		bestD, best, found = d, pos, true
	}
	if found {
		return best.x, best.y + 1, best.z, true
	}
	// vanilla: sky-visible living entities within ±3 of the column
	type cand struct{ x, y, z float64 }
	var cands []cand
	for _, t := range players {
		if t.dim == 0 && math.Abs(t.x-float64(x)) <= 3 && math.Abs(t.z-float64(z)) <= 3 &&
			h.skyExposedColumn(int(math.Floor(t.x)), int(math.Floor(t.z))) {
			cands = append(cands, cand{t.x, t.y, t.z})
		}
	}
	for _, m := range h.mobs {
		if m.dim == 0 && m.dying == 0 && math.Abs(m.x-float64(x)) <= 3 && math.Abs(m.z-float64(z)) <= 3 &&
			h.skyExposedColumn(int(math.Floor(m.x)), int(math.Floor(m.z))) {
			cands = append(cands, cand{m.x, m.y, m.z})
		}
	}
	if len(cands) > 0 {
		c := cands[h.rng.Intn(len(cands))]
		return int(math.Floor(c.x)), int(math.Floor(c.y)), int(math.Floor(c.z)), false
	}
	return x, surf, z, false
}

// isRainingAt ports Level.precipitationAt == RAIN: open sky above, and a
// biome that rains (not dry, not cold enough to snow) at that height.
func (h *hub) isRainingAt(x, y, z int) bool {
	if !h.skyExposedColumn(x, z) {
		return false
	}
	return worldgen.PrecipitationAt(h.world.BiomeAt(x, z), y) == worldgen.PrecipRain
}

// effectiveDifficulty ports DifficultyInstance.calculateDifficulty with the
// per-chunk inhabited-time term zeroed (we don't track it); the moon phase
// follows vanilla's 8-day brightness table.
func (h *hub) effectiveDifficulty() float32 {
	base := h.rules.Difficulty
	if base == diffPeaceful {
		return 0
	}
	global := min(max((float32(h.tick.Load())+-72000.0)/1440000.0, 0), 1) * 0.25
	local := min(moonBrightness(h.dayTime.Load())*0.25, global)
	if base == diffEasy {
		local *= 0.5
	}
	return float32(base) * (0.75 + global + local)
}

// moonBrightness is vanilla DimensionType.MOON_BRIGHTNESS_PER_PHASE.
func moonBrightness(dayTime uint64) float32 {
	phases := [8]float32{1.0, 0.75, 0.5, 0.25, 0.0, 0.25, 0.5, 0.75}
	return phases[(dayTime/dayLengthTicks)%8]
}

// strikeLightning spawns the bolt flash, cracks the thunder, and (unless the
// bolt is a trap's visual) hurts everything at the strike point and starts
// fires on normal+ difficulty.
func (h *hub) strikeLightning(players map[int32]*tracked, x, y, z float64, visualOnly bool) {
	eid := h.allocEID()
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(eid))
	h.toNearbyEv(players, 0, x, z, entAdd(eid, entityLightning, uuid, x, y, z, 0, 0))
	h.bolts = append(h.bolts, bolt{eid: eid, x: x, z: z, dieAt: h.tick.Load() + boltLifeTicks})
	// Thunder is heard far beyond the chunk-tracking radius in vanilla; the
	// crack at volume 10 carries like the real thing.
	h.playSound(players, "minecraft:entity.lightning_bolt.thunder", sndBlock, x, y, z, 10, 0.8+h.rng.Float32()*0.4)
	h.playSound(players, "minecraft:entity.lightning_bolt.impact", sndBlock, x, y, z, 2, 1)
	h.bus.publish("lightning", map[string]any{"x": x, "y": y, "z": z})
	if visualOnly {
		return
	}

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
	h.strikeFire(players, int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z)))
}

// strikeFire ports LightningBolt.spawnFire: on normal+ difficulty, fire at
// the strike cell plus four attempts one block around it.
func (h *hub) strikeFire(players map[int32]*tracked, x, y, z int) {
	if h.rules.Difficulty < diffNormal {
		return
	}
	try := func(pos blockPos) {
		if h.world.At(pos.x, pos.y, pos.z) == worldgen.Air && h.validFireLocation(pos) {
			h.igniteFire(players, pos, 0)
		}
	}
	try(blockPos{x, y, z})
	for i := 0; i < 4; i++ {
		try(blockPos{x + h.rng.Intn(3) - 1, y + h.rng.Intn(3) - 1, z + h.rng.Intn(3) - 1})
	}
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

// cmdWeather is the op command: /weather <clear|rain|thunder> [duration],
// where duration takes vanilla time suffixes (300 ticks, 15s, 1d).
func (s *Server) cmdWeather(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to change the weather.")
		return
	}
	if len(args) < 1 || len(args) > 2 {
		p.tell("Usage: /weather <clear|rain|thunder> [duration]")
		return
	}
	switch args[0] {
	case "clear", "rain", "thunder":
	default:
		p.tell("Usage: /weather <clear|rain|thunder> [duration]")
		return
	}
	dur := -1
	if len(args) == 2 {
		d, err := parseTimeArg(args[1])
		if err != nil {
			p.tell("Bad duration (try 6000, 300s or 1d): " + args[1])
			return
		}
		dur = d
	}
	s.hub.post(evSetWeather{kind: args[0], duration: dur})
	p.tell(fmt.Sprintf("Weather set to %s", args[0]))
}

// parseTimeArg parses vanilla's TimeArgument: an integer with an optional
// unit suffix (t = ticks, s = seconds, d = in-game days; bare = ticks).
func parseTimeArg(s string) (int, error) {
	mult := 1
	switch {
	case strings.HasSuffix(s, "t"):
		s = strings.TrimSuffix(s, "t")
	case strings.HasSuffix(s, "s"):
		s, mult = strings.TrimSuffix(s, "s"), 20
	case strings.HasSuffix(s, "d"):
		s, mult = strings.TrimSuffix(s, "d"), 24000
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("bad time %q", s)
	}
	return n * mult, nil
}

type evSetWeather struct {
	kind     string // clear | rain | thunder
	duration int    // ticks; -1 = sample the vanilla distribution
}

func (evSetWeather) isHubEvent() {}
