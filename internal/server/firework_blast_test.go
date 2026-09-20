package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// FireworkRocketEntity.dealExplosionDamage. A rocket with no star in it is
// safe to fly with — that is the whole point of a plain one — but a rocket
// full of stars hurts: the glider it is boosting takes the full 5 + 2 a star
// wherever it is, and anything living within five blocks with a clear line to
// the burst takes that scaled by how close it stood.
func TestFireworkRocketBlast(t *testing.T) {
	w := world.New(3)
	h := newHub(w)
	h.initStars(newStarStore())
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.dim, pl.x, pl.y, pl.z = 0, 0.5, 180, 0.5

	// A plain rocket is harmless.
	plain := h.spawnRocket(players, 0, pl.x, pl.y+1, pl.z, 0, invStack{item: itemByName["firework_rocket"], count: 1})
	if plain.explosions != 0 {
		t.Fatalf("a starless rocket carries %d explosions", plain.explosions)
	}
	before := pl.health
	h.popRocket(players, plain)
	if pl.health != before {
		t.Errorf("a rocket with no star must not hurt: %v → %v", before, pl.health)
	}

	// Two stars: 5 + 2×2 = 9 to the glider it boosts.
	starred := invStack{item: itemByName["firework_rocket"], count: 1,
		starID: h.stars.intern([]fireworkBurst{{Shape: 0}, {Shape: 1}})}
	r := h.spawnRocket(players, 0, pl.x, pl.y+1, pl.z, pl.p.eid, starred)
	if r.explosions != 2 {
		t.Fatalf("the rocket carries %d explosions, want 2", r.explosions)
	}
	pl.health, pl.hurtAt, pl.lastHurt = 20, 0, 0
	h.tick.Store(1000)
	h.popRocket(players, r)
	if pl.health >= 20 {
		t.Fatal("the glider takes the rocket's full blast")
	}
	if pl.lastCause.dt != dtFireworks {
		t.Errorf("death cause is %s, want fireworks", dmgTypeNames[pl.lastCause.dt])
	}

	// A bystander four blocks away takes less than the full blast; one behind
	// a wall takes none.
	bystander := survPlayer(h)
	bystander.p.eid = 77
	players[77] = bystander
	bystander.dim, bystander.x, bystander.y, bystander.z = 0, 4.5, 180, 0.5
	loose := h.spawnRocket(players, 0, 0.5, 180, 0.5, 0, starred)
	bystander.health, bystander.hurtAt, bystander.lastHurt = 20, 0, 0
	h.tick.Store(2000)
	h.popRocket(players, loose)
	hurt := 20 - bystander.health
	if hurt <= 0 {
		t.Fatal("a bystander within five blocks is caught in the blast")
	}
	if hurt >= 9 {
		t.Errorf("distance must soften the blast, got %v of 9", hurt)
	}

	for y := 179; y <= 182; y++ { // a wall between them
		w.SetBlock(2, y, 0, worldgen.BlockBase("stone"))
	}
	shielded := h.spawnRocket(players, 0, 0.5, 180, 0.5, 0, starred)
	bystander.health, bystander.hurtAt, bystander.lastHurt = 20, 0, 0
	h.tick.Store(3000)
	h.popRocket(players, shielded)
	if bystander.health != 20 {
		t.Errorf("a wall between them blocks the blast, health %v", bystander.health)
	}
}
