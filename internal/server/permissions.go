package server

import "strconv"

// Operator permission levels — vanilla's PermissionLevel: 0 everyone,
// 1 moderators, 2 gamemasters, 3 admins, 4 owners. Every command's root
// node carries one of Commands.LEVEL_* (cmdPermission); a player's level
// is the highest their op entries give them, and the server console is 4.
//
// Where a level comes from:
//   - the -ops flag: level 4, or name:N for another level;
//   - tachyne-access roles, read at join: "op" is level 4 (what every
//     operator has always been), "op1" … "op4" name their level;
//   - /op, which grants the role for the server's op-permission-level
//     setting (DedicatedServerProperties "op-permission-level", default 4).
const (
	permAll         = 0
	permModerators  = 1
	permGamemasters = 2
	permAdmins      = 3
	permOwners      = 4
)

// cmdPermission is each command's required level: the requires() on its
// root node. Commands missing here are open to everyone (help, list, msg,
// me, teammsg, trigger, random's value/roll, and the engine's own).
var cmdPermission = map[string]int{
	// LEVEL_GAMEMASTERS
	"advancement": permGamemasters, "attribute": permGamemasters, "bossbar": permGamemasters,
	"clear": permGamemasters, "clone": permGamemasters, "damage": permGamemasters,
	"defaultgamemode": permGamemasters, "difficulty": permGamemasters, "effect": permGamemasters,
	"enchant": permGamemasters, "execute": permGamemasters, "experience": permGamemasters, "xp": permGamemasters,
	"fetchprofile": permGamemasters, "fill": permGamemasters, "forceload": permGamemasters,
	"gamemode": permGamemasters, "gm": permGamemasters, "gamerule": permGamemasters,
	"give": permGamemasters, "item": permGamemasters, "kill": permGamemasters,
	"locate": permGamemasters, "loot": permGamemasters, "particle": permGamemasters,
	"playsound": permGamemasters, "posteffect": permGamemasters, "recipe": permGamemasters,
	"ride": permGamemasters, "rotate": permGamemasters, "say": permGamemasters,
	"scoreboard": permGamemasters, "seed": permGamemasters, "setblock": permGamemasters,
	"spawnpoint": permGamemasters, "setworldspawn": permGamemasters, "spectate": permGamemasters,
	"spreadplayers": permGamemasters, "stopsound": permGamemasters, "stopwatch": permGamemasters,
	"summon": permGamemasters, "swing": permGamemasters, "tag": permGamemasters,
	"team": permGamemasters, "teleport": permGamemasters, "tp": permGamemasters,
	"tellraw": permGamemasters, "time": permGamemasters, "title": permGamemasters,
	"version": permGamemasters, "waypoint": permGamemasters, "weather": permGamemasters,
	"worldborder": permGamemasters,
	// LEVEL_ADMINS
	"ban": permAdmins, "ban-ip": permAdmins, "banlist": permAdmins, "deop": permAdmins,
	"kick": permAdmins, "op": permAdmins, "pardon": permAdmins, "pardon-ip": permAdmins,
	"setidletimeout": permAdmins, "tick": permAdmins, "transfer": permAdmins,
	"whitelist": permAdmins,
	// LEVEL_OWNERS
	"save-all": permOwners, "save-off": permOwners, "save-on": permOwners, "stop": permOwners,
}

// opRoleLevel reads one tachyne-access role as an op level (0: not an
// op role).
func opRoleLevel(role string) int {
	switch role {
	case roleOp:
		return permOwners
	case "op1", "op2", "op3", "op4":
		return int(role[2] - '0')
	}
	return 0
}

// roleForLevel is the access role /op grants for a level.
func roleForLevel(level int) string {
	if level >= permOwners {
		return roleOp
	}
	return "op" + strconv.Itoa(level)
}

// opEntry is what a player's access roles make them: their level and the
// op roles that give it (what /deop revokes).
type opEntry struct {
	level int
	roles []string
}

// opEntryFor reads the op roles among a player's access roles (ok false:
// none).
func opEntryFor(roles []string) (opEntry, bool) {
	var e opEntry
	for _, r := range roles {
		if lvl := opRoleLevel(r); lvl > 0 {
			e.roles = append(e.roles, r)
			e.level = max(e.level, lvl)
		}
	}
	return e, len(e.roles) > 0
}

// opPermissionLevel is the level /op grants: the server's setting, 4 when
// unset or out of range.
func (s *Server) opPermissionLevel() int {
	if s.OpPermissionLevel < permModerators || s.OpPermissionLevel > permOwners {
		return permOwners
	}
	return s.OpPermissionLevel
}

// opLevel is a player's permission level: the highest of the -ops flag's
// and their access roles'; the console is 4.
func (s *Server) opLevel(name string) int {
	if name == consoleName {
		return permOwners
	}
	if v, ok := s.execLevels.Load(name); ok { // an /execute source keeps its runner's level (execute.go)
		if lvl, ok := v.(int); ok {
			return lvl
		}
	}
	lvl := permAll
	if s.Ops[name] {
		lvl = permOwners
		if n, ok := s.OpLevels[name]; ok && n >= permAll && n <= permOwners {
			lvl = n
		}
	}
	if v, ok := s.roleOps.Load(name); ok {
		if e, ok := v.(opEntry); ok {
			lvl = max(lvl, e.level)
		}
	}
	return lvl
}

// hasPermission is CommandSourceStack.permissions().hasPermission(level).
func (s *Server) hasPermission(name string, level int) bool { return s.opLevel(name) >= level }

// isOp reports whether a player may run the gamemaster commands (level 2)
// — the level of nearly every command that changes the world.
func (s *Server) isOp(name string) bool { return s.hasPermission(name, permGamemasters) }

// isAnyOp is PlayerList.isOp: on the op list at any level. Operators hear
// the admin broadcasts of other operators' commands.
func (s *Server) isAnyOp(name string) bool { return s.opLevel(name) > permAll }

// commandPermitted is the dispatcher's requires() check on a command's root
// node.
func (s *Server) commandPermitted(p *player, cmd string) bool {
	need, ok := cmdPermission[cmd]
	return !ok || s.hasPermission(p.name, need)
}
