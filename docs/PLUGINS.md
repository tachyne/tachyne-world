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

import "tachyne/plugin"

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
import _ "tachyne/plugins/myplugin"
```

and rebuild. For now plugins live in this repo under `plugins/` (the module
path isn't fetchable externally); a builder tool that assembles a server
binary from external plugin modules is planned.

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
event struct itself as the JSON payload — same names, same fields as the
in-process API. Cancelled events are not published (the action didn't
happen). A few engine events without a catalog equivalent yet publish ad-hoc
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
