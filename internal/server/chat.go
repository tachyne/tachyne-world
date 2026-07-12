package server

import (
	"fmt"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
	"strconv"
	"strings"
)

// readLeadingString reads a VarInt-length-prefixed string from the start of a
// packet body. Both Chat Message and Chat Command lead with their text, and the

// tell sends a private system message to one player.
func (p *player) tell(text string) { p.sendEv(chatEv(text)) }

// handleCommand parses and runs a "/command" (the leading slash is already
// stripped by the client). Unknown or malformed commands just get a hint.
func (s *Server) handleCommand(p *player, cmd string) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return
	}
	// Plugins first: a PlayerCommandEvent listener may cancel or rewrite the
	// line, and a plugin-registered command consumes it. Unhandled (possibly
	// rewritten) lines fall through to the switch below.
	if v := s.pluginCommand(p, cmd, fields[0]); v.handled {
		return
	} else if v.line != cmd {
		fields = strings.Fields(v.line)
		if len(fields) == 0 {
			return
		}
	}
	switch fields[0] {
	case "help":
		help := "Commands: /help /say <msg> /list /time <day|night|noon|midnight|N> /tp <x> <y> <z> /weather <clear|rain|thunder> /effect /give /kill /xp /summon /difficulty /gamerule /gamemode <mode> [player] /hud [on|off]"
		if s.hub.plugHost != nil {
			help += s.hub.plugHost.pluginHelp()
		}
		p.tell(help)
	case "say":
		if len(fields) > 1 {
			s.hub.post(evChat{text: fmt.Sprintf("[%s] %s", p.name, strings.Join(fields[1:], " "))})
		}
	case "list":
		s.hub.post(evList{p: p})
	case "plugin":
		s.cmdPlugin(p, fields[1:])
	case "time":
		s.cmdTime(p, fields[1:])
	case "tp", "teleport":
		s.cmdTeleport(p, fields[1:])
	case "whitelist":
		s.cmdWhitelist(p, fields[1:])
	case "ban":
		s.cmdBan(p, fields[1:])
	case "pardon":
		s.cmdPardon(p, fields[1:])
	case "refresh": // force-resend every chunk in view (fixes client render loss)
		p.sendEv(attachproto.Resync{})
		if p.dim == 2 {
			s.hub.post(evEndRefresh{eid: p.eid}) // dragon + crystals too
		}
		p.tell("World re-sent.")
	case "rescue": // clean overworld extraction for a stuck player
		if p.dim != 0 {
			s.switchDimension(p, 0)
		} else {
			w := s.worldFor(p)
			p.x, p.y, p.z = 0.5, w.SurfaceY(0, 0), 0.5
			p.setHubPos(p.x, p.z)
			p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
			s.hub.post(evDim{eid: p.eid, dim: 0, x: p.x, y: p.y, z: p.z})
		}
		p.tell("Rescued to safety.")
	case "where":
		w := s.worldFor(p)
		hx, hz := p.hubPos()
		p.tell(fmt.Sprintf("dim=%d pos=(%.1f,%.1f,%.1f) gate=(%.0f,%.0f) feet-block=%d below=%d",
			p.dim, p.x, p.y, p.z, hx, hz,
			w.At(int(math.Floor(p.x)), int(math.Floor(p.y)), int(math.Floor(p.z))),
			w.At(int(math.Floor(p.x)), int(math.Floor(p.y))-1, int(math.Floor(p.z)))))
	case "nether":
		if !s.isOp(p.name) {
			p.tell("You don't have permission.")
			break
		}
		if p.dim == 1 {
			s.switchDimension(p, 0)
		} else {
			s.switchDimension(p, 1)
		}
	case "end":
		if !s.isOp(p.name) {
			p.tell("You don't have permission.")
			break
		}
		if p.dim == 2 {
			s.switchDimension(p, 0)
		} else {
			s.switchDimension(p, 2)
		}

	case "weather":
		s.cmdWeather(p, fields[1:])
	case "scoreboard":
		if !s.isOp(p.name) {
			p.tell("You don't have permission to use the scoreboard.")
			return
		}
		s.hub.post(evScoreboardCmd{p: p, args: fields[1:]})
	case "team":
		if !s.isOp(p.name) {
			p.tell("You don't have permission to manage teams.")
			return
		}
		s.hub.post(evTeamCmd{p: p, args: fields[1:]})
	case "effect":
		s.cmdEffect(p, fields[1:])
	case "give":
		s.cmdGive(p, fields[1:])
	case "kill":
		s.cmdKill(p, fields[1:])
	case "xp":
		s.cmdXP(p, fields[1:])
	case "summon":
		s.cmdSummon(p, fields[1:])
	case "difficulty":
		s.cmdDifficulty(p, fields[1:])
	case "gamerule":
		s.cmdGamerule(p, fields[1:])
	case "gamemode", "gm":
		s.cmdGamemode(p, fields[1:])
	case "hud":
		s.cmdHud(p, fields[1:])
	default:
		p.tell("Unknown command: /" + fields[0] + " (try /help)")
	}
}

// cmdHud toggles the player's action-bar HUD (or sets it on/off explicitly).
func (s *Server) cmdHud(p *player, args []string) {
	on := !p.hudOn // no arg = toggle
	if len(args) >= 1 {
		switch args[0] {
		case "on":
			on = true
		case "off":
			on = false
		default:
			p.tell("Usage: /hud [on|off]")
			return
		}
	}
	p.hudOn = on
	s.hub.post(evSetHud{eid: p.eid, on: on})
	if on {
		p.tell("HUD enabled.")
	} else {
		p.tell("HUD disabled.")
	}
}

// cmdTime sets or queries the day/night clock.
func (s *Server) cmdTime(p *player, args []string) {
	if len(args) == 0 {
		p.tell(fmt.Sprintf("Time of day: %d", s.hub.dayTime.Load()%dayLengthTicks))
		return
	}
	var t uint64
	switch args[0] {
	case "day":
		t = 1000
	case "noon":
		t = 6000
	case "night":
		t = 13000
	case "midnight":
		t = 18000
	default:
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 {
			p.tell("Usage: /time <day|night|noon|midnight|N>")
			return
		}
		t = uint64(n)
	}
	s.hub.post(evSetTime{t: t}) // through the hub so the plugin TimeSetEvent fires
	p.tell(fmt.Sprintf("Set the time to %d", t%dayLengthTicks))
}

// cmdTeleport moves the player to absolute coordinates and re-streams chunks.
func (s *Server) cmdTeleport(p *player, args []string) {
	if len(args) != 3 {
		p.tell("Usage: /tp <x> <y> <z>")
		return
	}
	x, ex := strconv.ParseFloat(args[0], 64)
	y, ey := strconv.ParseFloat(args[1], 64)
	z, ez := strconv.ParseFloat(args[2], 64)
	if ex != nil || ey != nil || ez != nil {
		p.tell("Usage: /tp <x> <y> <z> (numbers)")
		return
	}
	p.x, p.y, p.z = x, y, z
	p.setHubPos(p.x, p.z)
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evMove{eid: p.eid, x: x, y: y, z: z, yaw: p.yaw, pitch: p.pitch, onGround: false, teleport: true})
	p.tell(fmt.Sprintf("Teleported to %.1f %.1f %.1f", x, y, z))
}

// cmdGamemode changes a player's game mode and persists it, so a mixed
// survival/creative server remembers it. Restricted to ops; with a trailing
// name an op can set someone else's mode.
func (s *Server) cmdGamemode(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to change game mode.")
		return
	}
	if len(args) < 1 || len(args) > 2 {
		p.tell("Usage: /gamemode <survival|creative|adventure|spectator> [player]")
		return
	}
	mode, ok := ParseGamemode(args[0])
	if !ok {
		p.tell("Unknown gamemode: " + args[0])
		return
	}

	target := p.name
	if len(args) == 2 {
		target = args[1]
	}
	s.modes.set(target, mode) // persist for next join too

	// Always apply via the hub: it owns the authoritative tracked.gamemode that
	// pickup and the survival sim read. (The old self path only updated the client,
	// so a player who switched themselves to survival still couldn't pick up items.)
	s.hub.post(evSetGamemode{name: target, mode: mode, by: p.name})
	if target == p.name {
		p.tell("Set own game mode to " + args[0])
	} else {
		p.tell("Set " + target + "'s game mode to " + args[0])
	}
}

// applyGamemode tells a connected player their new mode + matching abilities.
func applyGamemode(p *player, mode int) {
	p.sendEv(attachproto.GameEvent{Event: gameEventChangeGameMode, Value: float32(mode)})
	p.sendEv(abilitiesFor(mode))
}

// ParseGamemode maps a name or number to a game mode id.
func ParseGamemode(name string) (int, bool) {
	switch name {
	case "survival", "0":
		return gmSurvival, true
	case "creative", "1":
		return gmCreative, true
	case "adventure", "2":
		return gmAdventure, true
	case "spectator", "3":
		return gmSpectator, true
	}
	return 0, false
}
