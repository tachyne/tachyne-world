package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// An active vault shows one of its table's items; brushing uncovers an item.
func TestBlockDisplays(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	v := &vaultRecord{pos: blockPos{int(pl.x), int(pl.y), int(pl.z)}, state: vaultActive}
	drainOut(pl.p)
	h.vaultDisplay(players, v, 40, true)
	var shown string
	for _, ev := range drainEvs(pl.p) {
		if d, ok := ev.(attachproto.BlockDisplay); ok && d.Kind == attachproto.DisplayVault {
			shown = d.Name
		}
	}
	if shown == "" {
		t.Fatal("an active vault should show an item from its table")
	}
	v.state = vaultInactive
	h.vaultDisplay(players, v, 41, true)
	for _, ev := range drainEvs(pl.p) {
		if d, ok := ev.(attachproto.BlockDisplay); ok && d.Name != "" {
			t.Fatal("an inactive vault shows nothing")
		}
	}
}

// A stroke that moves the dust on shows the item with the block's dusted
// stage and which suspicious block it is (Bedrock draws both from the tag).
func TestBrushDisplayCarriesTheDust(t *testing.T) {
	h, w := findWell(t)
	pos := blockPos{w.Sus[0][0], w.Sus[0][1], w.Sus[0][2]}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(pos.x), float64(pos.y), float64(pos.z)
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}
	stroke := evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1}
	for i := 0; i < 3; i++ { // dust stages 1 then 2
		h.tick.Store(uint64(i) * brushCooldown)
		h.brush(players, pl, stroke)
	}
	var last *attachproto.BlockDisplay
	for _, ev := range drainEvs(pl.p) {
		if d, ok := ev.(attachproto.BlockDisplay); ok && d.Kind == attachproto.DisplayBrushable {
			last = &d
		}
	}
	if last == nil {
		t.Skip("this well's cell buried nothing")
	}
	if last.Dusted != 2 || last.Block != "minecraft:suspicious_sand" || last.HitDir != 2 {
		t.Errorf("display %+v, want dusted 2 of suspicious_sand out of the top (hit_dir 1+1)", *last)
	}
}
