package plugin

// Handles are lightweight, ID-keyed views over live game state. They
// re-resolve the underlying entity on every call, so holding a handle
// past the entity's lifetime is safe: methods become no-ops and Valid()
// reports false. All handle methods are tick-goroutine-only.

// ItemStack is an item id + count pair. Item ids are canonical registry
// ids; resolve names with Server.ItemByName — never hardcode numbers.
type ItemStack struct {
	Item  int32
	Count int
}

// SpawnOpts optionally overrides a plugin-spawned mob's behavior and
// stats. Zero values mean "species default".
type SpawnOpts struct {
	Behavior  string
	MaxHealth int
	Speed     float64
	Damage    float64
}

// Server is the top-level game facade.
type Server interface {
	// BroadcastMessage sends a chat line to every player.
	BroadcastMessage(text string)

	Player(name string) (Player, bool)
	Players() []Player
	Mob(eid int32) (Mob, bool)
	Mobs() []Mob

	// World returns the dimension view: 0 overworld, 1 nether, 2 end.
	World(dim int) World

	// SpawnMob spawns a mob of the given entity type. Returns false if
	// the type is unknown or a MobSpawnEvent handler cancelled it.
	SpawnMob(dim, etype int, x, y, z float64, opts *SpawnOpts) (Mob, bool)

	// SetWeather sets "clear", "rain", or "thunder" for durationTicks;
	// duration < 0 rolls a vanilla-length duration.
	SetWeather(kind string, durationTicks int)
	Weather() (raining, thundering bool)

	SetTime(ticks uint64)
	Time() uint64 // current day time
	Tick() uint64 // world age

	// SetGamerule sets one of the boolean gamerules (keepInventory,
	// doDaylightCycle, doMobSpawning, mobGriefing, doWeatherCycle).
	SetGamerule(rule string, on bool) error
	SetDifficulty(level int)

	IsOp(name string) bool

	EntityTypeByName(name string) (int, bool)
	ItemByName(name string) (int32, bool)
}

// World is a dimension-bound block view.
type World interface {
	Block(x, y, z int) uint32
	// SetBlock applies a block state, broadcasts it, and schedules
	// neighbor simulation, exactly like a player edit.
	SetBlock(x, y, z int, state uint32)
	SurfaceY(x, z int) float64
	BiomeAt(x, z int) string
}

// Player is a handle over a connected player.
type Player interface {
	Valid() bool
	Name() string
	EID() int32
	Pos() (x, y, z float64, dim int)
	// SendMessage sends a private chat line to this player only.
	SendMessage(text string)
	Gamemode() int
	Health() float32
	SetHealth(v float32)
	Give(item int32, count int)
	Teleport(x, y, z float64)
	IsOp() bool
}

// Mob is a handle over a living non-player entity.
type Mob interface {
	Valid() bool
	EID() int32
	Type() int
	TypeName() string
	Pos() (x, y, z float64, dim int)

	Health() int
	SetHealth(v int)
	MaxHealth() int
	// SetMaxHealth raises or lowers the health cap; heal also sets
	// current health to the new cap.
	SetMaxHealth(v int, heal bool)
	Speed() float64
	SetSpeed(v float64)
	MeleeDamage() float64
	SetMeleeDamage(v float64)

	// SetBehavior swaps the mob's AI behavior by registry name
	// (idle/wander/herd/hostile/...). Stat overrides survive the swap.
	SetBehavior(name string) bool

	// Remove despawns silently (no animation, no loot).
	Remove()
	// Kill runs the normal death path (animation, loot, XP).
	Kill()
}
