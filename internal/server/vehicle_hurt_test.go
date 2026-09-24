package server

import "testing"

// punchVehicle waits out the fist's recovery (a full-strength swing), then
// hits the vehicle through the attack event's own route.
func punchVehicle(h *hub, players map[int32]*tracked, pl *tracked, v *vehicle) {
	for i := 0; i < 6; i++ {
		h.tick.Add(1)
		h.updateVehicles(players)
	}
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: v.eid})
}

// A survival fist does not break a minecart in one punch: every blow adds
// ten times its worth, a point drains away each tick, and it breaks only
// once the built-up damage passes 40 (VehicleEntity.hurtServer).
func TestVehicleSurvivesOnePunch(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	pl.gamemode = gmSurvival
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["minecart"], x: x, y: y, z: z, slot: 0})
	v := firstVehicle(h)
	punchVehicle(h, players, pl, v)
	if len(h.vehicles) != 1 {
		t.Fatal("one bare-handed punch must not break a minecart")
	}
	if v.hurtTime != vehHurtTicks || v.damage != 10 || v.hurtDir() != -1 {
		t.Fatalf("hurt state after a punch: time=%d damage=%.1f dir=%d", v.hurtTime, v.damage, v.hurtDir())
	}
	punches := 1
	for len(h.vehicles) == 1 && punches < 20 {
		punchVehicle(h, players, pl, v)
		punches++
	}
	if len(h.vehicles) != 0 {
		t.Fatal("repeated punches should break the minecart")
	}
	// 10 a punch, 6 drained between them: 10, 14, 18 … passes 40 on the 9th.
	if punches != 9 {
		t.Errorf("broke after %d punches, want 9", punches)
	}
	found := false
	for _, it := range h.items {
		if it.item == itemByName["minecart"] {
			found = true
		}
	}
	if !found {
		t.Error("a minecart broken in survival drops its item")
	}
}

// Left alone, the built-up damage drains away a point a tick.
func TestVehicleDamageDrains(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["minecart"], x: x, y: y, z: z, slot: 0})
	v := firstVehicle(h)
	punchVehicle(h, players, pl, v)
	for i := 0; i < 12; i++ {
		h.updateVehicles(players)
	}
	if v.damage != 0 || v.hurtTime != 0 {
		t.Fatalf("damage and wobble should have drained: damage=%.1f time=%d", v.damage, v.hurtTime)
	}
}

// A creative player's blow removes the vehicle at once and drops nothing.
func TestVehicleCreativeBreaksAtOnce(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["minecart"], x: x, y: y, z: z, slot: 0})
	pl.gamemode = gmCreative
	v := firstVehicle(h)
	punchVehicle(h, players, pl, v)
	if len(h.vehicles) != 0 {
		t.Fatal("a creative punch removes the vehicle")
	}
	for _, it := range h.items {
		if it.item == itemByName["minecart"] {
			t.Fatal("a creative punch drops no minecart item")
		}
	}
}
