package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// TestHoglinZombifiesAndFleesFungus: a hoglin in the overworld turns into a
// nauseous zoglin after three hundred ticks; one near warped fungus is
// pacified and walks away from it; the bite rolls half-plus damage.
func TestHoglinZombifiesAndFleesFungus(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	hog := h.spawnMob(players, entityHoglin, 0.5, 180, 0.5)
	hog.baby = false
	for i := 0; i < 20; i++ {
		d := h.hoglinBiteDamage(hog)
		if d < 3 || d > 8 {
			t.Fatalf("an adult's bite is 3..8 for damage 6: %.1f", d)
		}
	}
	w.SetBlock(3, 180, 0, worldgen.BlockBase("warped_fungus"))
	h.tick.Store(20)
	hog.hasTarget = true
	if !h.hoglinStep(players, hog) || hog.hasTarget || hog.hogPacified != hoglinPacifyTicks || hog.vx >= 0 {
		t.Fatalf("warped fungus pacifies it and sends it off: target %v pacified %d vx %.2f", hog.hasTarget, hog.hogPacified, hog.vx)
	}
	eid := hog.eid
	for i := 0; i < 160 && h.mobs[eid] != nil; i++ {
		h.zombifyTick(players, hog)
	}
	if h.mobs[eid] != nil {
		t.Fatal("three hundred ticks outside the Nether should zombify it")
	}
	var zog *mob
	for _, o := range h.mobs {
		if o.etype == entityZoglin {
			zog = o
		}
	}
	if zog == nil || zog.hasEffect(effNausea) == 0 {
		t.Fatalf("a nauseous zoglin should stand in its place: %v", zog != nil)
	}
	// In the Nether nothing happens; an immune hoglin is safe anywhere.
	hog2 := h.spawnMobIn(players, entityHoglin, 1, 0.5, 80, 0.5)
	if hog2 != nil {
		for i := 0; i < 200; i++ {
			h.zombifyTick(players, hog2)
		}
		if hog2.overworldTicks != 0 {
			t.Fatal("the Nether never converts")
		}
	}
	hog3 := h.spawnMob(players, entityHoglin, 0.5, 180, 0.5)
	hog3.immuneZombify = true
	for i := 0; i < 200; i++ {
		h.zombifyTick(players, hog3)
	}
	if h.mobs[hog3.eid] == nil {
		t.Fatal("an immune hoglin stays a hoglin")
	}
}

// ATTACK_KNOCKBACK was declared and never set, so the four mobs whose whole
// character is sending you flying hit like anything else — and a hoglin's
// throw, which reads the attribute directly, did nothing at all.
func TestAttackKnockbackIsSet(t *testing.T) {
	for etype, want := range map[int]float64{
		entityRavager: 1.5, entityWarden: 1.5,
		entityHoglin: 1, entityZoglin: 1,
		entityZombie: 0, entityCreeper: 0,
	} {
		m := &mob{etype: etype}
		if got := m.mobAttrs().Value(attr.AttackKnockback); got != want {
			t.Errorf("%s ATTACK_KNOCKBACK = %v, want %v", entityNameByID[etype], got, want)
		}
	}
}

// A hoglin's throw now actually leaves the ground.
func TestHoglinThrowMoves(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	m := &mob{etype: entityHoglin, x: 0, y: 70, z: 0}
	pl.x, pl.y, pl.z = 2, 70, 0

	h.hoglinThrow(pl, m)
	var thrown bool
	for {
		select {
		case pkt := <-pl.p.out:
			if v, ok := pkt.ev.(attachproto.Velocity); ok && (v.VX != 0 || v.VY != 0 || v.VZ != 0) {
				thrown = true
			}
			continue
		default:
		}
		break
	}
	if !thrown {
		t.Fatal("a hoglin's bite sent the player nowhere")
	}
}

// STEP_HEIGHT was never set, so every mob sat on the 0.6 default. It is a
// SYNCED attribute and a ridden mount is moved by the riding client, so a
// camel on 0.6 has to jump the fence its 1.5 is famous for walking over.
func TestStepHeightMatchesVanilla(t *testing.T) {
	for etype, want := range map[int]float64{
		entityCamel: 1.5, entityCamelHusk: 1.5, // Camel overrides the horse base
		entityCreaking: 1.0625,
		entityHorse:    1, entityDonkey: 1, entityMule: 1, entityLlama: 1,
		entityIronGolem: 1, entityCopperGolem: 1, entityEnderman: 1,
		entityRavager: 1, entityDrowned: 1, entityAxolotl: 1,
		entityFrog: 1, entityTurtle: 1,
		// everything else keeps the registry default
		entityZombie: 0.6, entityCow: 0.6, entityCreeper: 0.6, entitySheep: 0.6,
	} {
		m := &mob{etype: etype}
		if got := m.mobAttrs().Value(attr.StepHeight); got != want {
			t.Errorf("%s step height = %v, want %v", entityNameByID[etype], got, want)
		}
	}
	// It has to be in the synced set or the riding client never learns it.
	if !syncableAttrs[attr.StepHeight] {
		t.Error("step height must be syncable — the riding client reads it")
	}
}
