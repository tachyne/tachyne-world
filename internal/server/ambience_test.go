package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Mob.baseTick's idle-voice cadence: after a call the counter sits at
// minus the species' interval and no call can come until it climbs back
// past zero; the interval is per class (a cow 120, a horse 400, a zombie 80).
func TestAmbientCadence(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	cow := &mob{eid: 1, etype: entityCow, x: 0.5, y: 200, z: 0.5}
	h.mobs[cow.eid] = cow
	cow.ambientTime = -ambientInterval(cow)
	for i := 0; i < 120; i++ {
		h.mobAmbience(players)
		if cow.ambientTime < 0 && cow.ambientTime != -120+int32(i)+1 {
			t.Fatalf("the counter must climb one per tick, tick %d: %d", i, cow.ambientTime)
		}
	}
	if cow.ambientTime != 0 {
		t.Fatalf("no call can land inside the interval: counter %d after 120 ticks", cow.ambientTime)
	}
	if ambientInterval(&mob{etype: entityHorse}) != 400 || ambientInterval(&mob{etype: entityZombie}) != 80 || ambientInterval(&mob{etype: entityTurtle}) != 200 {
		t.Error("per-class intervals: horse 400, zombie 80, turtle 200")
	}
	// Past zero the roll can hit; once it does the counter resets to -interval.
	for i := 0; i < 5000 && cow.ambientTime >= 0; i++ {
		h.mobAmbience(players)
	}
	if cow.ambientTime != -120 {
		t.Fatalf("a call resets the counter to -120, got %d", cow.ambientTime)
	}
}
