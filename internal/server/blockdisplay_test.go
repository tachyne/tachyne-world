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
