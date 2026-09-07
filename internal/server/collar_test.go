package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A newly tamed wolf wears a red collar; the owner's dye recolours it and is
// spent; the collar rides in the mob's metadata at DATA_COLLAR_COLOR.
func TestPetCollarDye(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.spawnAnimal(players, entityWolf, 0, 0)
	if w == nil {
		t.Fatal("no wolf")
	}
	w.tamed, w.owner, w.collar = true, pl.p.eid, collarDefault
	blue := int32(itemByName["blue_dye"])
	pl.inv.slots[0] = invStack{item: blue, count: 2}
	pl.p.setHotbarSlot(0, blue)
	pl.p.held = 0
	if !h.tryTame(players, pl, w) || w.collar != 11 || pl.inv.slots[0].count != 1 {
		t.Fatalf("blue dye should recolour the collar: collar=%d left=%d", w.collar, pl.inv.slots[0].count)
	}
	if h.tryTame(players, pl, w) {
		t.Fatal("the same dye again does nothing")
	}
	meta := variantMeta(w)
	r := bytes.NewReader(meta)
	protocol.ReadVarInt(r) // eid
	idx, _ := r.ReadByte()
	typ, _ := protocol.ReadVarInt(r)
	val, _ := protocol.ReadVarInt(r)
	if idx != metaIndexWolfCollar || typ != metaTypeInt || val != 11 {
		t.Fatalf("collar metadata = idx %d typ %d val %d", idx, typ, val)
	}
	// Persistence keeps it.
	row := toSavedMob(w)
	if row.Collar != 11 {
		t.Fatalf("saved collar = %d", row.Collar)
	}
}
