package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// buildWitherFrame lays the soul-sand T + three skulls (row along X) centred at
// (cx,topY,cz), leaving the final skull for checkWitherBuild to "complete".
func buildWitherFrame(w *world.World, cx, topY, cz int) {
	skull := worldgen.BlockBase("wither_skeleton_skull") + 16 // default
	for j := -1; j <= 1; j++ {
		w.SetBlock(cx+j, topY-1, cz, blockSoulSand) // arms
		w.SetBlock(cx+j, topY, cz, skull)           // skulls
	}
	w.SetBlock(cx, topY-2, cz, blockSoulSand) // stem
}

func TestWitherBuildSpawns(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	cx, cy, cz := 5, 70, 5
	buildWitherFrame(w, cx, cy, cz)

	before := len(h.mobs)
	h.checkWitherBuild(players, 0, 0, cx+1, cy, cz, (worldgen.BlockBase("wither_skeleton_skull") + 16)) // "placed" the last skull
	if len(h.mobs) != before+1 {
		t.Fatalf("completing the frame should spawn a wither: %d mobs", len(h.mobs))
	}
	var wm *mob
	for _, m := range h.mobs {
		if m.etype == entityWither {
			wm = m
		}
	}
	if wm == nil {
		t.Fatal("no wither spawned")
	}
	if wm.spawnInvuln <= 0 {
		t.Fatal("a fresh wither should be charging (invulnerable)")
	}
	// The frame is consumed.
	if w.At(cx, cy, cz) != worldgen.Air || w.At(cx, cy-1, cz) != worldgen.Air || w.At(cx, cy-2, cz) != worldgen.Air {
		t.Fatal("the soul sand + skulls should be consumed on spawn")
	}
}

func TestWitherInvulnerableWhileCharging(t *testing.T) {
	m := &mob{etype: entityWither, health: witherHealth, spawnInvuln: 10}
	m.hurt(50)
	if m.health != witherHealth {
		t.Fatalf("a charging wither must take no damage, health=%d", m.health)
	}
	m.spawnInvuln = 0
	m.hurt(50)
	if m.health >= witherHealth {
		t.Fatalf("a charged wither should take damage, health=%d", m.health)
	}
}

func TestWitherChargeReleases(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	m := h.spawnSpecies(players, entityWither, 0, 5.5, 70, 5.5)
	m.health = witherHealth
	m.spawnInvuln = 3
	for i := 0; i < 3; i++ {
		h.updateWithers(players)
	}
	if m.spawnInvuln != 0 {
		t.Fatalf("charge should count down to 0, got %d", m.spawnInvuln)
	}
}

func TestNonSkullDoesNotSpawnWither(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	buildWitherFrame(w, 5, 70, 5)
	// A non-skull placement (e.g. soul sand) must not trigger the build.
	h.checkWitherBuild(players, 0, 0, 6, 70, 5, blockSoulSand)
	for _, m := range h.mobs {
		if m.etype == entityWither {
			t.Fatal("only a wither skull completes the build")
		}
	}
}

// The wither's side heads pick their own victims — any living thing nearby
// that is not undead — and a head with nobody to shoot at eventually fires
// into the scenery.
func TestWitherSideHeadsPickVictims(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.rules.Difficulty = diffNormal
	m := h.spawnMob(players, entityWither, 0.5, 100, 0.5)
	cow := h.spawnMob(players, entityCow, 4.5, 100, 0.5)
	h.spawnMob(players, entityZombie, 5.5, 100, 0.5) // undead: not a victim

	for i := 0; i < 4 && (m.headTarget[0] == 0 || m.headTarget[1] == 0); i++ {
		h.tick.Add(40)
		h.witherHeadsTick(players, m)
	}
	if m.headTarget[0] != cow.eid && m.headTarget[1] != cow.eid {
		t.Fatalf("a side head should pick the cow, got %v", m.headTarget)
	}
	for _, eid := range m.headTarget {
		if o := h.mobs[eid]; o != nil && undeadTypes[o.etype] {
			t.Error("a wither does not shoot the undead")
		}
	}
	// It fires at the victim it holds.
	before := len(h.arrows)
	h.tick.Add(60)
	h.witherHeadsTick(players, m)
	if len(h.arrows) <= before {
		t.Error("a head with a victim fires a skull at it")
	}

	// With nothing around, a head eventually fires into the scenery.
	lonely := h.spawnMob(players, entityWither, 500.5, 100, 500.5)
	fired := false
	for i := 0; i < 40 && !fired; i++ {
		n := len(h.arrows)
		h.tick.Add(20)
		h.witherHeadsTick(players, lonely)
		fired = len(h.arrows) > n
	}
	if !fired {
		t.Error("a bored head should fire at a random point near the boss")
	}
}

// A blow arms the wither's block-smashing: twenty ticks later everything
// breakable around it comes down, bedrock excepted.
func TestWitherSmashesAfterBeingHurt(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.rules.MobGriefing = true
	m := h.spawnMob(players, entityWither, 0.5, 100, 0.5)
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			h.world.SetBlock(x, 100, z, worldgen.Stone)
		}
	}
	h.world.SetBlock(0, 101, 0, worldgen.BlockBase("bedrock"))

	m.hurtKind(1, dtGeneric)
	if m.witherSmash <= 0 {
		t.Fatal("being hurt should arm the smash")
	}
	for i := 0; i < 8 && m.witherSmash > 0; i++ {
		h.witherSmashTick(players, m)
	}
	if got := h.world.At(0, 100, 0); got != worldgen.Air {
		t.Errorf("the stone under the wither should be gone, state %d", got)
	}
	if h.world.At(0, 101, 0) == worldgen.Air {
		t.Error("bedrock is wither-immune")
	}
}

// WitherBoss's second phase. Below half health it shrugs off arrows and wind
// charges entirely — that rule is what stops the fight being a sniping
// exercise — and it heals a health point a second from the moment its charge
// ends, which is what makes the damage race matter.
func TestWitherPhaseTwoAndRegen(t *testing.T) {
	h := newHub(world.New(89))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityWither, 0, 180, 0)
	m.health = witherHealth

	if witherPowered(m) {
		t.Error("a full-health wither is not in its second phase")
	}
	arrow := &arrowEntity{etype: entityArrow, dmg: 9}
	if got := projectileHitDamage(arrow, m); got != 9 {
		t.Errorf("an arrow does %d to a healthy wither, want 9", got)
	}

	m.health = witherHealth / 2
	if !witherPowered(m) {
		t.Error("at exactly half health it IS powered (vanilla's <=)")
	}
	for _, et := range []int{entityArrow, entitySpectralArrow, entityWindCharge} {
		if got := projectileHitDamage(&arrowEntity{etype: et, dmg: 9}, m); got != 0 {
			t.Errorf("%d does %d to a powered wither, want 0", et, got)
		}
	}
	// Melee and everything else still land.
	if got := projectileHitDamage(&arrowEntity{etype: entityTrident, dmg: 9}, m); got != 9 {
		t.Errorf("a trident does %d to a powered wither, want 9", got)
	}

	// Regeneration: a health point a second, banked across the four-tick
	// sweep rather than healed five times over.
	m.health = 100
	for i := 0; i < 20/witherSweepTicks; i++ {
		h.witherRegen(m)
	}
	if m.health != 101 {
		t.Errorf("a second of regen took the wither to %d, want 101", m.health)
	}

	// …and nothing heals while the spawn charge is still running.
	m.health, m.spawnInvuln = 100, 10
	for i := 0; i < 40; i++ {
		h.witherRegen(m)
	}
	if m.health != 100 {
		t.Errorf("a charging wither healed to %d, want 100", m.health)
	}
}

// The blue skull. A bored side head always fires one; the centre head's aimed
// shot is blue once in a thousand. A blue skull drags harder in the air and
// holds every block it is allowed to break to 0.8 resistance, which is how it
// eats through obsidian an ordinary one cannot touch.
func TestWitherFiresBlueSkulls(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnHostileY(players, entityWither, 100.5, 70, 100.5)
	if m == nil {
		t.Fatal("the wither should have spawned")
	}

	h.witherSkullAt(players, m, 110, 70, 100, true)
	var blue *arrowEntity
	for _, a := range h.arrows {
		blue = a
	}
	if blue == nil || !blue.dangerous {
		t.Fatal("the bored head's skull should be a blue one")
	}
	if blue.explode != witherSkullBlast {
		t.Fatalf("a skull should carry its own blast, got %d", blue.explode)
	}
	// WitherSkull.getInertia: 0.73 rather than the ordinary 0.95.
	if _, inertia, ok := hurtingMotion(blue, false); !ok || inertia != 0.73 {
		t.Fatalf("a blue skull's inertia is %v, want 0.73", inertia)
	}
	for eid := range h.arrows {
		delete(h.arrows, eid)
	}

	h.witherSkullAt(players, m, 110, 70, 100, false)
	for _, a := range h.arrows {
		if a.dangerous {
			t.Fatal("an aimed shot asked for black should not be blue")
		}
		if _, inertia, _ := hurtingMotion(a, false); inertia != 0.95 {
			t.Fatalf("a black skull's inertia is %v, want 0.95", inertia)
		}
	}
}

// The resistance cap is what the blue skull is FOR: obsidian resists 1200 and
// shrugs off an ordinary blast, and goes down to a blue skull.
func TestBlueSkullBlastEatsObsidian(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	cx, cy, cz := 200, 70, 200
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(cx+dx, cy, cz+dz, worldgen.Obsidian)
		}
	}
	plain := h.blastPositions(w, float64(cx)+0.5, float64(cy)+0.5, float64(cz)+0.5, 3)
	capped := h.blastPositionsCapped(w, float64(cx)+0.5, float64(cy)+0.5, float64(cz)+0.5, 3, witherSkullResistCap)
	if len(capped) <= len(plain) {
		t.Fatalf("the capped blast should reach further through obsidian: %d vs %d", len(capped), len(plain))
	}
	_ = players
}
