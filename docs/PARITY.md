# Road to parity — how tachyne reaches Java-server behaviour

A from-scratch server "on par with the Java variants" is a multi-year effort if
taken literally. This doc is the strategy that makes it *tractable*: turn "all the
rules of Minecraft" into **generated data + a small set of engine subsystems**, and
grow it in **client-verifiable vertical slices**, the way the project already runs.

Read this alongside `CLAUDE.md` (status + conventions) and `docs/MECHANICS.md`
(vanilla-vs-ours tuning). This is the *plan*; those are the *state*.

> **Post-refactor note (2026-07-07):** file references to `internal/protocol`
> and `play.go` in the checklists below predate the domain-events refactor —
> the wire layer now lives in `tachyne-common` (`protocol/` + `render770/`)
> and chunk streaming is the attach protocol (`internal/attach` +
> gateway-side rendering). The subsystem statuses themselves are unaffected:
> parity is a property of the GAME engine, which is exactly what this repo
> still is.

## The core reframe

Mojang's server is mostly **data interpreted by a dozen engine loops**: registries,
tags, loot tables, recipes, worldgen JSON, block/entity definitions — all read by
block-behaviour dispatch, an entity/AI system, physics, worldgen, etc. Parity is
therefore *not* one giant codebase to hand-write; it is:

1. **Generate everything generable.** Every rule expressible as a table pulled from
   minecraft-data / mcmeta is a rule we don't hand-write or hand-maintain across
   versions (and it keeps the multi-version translation seam viable). This is the
   established `scripts/gen_*.py -> *_gen.go` pattern — widen it.
2. **Hand-write the ~12 engine loops** that interpret that data.
3. **Land each subsystem as a vertical slice** a real client can see working — not
   "architecturally complete." Every hard bug so far was caught by a real client;
   keep that loop.

## Parity target: behavioural, not bug-for-bug

Aim for **behavioural parity** — a player can't tell the difference in normal play.
Do **not** chase *identical* internals (same seed→same worldgen output, same
redstone sub-tick quirks): that often needs vanilla internals we can't legally
copy (ViaVersion/Mojang code is read for *facts*, never copied — see CLAUDE.md), and
the effort is disproportionate to the payoff. Where we deliberately diverge, record
it in `docs/MECHANICS.md`.

**Prioritise by what players and (goal #4) LLM NPCs actually touch.** The
differentiator doesn't need full parity — it needs a believable-enough world. Let
that steer the order, not a completionist checklist.

## Subsystems (dependency order)

Legend: ✅ built · 🟡 partial · ⬜ not started

### Foundation
- ✅ **Wire protocol + framing + translation seam** (`internal/protocol`, 770 core,
  chained translation to 776).
- ✅ **Registries / tags / persistence** (`registries_gen.go`, world edit overlay,
  FileStore). 🟡 tag *contents* for 26.x still empty — see `tachyne-26x-tags-plan`.
- ✅ **Chunk streaming + sky/block lighting** (`play.go`, `world/light.go`).
- ✅ **Central 20-TPS tick loop**, single-writer world (`hub.go`).

### Simulation core — where "the rules" live
- 🟡 **Block-behaviour dispatch** — THE big one. Give each block typed hooks:
  `onPlace / onNeighborUpdate / randomTick / onEntityCollide / getCollisionShape /
  onBreak`. Redstone, farming/growth, fluids, fire spread, gravity, pistons, doors,
  and collision shapes (the fence rule belongs here long-term) all become behaviours
  keyed off block state. Today this is scattered: `sim.go` (falling/fluids), `grow.go`,
  `interaction.go` orientation, `worldgen.IsTallCollision`. **Next architectural
  step: a real block-behaviour registry** so these stop being special-cases.
- 🟡 **Entity system** — AI goal selector, **pathfinding (A\* over a walkability
  graph — the correct long-term home for the fence rule)**, attributes, mob effects,
  spawn rules, despawning. Today: wander/herd/flee + **hostile hunt/attack with
  night-spawn + daylight burn** (`mob.go`, `behavior.go`, `hostile.go`); steering is
  still straight-line, not true pathfinding.
- ⬜ **Collision / movement physics** — real per-entity AABB step; eventually
  server-authoritative *player* movement (needed for anti-cheat + honest survival).
- 🟡 **Combat / damage / health / hunger / status effects** — `combat.go`,
  `survival.go`: health, hunger/saturation/exhaustion, regen, fall/void/**drown/lava/
  cactus** damage, death + **inventory drop-on-death**, respawn, eating, mob combat +
  loot, **hostile mobs (zombies) that hunt + bite players, night-spawn, and burn at
  dawn** (`hostile.go`) all done. Remaining: status effects, armor reduction, XP,
  fire/suffocation, player knockback, mob pathfinding.
- 🟡 **Inventory / crafting / recipes / loot / containers** — `inventory.go`,
  `loot_gen.go`, `crafting.go`, `recipebook.go`, `furnace.go`: server-side
  container clicks, generated recipe tables (1.5k), 2x2 + crafting-table 3x3,
  recipe book with click-to-fill, weapon damage, **furnace smelting (generated
  recipes + fuels, lit state, progress bars)**, **ore veins in worldgen**.
  Remaining: chest UIs, durability, furnace persistence, XP.
- ⬜ **Worldgen parity** — structures, exact biome placement, features, carvers,
  ore distribution. Hardest to match *exactly*; behavioural parity (looks/plays right)
  is the goal, not seed-identical output.

### Content layers (data-heavy, low-risk once the engines exist)
- ⬜ Full mob roster + per-mob AI, items/tools/durability, enchantments (needs Update
  Tags — partly unblocked now), potions/brewing, villagers/trading, the Nether/End
  dimensions, weather, mobs' drops/XP.

### Cross-cutting
- ⬜ **Online-mode auth + encryption** (offline today; compression done).
- ✅ **Multi-version translation** — the answer to "support new clients," not
  re-targeting the core.
- ⬜ **Bedrock frontend** via gophertunnel (goal #2).
- 🟡 **Extensibility** — message bus (`bus.go`) built; behaviour packs pending.

## Working method (keep doing this)

- **Vertical slices, client-verified.** Each subsystem lands as "you can see it in a
  real client," per the testing loop in `CLAUDE.md`.
- **Generate, don't hand-maintain.** Prefer a `gen_*.py` table over hand-written
  rules whenever minecraft-data/mcmeta carries the fact.
- **Conformance tests as the definition of "done."** Capture each behaviour as a Go
  test (e.g. `TestMobPennedByFence`) so parity is *measured*, not vibes. This suite
  becomes the running scorecard.
- **Respect the module boundaries** (`worldgen`/`world` stay wire-agnostic; only
  `server` touches the wire) — it's what keeps dual-edition + translation viable.

## Suggested next few slices (subject to the user's priorities)

1. **Survival mechanics** — mostly done (health, hunger, fall/void/drown/lava/cactus
   damage, death-drops, eating). Remaining depth: status effects, armor, XP, fire.
2. **Block-behaviour registry** — refactor the scattered block special-cases
   (sim/grow/collision) behind one dispatch, so future rules are additive.
3. **Entity A\* pathfinding** — replace the toy wander; the fence rule then falls out
   of the walkability graph instead of a movement-gate clause.
4. **26.x real tag contents** — see `tachyne-26x-tags-plan`; restores client-side
   mining/fire/food for translated clients.
5. **Recipes + container UIs** — crafting table, chest, furnace.

None of these is the whole ocean; each is a slice you can float and verify.
