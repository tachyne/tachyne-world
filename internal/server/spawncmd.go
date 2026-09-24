package server

import (
	"fmt"
	"math"
)

// /setworldspawn [<pos> [<angle> [<pitch>]]] (SetWorldSpawnCommand) and
// /defaultgamemode <mode> (DefaultGameModeCommands). Both are world settings
// and persist in settings.json, and both outrank the boot flags (-spawn,
// -gamemode) once set: a flag describes where a fresh world starts, the
// command is where the running one was moved to.

// worldSpawnSave is the saved world spawn: the block and the facing a player
// arrives with (LevelData.RespawnData).
type worldSpawnSave struct {
	X     int     `json:"x"`
	Y     int     `json:"y"`
	Z     int     `json:"z"`
	Yaw   float32 `json:"yaw,omitempty"`
	Pitch float32 `json:"pitch,omitempty"`
}

type evSetWorldSpawn struct {
	by         int32
	dim        int
	x, y, z    int
	yaw, pitch float32
}

func (evSetWorldSpawn) isHubEvent() {}

func (s *Server) cmdSetWorldSpawn(p *player, args []string) {
	if !s.isOp(p.name) { // SetWorldSpawnCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	usage := "Usage: /setworldspawn [<x> <y> <z> [<angle> [<pitch>]]]"
	e := evSetWorldSpawn{by: p.eid, dim: p.dim}
	fx, fy, fz := p.x, p.y, p.z
	switch len(args) {
	case 0:
	case 3, 4, 5:
		x, y, z, ok := parsePosition(args, p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell(usage)
			return
		}
		fx, fy, fz = x, y, z
		// RotationArgument: the angle, then the pitch; ~ is relative to the
		// caller's own, and an omitted one is zero (ZERO_ROTATION).
		rot := []float64{0, 0}
		base := []float64{float64(p.yaw), float64(p.pitch)}
		for i, a := range args[3:] {
			v, ok := parseCoord(a, base[i])
			if !ok {
				p.tell(usage)
				return
			}
			rot[i] = v
		}
		e.yaw, e.pitch = float32(rot[0]), float32(rot[1])
	default:
		p.tell(usage)
		return
	}
	e.x, e.y, e.z = int(math.Floor(fx)), int(math.Floor(fy)), int(math.Floor(fz))
	s.hub.post(e)
}

// applySetWorldSpawn runs /setworldspawn on the hub: the respawn fallback, the
// join position and every client's compass move to the new point.
func (h *hub) applySetWorldSpawn(players map[int32]*tracked, e evSetWorldSpawn) {
	tell := cmdTeller(players, e.by)
	if e.dim != dimOverworld {
		tell("Can only set the world spawn for the Overworld")
		return
	}
	sp := worldSpawnSave{X: e.x, Y: e.y, Z: e.z, Yaw: e.yaw, Pitch: e.pitch}
	h.setWorldSpawn(sp)
	h.rules.WorldSpawn = &sp
	h.saveRules()
	for _, t := range players {
		h.sendDefaultSpawn(t)
	}
	tell(fmt.Sprintf("Set the world spawn point to %d, %d, %d [%s]", e.x, e.y, e.z, jFloat(e.yaw)))
}

// setWorldSpawn installs a world spawn: the hub's respawn fallback, and the
// copy a joining session reads (spawnPub) — the centre of the block, as
// vanilla stands an arriving player.
func (h *hub) setWorldSpawn(sp worldSpawnSave) {
	h.worldSpawnX, h.worldSpawnY, h.worldSpawnZ = float64(sp.X)+0.5, float64(sp.Y), float64(sp.Z)+0.5
	h.worldSpawnYaw, h.worldSpawnPitch = sp.Yaw, sp.Pitch
	h.hasWorldSpawn = true
	h.spawnPub.Store(&[3]float64{h.worldSpawnX, h.worldSpawnY, h.worldSpawnZ})
}

// restoreWorldSpawn applies a saved /setworldspawn at boot (after loadRules),
// over whatever -spawn said. Returns false when nothing was saved.
func (s *Server) restoreWorldSpawn() bool {
	sp := s.hub.rules.WorldSpawn
	if sp == nil {
		return false
	}
	x, y, z := float64(sp.X)+0.5, float64(sp.Y), float64(sp.Z)+0.5
	if s.hub.ownedAt(x, z) { // as with -spawn: a shard respawns only onto its own turf
		s.hub.setWorldSpawn(*sp)
	}
	s.SpawnX, s.SpawnY, s.SpawnZ = x, y, z
	s.SpawnSet, s.SpawnAuto = true, false
	return true
}

// joinSpawn is where a player with no saved position arrives: the command's
// world spawn when one was set, else the configured one, else the surface at
// the origin. Safe off the hub goroutine.
func (s *Server) joinSpawn() (x, y, z float64) {
	if sp := s.hub.spawnPub.Load(); sp != nil {
		return sp[0], sp[1], sp[2]
	}
	if s.SpawnSet {
		return s.SpawnX, s.SpawnY, s.SpawnZ
	}
	return 0.5, s.world.SurfaceY(0, 0), 0.5
}

// ---- /defaultgamemode -------------------------------------------------------

type evDefaultGamemode struct {
	by    int32
	mode  int
	modes *modeStore
}

func (evDefaultGamemode) isHubEvent() {}

func (s *Server) cmdDefaultGamemode(p *player, args []string) {
	if !s.isOp(p.name) { // DefaultGameModeCommands: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 1 {
		p.tell("Usage: /defaultgamemode <survival|creative|adventure|spectator>")
		return
	}
	mode, ok := ParseGamemode(args[0])
	if !ok {
		p.tell("Unknown gamemode: " + args[0])
		return
	}
	s.hub.post(evDefaultGamemode{by: p.eid, mode: mode, modes: s.modes})
}

// gameModeLongName is GameType.getLongDisplayName.
func gameModeLongName(mode int) string {
	switch mode {
	case gmCreative:
		return "Creative Mode"
	case gmAdventure:
		return "Adventure Mode"
	case gmSpectator:
		return "Spectator Mode"
	}
	return "Survival Mode"
}

// applyDefaultGamemode runs /defaultgamemode on the hub. Players already known
// keep the mode they have (their modes are pinned at join); only players new
// to the server start in this one.
func (h *hub) applyDefaultGamemode(players map[int32]*tracked, e evDefaultGamemode) {
	if e.modes != nil {
		e.modes.setDefault(e.mode)
	}
	mode := e.mode
	h.rules.DefaultGamemode = &mode
	h.saveRules()
	cmdTeller(players, e.by)("The default game mode is now " + gameModeLongName(e.mode))
}
