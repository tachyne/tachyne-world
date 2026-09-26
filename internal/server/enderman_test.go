package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// TestZombieReinforcements: on hard difficulty a hurt zombie with a charged
// SPAWN_REINFORCEMENTS_CHANCE summons a same-species backup targeting the
// attacker, and both lose 0.05 charge.
func TestZombieReinforcements(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffHard
	pl := testTracked()
	pl.gamemode = gmSurvival
	px, pz, found := 0, 0, false
	for x := 0; x < 300 && !found; x += 3 {
		for z := 0; z < 300 && !found; z += 3 {
			if h.world.Spawnable(x, z) {
				px, pz, found = x, z, true
			}
		}
	}
	if !found {
		t.Skip("no spawnable column")
	}
	pl.x, pl.y, pl.z = float64(px)+0.5, h.world.SurfaceY(px, pz), float64(pz)+0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnHostile(players, entityZombie, px+2, pz)
	if c := m.reinforcementChance(); c < 0 || c >= 0.1+0.75 {
		t.Fatalf("a fresh zombie's reinforcement chance is %v, outside vanilla's roll", c)
	}
	// /attribute sets the chance the call reads.
	h.applyAttributeCommand(players, evAttributeCmd{by: pl.p.eid, target: "@e[type=zombie]", id: attr.SpawnReinforcements, op: "base set", value: 1})
	before := len(h.mobs)

	h.mobStruck(players, m, pl, dtPlayerAttack) // the player's blow lands
	if len(h.mobs) != before+1 {
		t.Fatalf("reinforcement should have spawned: %d mobs, want %d", len(h.mobs), before+1)
	}
	if c := m.reinforcementChance(); math.Abs(c-0.95) > 1e-9 {
		t.Fatalf("caller charge should drop 0.05: got %v", c)
	}
	for _, o := range h.mobs {
		if o != m && o.etype == entityZombie {
			in := o.mobAttrs().Get(attr.SpawnReinforcements)
			if !in.HasModifier(reinforceCalleeSource) {
				t.Fatal("the recruit must start 0.05 down on its own chance")
			}
			if in.Value() >= 0.05+0.75 {
				t.Fatalf("the recruit's chance %v was copied from its caller, not rolled", in.Value())
			}
			if !o.hasTarget {
				t.Fatal("the recruit must already hunt the attacker")
			}
		}
	}
	// A second recruit: the caller's charge grows, it does not reset.
	m.mobAttrs().SetBase(attr.SpawnReinforcements, 5) // still certain after the charge
	before = len(h.mobs)
	h.zombieReinforce(players, m, pl)
	if len(h.mobs) != before+1 {
		t.Fatal("the second call summoned nobody")
	}
	for _, mod := range m.mobAttrs().Get(attr.SpawnReinforcements).Modifiers() {
		if mod.Source == reinforceCallerSource && math.Abs(mod.Amount+0.10) > 1e-9 {
			t.Fatalf("after two recruits the caller's charge is %v, want -0.10", mod.Amount)
		}
	}

	// Never on normal difficulty.
	h.rules.Difficulty = diffNormal
	before = len(h.mobs)
	h.zombieReinforce(players, m, pl)
	if len(h.mobs) != before {
		t.Fatal("reinforcements are a HARD-difficulty mechanic")
	}
}
