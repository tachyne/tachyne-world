package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/attach"
	wattach "github.com/tachyne/tachyne-world/internal/attach"
)

// A player whose access roles include op is an operator for as long as that
// is what its gateway says; its skin rides the player-list entry.
func TestAdoptIdentityRolesAndSkin(t *testing.T) {
	s := &Server{Ops: map[string]bool{"LegionZA": true}}
	p := newPlayer(1, "EdgeZA", [16]byte{1})
	s.adoptIdentity(p, wattach.Identity{Name: "EdgeZA", Roles: []string{"op"},
		Props: []attach.Property{{Name: "textures", Value: "v", Signature: "s"}}})
	if !s.isOp("EdgeZA") || !s.isOp("LegionZA") || s.isOp("asananica81") {
		t.Error("op should come from the role or the -ops list, and nowhere else")
	}
	pi := infoAdd(p, gmSurvival)
	if len(pi.Props) != 1 || pi.Props[0].Name != "textures" || pi.Props[0].Signature != "s" {
		t.Errorf("player-list entry props %+v, want the textures", pi.Props)
	}
	// Joining again without the role takes it away.
	s.adoptIdentity(newPlayer(2, "EdgeZA", [16]byte{1}), wattach.Identity{Name: "EdgeZA"})
	if s.isOp("EdgeZA") {
		t.Error("a player whose role was revoked is still an operator")
	}
}
