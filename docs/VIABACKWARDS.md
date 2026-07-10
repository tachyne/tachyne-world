# ViaBackwards — backward-translation mechanism reference

A comprehensive sweep of how **ViaBackwards** (the Via project that lets OLDER
Minecraft clients connect to NEWER servers) translates packets **down**, and how
each mechanism maps onto tachyne's own translation layer
(`tachyne-common/protocol` + `render770`).

Direction matters. Our engine emits **canonical 1.21.11 (proto 774)**; the
gateways render + translate to whatever the client speaks. `gw-java-770` serving
a 1.21.5–1.21.8 client is exactly the **ViaBackwards direction** (newer "server"
→ older client). `ViaVersion` proper is the opposite (newer client → older
server) and is not our case. All facts here are extracted mechanism (method
names, algorithms, data shapes) from the GPL ViaBackwards/ViaVersion source —
**facts, not code**; nothing is copied. Source: `ViaVersion/ViaBackwards` and
`ViaVersion/ViaVersion` `master`, plus the `ViaVersion/Mappings` repo.

> Why this doc exists: our translation layer is a **numeric delta-range shift**
> (`translationTables[reg][version]`). That is only the *crudest* of Via's three
> fallback layers, and for **content that does not exist downstream** (new mobs,
> new blocks) a pure shift produces a *wrong* id — a wrong mob (and a likely
> metadata-decode disconnect) or a wrong block. This sweep documents the full
> ViaBackwards design so we can adopt the parts we need deliberately.

---

## 0. The core doctrine

ViaBackwards **never invents a wire packet the old client cannot parse.** For
anything new it does exactly one of:

1. **Rewrite a field/NBT in place** (remap an id, downgrade a component dialect).
2. **Strip** a registry/tag/component entry the old client would choke on.
3. **Synthesize a surrogate** built entirely from packets the old client already
   understands (a chest/anvil GUI for a dialog, a fake tab-list player for a
   mannequin, a fabricated `UPDATE_ATTRIBUTES` for a scale).

Every stateful downgrade caches what it needs in a per-connection `StorableObject`
attached to the connection (or per-entity on the tracker).

The whole design rests on **one sentinel**: a mapping returns **`-1`** when a
newer id has no older equivalent, and the caller substitutes a **safe default**
(the "crux", see §4).

---

## 1. Entities (`api/rewriters/EntityRewriter*`, `api/entities/*`)

### Substitution is a *bundle*, not an id swap
`mapEntityTypeWithData(newType, oldType)` builds an `EntityReplacement`
`{ replacementId, nameComponent, baseMetadataLambda }`:
- rewrites the spawn packet's type id to `oldType`;
- **injects a display name** (`.tagName()`/`.jsonName()`/`.plainName()`) resolved
  from mapping data so a stand-in Ghast still floats the label "Happy Ghast";
- optionally injects **base metadata** (a lambda over a `WrappedEntityData`) so
  the stand-in physically resembles the original (size flags, slime size, …).

`mapEntityType(a,b)` is the plain id-only remap when the old client already has an
equivalent entity at a different index.

### Per-eid tracking, keyed on the UNMAPPED (server) type
Metadata is per-type (index N means different things on different mobs), so every
spawn packet populates the shared `EntityTracker` (`addEntity(eid, type)`) BEFORE
any metadata handler runs. Handlers look the type back up per eid; **untracked
entities are skipped defensively** rather than crashed. The tracker stores the
*unmapped* (newer) type; id-rewriting to the client id is a separate write, so the
tracker vocabulary never drifts.

### Metadata ("entity data") rewriting
A `filter().type(X).index(i).handler(…)` DSL. A handler can **remove** an entry
(`cancel()`/`removeData(i)`), **re-index** it (old client expects the flag
elsewhere), **change its value-type** (converting the payload), or **add** one.
A catch-all `registerEntityDataTypeHandler(...)` routes embedded item / blockstate
/ particle / component values through the respective sub-rewriters. New metadata
value-types the old client can't parse are **removed**; moved ones are re-indexed;
reshaped payloads (item/particle/component/blockstate) are transcoded. On the
*initial* batch only, the injected name/base-data is appended (never clobbering a
real custom name).

### Faking size via a synthetic attribute packet
`EntityScaleHelper.addBabyScale(type, factor, babyIndex)` watches the baby-flag
metadata index and, when it flips, **fabricates an `UPDATE_ATTRIBUTES`** carrying
`scale` (base value, zero modifiers), deduped via a per-eid `EntityScaleData` so
it isn't resent every tick. This is how a Happy-Ghast-as-Ghast is shrunk to ~1×1
(factor **0.2375**).

### Position storages
`EntityPositionHandler` accumulates relative-move deltas into an absolute position
(`RELATIVE_MOVE_FACTOR = 4096`) so a stream of one format can be re-emitted as the
other; `writeFacingAngles/Degrees` compute yaw/pitch to aim a substitute at a
point.

**tachyne mapping.** render770's `EntityView.Add` writes the type **raw**; the
entity-type remap happens in `remapSpawnEntityType` (tachyne-common). Our
`EntityView` is the analog of the tracker but it does **not** track type→eid.
Adopted so far (§7): the type-substitution bundle's *first* element (id swap). Not
yet adopted: name injection, base-metadata injection, metadata index dropping,
scale attributes — all of which need per-eid type tracking on the gateway (the
same `entTypes` map the creeper fix added to gw-776).

---

## 2. Items & data components (`api/rewriters/BackwardsItem*`, `Structured*`)

### The backup/restore round-trip contract (the spine)
Every lossy downgrade **stashes what it destroyed** into a private,
protocol-namespaced tag (`VB|<Protocol>|…`; inside the `custom_data` component for
1.20.5+ structured items, in root NBT for legacy) and the serverbound path
**restores it byte-for-byte**. This is what lets an item survive
client→server→client round-trips (creative grabs, inventory shuffles, anvil
renames) without corruption. Backups are idempotent and layered per protocol hop.

### New item with no old equivalent → `MappedItem`
`{ fallbackId, displayName (pre-styled non-italic white), optional customModelData }`
— the same substitution-bundle shape as entities. On a miss, plain id remap; on a
hit, save the original id into the private tag and inject the readable name so the
player still sees the item's true modern name.

### Component (1.20.5+) downgrade
The structured `DataComponent` map is walked and converted back to legacy NBT
(`display.Name`, `Enchantments`, `AttributeModifiers`, …). Components with no
legacy analogue are **dropped**; for steps that stay structured, `removeDataComponents(...)`
strips the ones the "from" version introduced. The original `custom_data` is
snapshotted and re-attached so it round-trips.

### The 1.21.5 hashed-item trap
1.21.5 serverbound container clicks echo **component hashes**, not full items; if
the proxy mutated an item downstream the hashes won't match and the click
corrupts. VB stores the original hashes in `custom_data[VV|original_hashes]` and
replays them serverbound.

### Enchantments
Enchants absent downstream are removed and re-expressed as **lore lines**, with a
**glint hack** (a dummy `{id:"",lvl:0}` legacy, or forced `glint_override=true`
structured) so the item still shimmers.

**tachyne mapping.** Items already route through `remapSetSlot`/`remapWindowItems`/
`remapEntityMeta`/`remapEquipment` via `RegItem`. We have **no** `MappedItem`
equivalent — a new 1.21.11 item shown to a 1.21.5 client range-shifts to a wrong
item (cosmetic, non-crashing; and old clients don't get new items from their own
creative menu anyway). The **hashed-item trap does not bite us**: the engine owns
inventory state server-side (the hub is the authority) rather than trusting client
hashes, so serverbound clicks are validated against hub state, not echoed hashes.

---

## 3. Blocks / chunks / particles / sounds

### The universal miss-fallback is id 0
`checkValidity(id, mappedId)` → when `mappedId == -1`, log (config-gated) and
**return 0**: unmapped block-state → **air**, particle → particle 0, id-sound →
sound 0. The "nearest equivalent" is **pre-resolved at mapping-generation time**
(hand-authored diffs), never computed at runtime.

### Chunks remap palette entries, not blocks
For each section, only the (small) set of distinct palette entries is remapped;
the packed index array is untouched. Bits-per-entry is recomputed from the
**target** `mappedSize()`. The chunk **container format itself** changes across
versions (typed heightmap array ⇄ named-NBT compound), so it's a structural
transcode, not just id swaps.

### Block entities: remap by type-id; `-1` = delete
A block entity whose type has no downstream id is **removed from the chunk list**;
survivors get per-type NBT fixups (sign text downgrade, `CustomName` component
dialect conversion). The flattening era (1.13→1.12.2) additionally **synthesizes**
block entities from block-state-id arithmetic (skull rotation, banner `15-color`
inversion, spawner entity-name rewrite).

### Particles substituted, never dropped; sounds are tri-state
Unmapped particle → particle 0, with per-particle **argument surgery** after the
id remap (remove high-index args first). **Named sounds** are tri-state: `null` =
keep (custom/resource-pack sounds pass through), `""` = **cancel the packet**
(removed downstream), value = replace. **Id-sounds** only ever substitute-to-0.

### Skip identity tables
`Mappings.isFullIdentity(...)` → the rewriter is not installed at all (matches our
"770→772 are pure ID remaps" note).

**tachyne mapping.** Chunk/block-update block-states route through `RegBlockState`
(`remapChunkBlocks`/`remapBlockUpdate`). **Gap:** for a NEW block absent in 1.21.5
we currently **range-shift to a wrong block** instead of degrading to air. This is
the block-side analog of the entity bug — cosmetic (no disconnect) but wrong.
Follow-up: give the block-state table a `-1`/air fallback for canonical states
above the target's max, plus a hand-authored "nearest" diff for the ones worth
approximating (copper family → a plausible 1.21.5 block). We do not send arbitrary
particles/sounds by id (they're names in the domain events), so the sound/particle
tri-state largely doesn't apply.

---

## 4. Mapping-data layer — the crux (`api/data/*`, `ViaVersion/Mappings`)

### "Closest older analogue" is DATA, not code
The judgement lives in **hand-authored per-registry diff files**
(`mapping-<from>to<to>.json`): string→string maps picking the closest older
analogue for anything absent downstream —
`"bush"→"short_grass"`, `"wildflowers"→"pink_petals["` (a partial value = "prefix
onto pink_petals, preserving trailing properties"), `"firefly"→"ash"`,
`"blue_egg"→"egg"`. These compile into the runtime tables; a stub-generator emits
empty entries and the optimizer **warns on missing keys** so nothing is silently
forgotten.

### Three flavors of "no analogue"
- int **`-1`** → a fixed safe default (**0** = air/first for block/item/particle,
  `1` reverse block/item, `0` sound);
- string **`""`** → a **sentinel** (`minecraft:intentionally_empty` for sounds —
  a valid-but-silent id the old client accepts instead of an unknown-key crash);
- a same-typed **approximate substitute** (the diff-file entries).

### Runtime `Mappings`
`getNewId(id)` → mapped id or **`-1`**. Impls: `IdentityMappings` (bounds-checked
passthrough), `IntArrayMappings` (the default; array read, oob → -1),
`Int2IntMapMappings` (sparse, defaultReturnValue -1), `BiMappings` (forward+reverse
sharing references so `inverse()` is O(1) — this is what makes reverse lookups
cheap), `FullMappings` (adds string↔id). On-disk the compact tables use four
encodings — DIRECT / SHIFTS (run-length; **this is what our range table is**) /
CHANGES (sparse ± identity fill) / IDENTITY — all decoding to the same
`index=oldId → newId, -1=none` slice.

### VB's relabel companion
`itemnames`/`itemdata` tables re-label a substituted item's display name;
`entitynames`/`enchantmentnames` supply the human names injected onto substituted
entities/enchants.

**tachyne mapping.** Our `translationTables` are the **SHIFTS** encoding, decoded
to `idRange{Min,Max,Delta}`. We have **only layer-1 done wrong**: an unmapped new
id range-shifts to a *neighbor* rather than to `-1`/air/a hand-picked analogue. The
fix shape is exactly Via's: (a) treat canonical ids above the target's max (or in a
"not present" set) as unmapped → safe default; (b) add a small hand-authored
substitution overlay for the ids worth approximating. The **entity guard in §7 is
precisely this overlay for the entity registry.**

---

## 5. Text / registries / tags / dialogs (`api/rewriters/text/*`, `Backwards*Registry*`)

### Translatable components → baked English literal
`translation-mappings.json` is `version → { translationKey → English text }`,
selected by the *server* version (reflected off the protocol class name). A
`translate` node's key is replaced with the literal text **in place** (still the
`translate` field), so the old client renders it verbatim. Downside VB accepts:
the fallback is always English (no localization). Keys the old client already
knows aren't in the table and pass through.

### Registry data
`BackwardsRegistryRewriter` sanitizes registry NBT (rewrites embedded sound names
via `updateSound`, substituting the `intentionally_empty` sentinel for unknown
sounds), **removes** whole registries the old client can't handle (`remove("dialog")`),
and can **capture-then-strip** (stash raw entries in per-user storage before
removing them from the packet).

### Tags — two-pass
Read-and-capture (stash `dialog` tag id-lists), `resetReader()`, then the generic
`TagRewriter` renumbers tag entry ids to the target version and drops tags that
reference removed registries. Tags matter because they drive client-side
validation (our own gotcha #3: `minecraft:enchantment` needs Update Tags first).

### Dialogs → chest/anvil surrogate (the flagship synthesis)
The 1.21.6 server-driven dialog UI is rebuilt as a **fake 27-slot chest** (and an
**anvil** for text entry) from `OPEN_SCREEN`/`CONTAINER_SET_CONTENT` + serverbound
`CONTAINER_CLICK`/`RENAME_ITEM` — all packets 1.21.5 already speaks. Widgets map to
items (boolean→dye, number-range→clock, option→bookshelf, text→writable_book→anvil,
button→oak_button); serverbound clicks are intercepted, cancelled, and translated
into the real new-version action (`run_command`→a synthesized `CHAT_COMMAND`).
Reserved high-byte container ids avoid collisions; a `Phase` state machine
(DIALOG_VIEW / ANVIL_VIEW / WAITING_FOR_RESPONSE) animates a countdown.

### Dimension scale & mannequin (1.21.9)
`DimensionScaleStorage` caches each dimension's `coordinate_scale` from the
registry so the world-border-center packet can be **pre-multiplied** for the old
client (1.21.9 moved that scaling client-side). `MannequinData` re-skins the new
mannequin entity as a **fake player** (player-info add + team nameplate + player
spawn with the mannequin's UUID), dropping the unparseable profile metadata and
rerouting the skin through player-info properties.

**tachyne mapping.** Our config/registry phase lives in the gateways
(`ConfigRegistryPackets`/`UpdateTagsPacket`, 770 and 26.x). We do **not** yet
downgrade translatable keys (a 1.21.5 client shown a 1.21.11-only item/mob name
key would see a raw key — but since we substitute the entity/item to a real older
one, its name resolves normally, so this is mostly moot). We have **no** dialog
system to downgrade. The **capture-then-strip** and **per-connection storage**
patterns are the ones to remember if we ever emit new registries/tags to old
clients.

---

## 6. Cross-cutting patterns worth stealing

- **Substitution is a bundle** `{replacementId, name, baseData}` — model it as one
  struct with a builder.
- **Per-connection / per-eid storage** keyed by type; the downgrade is stateful.
- **Capture-then-strip**: observe a new registry/tag in its handler, stash raw
  entries, then remove from the client packet.
- **Safe-sentinel substitution** for unknown values (id 0 / `intentionally_empty`)
  rather than dropping or erroring.
- **Two-pass packet handling** (passthrough-read, `resetReader()`, transform).
- **Surrogate UIs** assembled entirely from old packets; serverbound interactions
  intercepted and translated back to the real new action.
- **Skip identity tables** entirely.
- **`-1` is the one sentinel**; every caller turns it into a safe default.
- **Asymmetric, config-gated warnings** — loud only in the direction where a miss
  is a real bug.

---

## 7. The entity substitution guard (implemented)

`tachyne-common/protocol/entity_substitute.go` + `remapSpawnEntityType`. For a
client whose protocol predates an entity, the spawn packet's type is replaced with
a hand-picked older stand-in **before** the range-shift. The fallback is itself a
canonical id, so it range-maps to the client's numbering through the existing
table — we reuse the machinery and only override the id fed into it.

Fallbacks follow ViaBackwards' choices (closest body-plan/movement):

| new mob (canonical 1.21.11) | added @ proto | fallback | 1.21.5 wire id | source |
|---|---|---|---|---|
| happy_ghast (58) | 771 (1.21.6) | ghast (57) | 55 | ViaBackwards |
| copper_golem (28) | 773 (1.21.9) | frog (55) | 53 | ViaBackwards |
| mannequin (83) | 773 | armor_stand (5) | 5 | ours (VB fakes a player) |
| camel_husk (20) | 774 (1.21.11) | camel (19) | 19 | ViaBackwards |
| nautilus (88) | 774 | squid (127) | 121 | ViaBackwards |
| zombie_nautilus (152) | 774 | glow_squid (61) | 58 | ViaBackwards |
| parched (97) | 774 | skeleton (115) | 109 | ViaBackwards |

A 26.2 (776) client is ≥ every introduction version, so it receives the real
entity untouched. Covered by `entity_substitute_test.go`.

**What the guard does NOT yet do** (the stateful, gateway-side half): drop the
metadata indices the older renderer can't parse for a substituted entity, inject
the real name as a floating tag, or fabricate a scale attribute. This is safe to
defer because **our engine controls what metadata it emits** — implementing these
mobs as plain roster species emits only baseline (shared) metadata, so the type
substitution alone prevents the crash. If a new mob ever needs exotic metadata,
add per-eid type tracking on the gateway (mirroring gw-776's `entTypes` map from
the creeper fix) and a metadata-index drop pass keyed on the substituted type.

---

## 8. Follow-up backlog (from this sweep)

1. **Block-state safe-default** — an unmapped new block-state (copper family, etc.)
   should degrade to air (or a hand-picked 1.21.5 analogue) on old clients instead
   of range-shifting to a wrong block. The block-side twin of the entity guard.
2. **Gateway metadata sanitizing** for substituted entities (only when we emit a
   mob with non-baseline metadata) — per-eid type tracking + index-drop pass.
3. **Name-tag injection** on substituted entities (cosmetic; so a 1.21.5 player
   sees "Happy Ghast" over the stand-in Ghast).
4. **Translatable-key downgrade** — only needed if we surface a new-version-only
   name key that isn't already resolved via substitution.
