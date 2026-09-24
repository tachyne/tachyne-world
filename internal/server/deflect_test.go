package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A punched ghast fireball flies back along the player's look as the
// player's own shot; a ghast it kills is a player's kill by a fireball,
// which drops the disc. Melee and mob-owned fireballs never do.
func TestDeflectFireball(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.yaw, pl.pitch = 0, 180, 0, 0, 0 // looking +z
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)
	w.ForceLoad(0, 0, 2) // projectiles fly only through loaded chunks
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	ghast := h.spawnMob(players, entityGhast, 0, 180, 6)
	ghast.health = 5
	a := h.launchProjectileIn(players, entityLargeFireball, 0, 0, 181, 2, -0.3, 0, -0.95)
	a.shooter, a.dmg, a.explode, a.fire = ghast.eid, 6, 1, true
	if !h.deflectProjectile(players, pl, a) { // what evAttack does for a projectile target
		t.Fatal("a fireball is redirectable")
	}
	if a.shooter != pl.p.eid || !a.playerShot || a.mobShot {
		t.Fatalf("the punched fireball should be the player's own (shooter %d playerShot %v)", a.shooter, a.playerShot)
	}
	if a.vx != 0 || a.vz <= 0.99 {
		t.Fatalf("AIM_DEFLECT sends it along the player's look, got (%.2f, %.2f, %.2f)", a.vx, a.vy, a.vz)
	}
	for i := 0; i < 12 && ghast.dying == 0; i++ {
		h.updateArrows(players)
	}
	if ghast.dying == 0 {
		t.Fatalf("the returned fireball should have struck the ghast (health %d, fireball at z=%.1f)", ghast.health, a.z)
	}
	if ghast.lastDirect != entityLargeFireball || !ghast.hitByPlayer {
		t.Fatalf("the killing blow should be the player's fireball (direct %d, byPlayer %v)", ghast.lastDirect, ghast.hitByPlayer)
	}
	h.despawnMob(players, ghast) // the death animation's end: the drops
	disc := int32(itemByName["music_disc_tears"])
	found := false
	for _, it := range h.items {
		if it.item == disc {
			found = true
		}
	}
	if !found {
		t.Fatal("a ghast killed by its returned fireball drops Tears")
	}

	// The table itself: no disc for a melee kill or a ghast's own shot.
	ctx := lootCtx{killedByPlayer: true, dt: dtPlayerAttack, rng: h.rng.Intn, randf: h.rng.Float64}
	if ds, _ := h.evalEntityLoot(int32(entityGhast), ctx); hasItem(ds, disc) {
		t.Fatal("a melee kill drops no disc")
	}
	ctx = lootCtx{killedByPlayer: false, direct: "fireball", dt: dtFireball, rng: h.rng.Intn, randf: h.rng.Float64}
	if ds, _ := h.evalEntityLoot(int32(entityGhast), ctx); hasItem(ds, disc) {
		t.Fatal("a ghast's own fireball drops no disc")
	}
	// A turtle's bowl asks for lightning the same way.
	bowl := int32(itemByName["bowl"])
	ctx = lootCtx{dt: dtLightningBolt, rng: h.rng.Intn, randf: h.rng.Float64}
	if ds, ok := h.evalEntityLoot(int32(entityTurtle), ctx); !ok || !hasItem(ds, bowl) {
		t.Fatal("a turtle struck by lightning drops a bowl")
	}
	ctx = lootCtx{dt: dtPlayerAttack, rng: h.rng.Intn, randf: h.rng.Float64}
	if ds, _ := h.evalEntityLoot(int32(entityTurtle), ctx); hasItem(ds, bowl) {
		t.Fatal("no bowl otherwise")
	}
	// An arrow is not redirectable.
	arrow := h.launchProjectileIn(players, entityArrow, 0, 0, 181, 2, 0, 0, -1)
	arrow.shooter, arrow.mobShot = ghast.eid, true
	if h.deflectProjectile(players, pl, arrow) || arrow.shooter != ghast.eid {
		t.Fatal("only #redirectable_projectile turns back")
	}
}

func hasItem(ds []drop, id int32) bool {
	for _, d := range ds {
		if d.item == id {
			return true
		}
	}
	return false
}
