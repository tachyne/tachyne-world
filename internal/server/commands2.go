package server

import (
	"fmt"
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
			t.p.trySendEv(chatEv("Kicked: " + e.reason))
			t.p.disconnect()
			e.by.trySendEv(chatEv("Kicked " + t.p.name + "."))
			return
		}
	}
	e.by.trySendEv(chatEv("No player named " + e.name + " is online."))
}

// cmdClear empties an inventory (op; defaults to self).
func (s *Server) cmdClear(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	target := p.name
	if len(args) > 0 {
		target = args[0]
	}
	s.hub.post(evClearInv{by: p, name: target})
}

type evClearInv struct {
	by   *player
	name string
}

func (evClearInv) isHubEvent() {}

func (h *hub) onClearInv(players map[int32]*tracked, e evClearInv) {
	for _, t := range players {
		if strings.EqualFold(t.p.name, e.name) {
			if t.inv == nil {
				return
			}
			n := 0
			for i := range t.inv.slots {
				if t.inv.slots[i].item != 0 {
					n += t.inv.slots[i].count
					t.inv.slots[i] = invStack{}
				}
			}
			for i := range t.armor {
				if t.armor[i].item != 0 {
					n += t.armor[i].count
					t.armor[i] = invStack{}
				}
			}
			if t.offhand.item != 0 {
				n += t.offhand.count
				t.offhand = invStack{}
			}
			t.cursor = invStack{}
			h.sendInventory(t)
			h.sendCursor(t)
			h.broadcastEquipment(players, t)
			e.by.trySendEv(chatEv(fmt.Sprintf("Removed %d item(s) from %s.", n, t.p.name)))
			return
		}
	}
	e.by.trySendEv(chatEv("No player named " + e.name + " is online."))
}

// cmdSpawnpoint sets the caller's respawn point to where they stand.
func (s *Server) cmdSpawnpoint(p *player, args []string) {
	s.hub.post(evSetSpawnpoint{eid: p.eid})
}

type evSetSpawnpoint struct{ eid int32 }

func (evSetSpawnpoint) isHubEvent() {}

func (h *hub) onSetSpawnpoint(players map[int32]*tracked, e evSetSpawnpoint) {
	t := players[e.eid]
	if t == nil || t.dim != 0 {
		if t != nil {
			t.p.trySendEv(chatEv("Spawn points only take in the overworld."))
		}
		return
	}
	pos := blockPos{int(t.x), int(t.y), int(t.z)}
	h.spawns.set(t.p.name, pos)
	t.p.trySendEv(chatEv(fmt.Sprintf("Spawn point set to %d, %d, %d.", pos.x, pos.y, pos.z)))
}

// cmdPlaysound plays a named sound at the caller (or a target player).
// /playsound <name> [player] [volume] [pitch]
func (s *Server) cmdPlaysound(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 1 {
		p.tell("Usage: /playsound <sound> [player] [volume] [pitch]")
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
	for _, t := range players {
		if strings.EqualFold(t.p.name, e.target) {
			t.p.trySendEv(soundEv(e.name, sndPlayer, t.x, t.y, t.z, e.vol, e.pitch))
			return
		}
	}
	e.by.trySendEv(chatEv("No player named " + e.target + " is online."))
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
	x, e1 := strconv.ParseFloat(args[1], 64)
	y, e2 := strconv.ParseFloat(args[2], 64)
	z, e3 := strconv.ParseFloat(args[3], 64)
	if e1 != nil || e2 != nil || e3 != nil {
		p.tell("Bad coordinates.")
		return
	}
	count := 10
	if len(args) > 4 {
		if c, err := strconv.Atoi(args[4]); err == nil {
			count = c
		}
	}
	s.hub.post(evParticleCmd{pid: int32(pid), x: x, y: y, z: z, count: int32(count)})
}

type evParticleCmd struct {
	pid     int32
	x, y, z float64
	count   int32
}

func (evParticleCmd) isHubEvent() {}
