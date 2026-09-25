package server

import (
	"encoding/binary"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Ominous item spawners — the floating item an ominous trial spawner
// conjures above a player every 160 ticks while its fight is on
// (TrialSpawnerState.spawnOminousOminousItemSpawner) which, 60–120 ticks
// later, drops what it holds as a projectile straight down
// (OminousItemSpawner): a lingering potion, a tipped arrow or a charge.

const (
	itemSpawnerGap       = 160 // TrialSpawnerConfig.ticksBetweenItemSpawners
	itemSpawnerDelayMin  = 60  // OminousItemSpawner.SPAWN_ITEM_DELAY_MIN
	itemSpawnerDelaySpan = 61  // …to 120 inclusive
	itemSpawnerWarn      = 36  // TICKS_BEFORE_ABOUT_TO_SPAWN_SOUND
	worldEventItemSpawn  = 3021
	metaIndexSpawnerItem = 8 // OminousItemSpawner DATA_ITEM (Entity's 8 fields first)
)

var entityOminousItemSpawner = entityID("ominous_item_spawner")

// ominousDrop is one entry of spawners/trial_chamber/items_to_drop_when_ominous
// (twelve at equal weight).
type ominousDrop struct {
	item      string
	potion    int8 // the potion or tipped-arrow kind
	lingering bool
	count     [2]int
}

var ominousDrops = []ominousDrop{
	{"lingering_potion", potWindCharged, true, [2]int{1, 1}},
	{"lingering_potion", potOozing, true, [2]int{1, 1}},
	{"lingering_potion", potWeaving, true, [2]int{1, 1}},
	{"lingering_potion", potInfested, true, [2]int{1, 1}},
	{"lingering_potion", potStrength, true, [2]int{1, 1}},
	{"lingering_potion", potSwiftness, true, [2]int{1, 1}},
	{"lingering_potion", potSlowFalling, true, [2]int{1, 1}},
	{"arrow", potNone, false, [2]int{1, 1}},
	{"arrow", potPoison, false, [2]int{1, 1}},
	{"arrow", potStrongSlowness, false, [2]int{1, 1}},
	{"fire_charge", potNone, false, [2]int{1, 3}},
	{"wind_charge", potNone, false, [2]int{1, 3}},
}

// itemSpawnerEnt is one conjured spawner in the air.
type itemSpawnerEnt struct {
	eid     int32
	dim     int
	x, y, z float64
	drop    ominousDrop
	dueAt   uint64
	warned  bool
}

// trialItemSpawner conjures a spawner above a random player the trial has
// detected, on the 160-tick cadence.
func (h *hub) trialItemSpawner(players map[int32]*tracked, ts *trialSpawner) {
	now := h.tick.Load()
	if now < ts.itemSpawnAt {
		return
	}
	var near []*tracked
	for _, t := range players {
		if t.dim != ts.dim || t.dead || t.gamemode == gmSpectator || !ts.detected[t.p.uuid] {
			continue
		}
		if dist3(t.x, t.y, t.z, ts.fx(), ts.fy(), ts.fz()) <= trialPlayerRange {
			near = append(near, t)
		}
	}
	if len(near) == 0 {
		return
	}
	t := near[h.rng.Intn(len(near))]
	w := h.worldFor(ts.dim)
	if w == nil {
		return
	}
	x := t.x + float64(h.rng.Intn(3)-1)
	y := t.y + 2 + float64(h.rng.Intn(2))
	z := t.z + float64(h.rng.Intn(3)-1)
	if w.At(floorInt(x), floorInt(y), floorInt(z)) != worldgen.Air {
		return
	}
	drop := ominousDrops[h.rng.Intn(len(ominousDrops))]
	if h.itemSpawners == nil {
		h.itemSpawners = map[int32]*itemSpawnerEnt{}
	}
	e := &itemSpawnerEnt{eid: h.allocEID(), dim: ts.dim, x: x, y: y, z: z, drop: drop,
		dueAt: now + itemSpawnerDelayMin + uint64(h.rng.Intn(itemSpawnerDelaySpan))}
	h.itemSpawners[e.eid] = e
	h.showItemSpawner(players, e)
	pitch := (h.rng.Float32()-h.rng.Float32())*0.2 + 1
	h.playSoundDim(players, ts.dim, "minecraft:block.trial_spawner.spawn_item_begin", sndBlock, x, y, z, 1, pitch)
	ts.itemSpawnAt = now + itemSpawnerGap
}

// showItemSpawner adds the entity with the item it holds (DATA_ITEM).
func (h *hub) showItemSpawner(players map[int32]*tracked, e *itemSpawnerEnt) {
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(e.eid))
	h.toTracking(players, e.eid, e.dim, e.x, e.z, entAdd(e.eid, entityOminousItemSpawner, uuid, e.x, e.y, e.z, 0, 0))
	h.toNearbyEv(players, e.dim, e.x, e.z, metaEv(itemSpawnerMeta(e)))
}

// itemSpawnerMeta is the spawner's DATA_ITEM: the stack it holds.
func itemSpawnerMeta(e *itemSpawnerEnt) []byte {
	st := invStack{item: itemByName[e.drop.item], count: 1, potion: e.drop.potion}
	b := protocol.AppendVarInt(nil, e.eid)
	b = protocol.AppendU8(b, metaIndexSpawnerItem)
	b = protocol.AppendVarInt(b, itemMetaTypeSlot)
	b = appendStack(b, st)
	return protocol.AppendU8(b, itemMetaEnd)
}

// sendItemSpawnersTo replays the conjured spawners to a joining player, each
// with the item it holds (it used to arrive empty).
func (h *hub) sendItemSpawnersTo(t *tracked) {
	for _, e := range h.itemSpawners {
		if e.dim == t.dim {
			var uuid [16]byte
			binary.BigEndian.PutUint32(uuid[12:], uint32(e.eid))
			t.p.trySendEv(entAdd(e.eid, entityOminousItemSpawner, uuid, e.x, e.y, e.z, 0, 0))
			t.p.trySendEv(metaEv(itemSpawnerMeta(e)))
		}
	}
}

// updateItemSpawners runs every tick: the warning sound, then the drop.
func (h *hub) updateItemSpawners(players map[int32]*tracked) {
	if len(h.itemSpawners) == 0 {
		return
	}
	now := h.tick.Load()
	for eid, e := range h.itemSpawners {
		if !e.warned && e.dueAt-now <= itemSpawnerWarn {
			e.warned = true
			h.playSoundDim(players, e.dim, "minecraft:block.trial_spawner.about_to_spawn_item", sndNeutral, e.x, e.y, e.z, 1, 1)
		}
		if now < e.dueAt {
			continue
		}
		delete(h.itemSpawners, eid)
		h.entityGone(players, e.dim, e.eid)
		h.toNearbyEv(players, e.dim, e.x, e.z, attachproto.WorldFX{Event: worldEventItemSpawn, X: floorInt(e.x), Y: floorInt(e.y), Z: floorInt(e.z), Data: 1})
		h.dropOminousItem(players, e)
	}
}

// dropOminousItem is OminousItemSpawner.spawnProjectile: the held item goes
// down as ONE projectile, whatever the stack's count, shot along DOWN with
// its item's DispenseConfig — a lingering potion at 1.375 and uncertainty
// 3, an arrow at 1.1 and 6, a fire or wind charge at 1.0 and 6.67 with its
// own dispense event (1018, 1051).
func (h *hub) dropOminousItem(players map[int32]*tracked, e *itemSpawnerEnt) {
	shot := func(etype int, pow, unc float64) *arrowEntity {
		vx, vy, vz := h.shootVector(0, -1, 0, pow, unc)
		return h.launchProjectileIn(players, etype, e.dim, e.x, e.y, e.z, vx, vy, vz)
	}
	event := func(id int32) {
		h.toNearbyEv(players, e.dim, e.x, e.z, attachproto.WorldFX{Event: id, X: floorInt(e.x), Y: floorInt(e.y), Z: floorInt(e.z)})
	}
	switch e.drop.item {
	case "lingering_potion":
		a := shot(entityLingerProj, 1.1*1.25, 6*0.5)
		a.splash, a.breaks, a.potion, a.lingering = true, true, e.drop.potion, true
	case "arrow":
		a := shot(entityArrow, 1.1, 6)
		a.dmg = arrowDamage
		if e.drop.potion != potNone {
			a.tipped, a.potion = true, e.drop.potion
		}
	case "fire_charge":
		event(1018)
		a := shot(entitySmallFireball, 1.0, 6.6666665)
		a.dmg, a.fire = 5, true
	case "wind_charge":
		event(1051)
		a := shot(entityWindCharge, 1.0, 6.6666665)
		a.breaks = true
	}
}
