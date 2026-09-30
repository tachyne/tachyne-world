package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"

	"fmt"
	"math"
	"strconv"
	"strings"
)

// commands2.go — the survival-multiplayer command-parity batch: private
// messages, kick, clear, spawnpoint, playsound, particle.

// cmdMsg sends a private whisper (/msg, /tell, /w).
func (s *Server) cmdMsg(p *player, args []string) {
	if len(args) < 2 {
		p.tell("Usage: /msg <player> <message>")
		return
	}
	text := strings.Join(args[1:], " ")
	s.hub.post(evWhisper{from: p, to: args[0], text: text})
}

type evWhisper struct {
	from *player
	to   string
	text string
}

func (evWhisper) isHubEvent() {}

// onWhisper is MsgCommand: the targets are EntityArgument.players(), so a
// name or any player selector (@a, @p, @r, @a[team=red]…); each target
// hears it and the sender sees one outgoing line per target.
func (h *hub) onWhisper(players map[int32]*tracked, e evWhisper) {
	spec, ok := parseTargetSpec(e.to)
	if !ok {
		e.from.trySendEv(chatEv("Invalid name or UUID"))
		return
	}
	if spec.selectsEntities() { // EntityArgument.players(): ERROR_ONLY_PLAYERS_ALLOWED
		e.from.trySendEv(chatEv("Only players may be affected by this command, but the provided selector includes entities"))
		return
	}
	targets := h.selectPlayers(players, players[e.from.eid], spec)
	if len(targets) == 0 {
		e.from.trySendEv(chatEv("No player was found"))
		return
	}
	for _, t := range targets {
		t.p.trySendEv(chatEv(fmt.Sprintf("%s whispers to you: %s", sourceName(e.from), e.text)))
		e.from.trySendEv(chatEv(fmt.Sprintf("You whisper to %s: %s", t.p.name, e.text)))
	}
}

// cmdKick disconnects an online player (op only).
func (s *Server) cmdKick(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 1 {
		p.tell("Usage: /kick <player> [reason]")
		return
	}
	reason := "Kicked by an operator."
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}
	s.hub.post(evKick{by: p, name: args[0], reason: reason})
}

type evKick struct {
	by     *player
	name   string
	reason string
}

func (evKick) isHubEvent() {}

func (h *hub) onKick(players map[int32]*tracked, e evKick) {
	for _, t := range players {
		if strings.EqualFold(t.p.name, e.name) {
			// The disconnect SCREEN, with the reason on it — a chat line
			// followed by a dropped socket left the player looking at
			// "connection lost" and none the wiser.
			reason := strings.TrimSpace(e.reason)
			if reason == "" {
				reason = "Kicked by an operator"
			}
			t.p.trySendEv(attachproto.Disconnect{Reason: reason})
			t.p.disconnect()
			h.cmdSuccess(players, e.by, "Kicked "+t.p.name+".", true)
			return
		}
	}
	e.by.trySendEv(chatEv("No player named " + e.name + " is online."))
}

// cmdClear is ClearInventoryCommands: /clear [targets] [item] [maxCount]
// takes (up to maxCount of) an item, or everything, from the targets; a
// maxCount of 0 only counts.
func (s *Server) cmdClear(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	e := evClearInv{by: p, name: p.name, max: -1}
	if len(args) > 0 {
		e.name = args[0]
	}
	if len(args) > 1 {
		id, ok := itemByName[strings.TrimPrefix(args[1], "minecraft:")]
		if !ok {
			p.tell("Unknown item: " + args[1])
			return
		}
		e.item = int32(id)
	}
	if len(args) > 2 {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 {
			p.tell("Usage: /clear [targets] [item] [maxCount]")
			return
		}
		e.max = n
	}
	if len(args) > 3 {
		p.tell("Usage: /clear [targets] [item] [maxCount]")
		return
	}
	s.hub.post(e)
}

type evClearInv struct {
	by   *player
	name string
	item int32 // 0 = any item
	max  int   // -1 = no limit; 0 = count only
}

func (evClearInv) isHubEvent() {}

func (h *hub) onClearInv(players map[int32]*tracked, e evClearInv) {
	targets := h.commandTargets(players, e.by.eid, e.name)
	if len(targets) == 0 {
		for _, t := range players { // a case-insensitive name still works
			if strings.EqualFold(t.p.name, e.name) {
				targets = append(targets, t)
			}
		}
	}
	if len(targets) == 0 {
		e.by.trySendEv(chatEv("No player matched " + e.name + "."))
		return
	}
	total, who := 0, ""
	for _, t := range targets {
		if t.inv == nil {
			continue
		}
		left := e.max
		n := 0
		// Inventory.clearOrCountMatchingItems: the slots, the armour, the
		// offhand, and what the cursor carries.
		take := func(st *invStack) {
			if st.item == 0 || st.count <= 0 || (e.item != 0 && st.item != e.item) {
				return
			}
			k := st.count
			if left >= 0 && k > left {
				k = left
			}
			n += k
			if e.max == 0 {
				return // counting only
			}
			if left >= 0 {
				left -= k
			}
			if st.count -= k; st.count <= 0 {
				*st = invStack{}
			}
		}
		for i := range t.inv.slots {
			take(&t.inv.slots[i])
		}
		for i := range t.armor {
			take(&t.armor[i])
		}
		take(&t.offhand)
		take(&t.cursor)
		if e.max != 0 {
			h.sendInventory(t)
			h.sendCursor(t)
			h.broadcastEquipment(players, t)
		}
		total += n
		who = t.p.name
	}
	switch {
	case total == 0 && len(targets) == 1:
		e.by.trySendEv(chatEv("No items were found on player " + targets[0].p.name))
	case total == 0:
		e.by.trySendEv(chatEv(fmt.Sprintf("No items were found on %d players", len(targets))))
	case e.max == 0 && len(targets) == 1:
		h.cmdSuccess(players, e.by, fmt.Sprintf("Found %d matching item(s) on player %s", total, who), true)
	case e.max == 0:
		h.cmdSuccess(players, e.by, fmt.Sprintf("Found %d matching item(s) on %d players", total, len(targets)), true)
	case len(targets) == 1:
		h.cmdSuccess(players, e.by, fmt.Sprintf("Removed %d item(s) from player %s", total, who), true)
	default:
		h.cmdSuccess(players, e.by, fmt.Sprintf("Removed %d item(s) from %d players", total, len(targets)), true)
	}
}

// cmdSpawnpoint is SetSpawnCommand: /spawnpoint [targets] [pos] [yaw pitch]
// sets the targets' (by default the caller's) respawn point in the caller's
// dimension, at the caller's position unless one is given.
func (s *Server) cmdSpawnpoint(p *player, args []string) {
	if !s.isOp(p.name) { // LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /spawnpoint [<targets> [<x> <y> <z> [<yaw> <pitch>]]]"
	e := evSetSpawnpoint{eid: p.eid, target: p.name,
		pos: blockPos{floorInt(p.x), floorInt(p.y), floorInt(p.z)}}
	switch len(args) {
	case 0:
	case 1, 4, 6:
		e.target = args[0]
		if len(args) >= 4 {
			x, y, z, ok := parsePosition(args[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
			if !ok {
				p.tell(usage)
				return
			}
			e.pos = blockPos{floorInt(x), floorInt(y), floorInt(z)}
		}
		if len(args) == 6 {
			yaw, ok1 := parseCoord(args[4], float64(p.yaw))
			pitch, ok2 := parseCoord(args[5], float64(p.pitch))
			if !ok1 || !ok2 {
				p.tell(usage)
				return
			}
			e.yaw, e.pitch = float32(math.Remainder(yaw, 360)), float32(math.Max(-90, math.Min(90, pitch)))
		}
	default:
		p.tell(usage)
		return
	}
	s.hub.post(e)
}

type evSetSpawnpoint struct {
	eid        int32
	target     string
	pos        blockPos
	yaw, pitch float32
}

func (evSetSpawnpoint) isHubEvent() {}

func (h *hub) onSetSpawnpoint(players map[int32]*tracked, e evSetSpawnpoint) {
	caller := players[e.eid]
	if caller == nil {
		return
	}
	targets := h.commandTargets(players, e.eid, e.target)
	if len(targets) == 0 {
		caller.p.tell("No player was found")
		return
	}
	for _, t := range targets {
		h.spawns.set(t.p.key(), e.pos, caller.dim)
	}
	who := targets[0].p.name
	if len(targets) > 1 {
		who = fmt.Sprintf("%d players", len(targets))
	}
	dim := dimRegistryName(caller.dim)
	h.cmdSuccess(players, caller.p, fmt.Sprintf("Set spawn point to %d, %d, %d [%.1f, %.1f] in %s for %s",
		e.pos.x, e.pos.y, e.pos.z, e.yaw, e.pitch, dim, who), true)
}

// soundSources are SoundSource's names, in its ordinal order.
var soundSources = map[string]int32{"master": 0, "music": 1, "record": 2, "weather": 3, "block": 4,
	"hostile": 5, "neutral": 6, "player": 7, "ambient": 8, "voice": 9, "ui": 10}

// cmdPlaysound is PlaySoundCommand:
// /playsound <sound> [source] [targets] [x y z] [volume] [pitch] [minVolume]
// — master and yourself by default, at your own position. (A second word
// that is not a source is taken as the targets, as this command once read.)
func (s *Server) cmdPlaysound(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /playsound <sound> [source] [targets] [<x> <y> <z>] [volume] [pitch] [minVolume]"
	if len(args) < 1 {
		p.tell(usage)
		return
	}
	e := evPlaysound{by: p, name: args[0], target: p.name, source: 0,
		x: p.x, y: p.y, z: p.z, vol: 1, pitch: 1}
	if !strings.Contains(e.name, ":") {
		e.name = "minecraft:" + e.name
	}
	rest := args[1:]
	if len(rest) > 0 {
		if src, ok := soundSources[rest[0]]; ok {
			e.source, rest = src, rest[1:]
		}
	}
	if len(rest) > 0 {
		e.target, rest = rest[0], rest[1:]
	}
	if len(rest) >= 3 {
		x, y, z, ok := parsePosition(rest[:3], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell(usage)
			return
		}
		e.x, e.y, e.z, rest = x, y, z, rest[3:]
	}
	for i, dst := range []*float32{&e.vol, &e.pitch, &e.minVol} {
		if i >= len(rest) {
			break
		}
		v, err := strconv.ParseFloat(rest[i], 32)
		if err != nil || v < 0 {
			p.tell(usage)
			return
		}
		*dst = float32(v)
	}
	s.hub.post(e)
}

type evPlaysound struct {
	by                 *player
	name               string
	target             string
	source             int32
	x, y, z            float64
	vol, pitch, minVol float32
}

func (evPlaysound) isHubEvent() {}

// onPlaysound sends the sound to each target in range of it (16 blocks, or
// 16 × a volume above one); one further off hears it two blocks away in its
// direction at minVolume, or not at all.
func (h *hub) onPlaysound(players map[int32]*tracked, e evPlaysound) {
	caller := players[e.by.eid]
	targets := h.commandTargets(players, e.by.eid, e.target)
	if len(targets) == 0 { // a case-insensitive name, as this command always took
		for _, t := range players {
			if strings.EqualFold(t.p.name, e.target) {
				targets = append(targets, t)
			}
		}
	}
	rng := 16.0
	if e.vol > 1 {
		rng *= float64(e.vol)
	}
	played, who := 0, ""
	for _, t := range targets {
		if caller != nil && t.dim != caller.dim {
			continue
		}
		x, y, z, vol := e.x, e.y, e.z, e.vol
		dx, dy, dz := e.x-t.x, e.y-t.y, e.z-t.z
		if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d > rng {
			if e.minVol <= 0 {
				continue
			}
			x, y, z, vol = t.x+dx/d*2, t.y+dy/d*2, t.z+dz/d*2, e.minVol
		}
		t.p.trySendEv(soundEv(e.name, e.source, x, y, z, vol, e.pitch))
		played, who = played+1, t.p.name
	}
	switch {
	case played == 0:
		e.by.tell("The sound is too far away to be heard")
	case played == 1:
		h.cmdSuccess(players, e.by, fmt.Sprintf("Played sound %s to %s", e.name, who), true)
	default:
		h.cmdSuccess(players, e.by, fmt.Sprintf("Played sound %s to %d players", e.name, played), true)
	}
}

// cmdStopsound is StopSoundCommand: /stopsound <targets> [<source>|*]
// [<sound>] — every sound, every sound of a source, or one sound.
func (s *Server) cmdStopsound(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /stopsound <targets> [<source>|*] [<sound>]"
	if len(args) < 1 || len(args) > 3 {
		p.tell(usage)
		return
	}
	e := evStopsound{by: p, target: args[0], source: -1}
	if len(args) > 1 && args[1] != "*" {
		src, ok := soundSources[args[1]]
		if !ok {
			p.tell(usage)
			return
		}
		e.source = src
	}
	if len(args) > 2 {
		e.name = args[2]
		if !strings.Contains(e.name, ":") {
			e.name = "minecraft:" + e.name
		}
	}
	s.hub.post(e)
}

type evStopsound struct {
	by     *player
	target string
	source int32 // -1 = any
	name   string
}

func (evStopsound) isHubEvent() {}

func (h *hub) onStopsound(players map[int32]*tracked, e evStopsound) {
	targets := h.commandTargets(players, e.by.eid, e.target)
	if len(targets) == 0 {
		e.by.tell("No player was found")
		return
	}
	for _, t := range targets {
		t.p.trySendEv(attachproto.StopSound{Category: e.source, Name: e.name})
	}
	var srcName string
	for n, v := range soundSources {
		if v == e.source {
			srcName = n
		}
	}
	var msg string
	switch {
	case e.source >= 0 && e.name != "":
		msg = fmt.Sprintf("Stopped sound '%s' on source '%s'", e.name, srcName)
	case e.source >= 0:
		msg = fmt.Sprintf("Stopped all '%s' sounds", srcName)
	case e.name != "":
		msg = fmt.Sprintf("Stopped sound '%s'", e.name)
	default:
		msg = "Stopped all sounds"
	}
	h.cmdSuccess(players, e.by, msg, true)
}

func (s *Server) cmdParticle(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /particle <name>[<options>] [<pos> [<delta> <speed> <count> [force|normal [<viewers>]]]]"
	if len(args) < 1 || len(args) == 2 || len(args) == 3 || (len(args) > 4 && len(args) < 9) || len(args) > 11 {
		p.tell(usage)
		return
	}
	e := evParticleCmd{by: p.eid, dim: p.dim, x: p.x, y: p.y, z: p.z}
	pid, name, msg := parseParticleArg(args[0])
	if msg != "" {
		p.tell(msg)
		return
	}
	e.pid, e.name = pid, name
	if len(args) >= 4 {
		x, y, z, ok := parsePosition(args[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell(usage)
			return
		}
		e.x, e.y, e.z = x, y, z
	}
	if len(args) >= 9 {
		// Vec3Argument.vec3(false): the delta takes ~ (relative to zero) but
		// not ^, and no centring.
		for i := range e.delta {
			v, ok := parseCoord(args[4+i], 0)
			if !ok || strings.HasPrefix(args[4+i], "^") {
				p.tell(usage)
				return
			}
			e.delta[i] = v
		}
		speed, err := strconv.ParseFloat(args[7], 32)
		if err != nil || speed < 0 {
			p.tell(usage)
			return
		}
		count, err := strconv.Atoi(args[8])
		if err != nil || count < 0 {
			p.tell(usage)
			return
		}
		e.speed, e.count = float32(speed), int32(count)
	}
	if len(args) >= 10 {
		switch args[9] {
		case "force":
			e.force = true
		case "normal":
		default:
			p.tell(usage)
			return
		}
	}
	if len(args) == 11 {
		e.viewers = args[10]
	}
	s.hub.post(e)
}

// parseParticleArg is ParticleArgument: a particle type by id (26.3's
// registry), with its options as SNBT when the type takes them. The
// attach Particles frame carries a type id and nothing else, so a type
// that needs options — or options given at all — cannot reach a client yet
// and is refused by name, rather than sent bare (which a client cannot
// parse). A bare canonical id is still taken, as this command once did.
func parseParticleArg(arg string) (int32, string, string) {
	name, opts, hasOpts := strings.Cut(arg, "{")
	name = strings.TrimPrefix(name, "minecraft:")
	if pid, ok := particleByName[name]; ok && !hasOpts {
		return pid, "minecraft:" + name, ""
	}
	if n, err := strconv.Atoi(name); err == nil && !hasOpts {
		return int32(n), "minecraft:" + name, ""
	}
	if !particleTypes263[name] {
		return 0, "", "Unknown particle: minecraft:" + name
	}
	if hasOpts {
		if _, err := parseSNBT("{" + opts); err != nil {
			return 0, "", fmt.Sprintf("Can't parse particle options: %v", err)
		}
	}
	return 0, "", fmt.Sprintf("The particle minecraft:%s can't be shown yet: particles with options (or new in 26.x) have no way to the client", name)
}

// particleTypes263 is 26.3's minecraft:particle_type registry.
var particleTypes263 = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`angry_villager block block_marker bubble sulfur_bubbles noxious_gas
		noxious_gas_cloud geyser geyser_base geyser_poof geyser_plume cloud copper_fire_flame crit
		damage_indicator dragon_breath dripping_lava falling_lava landing_lava dripping_water
		falling_water dust dust_color_transition effect elder_guardian enchanted_hit enchant end_rod
		entity_effect explosion_emitter explosion gust small_gust gust_emitter_large
		gust_emitter_small sonic_boom falling_dust firework fishing flame infested cherry_leaves
		pale_oak_leaves red_poplar_leaves orange_poplar_leaves yellow_poplar_leaves tinted_leaves
		sculk_soul sculk_charge sculk_charge_pop soul_fire_flame soul flash happy_villager composter
		heart instant_effect item vibration trail pause_mob_growth reset_mob_growth item_slime
		item_cobweb item_snowball large_smoke lava mycelium note poof portal rain smoke white_smoke
		sneeze spit squid_ink sweep_attack totem_of_undying underwater splash witch bubble_pop
		current_down bubble_column_up nautilus dolphin campfire_cosy_smoke campfire_signal_smoke
		dripping_honey falling_honey landing_honey falling_nectar falling_spore_blossom ash
		crimson_spore warped_spore spore_blossom_air dripping_obsidian_tear falling_obsidian_tear
		landing_obsidian_tear reverse_portal white_ash small_flame snowflake
		dripping_dripstone_lava falling_dripstone_lava dripping_dripstone_water
		falling_dripstone_water glow_squid_ink glow wax_on wax_off electric_spark scrape shriek
		egg_crack dust_plume trial_spawner_detection trial_spawner_detection_ominous
		vault_connection dust_pillar ominous_spawning raid_omen trial_omen block_crumble firefly
		sulfur_cube_goo`) {
		m[n] = true
	}
	return m
}()

// evParticleCmd is one /particle, run on the hub.
type evParticleCmd struct {
	by      int32
	dim     int // the operator's dimension
	pid     int32
	name    string // the particle's id, for the reply
	x, y, z float64
	delta   [3]float64
	speed   float32
	count   int32
	force   bool   // force: seen from 512 blocks, not 32
	viewers string // a player selector ("" = every player)
}

// onParticleCmd is ParticleCommand.sendParticles: to each viewer in the
// operator's dimension whose block position is within 32 blocks of the
// point (512 with force), counting who saw it. The attach frame has one
// spread, not three, so the widest delta stands for all three axes.
func (h *hub) onParticleCmd(players map[int32]*tracked, e evParticleCmd) {
	var by *player
	if t := players[e.by]; t != nil {
		by = t.p
	}
	var viewers []*tracked
	if e.viewers == "" {
		for _, t := range players {
			viewers = append(viewers, t)
		}
	} else {
		viewers = h.commandTargets(players, e.by, e.viewers)
	}
	spread := float32(math.Max(math.Abs(e.delta[0]), math.Max(math.Abs(e.delta[1]), math.Abs(e.delta[2]))))
	r := 32.0
	if e.force {
		r = 512
	}
	seen := 0
	for _, t := range viewers {
		if t.dim != e.dim {
			continue
		}
		dx := float64(floorInt(t.x)) + 0.5 - e.x
		dy := float64(floorInt(t.y)) + 0.5 - e.y
		dz := float64(floorInt(t.z)) + 0.5 - e.z
		if dx*dx+dy*dy+dz*dz >= r*r { // BlockPos.closerToCenterThan
			continue
		}
		t.p.trySendEv(attachproto.Particles{PID: e.pid, X: e.x, Y: e.y, Z: e.z, Spread: spread, Speed: e.speed, Count: e.count})
		seen++
	}
	if seen == 0 {
		cmdFail(by, "The particle was not visible for anybody")
		return
	}
	h.cmdSuccess(players, by, "Displaying particle "+e.name, true)
}

func (evParticleCmd) isHubEvent() {}
