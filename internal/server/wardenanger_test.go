package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A Warden keeps a grudge per suspect: what it hears raises it, a blow
// raises it a lot, it goes for whoever it is angriest at, and the anger ebbs
// while nothing happens.
func TestWardenAngerManagement(t *testing.T) {
	h := newHub(world.New(1))
	loud := testTracked()
	loud.p.name, loud.p.eid = "Loud", 500 // well clear of the mob eids
	loud.x, loud.y, loud.z = 6, 70, 0
	quiet := testTracked()
	quiet.p.name, quiet.p.eid = "Quiet", 501
	quiet.x, quiet.y, quiet.z = 2, 70, 0
	players := map[int32]*tracked{loud.p.eid: loud, quiet.p.eid: quiet}
	m := h.spawnMob(players, entityWarden, 0.5, 70, 0.5)

	// Nothing heard yet: no grudge, so no angry target.
	if got := h.wardenAngerTickOne(players, m); got != nil {
		t.Fatalf("a calm Warden has nobody to be angry at, got %s", got.p.name)
	}
	// Three disturbances from the far player: 105 anger, past ANGRY.
	for i := 0; i < 3; i++ {
		h.wardenHeard(m.dim, loud.x, loud.y, loud.z, loud.p.eid)
	}
	if got := m.wardenAnger[loud.p.eid]; got != 3*wardenAngerHeard {
		t.Fatalf("three disturbances = %d anger, got %d", 3*wardenAngerHeard, got)
	}
	if got := h.wardenAngerTickOne(players, m); got != loud {
		t.Fatal("it should go for the noisy one, not the near one")
	}
	// Two blows from the quiet one outweigh that.
	h.wardenAngerAt(m, quiet.p.eid, wardenAngerHurt)
	h.wardenAngerAt(m, quiet.p.eid, wardenAngerHurt)
	if got := h.wardenAngerTickOne(players, m); got != quiet {
		name := "nobody"
		if got != nil {
			name = got.p.name
		}
		t.Fatalf("whoever hits it hardest becomes the target, got %s", name)
	}
	// Anger is capped, and ebbs while nothing happens.
	for i := 0; i < 10; i++ {
		h.wardenAngerAt(m, quiet.p.eid, wardenAngerHurt)
	}
	if got := m.wardenAnger[quiet.p.eid]; got != wardenAngerMax {
		t.Errorf("anger caps at %d, got %d", wardenAngerMax, got)
	}
	before := m.wardenAnger[loud.p.eid]
	for i := 0; i < wardenAngerTick/mobMoveInterval; i++ {
		h.wardenAngerTickOne(players, m)
	}
	if m.wardenAnger[loud.p.eid] >= before {
		t.Errorf("the grudge should ebb: %d → %d", before, m.wardenAnger[loud.p.eid])
	}
	// A Warden ignores its own noise.
	m.wardenAnger = map[int32]int{}
	h.wardenHeard(m.dim, m.x, m.y, m.z, m.eid)
	if len(m.wardenAnger) != 0 {
		t.Error("it does not get angry at itself")
	}
}

// CLIENT_ANGER_LEVEL is synced: the client paces the heartbeat and twitches
// the tendrils from it. Viewers get it when it changes, and a late viewer
// gets the current value with the rest of the Warden's state.
func TestWardenSyncsClientAnger(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -6; x <= 12; x++ {
		for z := -6; z <= 6; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 185; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.p.eid = 500 // well clear of the mob eids
	pl.x, pl.y, pl.z = 10.5, 180, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnHostileY(players, entityWarden, 0.5, 180, 0.5)
	pl.tracked = map[int32]bool{m.eid: true}
	drainEvs(pl.p)
	for i := 0; i < 3; i++ {
		h.wardenHeard(m.dim, pl.x, pl.y, pl.z, pl.p.eid)
	}
	var evs []any
	for i := 0; i < 5; i++ {
		h.updateMobs(players)
		evs = append(evs, drainEvs(pl.p)...)
	}
	var sent []int32
	for _, ev := range evs {
		if me, ok := ev.(attachproto.EntityMeta); ok && me.EID == m.eid && len(me.Meta) >= 3 && me.Meta[0] == metaIndexWardenAnger {
			v, _ := protocol.ReadVarInt(bytes.NewReader(me.Meta[2:]))
			sent = append(sent, v)
		}
	}
	if len(sent) == 0 || sent[len(sent)-1] < wardenAngerAngry {
		t.Fatalf("an angry Warden syncs CLIENT_ANGER_LEVEL at index 16, sent %v", sent)
	}
	if sm := speciesStateMeta(m); sm == nil || string(sm) != string(wardenAngerMeta(m.eid, m.wardenClientAnger)) {
		t.Fatal("a late viewer is sent the Warden's current anger")
	}
}
