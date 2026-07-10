package server

import (
	"encoding/json"
	"reflect"
	"testing"

	ho "github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/shard"
)

// TestPlayerHandoverRoundTrip is the fidelity guard: a live tracked snapshotted
// into a PlayerState, sent over the wire (JSON), and applied to a fresh tracked
// on the "destination pod" must preserve every field that has to survive a
// crossing. If you add a tracked field that must not reset on a seam crossing,
// add it to playerStateOf/applyPlayerState AND to the assertions here.
func TestPlayerHandoverRoundTrip(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 15}
	eid := shard.MintEID(5, shard.PlayerSID) // session-stable player-lane eid

	orig := &tracked{
		p:   newPlayer(eid, "wesley", uuid),
		dim: 0, x: -12.5, y: 71, z: 0.25, yaw: 90, pitch: -10,
		onGround: true, sprinting: true, airborne: false, peakY: 80,
		gamemode:   gmSurvival,
		health:     18.5,
		absorption: 4,
		food:       17,
		saturation: 2.5,
		exhaustion: 1.2,
		air:        280,
		fireSecs:   3,
		xpLevel:    12,
		xpPoints:   7,
		inv:        &inventory{},
		effects: map[int32]*activeEffect{
			effSpeed:  {amp: 1, left: 45},
			effPoison: {amp: 0, left: 8},
		},
	}
	orig.inv.slots[0] = invStack{item: 278, count: 1, dmg: 12, ench: [2]enchApply{{id: 3, lvl: 4}}}
	orig.inv.slots[9] = invStack{item: 1, count: 64}
	orig.armor[3] = invStack{item: 310, count: 1, dmg: 40}
	orig.offhand = invStack{item: 289, count: 16}

	// snapshot -> wire -> snapshot
	ps := playerStateOf(orig, "wesley", uuid)
	b, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire ho.PlayerState
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// apply on the destination
	dst := &tracked{p: newPlayer(0, "", [16]byte{})}
	dst.applyPlayerState(wire)

	// (1) the eid is preserved (no player despawn/respawn across the crossing)
	if dst.p.eid != orig.p.eid {
		t.Errorf("eid: got %d want %d", dst.p.eid, orig.p.eid)
	}
	if shard.Minter(dst.p.eid) != shard.PlayerSID {
		t.Errorf("resumed eid %d is not in the player lane", dst.p.eid)
	}

	// (2) live survival state carried (would reset if the transport used the
	// on-disk subset)
	if dst.health != orig.health || dst.absorption != orig.absorption ||
		dst.food != orig.food || dst.saturation != orig.saturation ||
		dst.exhaustion != orig.exhaustion || dst.air != orig.air ||
		dst.fireSecs != orig.fireSecs {
		t.Errorf("survival state not preserved: dst=%+v", dst)
	}
	if dst.xpLevel != orig.xpLevel || dst.xpPoints != orig.xpPoints {
		t.Errorf("xp not preserved: %d/%d", dst.xpLevel, dst.xpPoints)
	}
	if dst.dim != orig.dim || dst.x != orig.x || dst.y != orig.y || dst.z != orig.z ||
		dst.yaw != orig.yaw || dst.pitch != orig.pitch || dst.gamemode != orig.gamemode {
		t.Errorf("pose/gamemode not preserved")
	}

	// (3) effects (map order-independent)
	if !reflect.DeepEqual(dst.effects, orig.effects) {
		t.Errorf("effects not preserved:\n got %v\nwant %v", dst.effects, orig.effects)
	}

	// (4) inventory (name/potion intentionally dropped — not set here, so the
	// stacks compare equal)
	if dst.inv.slots[0] != orig.inv.slots[0] || dst.inv.slots[9] != orig.inv.slots[9] ||
		dst.armor[3] != orig.armor[3] || dst.offhand != orig.offhand {
		t.Errorf("inventory not preserved")
	}

	// (5) full inverse: re-snapshotting the destination yields the same wire form
	if got := playerStateOf(dst, "wesley", uuid); !reflect.DeepEqual(got, wire) {
		t.Errorf("re-snapshot differs from wire:\n got %+v\nwant %+v", got, wire)
	}
}

// A crossing must drop item rename/potion (the [4]int32 pack has no room) — this
// documents that known, accepted loss so it is a deliberate choice, not a
// surprise regression.
func TestHandoverDropsNameAndPotion(t *testing.T) {
	orig := &tracked{p: newPlayer(1, "n", [16]byte{}), inv: &inventory{}}
	orig.inv.slots[0] = invStack{item: 276, count: 1, name: "Excalibur", potion: 5}

	ps := playerStateOf(orig, "n", [16]byte{})
	dst := &tracked{p: newPlayer(0, "", [16]byte{})}
	dst.applyPlayerState(ps)

	got := dst.inv.slots[0]
	if got.item != 276 || got.count != 1 {
		t.Fatalf("item/count should survive, got %+v", got)
	}
	if got.name != "" || got.potion != 0 {
		t.Errorf("expected name/potion dropped (relog-equivalent), got name=%q potion=%d", got.name, got.potion)
	}
}
