# Plugins

tachyne has an in-process plugin API: typed game events with Bukkit-style
priorities and cancellation, mutation handles over the live world, a tick
scheduler, chat commands, and per-plugin config + storage. Plugins are
ordinary Go packages **compiled into the server binary** — the model Go
servers converged on (Caddy, CoreDNS, Dragonfly): no `.so` loading, no
embedded interpreter, full type safety, zero dispatch overhead when idle.

A second, out-of-process tier ships alongside it — the NATS bus (`-nats`):
external processes in any language observe events and send commands. The bus
is observe + async-mutate only; anything that needs to **veto or rewrite** an
action inside the tick (protection, combat tuning) belongs in-process. See
[The bus (out-of-process plugins)](#the-bus-out-of-process-plugins) below.

## Writing a plugin

```go
package myplugin

import "github.com/tachyne/tachyne-world/plugin"

func init() { plugin.Register(&MyPlugin{}) }

type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "myplugin" }

func (p *MyPlugin) Enable(ctx plugin.Context) error {
    srv := ctx.Server()

    // Listen: cancel block breaks below y=40 by non-ops.
    plugin.On(ctx.Events(), plugin.Normal, true, func(e *plugin.BlockBreakEvent) {
        if e.Y < 40 && !srv.IsOp(e.Name) {
            e.SetCancelled(true)
        }
    })

    // Command: /storm starts a thunderstorm.
    return ctx.RegisterCommand(plugin.Command{
        Name: "storm", OpOnly: true, Help: "start a thunderstorm",
        Run: func(c plugin.CommandContext) { c.Server().SetWeather("thunder", -1) },
    })
}

func (p *MyPlugin) Disable() {}
```

Select it into the binary with a blank import in `cmd/server/plugins.go`:

```go
import _ "github.com/tachyne/tachyne-world/plugins/myplugin"
```

and rebuild — that's the whole story for in-repo plugins.

**External plugins** are their own Go modules that import
`github.com/tachyne/tachyne-world/plugin` and register from `init()`.
Assemble a server binary containing them with **tachyne-build**
(the xcaddy model):

```bash
go install github.com/tachyne/tachyne-world/cmd/tachyne-build@latest

tachyne-build --with github.com/you/yourplugin            # fetch latest
tachyne-build --with github.com/you/yourplugin@v1.2.0 \
              --with github.com/other/plugin=../checkout \
              --world <engine version> -o my-world
```

`--with module[@version][=local-dir]` is repeatable; `=local-dir` points at
a checkout for plugins in development, and `--world =path` does the same
for the engine itself. The output binary is the engine plus exactly the
plugins you listed (the in-repo example plugin ships only in the stock
`cmd/server` binary). Requires the Go toolchain.

One go.mod rule for plugin authors: your module must `require` a real
engine version — run `go get github.com/tachyne/tachyne-world@latest` in
your plugin directory. `replace` directives in your own go.mod don't apply
inside the build workspace (that's how Go modules work); for the local dev
loop use `--with yourmodule=.` instead.

Per-plugin files live under `-plugindir` (default `plugins/`, next to
`settings.json`):

- `plugins/<name>/config.json` — operator-authored config, unmarshalled by
  `ctx.Config(&v)`. Missing file = your defaults. The reserved key
  `"enabled": false` skips the plugin entirely.
- `plugins/<name>/data.json` — the `ctx.Store()` KV store, flushed with world
  state (every 30 s and on graceful shutdown).

## Threading contract

The engine runs all game state on **one tick goroutine** (20 TPS). Event
handlers, command handlers, and scheduled tasks are always invoked there, so
they may use every `Context` API directly — and must **never block** (no
network calls, no channel waits, no sleeps; a stalled handler stalls the whole
world). From your own goroutines, the only legal API is `Scheduler`;
`Scheduler().NextTick(fn)` is the way back onto the tick goroutine.

Handles (`Player`, `Mob`) are ID-keyed views, re-resolved per call: holding
one past the entity's lifetime is safe (`Valid()` turns false, methods no-op).
Event structs carry scalars (EIDs, names, coordinates); resolve live handles
through `Server.Player` / `Server.Mob`.

## Events

Register with `plugin.On(dispatcher, priority, ignoreCancelled, fn)`; the
returned func unregisters. Priorities run **Lowest → Low → Normal → High →
Highest → Monitor**: later handlers get the final say over mutable fields and
cancellation; **Monitor observes only** (never mutate or cancel there). A
cancelled event still reaches later handlers (which may un-cancel);
`ignoreCancelled` handlers are skipped while the event is cancelled.

| Event | Cancellable | Mutable fields | Fires |
|---|---|---|---|
| `PlayerJoinEvent` | – | – | after a player fully joins |
| `PlayerQuitEvent` | – | – | after a player leaves |
| `PlayerChatEvent` | ✔ | `Message` | before a chat line broadcasts |
| `PlayerCommandEvent` | ✔ | `Line` | before a command executes (rewrites re-dispatch) |
| `PlayerMoveEvent` | – | – | per movement packet (hot; observe-only) |
| `BlockBreakEvent` | ✔ | – | before a dig removes a block (cancel reverts the client) |
| `BlockPlaceEvent` | ✔ | `State` | before a placement commits; once per action (doors/beds place both halves under it) |
| `EntityDamageByEntityEvent` | ✔ | `Damage` | before melee damage, player↔mob either direction |
| `MobSpawnEvent` | ✔ | – | on spawn, with a `Reason`; the mob is registered before the event so handlers can fetch its handle and adjust stats |
| `MobDeathEvent` | – | `Drops`, `XP` | on death, before loot appears; `KillerEID` = last attacker |
| `WeatherChangeEvent` | ✔ | – | before the rain state flips (a cancelled natural flip re-rolls the timer) |
| `ThunderChangeEvent` | ✔ | – | before the thunder state flips |
| `TimeSetEvent` | – | – | after an explicit time set (command/bus/plugin) |
| `GameruleChangeEvent` | – | – | after a gamerule or difficulty change |

Deferred to a later milestone: `PlayerInteractEvent`, a generic
`EntityDamageEvent` with causes (fall/fire/drown/projectile), cancellable
movement, and explosion events.

## Mutations (the `Server` facade)

Weather/time/rules: `SetWeather("clear"|"rain"|"thunder", ticks)` (negative
duration rolls a natural-length one), `SetTime`, `SetGamerule` (the five bool
rules), `SetDifficulty`. World: `World(dim).SetBlock/Block/SurfaceY/BiomeAt` —
`SetBlock` broadcasts and schedules simulation exactly like a player edit.
Entities: `SpawnMob` with optional `SpawnOpts{Behavior, MaxHealth, Speed,
Damage}`; `Mob` handles expose the same **per-mob attribute overlay**
(`SetMaxHealth`, `SetSpeed`, `SetMeleeDamage`) plus `SetBehavior`, `Remove`
(silent) and `Kill` (loot + animation). Players: `SendMessage`, `Give`,
`Teleport`, `SetHealth`, `Gamemode`, `IsOp`. Resolve ids by name only —
`ItemByName`, `EntityTypeByName` — never hardcode numeric ids.

## Commands

`ctx.RegisterCommand(plugin.Command{Name, Usage, Help, Aliases, OpOnly, Run})`.
Names must not collide with built-ins or other plugins. Commands appear in
`/help` and in client tab-completion; `Run` executes on the tick goroutine
with a `CommandContext` (`Sender()`, `Args()`, `Reply()`, `Server()`).
`PlayerCommandEvent` fires before *any* command (built-in or plugin) and may
cancel or rewrite the line.

## Scheduler

`NextTick(fn)`, `After(ticks, fn)`, `Every(ticks, fn)` → `Task.Cancel()`.
Tasks run at the top of the tick, in scheduling order, and see the world as
the previous tick left it. The scheduler is the one API safe to call from any
goroutine.

## The bus (out-of-process plugins)

With `-nats nats://…` the engine is a NATS client; any process on the broker
is a plugin. Three surfaces:

**Events** — every event in the table above publishes on
`mc.event.<name>` (`mc.event.block_break`, `mc.event.mob_death`, …) with the
event struct itself as the JSON payload. All wire fields are
lowercase/snake_case, e.g. on `mc.event.mob_spawn`:

```json
{"eid":15,"type":150,"type_name":"zombie","x":10.5,"y":61,"z":10.5,"dim":0,"reason":"bus"}
```

Cancelled events are not published (the action didn't happen). A few engine events without a catalog equivalent yet publish ad-hoc
payloads on the same namespace (`block_change`, `item_drop`, `explosion`,
`lightning`, `enchant`, `npc_say`, and the raw `chat` line); they'll migrate
into the catalog as it grows.

**Commands** — publish (or request) `mc.cmd.<name>` with JSON args; with
request-reply you get `{"ok":true[,"data":…]}` or `{"ok":false,"error":…}`:

| Command | Args | Effect |
|---|---|---|
| `say` | `{text}` | broadcast a chat line |
| `settime` | `{time}` | set the day clock |
| `weather` | `{kind: clear\|rain\|thunder, duration?}` | set the weather (duration in ticks; omit for a natural-length spell) |
| `gamerule` | `{rule, on}` / `{rule: "difficulty", num}` | set a gamerule / difficulty |
| `give` | `{player, item, count?}` | give items (item by **name**, e.g. `"bow"`) |
| `teleport` | `{player, x, y, z}` | move a player |
| `setblock` | `{x, y, z, state}` | set a block (broadcast + simulated) |
| `spawn` | `{type, x, z, y?, dim?, behavior?, max_health?, speed?, damage?}` | spawn by entity name with stat overrides; replies `{data:{eid}}` |
| `mobset` | `{eid, max_health?, health?, speed?, damage?, behavior?}` | mutate a live mob |
| `behavior` | `{eid, behavior}` | swap a mob's AI behavior |

**Queries** (request-reply): `players` (name/eid/position/gamemode/health),
`mobs` (`{dim?}` filter), `block` (`{x,y,z,dim?}` → state), `world` (age,
day time, weather, difficulty, counts).

### Daemon plugins

A daemon plugin is a standalone program that attaches to the bus — no
compiling into the core, hot add/remove, crash-isolated, any language. In Go
the `busplugin` package is the kit:

```go
c, _ := busplugin.ConnectEnv() // NATS_URL
busplugin.On(c, "player_join", func(e plugin.PlayerJoinEvent) {
    c.Command("say", map[string]any{"text": "Welcome, " + e.Name + "!"})
})
```

Distribution is the Go model itself — the module path is the repository.
**tachyne-daemon** pulls a daemon's source, builds it locally, boots it as
its own process with `NATS_URL` injected, and supervises it (restart with
backoff, prefixed logs):

```bash
go install github.com/tachyne/tachyne-world/cmd/tachyne-daemon@latest

tachyne-daemon run github.com/tachyne/tachyne-world/daemons/webmap
tachyne-daemon run github.com/you/yourdaemon@v1.0.0 -- --your-flags
tachyne-daemon -config daemons.json          # the managed set
```

In `-config` mode the manager is **live**: it listens on the bus for
`mc.daemon.install / uninstall / restart / list` (request-reply), so
daemons hot-install and hot-remove while everything runs, and the set
persists back to `daemons.json` across manager restarts. `restart` rebuilds
first, so an unpinned daemon picks up its latest code — that's the
hot-reload path. In game, ops drive everything with the one **`/plugin`**
command (op-only):

```
/plugin list                     compiled-in set + fleet daemon inventory,
                                 OUTDATED flags per shard
/plugin search <query>           search the registries
/plugin info <name>              a plugin's registry card
/plugin rate <name> <1-5>        rate it (per shard host)
/plugin install <name|module>    install fleet-wide (registry names resolve)
/plugin uninstall <name>
/plugin restart <name>
/plugin upgrade <name>           progressive shard-by-shard rollout
```

The first daemon in the tree is **`daemons/webmap`**: a live web map
(players in real time from movement events, mobs via the query, weather/
time in the corner) at `:8100`. It's the template — state primed by
queries, kept fresh by events, zero engine involvement.

Pick the tier by what the plugin does: daemons for everything that
observes and commands (maps, bridges, economies, bots); compiled-in
plugins only for tick-veto hooks (protection, combat tuning).

### The plugin registry

Discovery lives in the [tachyne registry](https://github.com/tachyne/tachyne-registry):
a REST index over git-hosted plugins (search, latest version, freshness,
ratings, install counts) that **indexes, never hosts** — listing a plugin
means putting a `tachyne-plugin.json` manifest beside your main package and
submitting the module path. Point managers at one or more registries
(`-registry` / `TACHYNE_REGISTRY`, comma-separated, merged like package
sources — running your own is one binary), and:

- `/plugin search <query>` finds plugins in game;
- `/plugin install <name>` resolves a registry name to its module;
- installs ping the registry's counters;
- `list` compares each daemon's built version against the registry's
  latest and flags stale shards.

### Fleets

Every shard's manager shares the bus, so `/plugin` speaks to the whole
fleet: managers identify themselves (`-name`, default `POD_NAME`), plain
`mc.daemon.<op>` is a fleet broadcast, `mc.daemon.at.<manager>.<op>`
targets one shard, and `list` scatter-gathers the full inventory with
out-of-date flags per shard. **`/plugin upgrade <name>` is the progressive
rollout**: one shard at a time — rebuild at latest, boot, verify the daemon
reports healthy — and any failure stops the roll with the remaining shards
untouched.

Bus mutations run through the same code paths as plugin facades, so
in-process plugin events fire for them too — a bus `spawn` is observed (and
cancellable) by an in-process `MobSpawnEvent` handler, and shows up on
`mc.event.mob_spawn` with reason `bus`.

## The example plugin

`plugins/example` exercises the whole surface — config-driven join greeting +
persistent join counter, depth protection (cancel break/place below a
configured Y for non-ops), a repeating announcement, and `/storm`, `/sun`,
`/buff <damage> [radius]` commands. It is compiled into the default binary
but **inert until configured**; use it as the template.
