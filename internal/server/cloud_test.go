package server

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// metaEntries reads a metadata body back into index → (serializer, value
// bytes), for the fixed-width entries a cloud sends.
func metaEntries(t *testing.T, body []byte) map[byte][2][]byte {
	t.Helper()
	out := map[byte][2][]byte{}
	r := bytes.NewReader(body)
	for {
		idx, err := r.ReadByte()
		if err != nil || idx == 0xff {
			return out
		}
		typ, _ := protocol.ReadVarInt(r)
		var n int
		switch typ {
		case metaTypeFloat:
			n = 4
		case metaTypeBool:
			n = 1
		default:
			t.Fatalf("index %d: unexpected serializer %d", idx, typ)
		}
		v := make([]byte, n)
		r.Read(v)
		out[idx] = [2][]byte{protocol.AppendVarInt(nil, typ), v}
	}
}

// cloudFixture is a viewer standing a few blocks from where a lingering
// Swiftness potion broke, in the sky over force-loaded chunks.
func cloudFixture(t *testing.T) (*hub, map[int32]*tracked, *tracked, *effectCloud) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survivalAt(8.5, 180, 8.5)
	pl.p.eid = 9000 // clear of the ids the hub hands out
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.splashPotion(players, 0, 0.5, 180, 0.5, potSwiftness, true)
	if len(h.clouds) != 1 {
		t.Fatalf("a lingering potion should leave one cloud, have %d", len(h.clouds))
	}
	var c *effectCloud
	for _, x := range h.clouds {
		c = x
	}
	drainEvs(pl.p)
	return h, players, pl, c
}

// The cloud is an entity: the tracker spawns it for a viewer in range as an
// area_effect_cloud with its radius (DATA_RADIUS, index 8, a FLOAT), then
// tells them when it stops waiting and as it shrinks, and removes it when it
// is gone.
func TestLingeringCloudIsAnEntity(t *testing.T) {
	h, players, pl, c := cloudFixture(t)
	h.syncTracking(players)
	var added bool
	var spawnMeta []byte
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.EntityAdd:
			if e.EID == c.eid && e.Type == int32(entityAreaEffectCloud) {
				added = true
			}
		case attachproto.EntityMeta:
			if e.EID == c.eid {
				spawnMeta = append(spawnMeta, e.Meta...)
			}
		}
	}
	if !added {
		t.Fatal("the cloud was never spawned for the viewer as an area_effect_cloud")
	}
	m := metaEntries(t, spawnMeta)
	if v, ok := m[metaIndexCloudRadius]; !ok || math.Float32frombits(binary.BigEndian.Uint32(v[1])) != 3 {
		t.Fatalf("the spawn should carry radius 3 at index 8: %v", m)
	}
	if _, ok := m[metaIndexCloudParticle]; ok {
		t.Fatal("DATA_PARTICLE went out while the gateways cannot translate it")
	}

	// It waits ten ticks (DATA_WAITING true, then false), then shrinks.
	var waitingOn, waitingOff, radiusUpdates int
	for i := 0; i < cloudLingerWait+2; i++ { // ages 1–12: shrinking from the tenth
		h.updateClouds(players)
		for _, ev := range drainEvs(pl.p) {
			e, ok := ev.(attachproto.EntityMeta)
			if !ok || e.EID != c.eid {
				continue
			}
			for idx, v := range metaEntries(t, e.Meta) {
				switch {
				case idx == metaIndexCloudWaiting && v[1][0] == 1:
					waitingOn++
				case idx == metaIndexCloudWaiting:
					waitingOff++
				case idx == metaIndexCloudRadius:
					radiusUpdates++
				}
			}
		}
	}
	if waitingOn != 1 || waitingOff != 1 {
		t.Fatalf("waiting flag on %d / off %d times, want once each", waitingOn, waitingOff)
	}
	if radiusUpdates != 3 {
		t.Fatalf("a shrinking cloud should send its radius each tick after the wait: %d updates", radiusUpdates)
	}
	if want := float32(3 - 3*3.0/600); math.Abs(float64(c.radius-want)) > 1e-4 {
		t.Fatalf("radius %v after three shrinking ticks, want %v", c.radius, want)
	}

	// Run out: the cloud goes, and the viewer is told.
	gone := false
	for i := 0; i < cloudLingerTicks && len(h.clouds) > 0; i++ {
		h.updateClouds(players)
		gone = goneFor(pl, c.eid) // (draining as it goes)
	}
	if len(h.clouds) != 0 {
		t.Fatal("the cloud outlived its duration")
	}
	if !gone {
		t.Fatal("the viewer was never told the cloud went")
	}
}

// Each creature it doses costs the cloud half a block of radius
// (radiusOnUse), and one creature is dosed again only after the
// reapplication delay.
func TestLingeringCloudShrinksPerDose(t *testing.T) {
	h, players, pl, c := cloudFixture(t)
	pl.x, pl.z = c.x, c.z
	for i := 0; i < cloudLingerWait; i++ {
		h.updateClouds(players)
	}
	if pl.hasEffect(effSpeed) == 0 {
		t.Fatal("the player in the cloud was not dosed when the wait ended")
	}
	if want := float32(3 - 3.0/600 - 0.5); math.Abs(float64(c.radius-want)) > 1e-4 {
		t.Fatalf("radius %v after a tick's shrink and one dose, want %v", c.radius, want)
	}
	for i := 0; i < cloudReapply-1; i++ {
		h.updateClouds(players)
	}
	if c.victims[pl.p.eid] != cloudLingerWait+cloudReapply {
		t.Fatalf("the player should wait out the reapplication delay: %v", c.victims)
	}
	r := c.radius
	h.updateClouds(players) // tick 30: dosed again
	if c.radius > r-0.49 {
		t.Fatalf("a second dose after the delay should cost another half block: %v → %v", r, c.radius)
	}
}

// Instant Harming in a cloud hurts at half strength (3 for Harming I), as
// magic, and only whoever stands in its half-block-tall disc: a player a
// block above it is not touched.
func TestHarmingCloudHurtsAtHalfStrength(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	in := survivalAt(0.5, 180, 0.5)
	above := survivalAt(1.5, 181, 0.5)
	in.p.eid = 9000
	above.p = newPlayer(9001, "above", [16]byte{2})
	initSurvival(above)
	above.gamemode = gmSurvival
	players := map[int32]*tracked{in.p.eid: in, above.p.eid: above}
	h.playersRef = players
	h.spawnPotionCloud(0, 0.5, 180, 0.5, potHarming)
	in.health, above.health = 20, 20
	for i := 0; i < cloudLingerWait; i++ {
		h.updateClouds(players)
	}
	if in.health != 17 {
		t.Fatalf("Harming I in a cloud should cost 3, health %v", in.health)
	}
	if above.health != 20 {
		t.Fatalf("a player a block above the disc was hurt: %v", above.health)
	}
}

// A creeper carrying effects leaves them behind in a cloud when it blows.
func TestCreeperWithEffectsLeavesACloud(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	m := h.spawnHostileY(players, entityCreeper, 0.5, 180, 0.5)
	h.applyMobEffectTicks(players, m, effSpeed, 1, 400)
	h.explodeCreeper(players, m)
	if len(h.clouds) != 1 {
		t.Fatalf("a creeper with an effect should leave one cloud, have %d", len(h.clouds))
	}
	for _, c := range h.clouds {
		if c.radius != cloudCreeperRadius || c.duration != cloudCreeperTicks || c.waitTime != cloudLingerWait {
			t.Fatalf("creeper cloud %+v", c)
		}
		if len(c.effects) != 1 || c.effects[0].id != effSpeed || c.effects[0].amp != 1 {
			t.Fatalf("the cloud should carry the creeper's Speed II: %+v", c.effects)
		}
	}
	plain := h.spawnHostileY(players, entityCreeper, 40.5, 180, 0.5)
	h.explodeCreeper(players, plain)
	if len(h.clouds) != 1 {
		t.Fatal("a creeper with no effects left a cloud")
	}
}

// The dragon fireball's cloud grows from 3 to 7 over its 600 ticks.
func TestDragonFireballCloudGrows(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	h.spawnFireballCloud(players, &arrowEntity{dim: 0, x: 0.5, y: 180, z: 0.5})
	var c *effectCloud
	for _, x := range h.clouds {
		c = x
	}
	if c == nil || !c.ownerDragon || !c.breath || c.radius != fireballRadius {
		t.Fatalf("fireball cloud %+v", c)
	}
	for i := 0; i < cloudWaitDefault+300; i++ {
		h.updateClouds(players)
	}
	if want := float32(fireballRadius + 300*(fireballRadiusTo-fireballRadius)/fireballCloudTicks); math.Abs(float64(c.radius-want)) > 0.01 {
		t.Fatalf("radius %v halfway through, want %v", c.radius, want)
	}
}

// DATA_PARTICLE's encoding, for when the gateways can carry it: index 10,
// the PARTICLE serializer, ENTITY_EFFECT and the potion's opaque colour.
func TestCloudParticleMeta(t *testing.T) {
	c := &effectCloud{eid: 5, color: 0x33EBFF}
	r := bytes.NewReader(cloudParticleMeta(c))
	eid, _ := protocol.ReadVarInt(r)
	idx, _ := r.ReadByte()
	typ, _ := protocol.ReadVarInt(r)
	pid, _ := protocol.ReadVarInt(r)
	var argb [4]byte
	r.Read(argb[:])
	if eid != 5 || idx != 10 || typ != 17 || pid != particleEntityEffect || binary.BigEndian.Uint32(argb[:]) != 0xFF33EBFF {
		t.Fatalf("particle entry eid %d index %d type %d particle %d colour %x", eid, idx, typ, pid, argb)
	}
}
