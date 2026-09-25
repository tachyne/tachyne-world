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

// A struck axolotl does not bolt and bears no grudge: AxolotlAi has no panic
// in any activity, and its one answer to a blow is the play-dead roll —
// which only a blow from something can set off, never fire or a fall.
func TestStruckAxolotlPlaysDeadNotPanics(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
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
	pl.x, pl.y, pl.z = 2.5, 181, 0.5
	ax := h.spawnMob(players, entityAxolotl, 0.5, 181, 0.5)
	h.gridDirty()
	played := false
	for i := 0; i < 40 && !played; i++ {
		ax.health, ax.invulnTicks = 1000, 0
		ax.axDead = 0
		h.attackMob(players, pl.p.eid, ax.eid)
		if ax.panic != 0 || ax.anger != 0 || ax.targetEID != 0 || ax.hostile {
			t.Fatalf("hit %d: an axolotl neither panics nor holds a grudge: panic %d anger %d target %d", i, ax.panic, ax.anger, ax.targetEID)
		}
		for j := 0; j < 6 && !played; j++ { // a sword's recharge: the knockback settles
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
			played = ax.axDead > 0
			if ax.panic != 0 {
				t.Fatalf("hit %d: the axolotl bolted", i)
			}
		}
	}
	if !played {
		t.Fatal("forty blows under water and it never played dead")
	}
	// Fire, a cactus or a fall carries no attacker: no roll at all.
	ax.axDead, ax.axHurt = 0, false
	for i := 0; i < 30; i++ {
		ax.health, ax.invulnTicks = 1000, 0
		ax.hurtKind(1, dtInFire)
		if ax.axHurt {
			t.Fatal("unattributed damage should not roll play-dead")
		}
	}
}
