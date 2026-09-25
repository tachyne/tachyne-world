package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// PiglinAi.angerNearbyPiglins from its other callers: breaking a
// #guarded_by_piglins block (Block.playerWillDestroy, no sight needed),
// opening a chest minecart and breaking one by hand
// (MinecartChest.interact, ContainerEntity.chestVehicleDestroyed).
func TestPiglinsGuardBlocksAndChestVehicles(t *testing.T) {
	setup := func() (*hub, map[int32]*tracked, *tracked, *mob) {
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 2)
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				h.world.SetBlock(x, 199, z, worldgen.Stone)
				for y := 200; y < 204; y++ {
					h.world.SetBlock(x, y, z, worldgen.Air)
				}
			}
		}
		pl := survPlayer(h)
		pl.x, pl.y, pl.z = 0.5, 200, 0.5
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		pg := h.spawnMob(players, entityPiglin, 4.5, 200, 0.5)
		pg.baby, pg.hostile, pg.anger, pg.targetEID = false, false, 0, 0
		return h, players, pl, pg
	}
	angry := func(m *mob, pl *tracked) bool { return m.angryAt == pl.p.eid || m.targetEID == pl.p.eid }

	h, players, pl, pg := setup()
	gold := worldgen.BlockBase("gold_block")
	h.onBlock(players, evBlock{x: 1, y: 200, z: 0, dim: 0, state: worldgen.Air, by: pl.p.eid, broken: gold})
	if !angry(pg, pl) {
		t.Error("breaking a gold block should anger the piglin")
	}

	h, players, pl, pg = setup()
	h.world.SetBlock(0, 200, 1, railMin+1)
	h.spawnVehicleAt(players, 0, entityByName["chest_minecart"], 0, 200, 1)
	var cart *vehicle
	for _, v := range h.vehicles {
		cart = v
	}
	if cart == nil || cart.chest == nil {
		t.Fatal("no chest minecart")
	}
	h.openVehicleChest(players, pl, cart)
	if !angry(pg, pl) {
		t.Error("opening a chest minecart should anger the piglin that sees it")
	}

	h, players, pl, pg = setup()
	h.world.SetBlock(0, 200, 1, railMin+1)
	h.spawnVehicleAt(players, 0, entityByName["chest_minecart"], 0, 200, 1)
	for _, v := range h.vehicles {
		cart = v
	}
	h.destroyVehicle(players, cart, vehHit{by: pl, causer: pl.p.eid})
	if !angry(pg, pl) {
		t.Error("breaking a chest minecart by hand should anger the piglin")
	}
}
