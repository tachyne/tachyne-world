package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// vehRig is a stone floor at y=179 with a rail at (0,180,0) and a survival
// player standing a few blocks back.
func vehRig(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, -4
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
			for y := 180; y <= 184; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	w.SetBlock(0, 180, 0, railMin+1)
	return h, pl, players
}

// rigVehicle places one vehicle of the kind at (0,180,0).
func rigVehicle(t *testing.T, h *hub, players map[int32]*tracked, name string) *vehicle {
	t.Helper()
	if name != "minecart" && name != "tnt_minecart" {
		h.worldFor(0).SetBlock(0, 180, 0, worldgen.WaterBase)
	}
	if !h.spawnVehicleAt(players, 0, entityByName[name], 0, 180, 0) {
		t.Fatalf("could not place a %s", name)
	}
	return firstVehicle(h)
}

func vehicleItemDropped(h *hub, name string) bool {
	for _, it := range h.items {
		if it.item == itemByName[name] {
			return true
		}
	}
	return false
}

// An arrow breaks a minecart it hits hard enough (6 × 10 > 40) and the
// cart drops as an item; a snowball deals nothing but still rocks it.
func TestVehicleProjectileHits(t *testing.T) {
	h, pl, players := vehRig(t)
	v := rigVehicle(t, h, players, "minecart")
	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 180.3, -2, 0, 0, 1)
	a.shooter, a.playerShot, a.dmg = pl.p.eid, true, 6
	for i := 0; i < 6 && len(h.vehicles) == 1; i++ {
		h.updateArrows(players)
	}
	if len(h.vehicles) != 0 {
		t.Fatal("an arrow did not break the minecart")
	}
	if h.arrows[a.eid] != nil {
		t.Error("the arrow should be spent on the cart")
	}
	if !vehicleItemDropped(h, "minecart") {
		t.Error("a minecart broken by an arrow drops its item")
	}

	h, pl, players = vehRig(t)
	v = rigVehicle(t, h, players, "minecart")
	s := h.launchProjectileIn(players, entitySnowball, 0, 0.5, 180.3, -2, 0, 0, 1)
	s.shooter, s.playerShot = pl.p.eid, true
	for i := 0; i < 6 && v.hurtTime == 0; i++ {
		h.updateArrows(players)
	}
	if len(h.vehicles) != 1 || v.hurtTime != vehHurtTicks || v.damage != 0 {
		t.Fatalf("a snowball should rock the cart without damage: vehicles=%d time=%d damage=%.1f",
			len(h.vehicles), v.hurtTime, v.damage)
	}
}

// A rider's own arrow never strikes the vehicle under them.
func TestVehicleRiderArrowPassesOwnVehicle(t *testing.T) {
	h, pl, players := vehRig(t)
	v := rigVehicle(t, h, players, "minecart")
	v.rider = pl.p.eid
	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 180.3, -2, 0, 0, 1)
	a.shooter, a.playerShot, a.dmg = pl.p.eid, true, 6
	for i := 0; i < 6; i++ {
		h.updateArrows(players)
	}
	if len(h.vehicles) != 1 || v.hurtTime != 0 {
		t.Fatal("the rider's own arrow hit the vehicle it was shot from")
	}
}

// A blast breaks the vehicles in it; a creeper's blast leaves them alone
// when mobGriefing is off (VehicleEntity.ignoreExplosion).
func TestVehicleExplosion(t *testing.T) {
	h, _, players := vehRig(t)
	rigVehicle(t, h, players, "oak_boat")
	h.explodeIn(players, 0, 2.5, 180, 0.5, 0, 4, blastTNT)
	if len(h.vehicles) != 0 {
		t.Fatal("a TNT blast two blocks away did not break the boat")
	}
	if !vehicleItemDropped(h, "oak_boat") {
		t.Error("a boat broken by a blast drops its item")
	}

	for _, griefing := range []bool{false, true} {
		h, _, players = vehRig(t)
		h.rules.MobGriefing = griefing
		v := rigVehicle(t, h, players, "minecart")
		c := h.spawnMob(players, entityCreeper, 2.5, 180, 0.5)
		h.explodeCreeper(players, c)
		if griefing && len(h.vehicles) != 0 {
			t.Error("a creeper blast did not break the cart")
		}
		if !griefing && (len(h.vehicles) != 1 || v.hurtTime != 0) {
			t.Error("a creeper blast hurt a cart with mobGriefing off")
		}
	}
}

// Lava breaks a minecart within the tick (4 twice, ×10); fire burns a boat
// down in a few ticks and sets it alight.
func TestVehicleLavaAndFire(t *testing.T) {
	h, _, players := vehRig(t)
	rigVehicle(t, h, players, "minecart")
	h.worldFor(0).SetBlock(0, 180, 0, worldgen.LavaBase)
	h.updateVehicles(players)
	if len(h.vehicles) != 0 {
		t.Fatal("lava did not destroy the minecart in a tick")
	}

	h, _, players = vehRig(t)
	v := rigVehicle(t, h, players, "oak_boat")
	h.worldFor(0).SetBlock(0, 180, 0, fireDefault)
	h.updateVehicles(players)
	if len(h.vehicles) != 1 || !v.burning || v.hurtTime != vehHurtTicks {
		t.Fatalf("one tick in fire should rock the boat and set it alight: vehicles=%d burning=%v", len(h.vehicles), v.burning)
	}
	for i := 0; i < 4 && len(h.vehicles) == 1; i++ {
		h.updateVehicles(players)
	}
	if len(h.vehicles) != 0 {
		t.Fatal("fire did not burn the boat down")
	}
}
