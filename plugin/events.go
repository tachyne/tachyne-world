package plugin

import "encoding/json"

// The event catalog. Every event is fired as a pointer on the tick
// goroutine. Struct fields are scalars (entity IDs, names, coordinates)
// and carry snake_case JSON tags — these structs ARE the bus payloads
// (mc.event.<name>), so the wire shape is lowercase/snake_case
// throughout. Fields documented as mutable are read back by the engine
// after the handler ladder finishes; everything else is informational.
// Resolve an EID to a live handle with Server.Player/Server.Mob — the
// handle may already be gone (Valid() == false) by the time a scheduled
// task runs.

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

// MarshalJSON puts the readable name on the wire (bus payloads).
func (r SpawnReason) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON accepts the readable name back (symmetry for consumers).
func (r *SpawnReason) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	switch s {
	case "natural":
		*r = SpawnNatural
	case "command":
		*r = SpawnCommand
	case "bus":
		*r = SpawnBus
	case "plugin":
		*r = SpawnPlugin
	default:
		*r = SpawnOther
	}
	return nil
}

// PlayerJoinEvent fires after a player has fully joined.
type PlayerJoinEvent struct {
	EID  int32   `json:"eid"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Z    float64 `json:"z"`
	Dim  int     `json:"dim"`
}

func (*PlayerJoinEvent) EventName() string { return "player_join" }

// PlayerQuitEvent fires after a player has left.
type PlayerQuitEvent struct {
	EID  int32  `json:"eid"`
	Name string `json:"name"`
}

func (*PlayerQuitEvent) EventName() string { return "player_quit" }

// PlayerChatEvent fires before a chat message is broadcast. Message is
// mutable; cancelling suppresses the broadcast entirely.
type PlayerChatEvent struct {
	Cancel
	EID     int32  `json:"eid"`
	Name    string `json:"name"`
	Message string `json:"message"` // mutable
}

func (*PlayerChatEvent) EventName() string { return "player_chat" }

// PlayerCommandEvent fires before a chat command executes. Line is the
// full command without the leading slash and is mutable (rewriting
// re-dispatches the new line); cancelling suppresses execution.
type PlayerCommandEvent struct {
	Cancel
	EID  int32  `json:"eid"`
	Name string `json:"name"`
	Line string `json:"line"` // mutable
}

func (*PlayerCommandEvent) EventName() string { return "player_command" }

// PlayerMoveEvent fires after a player movement is applied. Observe-only
// in this milestone. This event is hot — fires for every movement packet.
type PlayerMoveEvent struct {
	EID   int32   `json:"eid"`
	Name  string  `json:"name"`
	FromX float64 `json:"from_x"`
	FromY float64 `json:"from_y"`
	FromZ float64 `json:"from_z"`
	ToX   float64 `json:"to_x"`
	ToY   float64 `json:"to_y"`
	ToZ   float64 `json:"to_z"`
	Dim   int     `json:"dim"`
}

func (*PlayerMoveEvent) EventName() string { return "player_move" }

// BlockBreakEvent fires before a survival/creative dig removes a block.
// State is the block being broken. Cancelling restores the block on the
// digger's client and drops nothing.
type BlockBreakEvent struct {
	Cancel
	EID   int32  `json:"eid"`
	Name  string `json:"name"`
	Dim   int    `json:"dim"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	Z     int    `json:"z"`
	State uint32 `json:"state"`
}

func (*BlockBreakEvent) EventName() string { return "block_break" }

// BlockPlaceEvent fires before a block placement commits. State is the
// proposed block state and is mutable (swap what actually gets placed).
// Fires once per placement action: multi-cell placements (doors, beds)
// place all their cells under one event.
type BlockPlaceEvent struct {
	Cancel
	EID   int32  `json:"eid"`
	Name  string `json:"name"`
	Dim   int    `json:"dim"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	Z     int    `json:"z"`
	State uint32 `json:"state"` // mutable
}

func (*BlockPlaceEvent) EventName() string { return "block_place" }

// EntityDamageByEntityEvent fires before melee damage between a player
// and a mob (either direction) is applied. Damage is mutable.
type EntityDamageByEntityEvent struct {
	Cancel
	AttackerEID      int32   `json:"attacker_eid"`
	VictimEID        int32   `json:"victim_eid"`
	AttackerIsPlayer bool    `json:"attacker_is_player"`
	VictimIsPlayer   bool    `json:"victim_is_player"`
	Damage           float64 `json:"damage"` // mutable
}

func (*EntityDamageByEntityEvent) EventName() string { return "entity_damage_by_entity" }

// MobSpawnEvent fires when a mob spawns, after it is registered (so a
// handler may fetch the handle with Server.Mob and adjust stats) but
// before it is announced. Cancelling removes it silently.
type MobSpawnEvent struct {
	Cancel
	EID      int32       `json:"eid"`
	Type     int         `json:"type"`
	TypeName string      `json:"type_name"`
	X        float64     `json:"x"`
	Y        float64     `json:"y"`
	Z        float64     `json:"z"`
	Dim      int         `json:"dim"`
	Reason   SpawnReason `json:"reason"`
}

func (*MobSpawnEvent) EventName() string { return "mob_spawn" }

// MobDeathEvent fires when a mob dies, before loot appears. Drops and XP
// are mutable. KillerEID is the last attacking entity or 0.
type MobDeathEvent struct {
	EID       int32       `json:"eid"`
	Type      int         `json:"type"`
	TypeName  string      `json:"type_name"`
	X         float64     `json:"x"`
	Y         float64     `json:"y"`
	Z         float64     `json:"z"`
	Dim       int         `json:"dim"`
	KillerEID int32       `json:"killer_eid"`
	Drops     []ItemStack `json:"drops"` // mutable
	XP        int         `json:"xp"`    // mutable
}

func (*MobDeathEvent) EventName() string { return "mob_death" }

// WeatherChangeEvent fires before the rain state flips. Raining is the
// proposed new state. Cancelling a natural flip re-rolls the weather
// timer.
type WeatherChangeEvent struct {
	Cancel
	Raining bool `json:"raining"`
}

func (*WeatherChangeEvent) EventName() string { return "weather_change" }

// ThunderChangeEvent fires before the thunder state flips.
type ThunderChangeEvent struct {
	Cancel
	Thundering bool `json:"thundering"`
}

func (*ThunderChangeEvent) EventName() string { return "thunder_change" }

// TimeSetEvent fires after the day time is set explicitly (command, bus,
// or plugin) — not on the natural once-per-tick advance.
type TimeSetEvent struct {
	Old uint64 `json:"old"`
	New uint64 `json:"new"`
}

func (*TimeSetEvent) EventName() string { return "time_set" }

// GameruleChangeEvent fires after a gamerule change is applied. Bool
// rules use On; difficulty reports its level in Num.
type GameruleChangeEvent struct {
	Rule string `json:"rule"`
	On   bool   `json:"on"`
	Num  int    `json:"num"`
}

func (*GameruleChangeEvent) EventName() string { return "gamerule_change" }
