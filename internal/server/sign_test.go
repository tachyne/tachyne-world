package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"tachyne/internal/world"
)

const (
	oakSignRot0  = 5135 // oak_sign rotation=0 waterlogged=false
	oakWallNorth = 5627 // oak_wall_sign facing=north waterlogged=false
	oakCeilHang  = 5739 // oak_hanging_sign attached=false rotation=0
	oakWallHangN = 6475 // oak_wall_hanging_sign facing=north
)

func drainSign(pl *tracked) (texts []attachproto.SignText, editors []attachproto.SignEditor) {
	for {
		select {
		case pkt := <-pl.p.out:
			switch v := pkt.ev.(type) {
			case attachproto.SignText:
				texts = append(texts, v)
			case attachproto.SignEditor:
				editors = append(editors, v)
			}
		default:
			return
		}
	}
}

func TestSignStateMath(t *testing.T) {
	if r := yawToRotation16(0 + 180); r != 8 {
		t.Fatalf("south-facing player: rotation %d, want 8", r)
	}
	if r := yawToRotation16(-90 + 180); r != 4 {
		t.Fatalf("east-facing player: rotation %d, want 4", r)
	}
	if k, ok := signKind(oakSignRot0); !ok || k != signStanding {
		t.Fatalf("oak_sign kind %d ok=%v", k, ok)
	}
	if k, ok := signKind(oakWallNorth); !ok || k != signWall {
		t.Fatalf("oak_wall_sign kind %d ok=%v", k, ok)
	}
	if k, ok := signKind(oakCeilHang); !ok || k != signHangingCeiling {
		t.Fatalf("oak_hanging_sign kind %d ok=%v", k, ok)
	}
	if k, ok := signKind(oakWallHangN); !ok || k != signHangingWall {
		t.Fatalf("oak_wall_hanging_sign kind %d ok=%v", k, ok)
	}
	if _, ok := signKind(1); ok {
		t.Fatal("stone classified as a sign")
	}
	if a := signAngle(oakSignRot0); a != 0 {
		t.Fatalf("rotation-0 sign angle %v", a)
	}
	if a := signAngle(oakWallNorth); a != 180 {
		t.Fatalf("north wall sign angle %v", a)
	}
	if s := sanitizeSignLine("hello §cred§"); s != "hello red" {
		t.Fatalf("sanitize: %q", s)
	}
}

// TestSignEditFlow drives placement registration, the edit lock, text
// submission, applicators and waxing through the hub handlers.
func TestSignEditFlow(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.z = 0.5, 3.5 // south of the sign → front side
	players[1] = pl
	h.world.SetBlock(0, 70, 0, oakSignRot0)

	h.onSignPlaced(players, evSignPlaced{eid: 1, x: 0, y: 70, z: 0})
	texts, editors := drainSign(pl)
	if len(texts) != 1 || len(editors) != 1 || !editors[0].Front {
		t.Fatalf("placement: %d texts %d editors %+v", len(texts), len(editors), editors)
	}

	// the placer holds the lock → the update applies, §-codes stripped
	h.onSignUpdate(players, evSignUpdate{eid: 1, x: 0, y: 70, z: 0, front: true,
		lines: [4]string{"§6WELCOME", "to tachyne", "", ""}})
	texts, _ = drainSign(pl)
	if len(texts) != 1 || texts[0].Front.Lines[0] != "WELCOME" || texts[0].Front.Lines[1] != "to tachyne" {
		t.Fatalf("update broadcast: %+v", texts)
	}
	sd, ok := h.signs.get(0, 0, 70, 0)
	if !ok || sd.Front.Lines[0] != "WELCOME" {
		t.Fatalf("stored: %+v ok=%v", sd, ok)
	}

	// the lock cleared on success → a second stray update is rejected
	h.onSignUpdate(players, evSignUpdate{eid: 1, x: 0, y: 70, z: 0, front: true,
		lines: [4]string{"overwrite", "", "", ""}})
	if sd, _ := h.signs.get(0, 0, 70, 0); sd.Front.Lines[0] != "WELCOME" {
		t.Fatalf("lockless update applied: %+v", sd)
	}

	// dye the written front side, then glow it
	h.onUseSign(players, evUseSign{eid: 1, x: 0, y: 70, z: 0, item: itemByName["red_dye"]})
	h.onUseSign(players, evUseSign{eid: 1, x: 0, y: 70, z: 0, item: itemGlowInkSac})
	sd, _ = h.signs.get(0, 0, 70, 0)
	if sd.Front.Color != "red" || !sd.Front.Glow {
		t.Fatalf("applicators: %+v", sd.Front)
	}
	if sd.Back.Color != "" || sd.Back.Glow {
		t.Fatalf("applicators leaked to the back side: %+v", sd.Back)
	}

	// dye on the blank back side does nothing (vanilla canApplyToSign)
	pl.z = -2.5 // move north of the sign → back side
	h.onUseSign(players, evUseSign{eid: 1, x: 0, y: 70, z: 0, item: itemByName["lime_dye"]})
	if sd, _ := h.signs.get(0, 0, 70, 0); sd.Back.Color != "" {
		t.Fatalf("dye applied to a blank side: %+v", sd.Back)
	}

	// wax it — further edits and applicators refuse, the editor stays shut
	h.onUseSign(players, evUseSign{eid: 1, x: 0, y: 70, z: 0, item: itemHoneycomb})
	sd, _ = h.signs.get(0, 0, 70, 0)
	if !sd.Waxed {
		t.Fatal("honeycomb did not wax")
	}
	drainSign(pl)
	h.onUseSign(players, evUseSign{eid: 1, x: 0, y: 70, z: 0})
	if _, editors := drainSign(pl); len(editors) != 0 {
		t.Fatal("editor opened on a waxed sign")
	}
	h.signMayEdit[signKey(0, 0, 70, 0)] = 1 // even a forged lock can't edit wax
	h.onSignUpdate(players, evSignUpdate{eid: 1, x: 0, y: 70, z: 0, front: true,
		lines: [4]string{"vandalism", "", "", ""}})
	if sd, _ := h.signs.get(0, 0, 70, 0); sd.Front.Lines[0] != "WELCOME" {
		t.Fatalf("waxed sign edited: %+v", sd)
	}
}

// TestSignChunkNBT verifies the chunk block-entity section carries the sign's
// vanilla update tag.
func TestSignChunkNBT(t *testing.T) {
	w := world.New(1)
	w.SetBlock(0, 70, 0, oakSignRot0)
	store := newSignStore("")
	store.set(0, 0, 70, 0, signData{Front: signSide{Lines: [4]string{"hi", "", "", ""}, Color: "red"}})
	b := appendBlockEntities(nil, w, 0, 0, 0, store)
	if !bytes.Contains(b, []byte("front_text")) || !bytes.Contains(b, []byte("hi")) || !bytes.Contains(b, []byte("red")) {
		t.Fatalf("sign NBT missing from chunk section: %x", b)
	}
	// an unknown sign (no store entry) still gets a well-formed blank tag
	b2 := appendBlockEntities(nil, w, 0, 0, 0, newSignStore(""))
	if !bytes.Contains(b2, []byte("is_waxed")) {
		t.Fatalf("blank sign tag malformed: %x", b2)
	}
}
