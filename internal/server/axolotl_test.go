package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestAxolotlHuntsAndPlaysDead: an axolotl in a pool hunts the cod beside
// it (a cod it may not hunt again for two minutes after), plays dead when
// hurt, and a player who finishes its foe gets regeneration.
func TestAxolotlHuntsAndPlaysDead(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 183; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	pl.x, pl.y, pl.z = 3.5, 184, 0.5
	ax := h.spawnMob(players, entityAxolotl, 0.5, 181, 0.5)
	cod := h.spawnMob(players, entityCod, 3.5, 181, 0.5)
	h.gridDirty()
	h.tick.Store(5000)
	if !h.axolotlStep(players, ax) || ax.axTarget != cod.eid {
		t.Fatalf("the axolotl should go for the cod: target %d", ax.axTarget)
	}
	// The player finishes the cod: regeneration for them, a hunting rest for it.
	cod.lastAttacker = pl.p.eid
	cod.health = 0
	h.killMob(players, cod)
	h.axolotlStep(players, ax)
	if pl.hasEffect(effRegen) == 0 || ax.axHuntCD != 5000+axHuntCooldown || ax.axTarget != 0 {
		t.Fatalf("support and the hunting cooldown: regen %d cd %d", pl.hasEffect(effRegen), ax.axHuntCD)
	}
	cod2 := h.spawnMob(players, entityCod, 2.5, 181, 0.5)
	h.gridDirty()
	h.axolotlStep(players, ax)
	if ax.axTarget == cod2.eid {
		t.Fatal("no fish hunted during the cooldown")
	}
	dr := h.spawnMob(players, entityDrowned, 2.5, 181, -0.5)
	h.gridDirty()
	h.axolotlStep(players, ax)
	if ax.axTarget != dr.eid {
		t.Fatalf("a drowned is always fought: target %d", ax.axTarget)
	}
	// Hurt hard under water: it plays dead within a few rolls.
	ax.health = 6
	played := false
	for i := 0; i < 60 && !played; i++ {
		ax.axHurt, ax.axHurtDmg = true, 2
		h.axolotlStep(players, ax)
		played = ax.axDead > 0
	}
	if !played || ax.hasEffect(effRegen) == 0 || ax.axTarget != 0 {
		t.Fatalf("playing dead with regeneration: dead %d regen %d", ax.axDead, ax.hasEffect(effRegen))
	}
	if !h.axolotlStep(players, ax) || ax.vx != 0 {
		t.Fatal("held still while playing dead")
	}
}
