package server

import (
	"encoding/json"
	"fmt"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/shard"
	"github.com/tachyne/tachyne-world/internal/world"
)

// fakeMesh routes peer frames in-process (no sockets), delivering synchronously
// to the target hub's handlePeerFrame — enough to unit-test the release→ack→
// resume state machine end to end.
type fakeMesh struct {
	self    int32
	deliver map[int32]func(from int32, typ byte, payload []byte)
}

func (f *fakeMesh) send(peer int32, typ byte, v any) error {
	fn := f.deliver[peer]
	if fn == nil {
		return fmt.Errorf("peer %d not connected", peer)
	}
	payload, _ := json.Marshal(v)
	fn(f.self, typ, payload)
	return nil
}
func (f *fakeMesh) connected(peer int32) bool { return f.deliver[peer] != nil }

func TestPlayerHandoverAcrossSeam(t *testing.T) {
	topo := shard.Map{Version: 1, Regions: []shard.Region{
		{SID: 0, MinCX: -16, MinCZ: -8, W: 16, H: 16}, // west
		{SID: 1, MinCX: 0, MinCZ: -8, W: 16, H: 16},   // east
	}}
	shardOf := func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }

	hubA := newHub(world.New(1))
	hubA.sid, hubA.shardOf = 0, shardOf
	hubB := newHub(world.New(1))
	hubB.sid, hubB.shardOf = 1, shardOf

	playersA := map[int32]*tracked{}
	playersB := map[int32]*tracked{}
	hubA.peers = &fakeMesh{self: 0, deliver: map[int32]func(int32, byte, []byte){
		1: func(from int32, typ byte, payload []byte) { hubB.handlePeerFrame(playersB, from, typ, payload) },
	}}
	hubB.peers = &fakeMesh{self: 1, deliver: map[int32]func(int32, byte, []byte){
		0: func(from int32, typ byte, payload []byte) { hubA.handlePeerFrame(playersA, from, typ, payload) },
	}}

	// A fully-loaded player on the west pod.
	eid := shard.MintEID(1, shard.PlayerSID)
	p := newPlayer(eid, "wesley", [16]byte{1, 2, 3})
	src := &tracked{
		p: p, dim: 0, x: -16, y: 71, z: 0, yaw: 90,
		gamemode: gmSurvival, health: 18.5, food: 17, inv: &inventory{},
		effects: map[int32]*activeEffect{effSpeed: {amp: 1, left: 30}},
	}
	src.inv.slots[0] = invStack{item: 278, count: 1, dmg: 5}
	playersA[eid] = src

	// Walk east across the seam (block x >= 0 → chunk 0 → SID 1).
	src.x = 8
	hubA.checkSeamCrossing(playersA, src)

	// Gone from A…
	if _, ok := playersA[eid]; ok {
		t.Error("player still on source pod A after handover")
	}
	// …held PENDING on B for the gateway to resume (no live entity until resume).
	if len(playersB) != 0 {
		t.Error("player should not be live on B until it resumes")
	}
	ps, ok := hubB.claimPending("0.1") // migID = sid.migSeq = "0.1"
	if !ok {
		t.Fatal("player state not pending on B after handover")
	}
	if ps.EID != eid || ps.Health != 18.5 || ps.Food != 17 {
		t.Errorf("pending snapshot wrong: %+v", ps)
	}

	// Resume: the gateway reconnects to B; the player goes live from the snapshot.
	p2 := newPlayer(ps.EID, "wesley", ps.UUID)
	hubB.onJoin(playersB, evJoin{p: p2, x: ps.X, y: ps.Y, z: ps.Z, yaw: ps.Yaw, pitch: ps.Pitch, dim: int(ps.Dim), gamemode: int(ps.Gamemode), resume: &ps})
	dst := playersB[eid]
	if dst == nil {
		t.Fatal("player not live on B after resume")
	}
	if dst.p.eid != eid || shard.Minter(dst.p.eid) != shard.PlayerSID {
		t.Errorf("eid not preserved / not in player lane: %d", dst.p.eid)
	}
	if dst.health != 18.5 || dst.food != 17 || dst.x != 8 || dst.gamemode != gmSurvival {
		t.Errorf("survival state not preserved through resume: %+v", dst)
	}
	if dst.inv == nil || dst.inv.slots[0] != (invStack{item: 278, count: 1, dmg: 5}) {
		t.Errorf("inventory not preserved through resume: %+v", dst.inv)
	}
	if e := dst.effects[effSpeed]; e == nil || e.amp != 1 || e.left != 30 {
		t.Errorf("effects not preserved through resume: %+v", dst.effects)
	}

	// The source gateway session was told to re-point to the destination pod.
	select {
	case pkt := <-p.out:
		rh, ok := pkt.ev.(attachproto.Rehome)
		if !ok {
			t.Fatalf("first frame to source session is not a Rehome: %#v", pkt.ev)
		}
		if rh.DestSID != 1 {
			t.Errorf("Rehome DestSID = %d, want 1", rh.DestSID)
		}
	default:
		t.Error("no Rehome emitted to the source gateway session")
	}
}

func TestMobHandoverAcrossSeam(t *testing.T) {
	topo := shard.Map{Version: 1, Regions: []shard.Region{
		{SID: 0, MinCX: -16, MinCZ: -8, W: 16, H: 16},
		{SID: 1, MinCX: 0, MinCZ: -8, W: 16, H: 16},
	}}
	shardOf := func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }
	hubA := newHub(world.New(1))
	hubA.sid, hubA.shardOf = 0, shardOf
	hubB := newHub(world.New(1))
	hubB.sid, hubB.shardOf = 1, shardOf

	playersA := map[int32]*tracked{}
	playersB := map[int32]*tracked{}
	hubA.peers = &fakeMesh{self: 0, deliver: map[int32]func(int32, byte, []byte){
		1: func(from int32, typ byte, payload []byte) { hubB.handlePeerFrame(playersB, from, typ, payload) },
	}}
	hubB.peers = &fakeMesh{self: 1, deliver: map[int32]func(int32, byte, []byte){
		0: func(from int32, typ byte, payload []byte) { hubA.handlePeerFrame(playersA, from, typ, payload) },
	}}

	// A hostile mob on the west pod near the seam.
	m := &mob{eid: hubA.allocEID(), etype: entityZombie, dim: 0, x: -4, y: 64, z: 0, health: 20, hostile: true, behavior: hostileBehavior{}}
	hubA.mobs[m.eid] = m
	srcEID := m.eid

	// It steps east across the seam (nx=4 → chunk 0 → SID 1).
	if !hubA.migrateMobAcross(playersA, m, 4, 0) {
		t.Fatal("mob did not migrate across the seam")
	}
	if _, ok := hubA.mobs[srcEID]; ok {
		t.Error("mob still on source pod A after migration")
	}
	if len(hubB.mobs) != 1 {
		t.Fatalf("expected 1 mob on B, got %d", len(hubB.mobs))
	}
	var got *mob
	for _, mm := range hubB.mobs {
		got = mm
	}
	if got.etype != entityZombie || got.health != 20 || !got.hostile || got.x != 4 {
		t.Errorf("mob state not preserved on B: %+v", got)
	}
	if got.eid == srcEID {
		t.Error("migrated mob should get a fresh eid on the destination")
	}
	if shard.Minter(got.eid) != 1 {
		t.Errorf("migrated mob eid %d not re-minted in dest lane 1 (lane %d)", got.eid, shard.Minter(got.eid))
	}
	if _, ok := got.behavior.(hostileBehavior); !ok {
		t.Errorf("hostile behaviour not re-resolved on B: %T", got.behavior)
	}
}

// A crossing into VOID (past the world edge, not a neighbour) must NOT hand over.
func TestNoHandoverIntoVoid(t *testing.T) {
	topo := shard.Map{Version: 1, Regions: []shard.Region{
		{SID: 0, MinCX: -16, MinCZ: -8, W: 16, H: 16},
		{SID: 1, MinCX: 0, MinCZ: -8, W: 16, H: 16},
	}}
	h := newHub(world.New(1))
	h.sid, h.shardOf = 0, func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }
	h.peers = &fakeMesh{self: 0, deliver: map[int32]func(int32, byte, []byte){}} // no neighbours reachable

	eid := shard.MintEID(1, shard.PlayerSID)
	players := map[int32]*tracked{eid: {p: newPlayer(eid, "w", [16]byte{}), x: -300, z: 0}} // west of the world
	h.checkSeamCrossing(players, players[eid])
	if players[eid].migrating != "" {
		t.Error("stepping into void must not start a handover")
	}
}
