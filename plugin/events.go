package plugin

// The event catalog. Every event is fired as a pointer on the tick
// goroutine; struct fields are scalars (entity IDs, names, coordinates)
// so the same structs can later be published as JSON on the
// out-of-process bus. Fields documented as mutable are read back by the
// engine after the handler ladder finishes; everything else is
// informational. Resolve an EID to a live handle with
// Server.Player/Server.Mob — the handle may already be gone (Valid()
// == false) by the time a scheduled task runs.

// SpawnReason says what caused a mob spawn.
type SpawnReason int

const (
	SpawnNatural SpawnReason = iota // the natural spawner or a world mechanic
	SpawnCommand                    // /summon or another chat command
	SpawnBus                        // an out-of-process bus command
	SpawnPlugin                     // Server.SpawnMob from a plugin
	SpawnOther
)

func (r SpawnReason) String() string {
	switch r {
	case SpawnNatural:
		return "natural"
	case SpawnCommand:
		return "command"
	case SpawnBus:
		return "bus"
	case SpawnPlugin:
		return "plugin"
	default:
		return "other"
	}
}

// PlayerJoinEvent fires after a player has fully joined.
type PlayerJoinEvent struct {
	EID     int32
	Name    string
	X, Y, Z float64
	Dim     int
}

func (*PlayerJoinEvent) EventName() string { return "player_join" }

// PlayerQuitEvent fires after a player has left.
type PlayerQuitEvent struct {
	EID  int32
	Name string
}

func (*PlayerQuitEvent) EventName() string { return "player_quit" }

// PlayerChatEvent fires before a chat message is broadcast. Message is
// mutable; cancelling suppresses the broadcast entirely.
type PlayerChatEvent struct {
	Cancel
	EID     int32
	Name    string
	Message string // mutable
}

func (*PlayerChatEvent) EventName() string { return "player_chat" }

// PlayerCommandEvent fires before a chat command executes. Line is the
// full command without the leading slash and is mutable (rewriting
// re-dispatches the new line); cancelling suppresses execution.
type PlayerCommandEvent struct {
	Cancel
	EID  int32
	Name string
	Line string // mutable
}

func (*PlayerCommandEvent) EventName() string { return "player_command" }

// PlayerMoveEvent fires after a player movement is applied. Observe-only
// in this milestone. This event is hot — fires for every movement packet.
type PlayerMoveEvent struct {
	EID                 int32
	Name                string
	FromX, FromY, FromZ float64
	ToX, ToY, ToZ       float64
	Dim                 int
}

func (*PlayerMoveEvent) EventName() string { return "player_move" }

// BlockBreakEvent fires before a survival/creative dig removes a block.
// State is the block being broken. Cancelling restores the block on the
// digger's client and drops nothing.
type BlockBreakEvent struct {
	Cancel
	EID     int32
	Name    string
	Dim     int
	X, Y, Z int
	State   uint32
}

func (*BlockBreakEvent) EventName() string { return "block_break" }

// BlockPlaceEvent fires before a block placement commits. State is the
// proposed block state and is mutable (swap what actually gets placed).
// Fires once per placement action: multi-cell placements (doors, beds)
// place all their cells under one event.
type BlockPlaceEvent struct {
	Cancel
	EID     int32
	Name    string
	Dim     int
	X, Y, Z int
	State   uint32 // mutable
}

func (*BlockPlaceEvent) EventName() string { return "block_place" }

// EntityDamageByEntityEvent fires before melee damage between a player
// and a mob (either direction) is applied. Damage is mutable.
type EntityDamageByEntityEvent struct {
	Cancel
	AttackerEID      int32
	VictimEID        int32
	AttackerIsPlayer bool
	VictimIsPlayer   bool
	Damage           float64 // mutable
}

func (*EntityDamageByEntityEvent) EventName() string { return "entity_damage_by_entity" }

// MobSpawnEvent fires when a mob spawns, after it is registered (so a
// handler may fetch the handle with Server.Mob and adjust stats) but
// before it is announced. Cancelling removes it silently.
type MobSpawnEvent struct {
	Cancel
	EID      int32
	Type     int
	TypeName string
	X, Y, Z  float64
	Dim      int
	Reason   SpawnReason
}

func (*MobSpawnEvent) EventName() string { return "mob_spawn" }

// MobDeathEvent fires when a mob dies, before loot appears. Drops and XP
// are mutable. KillerEID is the last attacking entity or 0.
type MobDeathEvent struct {
	EID       int32
	Type      int
	TypeName  string
	X, Y, Z   float64
	Dim       int
	KillerEID int32
	Drops     []ItemStack // mutable
	XP        int         // mutable
}

func (*MobDeathEvent) EventName() string { return "mob_death" }

// WeatherChangeEvent fires before the rain state flips. Raining is the
// proposed new state. Cancelling a natural flip re-rolls the weather
// timer.
type WeatherChangeEvent struct {
	Cancel
	Raining bool
}

func (*WeatherChangeEvent) EventName() string { return "weather_change" }

// ThunderChangeEvent fires before the thunder state flips.
type ThunderChangeEvent struct {
	Cancel
	Thundering bool
}

func (*ThunderChangeEvent) EventName() string { return "thunder_change" }

// TimeSetEvent fires after the day time is set explicitly (command, bus,
// or plugin) — not on the natural once-per-tick advance.
type TimeSetEvent struct {
	Old, New uint64
}

func (*TimeSetEvent) EventName() string { return "time_set" }

// GameruleChangeEvent fires after a gamerule change is applied. Bool
// rules use On; difficulty reports its level in Num.
type GameruleChangeEvent struct {
	Rule string
	On   bool
	Num  int
}

func (*GameruleChangeEvent) EventName() string { return "gamerule_change" }
