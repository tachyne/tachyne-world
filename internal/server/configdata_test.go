package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// The world's dimension table, as the gateways take it, names the three
// dimensions exactly as their built-in default does — so a gateway renders
// the same login, registries and sky light whether it is sent the table or
// not — and a session's Welcome is given it.
func TestDimensionTableMatchesTheGatewayDefault(t *testing.T) {
	got := dimensionTable()
	want := protocol.DefaultDimensions
	if len(got) != len(want) {
		t.Fatalf("%d dimensions, want %d: %+v", len(got), len(want), got)
	}
	for i, d := range got {
		w := want[i]
		if d.ID != w.ID || d.Key != w.Key || d.Type != w.Type || d.SkyLight != w.SkyLight || d.Clock != w.Clock || d.TypeData != nil {
			t.Errorf("dimension %d: %+v, want %+v", i, d, w)
		}
	}
	s := &Server{}
	if cd := s.configData(); cd == nil || len(cd.Dimensions) != len(want) {
		t.Fatalf("configData %+v", cd)
	}
	r := &remotePlayer{s: s, p: newPlayer(1, "a", [16]byte{1})}
	r.p.dim = 2
	if r.Dim() != 2 {
		t.Errorf("Dim() = %d", r.Dim())
	}
}
