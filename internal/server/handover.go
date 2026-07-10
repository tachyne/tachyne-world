package server

import (
	"sort"

	ho "github.com/tachyne/tachyne-common/handover"
)

// Pod-to-pod handover mapping: the engine's live per-player state (tracked)
// <-> the wire snapshot (handover.PlayerState) that crosses a shard seam.
//
// The snapshot mirrors the LIVE state, not the on-disk subset — health, food,
// saturation, air, fire and active effects are hub-live-only and would reset on
// a crossing if omitted. Item name/potion and fine-grained tick cooldowns are
// intentionally dropped (a crossing is "at most as lossy as a relog"); effects
// and fire carry remaining time directly, so they need no per-pod tick fixup.
// See docs/SHARDING-BUILD.md §5. Bed spawn is sourced from spawnStore, not
// tracked, so it is carried by the caller, not here.

// playerStateOf builds the handover snapshot from a live tracked.
func playerStateOf(t *tracked, name string, uuid [16]byte) ho.PlayerState {
	ps := ho.PlayerState{
		EID: t.p.eid, Name: name, UUID: uuid, Dim: int32(t.dim),
		X: t.x, Y: t.y, Z: t.z, Yaw: t.yaw, Pitch: t.pitch,
		OnGround: t.onGround, Sprinting: t.sprinting, Airborne: t.airborne, PeakY: t.peakY,
		Gamemode:   int32(t.gamemode),
		Health:     t.health,
		Absorption: t.absorption,
		Food:       int32(t.food),
		Saturation: t.saturation,
		Exhaustion: t.exhaustion,
		Air:        int32(t.air),
		FireSecs:   int32(t.fireSecs),
		XPLevel:    int32(t.xpLevel),
		XPPoints:   int32(t.xpPoints),
	}
	if t.inv != nil {
		for i, st := range t.inv.slots {
			ps.Slots[i] = packStack(st)
		}
	}
	for i, st := range t.armor {
		ps.Armor[i] = packStack(st)
	}
	ps.Offhand = packStack(t.offhand)
	// Effects in a stable (id-sorted) order — map iteration is random, and the
	// snapshot must be deterministic for a comparable round-trip.
	ids := make([]int32, 0, len(t.effects))
	for id := range t.effects {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		e := t.effects[id]
		ps.Effects = append(ps.Effects, ho.EffectState{ID: id, Amp: int32(e.amp), Left: int32(e.left)})
	}
	return ps
}

// applyPlayerState writes a migrated snapshot into a tracked on the destination
// pod (resume). The eid is the session-stable player-lane id and is preserved so
// viewers never see the player despawn/respawn across the crossing.
func (t *tracked) applyPlayerState(ps ho.PlayerState) {
	if t.p != nil {
		t.p.eid = ps.EID
	}
	t.dim = int(ps.Dim)
	t.x, t.y, t.z, t.yaw, t.pitch = ps.X, ps.Y, ps.Z, ps.Yaw, ps.Pitch
	t.onGround, t.sprinting, t.airborne, t.peakY = ps.OnGround, ps.Sprinting, ps.Airborne, ps.PeakY
	t.gamemode = int(ps.Gamemode)
	t.health, t.absorption = ps.Health, ps.Absorption
	t.food, t.saturation, t.exhaustion = int(ps.Food), ps.Saturation, ps.Exhaustion
	t.air, t.fireSecs = int(ps.Air), int(ps.FireSecs)
	t.xpLevel, t.xpPoints = int(ps.XPLevel), int(ps.XPPoints)
	if t.inv == nil {
		t.inv = &inventory{}
	}
	for i, row := range ps.Slots {
		t.inv.slots[i] = unpackStack(row)
	}
	for i, row := range ps.Armor {
		t.armor[i] = unpackStack(row)
	}
	t.offhand = unpackStack(ps.Offhand)
	t.effects = map[int32]*activeEffect{}
	for _, e := range ps.Effects {
		t.effects[e.ID] = &activeEffect{amp: int(e.Amp), left: int(e.Left)}
	}
}
