package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BoatItem.use turns the new boat to the placer's yaw (setYRot(player.getYRot())):
// a boat put on water came out facing south whichever way its owner looked.
func TestPlacedBoatFacesThePlacer(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 135, 30 // looking down onto the pond, north-west
	boat := int32(itemByName["oak_boat"])
	pl.inv.slots[0] = invStack{item: boat, count: 1}
	pl.p.setHotbarSlot(0, boat)
	for x := -4; x <= 1; x++ {
		for z := -4; z <= 1; z++ {
			h.world.SetBlock(x, 179, z, worldgen.WaterBase)
		}
	}
	r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
	r.Action(attachproto.UseItem{Hand: 0})
	for len(h.events) > 0 {
		if e, ok := (<-h.events).(evPlaceVehicleLook); ok {
			h.placeVehicleFromLook(players, pl, e.item, e.slot)
		}
	}
	if len(h.vehicles) != 1 {
		t.Fatalf("one boat should be on the water, have %d", len(h.vehicles))
	}
	for _, v := range h.vehicles {
		if v.yaw != 135 {
			t.Fatalf("the boat faces %v, want the placer's 135", v.yaw)
		}
	}
}
