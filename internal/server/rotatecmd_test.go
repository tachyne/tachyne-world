package server

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// /rotate through the dispatcher: an absolute rotation, then facing a
// point due east (yaw -90) and level; a non-operator is refused.
func TestRotateCommand(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice, carol := ps["alice"], ps["carol"]
	var bx, by, bz float64
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; bx, by, bz = b.x, b.y, b.z })

	s.handleCommand(alice, "rotate bob 45 10")
	settle(t, h, logs, "R1")
	var yaw, pitch float32
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; yaw, pitch = b.yaw, b.pitch })
	if yaw != 45 || pitch != 10 {
		t.Fatalf("bob faces %v/%v, want 45/10", yaw, pitch)
	}
	s.handleCommand(alice, "rotate bob facing "+ftoa(bx+10)+" "+ftoa(by)+" "+ftoa(bz))
	s.handleCommand(carol, "rotate bob 0 0")
	settle(t, h, logs, "R2")
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; yaw, pitch = b.yaw, b.pitch })
	if math.Abs(float64(yaw)+90) > 0.01 || math.Abs(float64(pitch)) > 0.01 {
		t.Fatalf("facing east: %v/%v, want -90/0", yaw, pitch)
	}
	if !hasLine(linesBetween(logs["alice"], "", "R2"), "Rotated bob") {
		t.Errorf("no success line for alice: %q", linesBetween(logs["alice"], "", "R2"))
	}
	if !hasLine(linesBetween(logs["carol"], "", "R2"), "You don't have permission.") {
		t.Errorf("carol was not refused: %q", linesBetween(logs["carol"], "", "R2"))
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }

// /tp with targets: an operator sends another player to a place or to an
// entity; a non-operator may not teleport at all.
func TestTeleportTargets(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice, carol := ps["alice"], ps["carol"]
	s.handleCommand(alice, "tp bob 10 90 -4")
	settle(t, h, logs, "T1")
	var bx, by, bz float64
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; bx, by, bz = b.x, b.y, b.z })
	if bx != 10 || by != 90 || bz != -4 {
		t.Fatalf("bob is at %v,%v,%v, want 10,90,-4", bx, by, bz)
	}
	if !hasLine(linesBetween(logs["alice"], "", "T1"), "Teleported bob to 10.000000, 90.000000, -4.000000") {
		t.Errorf("alice's reply: %q", linesBetween(logs["alice"], "", "T1"))
	}
	var ax, az float64
	onHub(t, h, func() { a := h.playersRef[alice.eid]; ax, az = a.x, a.z })
	s.handleCommand(alice, "tp bob alice")
	s.handleCommand(carol, "tp 100 100 100")
	settle(t, h, logs, "T2")
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; bx, bz = b.x, b.z })
	if bx != ax || bz != az {
		t.Errorf("bob sent to alice is at %v,%v, want %v,%v", bx, bz, ax, az)
	}
	if !hasLine(linesBetween(logs["carol"], "", "T2"), "You don't have permission.") {
		t.Error("a non-operator teleported")
	}
}

// /summon puts the mob exactly where it was asked (not on the surface below),
// with its natural setup — a blaze is a blaze in the overworld too.
func TestSummonAtExactPosition(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice := ps["alice"]
	s.handleCommand(alice, "summon minecraft:zombie 5.5 150 5.5")
	s.handleCommand(alice, "summon blaze 8.5 150 8.5")
	s.handleCommand(alice, "summon not_a_mob")
	settle(t, h, logs, "U1")
	var zy float64
	var blazeOK bool
	onHub(t, h, func() {
		for _, m := range h.mobs {
			switch m.etype {
			case entityZombie:
				zy = m.y
			case entityBlaze:
				_, blazeOK = m.behavior.(blazeBehavior)
			}
		}
	})
	if zy != 150 {
		t.Errorf("the zombie is at y=%v, want 150", zy)
	}
	if !blazeOK {
		t.Error("a summoned blaze did not get its blaze behaviour")
	}
	if !hasLine(linesBetween(logs["alice"], "", "U1"), "Unknown entity: not_a_mob") {
		t.Errorf("alice's replies: %q", linesBetween(logs["alice"], "", "U1"))
	}
}

// /locate biome: the biome underfoot is found where you stand; a name that
// never generates searches out to 6400 blocks and says so.
func TestLocateBiome(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice := ps["alice"]
	here := h.world.Gen().CaveBiomeAt(int(math.Floor(alice.x)), int(math.Floor(alice.y)), int(math.Floor(alice.z)))
	s.handleCommand(alice, "locate biome "+here)
	start := time.Now()
	s.handleCommand(alice, "locate biome minecraft:not_a_biome")
	t.Logf("a full miss took %v", time.Since(start))
	settle(t, h, logs, "L1")
	a := linesBetween(logs["alice"], "", "L1")
	found := false
	for _, l := range a {
		if strings.HasPrefix(l, "The nearest "+here+" is at [") && strings.HasSuffix(l, "(0 blocks away)") {
			found = true
		}
	}
	if !found {
		t.Errorf("the biome underfoot was not found at 0 blocks: %q", a)
	}
	if !hasLine(a, `Could not find a biome of type "minecraft:not_a_biome" within reasonable distance`) {
		t.Errorf("no miss message: %q", a)
	}
}
