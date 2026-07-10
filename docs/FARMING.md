# Crop-loop pass — design & scope

Status: **PLANNED (not built).** Documented 2026-07-05 so it's ready to execute.
No `GenVersion` bump anywhere in here — tilling/planting are player edits and
growth is runtime, so worldgen chunk output is untouched.

## Why

Farming today is a **growth ticker with no player loop**: crops that already
exist (village wheat farms) random-tick to maturity, but a player can't till
soil, plant seeds, bonemeal, or harvest correctly. The one hand-plantable crop is
nether wart. Animal husbandry is the only complete pillar. This pass adds the
hands-on loop — **till → plant → (bonemeal) → grow → harvest → replant** — so
farming becomes playable. See the audit in the session notes for the full gap map.

Current authoritative facts (verified):
- Crop growth engine: `grow.go` (`runRandomTicks` → `randomTickBlock` →
  `tickCrop`), overworld-only, gated on `skyLit` (light proxy) — no moisture rule.
- Crop block-state ranges (`grow.go` `cropRanges`): wheat `4342-4349`, carrots
  `9380-9387`, potatoes `9388-9395`, beetroots `13532-13535`. Age 0 = just
  planted, top of range = mature.
- Farmland: `4350` (moisture 0, dry) … `4357` (moisture 7, moist). Consts
  `farmlandMin=4350` in `grow.go` and `Farmland=4357` in `worldgen/village.go` are
  currently **defined but unused** in the server-side logic.
- Tillable sources (`worldgen/blocks.go`): `GrassBlock=9`, `Dirt=10`,
  `CoarseDirt=11` (→ dirt, not farmland), `Podzol=13`, `DirtPath=13536`.
- Nether wart plant handler (the pattern to copy) is in `interaction.go` — a
  `p.heldItem() == itemNetherWart` branch: validate the block below, `putBlock`,
  `evConsume` in survival, else `sendBlockChange` to reject client-side.
- Tool durability exists: `hub.applyToolWear(t, slot, n)` (`durability.go`).
- Fall landing hook for trampling: `onFallAndExhaust` + `peakY` in `hub.go`.
- Item IDs (by name via `itemByName`): `wheat_seeds 893`, `wheat 894`,
  `carrot 1159`, `potato 1160`, `poisonous_potato 1162`, `beetroot 1217`,
  `beetroot_seeds 1218`, `bone_meal 1020`, `melon_seeds 1047`, `pumpkin_seeds 1046`,
  `nether_wart 1057`, `bucket 950`, `milk_bucket 956`.

---

## Phase 1 — the core crop loop (the actual deliverable)

### 1.1 Hoe tilling → farmland
- **Where:** `interaction.go`, in the right-click-block path (`tryUseBlock` /
  `handleUse`), a new branch when the held item is a hoe (IDs 862/867/872/877/882/
  887 — the same set `combat.go` uses for attack speed; add an `isHoe(id)` helper).
- **Rule:** clicking the **top face** of `GrassBlock`/`Dirt`/`Podzol`/`CoarseDirt`/
  `DirtPath` with air or a replaceable block directly above → set the block to
  `farmlandMin` (4350, dry). `CoarseDirt` → `Dirt` instead (vanilla). Play
  `item.hoe.till` sound. In survival, `applyToolWear(t, heldSlot, 1)`.
- **Reject** (send a `sendBlockChange` resync) if the above cell is occupied or the
  face isn't the top, so the client doesn't desync.

### 1.2 Seed / produce planting → crop age 0
- **Where:** `interaction.go`, generalise the nether-wart branch into a
  `plantOnFarmland` handler. Gate: clicked block is farmland (`4350..4357`) **and**
  the cell above is air.
- **Mapping (item → crop age-0 state):**
  | held item | crop block (age 0) |
  |---|---|
  | `wheat_seeds` | `4342` |
  | `carrot` | `9380` |
  | `potato` | `9388` |
  | `beetroot_seeds` | `13532` |
  | `melon_seeds` | `melon_stem` age 0 *(Phase 2 — stems)* |
  | `pumpkin_seeds` | `pumpkin_stem` age 0 *(Phase 2)* |
  - Keep nether wart on soul sand as its own existing branch.
- `putBlock` the crop above the farmland; `evConsume` one in survival; else
  `sendBlockChange` to reject.
- **Support break:** when farmland (or the block under any crop) is removed, the
  crop must pop off as a drop. Hook into the existing neighbour-update path
  (`onBlock` / `scheduleAround`) — add crops to the "needs solid/ farmland below"
  check alongside the small-plant support rule already in `simblocks.go`
  (`NeedsGroundSupport`).

### 1.3 Maturity-based harvest drops
- **Where:** `loot.go` `rollDrops` — add a crop switch BEFORE the generic
  `generatedDrop` fallback (which wrongly yields a single item for the whole range).
- **Vanilla drop rules to reproduce** (`cropDrops(state) []stack`):
  | crop | immature | mature |
  |---|---|---|
  | wheat | 1 `wheat_seeds` | 1 `wheat` + 0–3 `wheat_seeds` |
  | carrots | 1 `carrot` | 2–5 `carrot` |
  | potatoes | 1 `potato` | 2–5 `potato` (+2% `poisonous_potato`) |
  | beetroots | 1 `beetroot_seeds` | 1 `beetroot` + 1–4 `beetroot_seeds` |
  | nether wart | 1 `nether_wart` | 2–4 `nether_wart` |
  - "Mature" = top of the state range (e.g. wheat `4349`). Use the hub RNG.
  - No Fortune interaction required for v1 (note it as a stretch — vanilla adds
    a binomial bonus with Fortune).

### 1.4 Bonemeal
- **Where:** `interaction.go` right-click branch when held item is `bone_meal`.
- **Rule:** on a crop below maturity → advance the age by a random **2–5 stages**
  (clamp to the range max). On a sapling → call `growTree` immediately. Consume one
  in survival; spawn the happy-villager/green particle (reuse the existing particle
  helper) and `block.bone_meal.use`-ish feedback. Broadcast the new block state.
- **Stretch (defer):** bonemeal on `grass_block` scatters grass/flowers.

### 1.5 Farmland hydration, drying & trampling
- **Hydration + drying:** add a `farmland` case to `randomTickBlock` (`grow.go`):
  - Water within a 4-block horizontal radius (same Y or one above), or rain over an
    open-sky column → set moisture to 7 (`4357`).
  - No water → step moisture down by 1 per tick; at moisture 0 with **no crop
    above**, revert to `Dirt`. (Crop present ⇒ never reverts, matches vanilla.)
- **Trampling:** in `onFallAndExhaust` (`hub.go`, where `peakY`/fall distance is
  known), if the player lands on farmland after a fall of ≥ ~1 block → set it to
  `Dirt` and pop the crop above as a drop. Broadcast the change.
- **Growth still gated on `skyLit` for v1.** As a stretch, fold moisture + the
  vanilla neighbour-spacing bonus into the per-tick growth *chance* (dry farmland
  and crowded rows grow slower) — note it, don't block Phase 1 on it.

### Phase 1 tests (`grow_test.go` / `interaction_test.go`)
- Hoe on grass/dirt → farmland; hoe on coarse dirt → dirt; rejects when blocked.
- Plant each seed on farmland → age-0 crop; rejects off-farmland / when occupied;
  consumes in survival, not creative.
- `cropDrops`: immature vs mature yields for all four crops + nether wart (seed the
  RNG; assert ranges/among-set membership).
- Bonemeal advances a crop 2–5 stages and grows a sapling; consumed in survival.
- Farmland hydrates next to water, dries + reverts to dirt when unplanted, keeps a
  planted crop; trampling on landing reverts to dirt and pops the crop.
- Removing the farmland/ground under a crop pops it as a drop.

---

## Phase 2 — special plants (optional, after the core loop)

- **Melon/pumpkin stems → fruit.** Add stem states to the growth engine: stem ages
  0–7 (`tickCrop`-style), then at max age spawns a `melon`/`pumpkin` fruit block on a
  random adjacent air cell over dirt/grass/farmland, and flips the stem to its
  `attached_*_stem` facing that fruit. Fruit re-grows after harvest. (Needs the
  stem + attached-stem state IDs — look them up in the block-state table.)
- **Sapling species fix (small, high-value):** `growTree` always grows oak; make
  spruce/birch (and ideally jungle/acacia/cherry/dark-oak) grow their own species,
  and add the missing saplings to `saplingRanges`. Reuses `worldgen` tree features.
- **Sugar cane / cactus placement validation:** require adjacent water (cane) and
  sand + no solid neighbours (cactus); cactus breaks touching blocks. Growth
  already works via `tickStackPlant`.
- **Cocoa** on jungle-log sides (facing), **sweet-berry bushes** on grass/dirt
  (plant + 4-stage growth + berry harvest), **bamboo/kelp** vertical growth.

---

## Phase 3 — village / villager farming (optional, ties into the schedule)

- **Farmer-villager harvest & replant AI.** During the villager `vsWork` segment
  (see `villager_ai.go`), a farmer detects a mature crop within its work radius,
  paths to it, harvests (drop or pick up into an inventory), and replants age 0 from
  its "seed stock". Closes the loop where harvested village plots currently stay
  empty forever. Depends on Phase 1's plant/harvest primitives.
- **Composter.** Right-click with compostable items → fill (per-item % chance to
  raise the level, level 7 → `bone_meal` output on next click). Farmer job block.
- **Cow milking (cheap, can bundle into Phase 1):** right-click a cow holding a
  `bucket` → swap to `milk_bucket`. Pure item swap in the `evInteractMob` path
  (`hub.go`); no new state.

---

## Out of scope (leave for later, note only)
- Fortune/Silk-Touch interactions with crop drops.
- Bees / pollination / honey (bee is a mob only today).
- Villager composting economy, crop-based trades beyond the existing wheat→emerald.
- Big-tree (2×2 spruce/jungle) growth.

## Suggested execution order
1. Phase 1 as one commit (the playable loop) — the 80/20.
2. Sapling-species fix + cow milking as a small follow-up (cheap wins).
3. Phase 2 stems + validation.
4. Phase 3 villager farming AI + composter.
