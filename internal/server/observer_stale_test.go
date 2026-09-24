package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ObserverBlock.onPlace: an observer saved mid-pulse comes back POWERED with
// no tick to end it (ticks are not saved); the boot sweep's update switches
// it off instead of leaving it powered for good.
func TestObserverSavedMidPulseSwitchesOff(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	obs := setBoolProp(worldgen.BlockBase("observer"), "powered", true)
	w.SetBlock(x, y, z, obs)
	h.rescheduleRedstone() // the boot sweep
	stepTicks(h, players, 3)
	if boolProp(w.At(x, y, z), "powered") {
		t.Error("an observer saved mid-pulse stayed powered after the boot sweep")
	}
}

// A pulse in flight is left alone: its end tick is pending.
func TestObserverPulseStillEnds(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	obs := worldgen.BlockBase("observer")
	w.SetBlock(x, y, z, setBoolProp(obs, "powered", false))
	h.rsDim = dimOverworld
	h.observerTick(players, blockPos{x, y, z}, w.At(x, y, z)) // the pulse starts
	if !boolProp(w.At(x, y, z), "powered") {
		t.Fatal("the pulse did not start")
	}
	h.updateObserver(players, blockPos{x, y, z}, w.At(x, y, z)) // a neighbour update mid-pulse
	if !boolProp(w.At(x, y, z), "powered") {
		t.Error("a neighbour update cut a live pulse short")
	}
	stepTicks(h, players, observerPulseTicks+1)
	if boolProp(w.At(x, y, z), "powered") {
		t.Error("the pulse never ended")
	}
}

// A powered observer a piston carries lands unpowered.
func TestPistonLandsObserverUnpowered(t *testing.T) {
	obs := setBoolProp(worldgen.BlockBase("observer"), "powered", true)
	if boolProp(landedStateOf(obs), "powered") {
		t.Error("a piston put a powered observer down still powered")
	}
}
