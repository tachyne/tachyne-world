package server

import (
	"bytes"
	"path/filepath"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func shoulderFixture(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 184; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z, pl.onGround = 0.5, 180, 0.5, true
	h.tick.Store(1000)
	return h, players, pl
}

// tamedParrot is a parrot of the given colour owned by pl, old enough to ride.
func tamedParrot(t *testing.T, h *hub, players map[int32]*tracked, pl *tracked, colour int32, x, z float64) *mob {
	t.Helper()
	m := h.spawnSpecies(players, entityParrot, dimOverworld, x, 180, z)
	if m == nil {
		t.Fatal("no parrot")
	}
	m.tamed, m.owner, m.ownerUUID = true, pl.p.eid, pl.p.uuid
	petStance(m) // what tameMob sets
	m.variant, m.variantSet = colour, true
	m.spawnTick = h.tick.Load() - shoulderRideCooldown - 1
	return m
}

// shoulderValues decodes the last shoulder metadata frame queued to a player.
func shoulderValues(t *testing.T, pl *tracked, eid int32) (left, right int32, ok bool) {
	t.Helper()
	for _, ev := range drainEvs(pl.p) {
		e, isMeta := ev.(attachproto.EntityMeta)
		if !isMeta || e.EID != eid {
			continue
		}
		r := bytes.NewReader(e.Meta)
		for {
			idx, err := r.ReadByte()
			if err != nil || idx == 0xff {
				break
			}
			typ, _ := protocol.ReadVarInt(r)
			v, _ := protocol.ReadVarInt(r)
			if typ != metaTypeOptUInt {
				t.Fatalf("shoulder entry %d has serializer %d, want %d", idx, typ, metaTypeOptUInt)
			}
			switch idx {
			case metaIndexShoulderLeft:
				left, ok = v, true
			case metaIndexShoulderRight:
				right = v
			}
		}
	}
	return
}

// A tamed parrot touching its owner lands on the left shoulder, then a second
// one on the right; a third has nowhere to go.
func TestParrotLandsOnShoulder(t *testing.T) {
	h, players, pl := shoulderFixture(t)
	a := tamedParrot(t, h, players, pl, 2, 0.6, 0.6)
	b := tamedParrot(t, h, players, pl, 4, 0.4, 0.4)
	c := tamedParrot(t, h, players, pl, 1, 0.5, 0.7)
	drainEvs(pl.p)
	if !h.parrotLandOnShoulder(players, a) || !h.parrotLandOnShoulder(players, b) {
		t.Fatal("tamed parrots touching their owner did not land")
	}
	if h.parrotLandOnShoulder(players, c) {
		t.Error("a third parrot found a shoulder")
	}
	if h.mobs[a.eid] != nil || h.mobs[b.eid] != nil || h.mobs[c.eid] == nil {
		t.Error("the riders should leave the world, the third stay in it")
	}
	left, right, ok := shoulderValues(t, pl, pl.p.eid)
	if !ok || left != 3 || right != 5 {
		t.Errorf("shoulder meta left=%d right=%d (sent %v), want 3 and 5 (variant + 1)", left, right, ok)
	}
}

// Nothing lands before RIDE_COOLDOWN, on a sitting or leashed parrot, on a
// wild one, out of reach, or on an owner who is in the air or riding.
func TestParrotShoulderRefusals(t *testing.T) {
	h, players, pl := shoulderFixture(t)
	fresh := tamedParrot(t, h, players, pl, 0, 0.5, 0.5)
	fresh.spawnTick = h.tick.Load() - 50
	sitting := tamedParrot(t, h, players, pl, 0, 0.5, 0.5)
	sitting.sitting = true
	wild := tamedParrot(t, h, players, pl, 0, 0.5, 0.5)
	wild.tamed = false
	far := tamedParrot(t, h, players, pl, 0, 4.5, 4.5)
	for name, m := range map[string]*mob{"fresh": fresh, "sitting": sitting, "wild": wild, "far": far} {
		if h.parrotLandOnShoulder(players, m) {
			t.Errorf("a %s parrot landed", name)
		}
	}
	ok := tamedParrot(t, h, players, pl, 0, 0.5, 0.5)
	pl.onGround = false
	if h.parrotLandOnShoulder(players, ok) {
		t.Error("landed on an owner in the air")
	}
	pl.onGround, pl.ridingEID = true, 99
	if h.parrotLandOnShoulder(players, ok) {
		t.Error("landed on a riding owner")
	}
}

// A jump shakes them off — after the one-second settle — and they come back
// as the same parrots, owned, at the player's side.
func TestShoulderParrotsHopOff(t *testing.T) {
	h, players, pl := shoulderFixture(t)
	a := tamedParrot(t, h, players, pl, 3, 0.5, 0.5)
	a.customName = "Polly"
	if !h.parrotLandOnShoulder(players, a) {
		t.Fatal("no landing")
	}
	pl.airborne, pl.peakY, pl.y = true, 181.3, 180.5 // coming down from a jump
	h.shoulderTick(players)
	if pl.shoulders[0] == nil {
		t.Fatal("the parrot left before it had settled")
	}
	h.tick.Add(shoulderSettleTicks + 1)
	drainEvs(pl.p)
	h.shoulderTick(players)
	if pl.shoulderOccupied() {
		t.Fatal("a jump did not shake the parrot off")
	}
	var back *mob
	for _, m := range h.mobs {
		if m.etype == entityParrot {
			back = m
		}
	}
	if back == nil || !back.tamed || back.owner != pl.p.eid || back.variant != 3 || back.customName != "Polly" {
		t.Fatalf("the parrot came back as %+v", back)
	}
	if back.y != pl.y+shoulderRespawnLift || back.x != pl.x {
		t.Errorf("respawned at %.2f,%.2f, want the player's x and y + 0.7", back.x, back.y)
	}
	if left, right, ok := shoulderValues(t, pl, pl.p.eid); !ok || left != 0 || right != 0 {
		t.Errorf("shoulders not cleared on the client: %d %d %v", left, right, ok)
	}
	if h.parrotLandOnShoulder(players, back) {
		t.Error("a parrot that just hopped off landed again at once")
	}
}

// Being hurt knocks them off too.
func TestShoulderParrotsLeaveOnHurt(t *testing.T) {
	h, players, pl := shoulderFixture(t)
	if !h.parrotLandOnShoulder(players, tamedParrot(t, h, players, pl, 0, 0.5, 0.5)) {
		t.Fatal("no landing")
	}
	h.tick.Add(shoulderSettleTicks + 1)
	h.hurtBy(players, pl, 1, dtGeneric, deathCause{})
	if pl.shoulderOccupied() {
		t.Error("a hurt player kept their parrot")
	}
}

// A relog keeps the parrots on the shoulder.
func TestShoulderParrotsSurviveRelog(t *testing.T) {
	h, players, pl := shoulderFixture(t)
	if !h.parrotLandOnShoulder(players, tamedParrot(t, h, players, pl, 4, 0.5, 0.5)) {
		t.Fatal("no landing")
	}
	store := newInvStore(filepath.Join(t.TempDir(), "inv.json"))
	store.record(pl.p.name, pl)
	pl.shoulders = [2]*savedMob{}
	store.loadInto(pl, pl.p.name)
	if sm := pl.shoulders[0]; sm == nil || shoulderVariant(sm) != 4 || sm.Etype != entityParrot {
		t.Errorf("reloaded shoulder %+v, want the blue parrot", sm)
	}
}

// Through the mob tick itself: a tamed parrot left near its owner follows
// and lands, as LandOnOwnersShoulderGoal runs alongside FollowOwnerGoal.
func TestParrotLandsThroughMobTick(t *testing.T) {
	h, players, pl := shoulderFixture(t)
	m := tamedParrot(t, h, players, pl, 1, 6.5, 0.5) // beyond FollowOwnerGoal's five-block start
	for i := 0; i < 400 && !pl.shoulderOccupied(); i++ {
		h.tick.Add(1)
		h.updateMobs(players)
	}
	if !pl.shoulderOccupied() || h.mobs[m.eid] != nil {
		t.Fatalf("after 400 ticks the parrot is at %.2f,%.2f,%.2f and the shoulders are %v", m.x, m.y, m.z, pl.shoulders)
	}
}
