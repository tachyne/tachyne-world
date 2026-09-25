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
		cmd = v.line
		fields = strings.Fields(v.line)
		if len(fields) == 0 {
			return
		}
	}
	switch fields[0] {
	case "help":
		help := "Commands: /help /say /msg /teammsg /list /time /tp /weather /effect /give /kill /clear /kick /xp /summon /enchant /setblock /fill /seed /me /spawnpoint /setworldspawn /playsound /stopsound /tellraw /difficulty /gamerule /gamemode /defaultgamemode /hud /worldborder /locate /title /advancement /attribute /recipe /tag /ride /damage /spreadplayers /forceload /random /swing /clone /bossbar /save-all /save-off /save-on /version /stop /item /loot /bug" +
			" — targets take @s @p @a @r @e (with type=, distance=, limit=, name=, tag=), coordinates take ~ and ^." +
			" /bug <what went wrong> reports something with the blocks around you attached; /bug list shows the last few and /bug re <text> adds to one."
		if s.hub.plugHost != nil {
			help += s.hub.plugHost.pluginHelp()
		}
		p.tell(help)
	case "msg", "tell", "w":
		s.cmdMsg(p, fields[1:])
	case "kick":
		s.cmdKick(p, fields[1:])
	case "clear":
		s.cmdClear(p, fields[1:])
	case "spawnpoint":
		s.cmdSpawnpoint(p, fields[1:])
	case "playsound":
		s.cmdPlaysound(p, fields[1:])
	case "stopsound":
		s.cmdStopsound(p, fields[1:])
	case "tellraw":
		s.cmdTellraw(p, cmd)
	case "particle":
		s.cmdParticle(p, fields[1:])
	case "bug":
		s.cmdBug(p, fields[1:])
	case "title":
		s.cmdTitle(p, fields[1:])
	case "posteffect":
		s.cmdPostEffect(p, fields[1:])
	case "tick":
		s.cmdTick(p, fields[1:])
	case "transfer":
		s.cmdTransfer(p, fields[1:])
	case "spectate":
		s.cmdSpectate(p, fields[1:])
	case "say":
		if !s.isOp(p.name) { // SayCommand requires LEVEL_GAMEMASTERS
			p.tell("You don't have permission to use /say.")
			break
		}
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
	case "locate":
		s.cmdLocate(p, fields[1:])
	case "whitelist":
		s.cmdWhitelist(p, fields[1:])
	case "ban":
		if s.Access != nil && s.isOp(p.name) {
			s.cmdBanAccess(p, fields[1:])
		} else {
			s.cmdBan(p, fields[1:])
		}
	case "pardon":
		if s.Access != nil {
			s.cmdPardonAccess(p, fields[1:], false)
		} else {
			s.cmdPardon(p, fields[1:])
		}
	case "pardon-ip":
		if s.Access != nil {
			s.cmdPardonAccess(p, fields[1:], true)
		} else {
			p.tell("IP bans live in the access service, which is not configured.")
		}
	case "ban-ip":
		s.cmdBanIP(p, fields[1:])
	case "banlist":
		s.cmdBanlist(p, fields[1:])
	case "op":
		s.cmdOp(p, fields[1:], true)
	case "deop":
		s.cmdOp(p, fields[1:], false)
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
	case "worldborder":
		if !s.isOp(p.name) {
			p.tell("You don't have permission to change the world border.")
			return
		}
		s.hub.post(evBorderCmd{p: p, args: fields[1:]})
	case "trigger": // every player: the objective's enable is the permission
		s.hub.post(evTriggerCmd{p: p, args: fields[1:]})
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
	case "xp", "experience":
		s.cmdXP(p, fields[1:])
	case "summon":
		s.cmdSummon(p, fields[1:])
	case "setblock":
		s.cmdSetblock(p, fields[1:])
	case "enchant":
		s.cmdEnchant(p, fields[1:])
	case "fill":
		s.cmdFill(p, fields[1:])
	case "clone":
		s.cmdClone(p, fields[1:])
	case "bossbar":
		s.cmdBossbar(p, fields[1:])
	case "save-all":
		s.cmdSaveAll(p, fields[1:])
	case "save-off":
		s.cmdSaveOff(p, fields[1:])
	case "save-on":
		s.cmdSaveOn(p, fields[1:])
	case "version":
		s.cmdVersion(p, fields[1:])
	case "item":
		s.cmdItem(p, fields[1:])
	case "loot":
		s.cmdLoot(p, fields[1:])
	case "stop":
		s.cmdStop(p, fields[1:])
	case "seed":
		if !s.isOp(p.name) { // SeedCommand: gamemasters on a dedicated server
			p.tell("You don't have permission.")
			break
		}
		s.info(p, fmt.Sprintf("Seed: [%d]", s.Seed))
	case "me":
		if len(fields) > 1 { // EmoteCommands: "* name action" to everyone
			s.hub.post(evChat{text: fmt.Sprintf("* %s %s", p.name, strings.Join(fields[1:], " "))})
		}
	case "difficulty":
		s.cmdDifficulty(p, fields[1:])
	case "gamerule":
		s.cmdGamerule(p, fields[1:])
	case "gamemode", "gm":
		s.cmdGamemode(p, fields[1:])
	case "hud":
		s.cmdHud(p, fields[1:])
	case "advancement":
		s.cmdAdvancement(p, fields[1:])
	case "attribute":
		s.cmdAttribute(p, fields[1:])
	case "recipe":
		s.cmdRecipe(p, fields[1:])
	case "tag":
		s.cmdTag(p, fields[1:])
	case "ride":
		s.cmdRide(p, fields[1:])
	case "rotate":
		if !s.isOp(p.name) { // RotateCommand: LEVEL_GAMEMASTERS
			p.tell("You don't have permission.")
			return
		}
		s.hub.post(evRotate{eid: p.eid, args: fields[1:]})
	case "damage":
		s.cmdDamage(p, fields[1:])
	case "spreadplayers":
		s.cmdSpreadPlayers(p, fields[1:])
	case "forceload":
		s.cmdForceLoad(p, fields[1:])
	case "setworldspawn":
		s.cmdSetWorldSpawn(p, fields[1:])
	case "defaultgamemode":
		s.cmdDefaultGamemode(p, fields[1:])
	case "random":
		s.cmdRandom(p, fields[1:])
	case "swing":
		s.cmdSwing(p, fields[1:])
	case "teammsg", "tm":
		s.cmdTeamMsg(p, fields[1:])
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
	// TimeCommand requires LEVEL_GAMEMASTERS.
	if !s.isOp(p.name) {
		p.tell("You don't have permission to change the time.")
		return
	}
	now := s.hub.dayTime.Load()
	usage := "Usage: /time set <time|day|noon|night|midnight> | /time add <time> | /time query <daytime|gametime|day>"
	if len(args) == 0 {
		p.tell(usage)
		return
	}
	switch args[0] {
	case "query":
		if len(args) < 2 {
			p.tell(usage)
			return
		}
		switch args[1] {
		case "daytime":
			s.info(p, fmt.Sprintf("The time is %d", now%dayLengthTicks))
		case "gametime":
			s.info(p, fmt.Sprintf("The time is %d", s.hub.tick.Load()))
		case "day":
			s.info(p, fmt.Sprintf("The time is %d", now/dayLengthTicks))
		default:
			p.tell(usage)
		}
		return
	case "add":
		n, ok := parseTimeTicks(args[1:])
		if !ok {
			p.tell(usage)
			return
		}
		t := uint64(int64(now) + n)
		s.hub.post(evSetTime{t: t})
		s.ok(p, fmt.Sprintf("Set the time to %d", t%dayLengthTicks))
		return
	case "set":
		args = args[1:] // the marker or number follows
	}
	if len(args) == 0 {
		p.tell(usage)
		return
	}
	var t uint64
	switch args[0] { // the overworld clock's time markers
	case "day":
		t = 1000
	case "noon":
		t = 6000
	case "night":
		t = 13000
	case "midnight":
		t = 18000
	default:
		n, ok := parseTimeTicks(args)
		if !ok || n < 0 {
			p.tell(usage)
			return
		}
		t = uint64(n)
	}
	s.hub.post(evSetTime{t: t}) // through the hub so the plugin TimeSetEvent fires
	s.ok(p, fmt.Sprintf("Set the time to %d", t%dayLengthTicks))
}

// parseTimeTicks is TimeArgument: a number of ticks, or with a unit suffix
// d (a day, 24000 ticks), s (a second, 20) or t (a tick); fractions round.
func parseTimeTicks(args []string) (int64, bool) {
	if len(args) == 0 || args[0] == "" {
		return 0, false
	}
	a, mul := args[0], 1.0
	switch a[len(a)-1] {
	case 'd':
		a, mul = a[:len(a)-1], dayLengthTicks
	case 's':
		a, mul = a[:len(a)-1], 20
	case 't':
		a = a[:len(a)-1]
	}
	f, err := strconv.ParseFloat(a, 64)
	if err != nil {
		return 0, false
	}
	return int64(math.Round(f * mul)), true
}

// cmdTeleport moves the player to absolute coordinates and re-streams chunks.
func (s *Server) cmdTeleport(p *player, args []string) {
	if !s.isOp(p.name) { // TeleportCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	// /tp <player|selector> puts you where they are; /tp <x> <y> <z> takes
	// vanilla's coordinate forms — plain numbers, `~` relative to you, or
	// `^` along the way you are looking.
	if len(args) == 1 {
		s.hub.post(evTeleportTo{eid: p.eid, target: args[0]})
		return
	}
	// /tp <targets> <destination> and /tp <targets> <x> <y> <z> [<yaw> <pitch>]:
	// someone else goes (TeleportCommand's targets forms). Positions and
	// rotations are relative to the one running the command.
	// /tp <targets> <x y z> facing <x y z> | facing entity <target> [eyes|feet].
	if len(args) >= 6 && args[4] == "facing" {
		x, y, z, ok := parsePosition(args[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell("Usage: /tp <targets> <x> <y> <z> facing <x> <y> <z> | facing entity <target> [eyes|feet]")
			return
		}
		e := evTeleportTargets{by: p.eid, targets: args[0], x: x, y: y, z: z}
		switch {
		case args[5] == "entity" && (len(args) == 7 || len(args) == 8):
			e.faceEntity, e.faceEyes = args[6], len(args) == 7 || args[7] == "eyes"
		case len(args) == 8:
			fx, fy, fz, ok := parsePosition(args[5:8], p.x, p.y, p.z, p.yaw, p.pitch)
			if !ok {
				p.tell("Usage: /tp <targets> <x> <y> <z> facing <x> <y> <z>")
				return
			}
			e.face, e.fx, e.fy, e.fz = true, fx, fy, fz
		default:
			p.tell("Usage: /tp <targets> <x> <y> <z> facing <x> <y> <z> | facing entity <target> [eyes|feet]")
			return
		}
		s.hub.post(e)
		return
	}
	if len(args) == 2 || len(args) == 4 || len(args) == 6 {
		e := evTeleportTargets{by: p.eid, targets: args[0]}
		if len(args) == 2 {
			e.dest = args[1]
		} else {
			x, y, z, ok := parsePosition(args[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
			if !ok {
				p.tell("Usage: /tp <targets> <x> <y> <z> [<yaw> <pitch>]")
				return
			}
			e.x, e.y, e.z = x, y, z
			if len(args) == 6 {
				yaw, ok1 := parseCoord(args[4], float64(p.yaw))
				pitch, ok2 := parseCoord(args[5], float64(p.pitch))
				if !ok1 || !ok2 {
					p.tell("Usage: /tp <targets> <x> <y> <z> [<yaw> <pitch>]")
					return
				}
				e.rot, e.yaw, e.pitch = true, float32(yaw), float32(math.Max(-90, math.Min(90, pitch)))
			}
		}
		s.hub.post(e)
		return
	}
	if len(args) != 3 {
		p.tell("Usage: /tp <x> <y> <z> | /tp <player|@selector> | /tp <targets> <destination|x y z>")
		return
	}
	x, y, z, ok := parsePosition(args, p.x, p.y, p.z, p.yaw, p.pitch)
	if !ok {
		p.tell("Usage: /tp <x> <y> <z> (numbers, ~relative or ^local)")
		return
	}
	p.x, p.y, p.z = x, y, z
	p.setHubPos(p.x, p.z)
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evMove{eid: p.eid, x: x, y: y, z: z, yaw: p.yaw, pitch: p.pitch, onGround: false, teleport: true})
	s.ok(p, fmt.Sprintf("Teleported to %.1f %.1f %.1f", x, y, z))
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
		p.tell("Usage: /gamemode <survival|creative|adventure|spectator> [player|@selector]")
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
	if !strings.HasPrefix(target, "@") {
		s.modes.set(target, mode) // a named player who is offline still gets it at their next join
	}

	// Always apply via the hub: it owns the authoritative tracked.gamemode that
	// pickup and the survival sim read. (The old self path only updated the client,
	// so a player who switched themselves to survival still couldn't pick up items.)
	s.hub.post(evSetGamemode{name: target, mode: mode, by: p.name, eid: p.eid, modes: s.modes})
	if target == p.name {
		s.ok(p, "Set own game mode to "+args[0])
	} else {
		s.ok(p, "Set "+target+"'s game mode to "+args[0])
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
