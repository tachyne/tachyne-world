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

func (h *hub) onWhisper(players map[int32]*tracked, e evWhisper) {
	for _, t := range players {
		if strings.EqualFold(t.p.name, e.to) {
			t.p.trySendEv(chatEv(fmt.Sprintf("%s whispers to you: %s", e.from.name, e.text)))
			e.from.trySendEv(chatEv(fmt.Sprintf("You whisper to %s: %s", t.p.name, e.text)))
			return
		}
	}
	e.from.trySendEv(chatEv("No player named " + e.to + " is online."))
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

var dimensionNames = [...]string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"}

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
	dim := dimensionNames[0]
	if caller.dim >= 0 && caller.dim < len(dimensionNames) {
		dim = dimensionNames[caller.dim]
	}
	h.cmdSuccess(players, caller.p, fmt.Sprintf("Set spawn point to %d, %d, %d [%.1f, %.1f] in %s for %s",
		e.pos.x, e.pos.y, e.pos.z, e.yaw, e.pitch, dim, who), true)
}

// cmdPlaysound plays a named sound at the caller (or a target player).
// /playsound <name> [player] [volume] [pitch]
func (s *Server) cmdPlaysound(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 1 {
		p.tell("Usage: /playsound <sound> [player|@selector] [volume] [pitch]")
		return
	}
	name := args[0]
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	target := p.name
	vol, pitch := 1.0, 1.0
	if len(args) > 1 {
		target = args[1]
	}
	if len(args) > 2 {
		if v, err := strconv.ParseFloat(args[2], 64); err == nil {
			vol = v
		}
	}
	if len(args) > 3 {
		if v, err := strconv.ParseFloat(args[3], 64); err == nil {
			pitch = v
		}
	}
	s.hub.post(evPlaysound{by: p, name: name, target: target, vol: float32(vol), pitch: float32(pitch)})
}

type evPlaysound struct {
	by         *player
	name       string
	target     string
	vol, pitch float32
}

func (evPlaysound) isHubEvent() {}

func (h *hub) onPlaysound(players map[int32]*tracked, e evPlaysound) {
	hit := false
	for _, t := range h.commandTargets(players, e.by.eid, e.target) {
		t.p.trySendEv(soundEv(e.name, sndPlayer, t.x, t.y, t.z, e.vol, e.pitch))
		hit = true
	}
	if !hit { // fall back to a case-insensitive name, as this command always did
		for _, t := range players {
			if strings.EqualFold(t.p.name, e.target) {
				t.p.trySendEv(soundEv(e.name, sndPlayer, t.x, t.y, t.z, e.vol, e.pitch))
				return
			}
		}
		e.by.trySendEv(chatEv("No player matched " + e.target + "."))
	}
}

// cmdParticle spawns particles at coordinates (op debug tool).
// /particle <id> <x> <y> <z> [count]
func (s *Server) cmdParticle(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 4 {
		p.tell("Usage: /particle <id> <x> <y> <z> [count]")
		return
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil {
		p.tell("Particle ids are numeric (canonical registry).")
		return
	}
	x, y, z, okPos := parsePosition(args[1:], p.x, p.y, p.z, p.yaw, p.pitch)
	if !okPos {
		p.tell("Bad coordinates.")
		return
	}
	count := 10
	if len(args) > 4 {
		if c, err := strconv.Atoi(args[4]); err == nil {
			count = c
		}
	}
	s.hub.post(evParticleCmd{dim: p.dim, pid: int32(pid), x: x, y: y, z: z, count: int32(count)})
}

type evParticleCmd struct {
	dim     int // the operator's dimension
	pid     int32
	x, y, z float64
	count   int32
}

func (evParticleCmd) isHubEvent() {}
