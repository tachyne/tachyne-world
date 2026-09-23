package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Which blocks carry a block entity used to be guessed from their names, and
// the guess missed blocks the client can only draw through one. An end portal
// and an end gateway have no model at all (RenderShape.INVISIBLE) — their
// starfield IS the block entity — so a lit stronghold portal or the gateway
// after the dragon reached clients as nothing. The weathered and waxed copper
// golem statues were missed because the list knew only the plain one.
func TestBlocksThatRenderThroughABlockEntityCarryOne(t *testing.T) {
	for _, name := range []string{
		"end_portal", "end_gateway",
		"copper_golem_statue",
		"exposed_copper_golem_statue", "weathered_copper_golem_statue", "oxidized_copper_golem_statue",
		"waxed_copper_golem_statue", "waxed_exposed_copper_golem_statue",
		"waxed_weathered_copper_golem_statue", "waxed_oxidized_copper_golem_statue",
		// and a few the guess always had, so the table cannot lose them either
		// (not beds: 26.3 draws a bed without one, and has no bed block entity)
		"chest", "furnace", "oak_sign", "oak_hanging_sign", "white_banner", "shulker_box",
	} {
		if _, ok := protocol.BlockEntityType(worldgen.BlockID(name)); !ok {
			t.Errorf("%s has no block entity entry — the client has nothing to draw it with", name)
		}
	}
	// Every copper golem statue is the same block entity type, whatever its
	// oxidation or wax.
	plain, _ := protocol.BlockEntityType(worldgen.BlockID("copper_golem_statue"))
	for _, name := range []string{"oxidized_copper_golem_statue", "waxed_weathered_copper_golem_statue"} {
		if got, _ := protocol.BlockEntityType(worldgen.BlockID(name)); got != plain {
			t.Errorf("%s is block entity type %d, want the statue's %d", name, got, plain)
		}
	}
	// Plain blocks carry none.
	for _, name := range []string{"stone", "oak_planks", "glass"} {
		if _, ok := protocol.BlockEntityType(worldgen.BlockID(name)); ok {
			t.Errorf("%s should carry no block entity", name)
		}
	}
}
