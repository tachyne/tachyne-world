package server

import (
	"bytes"
	"math"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// A summoned stand takes the Pose compound (each part optional, a part
// wraps at 360°), syncs it as six ROTATIONS entries after the client flags,
// and keeps it across a save; a malformed Pose changes nothing.
func TestArmorStandSummonPose(t *testing.T) {
	h, players, _ := standFixture(t)
	nbt, err := parseSNBT(`{Pose:{Head:[10f,20f,30f],LeftArm:[400f,0f,-5f]}}`)
	if err != nil {
		t.Fatal(err)
	}
	h.summonAt(players, evSummon{etype: entityByName["armor_stand"], x: 1.5, y: 180, z: 1.5, nbt: nbt.(map[string]any)})
	var st *armorStand
	for _, s := range h.armorStands {
		if s.pose != nil {
			st = s
		}
	}
	if st == nil {
		t.Fatal("no posed stand")
	}
	want := defaultStandPose
	want[0] = [3]float32{10, 20, 30}
	want[2] = [3]float32{40, 0, -5}
	if st.poseOf() != want {
		t.Fatalf("pose %v", st.poseOf())
	}
	meta := standMeta(st)
	var wantTail []byte
	for i, r := range want {
		wantTail = append(wantTail, byte(16+i), 9)
		for _, c := range r {
			wantTail = protocol.AppendF32(wantTail, c)
		}
	}
	wantTail = append(wantTail, 0xff)
	if !bytes.HasSuffix(meta, wantTail) {
		t.Fatalf("pose metadata: % x", meta)
	}
	cs := newContainerStore("")
	cs.recordStands(map[int32]*armorStand{st.eid: st})
	for _, l := range cs.loadStands(func() int32 { return 999 }) {
		if l.poseOf() != want {
			t.Fatalf("the pose did not survive a save: %v", l.poseOf())
		}
	}

	bad := &armorStand{}
	bad.setStandFlagsFrom(map[string]any{"Pose": map[string]any{"Head": []any{1.0, 2.0}}})
	if bad.pose != nil {
		t.Fatal("a Pose part without three numbers fails the compound")
	}
	if rotationDeg(math.Inf(1)) != 0 || rotationDeg(-370) != -10 {
		t.Fatal("Rotations: infinite is 0, the rest mod 360")
	}
}

// ArmorStandItem places a bare stand (no arms) unless the item's
// entity_data says otherwise: /give's entity_data for an armor stand
// carries ShowArms and a Pose onto the placed stand, rides a saved row and
// a drop, and entity_data for another entity type does nothing.
func TestArmorStandItemEntityData(t *testing.T) {
	st, msg := parseItemArg(`armor_stand[entity_data={id:"minecraft:armor_stand",ShowArms:1b,Pose:{RightArm:[-90f,0f,0f]}}]`)
	if msg != "" {
		t.Fatal(msg)
	}
	if st.standTags == "" || unpackStack(packStack(st)).standTags != st.standTags {
		t.Fatalf("entity_data tags %q do not survive a row", st.standTags)
	}
	it := &itemEntity{}
	it.setFrom(st)
	if it.stack().standTags != st.standTags {
		t.Fatal("entity_data lost on the ground")
	}
	other, msg := parseItemArg(`armor_stand[entity_data={id:"minecraft:pig",ShowArms:1b}]`)
	if msg != "" || other.standTags != "" {
		t.Fatalf("another type's entity_data: %q %q", msg, other.standTags)
	}

	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		bx, bz, by := int(tr.x)+2, int(tr.z), int(tr.y)
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemArmorStand, count: 1}
		h.onPlaceStand(h.playersRef, evPlaceStand{eid: p.eid, x: bx, y: by, z: bz})
		var plain *armorStand
		for _, s := range h.armorStands {
			plain = s
		}
		if plain == nil || plain.arms || plain.pose != nil {
			t.Errorf("a plain item places an armless, unposed stand: %+v", plain)
			return
		}
		st.count = 1
		tr.inv.slots[tr.p.heldSlot()] = st
		h.onPlaceStand(h.playersRef, evPlaceStand{eid: p.eid, x: bx, y: by, z: bz + 2})
		var armed *armorStand
		for _, s := range h.armorStands {
			if s != plain {
				armed = s
			}
		}
		want := defaultStandPose
		want[3] = [3]float32{-90, 0, 0}
		if armed == nil || !armed.arms || armed.poseOf() != want {
			t.Errorf("entity_data places an armed, posed stand: %+v", armed)
		}
	})
}
