package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

var itemWhiteHarness = itemByName["white_harness"]

// riderAt makes a survival player with a distinct eid at a position.
func riderAt(eid int32, x, y, z float64) *tracked {
	t := &tracked{p: newPlayer(eid, "r", [16]byte{byte(eid)}), gamemode: gmSurvival}
	initSurvival(t)
	t.x, t.y, t.z = x, y, z
	return t
}

func TestHarnessEquipAndBoard(t *testing.T) {
	h := newHub(world.New(1))
	pilot := riderAt(1, 100.5, 70, 100.5)
	players := map[int32]*tracked{1: pilot}
	m := h.spawnSpecies(players, entityHappyGhast, 0, 101.5, 70, 100.5)

	give(pilot, itemWhiteHarness)
	if !h.tryHappyGhast(players, pilot, m) || m.harness != itemWhiteHarness {
		t.Fatalf("holding a harness should equip it: harness=%d", m.harness)
	}
	if pilot.inv.slots[0].item != 0 {
		t.Error("harness should be consumed in survival")
	}
	pilot.p.held = 0 // empty hand now boards
	if !h.tryHappyGhast(players, pilot, m) || len(m.riders) != 1 || m.riders[0] != 1 {
		t.Fatalf("harnessed ghast should board the pilot: riders=%v", m.riders)
	}
	for eid := int32(2); eid <= 5; eid++ { // fill to 4, reject the 5th
		r := riderAt(eid, 101.5, 70, 100.5)
		players[eid] = r
		h.boardGhast(players, r, m)
	}
	if len(m.riders) != ghastMaxRiders {
		t.Fatalf("happy ghast seats %d, got %d", ghastMaxRiders, len(m.riders))
	}
	if !h.leaveGhast(players, pilot) || len(m.riders) != 3 {
		t.Fatalf("pilot leaving should leave 3 riders, got %v", m.riders)
	}
}

func TestGhastlingCannotBeHarnessed(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 100.5, 70, 100.5)
	players := map[int32]*tracked{1: pl}
	m := h.spawnSpecies(players, entityHappyGhast, 0, 101.5, 70, 100.5)
	m.baby = true
	give(pl, itemWhiteHarness)
	if h.tryHappyGhast(players, pl, m) || m.harness != 0 {
		t.Fatal("a ghastling must not accept a harness or be ridden")
	}
}

func TestGhastPilotFlightDragsRiders(t *testing.T) {
	h := newHub(world.New(1))
	pilot := riderAt(1, 100.5, 70, 100.5)
	passenger := riderAt(2, 100.5, 70, 100.5)
	players := map[int32]*tracked{1: pilot, 2: passenger}
	m := h.spawnSpecies(players, entityHappyGhast, 0, 100.5, 70, 100.5)
	m.harness, m.riders = itemWhiteHarness, []int32{1, 2}

	// Pilot flies up + forward (delta within vehicleMoveCap).
	if !h.applyGhastMove(players, pilot, evVehicleMove{eid: 1, x: 101.5, y: 72, z: 100.5, yaw: 45}) {
		t.Fatal("applyGhastMove should handle the pilot's vehicle_move")
	}
	if m.x != 101.5 || m.y != 72 {
		t.Fatalf("ghast should adopt the pilot's flight pos, got %v,%v", m.x, m.y)
	}
	if passenger.y != 72+ghastRideHeight {
		t.Errorf("passenger should be dragged to flight altitude, got %v", passenger.y)
	}
	// A non-pilot rider cannot steer.
	if h.applyGhastMove(players, passenger, evVehicleMove{eid: 2, x: 200, y: 70, z: 200}) {
		t.Error("only the pilot (riders[0]) may fly the ghast")
	}
}
