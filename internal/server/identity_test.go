package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/attach"
	wattach "github.com/tachyne/tachyne-world/internal/attach"
	"github.com/tachyne/tachyne-world/internal/world"
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

// A joining player is sent their own tab-list entry, textures and all: the
// client draws its own skin from it (else a default skin by UUID), and it is
// the only entry a player alone on the server gets.
func TestJoinSendsOwnPlayerInfoWithSkin(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	p := newPlayer(h.allocEID(), "EdgeZA", [16]byte{0x43, 0x08})
	p.props = []skinProperty{{Name: "textures", Value: "dGV4", Signature: "c2ln"}}
	h.onJoin(players, evJoin{p: p, x: 0.5, y: 80, z: 0.5, gamemode: gmSurvival})
	var own *attach.PlayerInfo
	for _, ev := range drainEvs(p) {
		if pi, ok := ev.(attach.PlayerInfo); ok && pi.UUID == p.uuid {
			own = &pi
		}
	}
	if own == nil {
		t.Fatal("the joining player never got their own player-list entry")
	}
	if len(own.Props) != 1 || own.Props[0].Name != "textures" || own.Props[0].Signature != "c2ln" {
		t.Errorf("own entry props %+v, want the signed textures", own.Props)
	}
}
