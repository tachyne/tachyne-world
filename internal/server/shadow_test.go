package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/shard"
	"github.com/tachyne/tachyne-world/internal/world"
)

func axisCases(t *testing.T) {
	t.Helper()
	// [lo,hi) = [0,16): inside → 0, one west → 1, far west → 13, at hi → 1 past.
	for _, c := range []struct{ p, lo, hi, want int32 }{
		{5, 0, 16, 0}, {-1, 0, 16, 1}, {-13, 0, 16, 13}, {16, 0, 16, 1}, {20, 0, 16, 5},
	} {
		if got := axisChunkDist(c.p, c.lo, c.hi); got != c.want {
			t.Errorf("axisChunkDist(%d,%d,%d) = %d, want %d", c.p, c.lo, c.hi, got, c.want)
		}
	}
}

// twoShardMesh wires hubA (west, sid 0) and hubB (east, sid 1) with the standard
// two-tile topology and an in-process fake peer mesh in both directions.
func twoShardMesh(t *testing.T) (hubA, hubB *hub, playersA, playersB map[int32]*tracked) {
	t.Helper()
	topo := shard.Map{Version: 1, Regions: []shard.Region{
		{SID: 0, MinCX: -16, MinCZ: -8, W: 16, H: 16}, // west
		{SID: 1, MinCX: 0, MinCZ: -8, W: 16, H: 16},   // east
	}}
	shardOf := func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }
	hubA = newHub(world.New(1))
	hubA.sid, hubA.shardOf, hubA.topo = 0, shardOf, topo
	hubB = newHub(world.New(1))
	hubB.sid, hubB.shardOf, hubB.topo = 1, shardOf, topo
	playersA = map[int32]*tracked{}
	playersB = map[int32]*tracked{}
	hubA.peers = &fakeMesh{self: 0, deliver: map[int32]func(int32, byte, []byte){
		1: func(from int32, typ byte, payload []byte) { hubB.handlePeerFrame(playersB, from, typ, payload) },
	}}
	hubB.peers = &fakeMesh{self: 1, deliver: map[int32]func(int32, byte, []byte){
		0: func(from int32, typ byte, payload []byte) { hubA.handlePeerFrame(playersA, from, typ, payload) },
	}}
	return
}

// drain non-blockingly collects everything queued to a player's out channel.
func drainEvs(p *player) []any {
	var evs []any
	for {
		select {
		case pkt := <-p.out:
			evs = append(evs, pkt.ev)
		default:
			return evs
		}
	}
}

func TestShadowMobCrossesSeamIntoView(t *testing.T) {
	axisCases(t)
	hubA, hubB, playersA, playersB := twoShardMesh(t)

	// An observer standing on the EAST pod, near the seam.
	obEID := shard.MintEID(1, shard.PlayerSID)
	ob := &tracked{p: newPlayer(obEID, "obs", [16]byte{9}), dim: 0, x: 2, y: 64, z: 0}
	playersB[obEID] = ob
	drainEvs(ob.p) // clear anything from setup

	// A zombie on the WEST pod, near the seam (chunk -1, one from east's edge).
	m := &mob{eid: hubA.allocEID(), etype: entityZombie, dim: 0, x: -4, y: 64, z: 0, health: 20, hostile: true}
	hubA.mobs[m.eid] = m

	// West pushes shadows; the zombie is within view of the east tile.
	hubA.updateShadows(playersA)

	se := hubB.shadowIn[m.eid]
	if se == nil {
		t.Fatal("zombie not shadowed onto the east pod")
	}
	if se.etype != entityZombie || se.dim != 0 || se.x != -4 {
		t.Errorf("shadow state wrong: %+v", se)
	}
	if !hubA.shadowOut[m.eid][1] {
		t.Error("west pod did not record the shadow as pushed to sid 1")
	}
	// The east observer got a spawn for it (across the seam).
	var spawned bool
	for _, ev := range drainEvs(ob.p) {
		if add, ok := ev.(attachproto.EntityAdd); ok && add.EID == m.eid {
			spawned = true
		}
	}
	if !spawned {
		t.Error("east observer never received an EntityAdd for the cross-seam zombie")
	}

	// The zombie wanders deep into the west; the shadow must be retracted.
	m.x = -200
	hubA.updateShadows(playersA)
	if hubB.shadowIn[m.eid] != nil {
		t.Error("shadow not dropped after the mob left awareness range")
	}
	if len(hubA.shadowOut[m.eid]) != 0 {
		t.Error("west pod still records a shadow for the departed mob")
	}
	var removed bool
	for _, ev := range drainEvs(ob.p) {
		if rm, ok := ev.(attachproto.EntityRemove); ok {
			for _, e := range rm.EIDs {
				if e == m.eid {
					removed = true
				}
			}
		}
	}
	if !removed {
		t.Error("east observer never received an EntityRemove for the retracted shadow")
	}
}

func TestShadowPlayerCarriesProfile(t *testing.T) {
	hubA, hubB, playersA, playersB := twoShardMesh(t)
	obEID := shard.MintEID(2, shard.PlayerSID)
	ob := &tracked{p: newPlayer(obEID, "obs", [16]byte{9}), dim: 0, x: 2, y: 64, z: 0}
	playersB[obEID] = ob
	drainEvs(ob.p)

	// A player on the west pod near the seam.
	pEID := shard.MintEID(1, shard.PlayerSID)
	pl := &tracked{p: newPlayer(pEID, "wesley", [16]byte{1, 2, 3}), dim: 0, x: -4, y: 64, z: 0}
	playersA[pEID] = pl

	hubA.updateShadows(playersA)

	se := hubB.shadowIn[pEID]
	if se == nil || se.name != "wesley" {
		t.Fatalf("player not shadowed with its profile: %+v", se)
	}
	// The observer must get a PlayerInfo (tab/profile) before the entity add, or
	// the client renders a nameless/invisible player.
	evs := drainEvs(ob.p)
	var infoIdx, addIdx = -1, -1
	for i, ev := range evs {
		switch e := ev.(type) {
		case attachproto.PlayerInfo:
			if e.Name == "wesley" {
				infoIdx = i
			}
		case attachproto.EntityAdd:
			if e.EID == pEID {
				addIdx = i
			}
		}
	}
	if infoIdx < 0 || addIdx < 0 {
		t.Fatalf("missing PlayerInfo (%d) or EntityAdd (%d) for the shadow player", infoIdx, addIdx)
	}
	if infoIdx > addIdx {
		t.Error("PlayerInfo must precede EntityAdd for a shadow player")
	}
}

// Cross-seam aggro: a mob on the east pod must acquire a SURVIVAL player's
// shadow pushed from the west pod as its chase target — and never a creative
// player's, or a dead player's (whose shadow is retracted entirely).
func TestMobAggroOnShadowPlayer(t *testing.T) {
	hubA, hubB, playersA, _ := twoShardMesh(t)

	// A survival player on the west pod, 4 blocks west of the seam.
	pEID := shard.MintEID(1, shard.PlayerSID)
	pl := &tracked{p: newPlayer(pEID, "wesley", [16]byte{1}), dim: 0, x: -4, y: 64, z: 0, gamemode: gmSurvival}
	playersA[pEID] = pl
	hubA.updateShadows(playersA)

	// A zombie on the east pod, 8 blocks east of the seam (within follow range 35).
	m := &mob{eid: hubB.allocEID(), etype: entityZombie, dim: 0, x: 8, y: 64, z: 0, health: 20, hostile: true, aggro: 35}
	hubB.mobs[m.eid] = m
	hubB.acquireTarget(map[int32]*tracked{}, m) // no real players on east — only the shadow
	if !m.hasTarget || m.tx != -4 || m.tz != 0 {
		t.Fatalf("zombie did not aggro the cross-seam shadow: hasTarget=%v tx=%v tz=%v", m.hasTarget, m.tx, m.tz)
	}

	// Switch the player to creative: the shadow updates and the mob must let go.
	pl.gamemode = gmCreative
	hubA.updateShadows(playersA)
	hubB.acquireTarget(map[int32]*tracked{}, m)
	if m.hasTarget {
		t.Error("zombie must not hunt a creative player's shadow")
	}

	// Kill the (survival again) player: the shadow is retracted outright.
	pl.gamemode, pl.dead = gmSurvival, true
	hubA.updateShadows(playersA)
	if hubB.shadowIn[pEID] != nil {
		t.Error("a dead player's shadow must be retracted")
	}
}

// A completed handover must not leave a duplicate: when the crossed player
// becomes real on the destination it supersedes its own shadow there.
func TestShadowSupersededByRealPlayer(t *testing.T) {
	_, hubB, _, playersB := twoShardMesh(t)
	eid := shard.MintEID(1, shard.PlayerSID)
	// Pretend a shadow of this player already landed on B.
	hubB.shadowIn[eid] = &shadowEnt{kind: 1, name: "wesley", dim: 0, x: 1, added: true}
	// It resumes as a real player on B.
	ps := handover.PlayerState{EID: eid, Name: "wesley", UUID: [16]byte{1}, Dim: 0, X: 1, Y: 64, Z: 0, Gamemode: gmSurvival, Health: 20, Food: 20}
	p := newPlayer(eid, "wesley", [16]byte{1})
	hubB.onJoin(playersB, evJoin{p: p, x: 1, y: 64, z: 0, dim: 0, gamemode: gmSurvival, resume: &ps})
	if hubB.shadowIn[eid] != nil {
		t.Error("shadow bookkeeping not superseded when the real player arrived")
	}
	if playersB[eid] == nil {
		t.Error("real player not live on B after resume")
	}
}

// Flying past the last owned region must BOUNCE, not leave the player drifting
// in unserved void (a real phone client stalls there and times out — the
// disconnect Wesley hit flying off the map).
func TestWorldEdgeClamp(t *testing.T) {
	hubA, _, playersA, _ := twoShardMesh(t)
	eid := shard.MintEID(1, shard.PlayerSID)
	pl := &tracked{p: newPlayer(eid, "wesley", [16]byte{1}), dim: 0, x: -250, y: 80, z: 0, gamemode: gmCreative}
	playersA[eid] = pl

	// West edge of the test topology is block x=-256; try to fly past it.
	if hubA.validateMove(pl, evMove{eid: eid, x: -300, y: 80, z: 0}) {
		t.Fatal("move into the void was accepted")
	}
	if pl.x != -250 {
		t.Errorf("player position moved to %v after a rejected void move", pl.x)
	}
	// Moves INSIDE the world still work (seed the movement budget — the test
	// hub's tick clock never runs, so the per-tick allowance can't accrue).
	pl.moveBudget = 10
	if !hubA.validateMove(pl, evMove{eid: eid, x: -249, y: 80, z: 0}) {
		t.Error("legal in-world move rejected")
	}
}
