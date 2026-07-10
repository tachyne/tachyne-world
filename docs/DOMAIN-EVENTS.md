# The endgame refactor: hub emits domain events, the wire layer dies

Status: DONE (2026-07-06) — all six stages complete. The engine has no
Minecraft socket and no wire code; every frame on the attach protocol is
typed. Remaining debt (deliberate): recipe book not served to gateways
(the payload is inherently per-version — needs a gateway-side builder);
/refresh no longer force-resends already-sent chunks (needs a session
sent-set flush hook); same-dimension death respawn relies on the session's
sent-set view-window pruning for chunk re-send (pre-existing).

## Goal

The hub currently BUILDS canonical-770 packets and broadcasts bytes; attach
sessions reverse-parse them (remote.go decoder) and gateways re-render. The
endgame inverts this: the hub emits TYPED DOMAIN EVENTS; renderers turn them
into wire format — the gateway renderers in tachyne-common, and (until the
local-dev TCP path dies) a thin 770 renderer for direct connections. Then
these delete TOGETHER, each stage removing its own scaffolding:
conn/status/login/configuration + play's connection half + crypto, the hub's
packet builders, remote.go's decoder, and the MsgRaw/MsgRawServer bridge.

## Invariants (every stage)

1. ONE source of truth per packet family at all times: when a family moves to
   domain events, the SAME commit deletes its decoder case / raw-bridge
   allowance and its hub packet-builder. No parallel paths.
2. Byte-diff verification before rollout: capture gateway output for a
   scripted session (configdump-style walker, extended per stage) on old vs
   new code — identical or explained.
3. Real-client smoke (770 + 776) after each stage rollout.
4. The attach protocol only gains DOMAIN types (no wire blobs); each stage
   shrinks MsgRaw traffic, and the final stage deletes it.

## Stages (each one session-sized, shippable alone)

1. **Entity family — DONE (2026-07-06)**: hub emits EntityAdd/Move/Head/
   Remove/PlayerInfo domain events (replacing spawnPlayer/moveLook/
   playerInfoAdd builders at ~60 hub call sites); the `render770` package in
   tachyne-common renders them for BOTH gateways and the legacy TCP path
   (per-viewer EntityView tracker: rel-move vs absolute resync). Deleted in
   the same commit: all hub entity builders + decoder entity cases. Notable:
   events carry ABSOLUTE positions (delta math moved to the viewer-side
   renderer, so drops self-heal by construction); EntityMove.NoSync carries
   the dragon's never-sync constraint (776 clients lose entities to
   sync_entity_position); EntityAdd carries projectile launch velocity;
   PlayerInfo carries skin props. render770's tests use the deleted builders
   as byte oracles (the executable byte-diff verification).
2. **Presence UI — DONE (2026-07-06)**: chat/system messages (attach.Chat +
   ActionBar flag), boss bars (new attach.BossBar, MsgBossBar 0x1a), world
   clock (attach.Time + Age). Deleted: systemChat/actionBar/timePacket/
   bossBar* builders, the SystemChat decoder case (incl. its actionbar raw
   branch). Tab-list add/remove were already stage 1; no update forms
   existed. Time events now reach gateways (MsgTime) on the hub cadence —
   /time and bed-skip apply instantly instead of at the session ticker.
3. **Survival state — DONE (2026-07-06)**: health/food/saturation
   (attach.Health), XP (attach.XP), status effects (attach.Effect add/
   remove), hurt animation (attach.Hurt — players AND mobs), death screen
   (attach.Death). Deleted: sendHealth/sendExperience packet bodies,
   hurtAnimation/deathCombat builders, effect packet builders. Respawn
   itself stays with the dimension family (existing decode).
4. **Items — DONE (2026-07-06)**: attach.ItemStack (id, count, components
   as opaque canonical bytes — typed later; gateway translator renumbers),
   Equipment (players + mobs), WindowOpen/Items/Slot/Data (all 8 container
   windows), HeldSync, Collect, and EntityMeta (EID typed, metadata list
   opaque — same typed-later story). Deleted: equipmentPacket/collectPacket
   and the raw window/slot/meta byte-building at every sender. Trade OFFERS
   (merchant_offers) and the recipe book still flow raw (stage 5/6).
5. **World effects — DONE (2026-07-06)**: attach.Sound (by name —
   version-proof), attach.Particles (canonical pid, chain remaps),
   attach.WorldFX (2001 break FX), and every raw block_update sender folded
   into BlockSet events (portal frames, sim, dragon platform, prediction
   reverts). The hub package no longer imports the protocol package at all.
   Deleted: soundBody/particleBody/blockUpdate builders + the decoder's
   BlockUpdate case.
6. **Deletion — DONE (2026-07-06)**, ordered sub-steps:
   a. *Clientbound stragglers → events* (keeps TCP rendering via render770):
      GameEvent (rain/gamemode/wait-chunks), Abilities, Passengers,
      VehicleMove, EntityVelocity, WinProperty, CursorItem, Difficulty,
      Trades (opaque envelope), CommandTree (opaque envelope — the join-time
      raw emissions in JoinRemote become typed).
   b. *Serverbound typed actions* replacing MsgRawServer + dispatchPlay's
      byte parsing: UseItem, UseEntity, WindowClick, WindowClose,
      EntityAction, ClientInfo, CreativeSlot, EnchantOpt. Gateways parse
      client wire → typed frames; handlers take typed args (TCP keeps its
      own wire parsing at the boundary until (c)).
   c. *The deletion*: conn.go, status.go, login.go, configuration.go,
      crypto, play.go's connection half, compression, `-addr`. With TCP
      gone, switchDimension/teleport/chunk-stream become remote-only —
      Dimension/Teleport events replace the Respawn/SyncPosition raw sends
      AND their decoder cases in the same commit; then remote.go's decode()
      and MsgRaw/MsgRawServer delete. cmd/server becomes the world-pod
      binary only; local dev = local gateway (script/compose).

## Design notes

- The hub's `trySend(id, prebuilt)` call sites become `emit(ev)` on a per-
  player event queue; the TCP writer goroutine renders via render770 exactly
  where it serializes today, so backpressure semantics are unchanged.
- Entity IDs, dimensions, positions are already domain-typed in attach —
  stage 1 reuses those exact types (tachyne-common/attach).
- Sharding (SIDs, region ownership, gateway handover) builds on the SAME
  event stream: a handover replays presence state from typed events, which
  is why this refactor precedes the sharding milestone.
