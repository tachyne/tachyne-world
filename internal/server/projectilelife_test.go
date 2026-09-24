package server

import "testing"

// agedProjectile launches a projectile that has been in the air far longer
// than the transient lifetime, in the loaded open air of breezeRig.
func agedProjectile(h *hub, players map[int32]*tracked, etype int, shooter int32) *arrowEntity {
	h.tick.Add(5000)
	a := h.launchProjectileIn(players, etype, 0, 0.5, 185, 4.5, 0, 0, 0.01)
	a.shooter = shooter
	a.born = h.tick.Load() - 1000
	return a
}

// AbstractHurtingProjectile has no age: a ghast's fireball or a wither's
// skull flies on for as long as its chunk is loaded, even after its owner
// has left the world.
func TestHurtingProjectilesHaveNoAge(t *testing.T) {
	for _, et := range []int{entityLargeFireball, entitySmallFireball, entityWitherSkull, entityWindCharge} {
		h, players, _ := breezeRig(t)
		owner := h.spawnMob(players, entityGhast, 0.5, 182, -6)
		a := agedProjectile(h, players, et, owner.eid)
		h.tick.Add(1)
		h.updateArrows(players)
		if h.arrows[a.eid] == nil {
			t.Fatalf("entity type %d expired of old age; vanilla's hurting projectiles have none", et)
		}
		delete(h.mobs, owner.eid)
		h.tick.Add(1)
		h.updateArrows(players)
		if h.arrows[a.eid] == nil {
			t.Fatalf("entity type %d was discarded when its owner left the world", et)
		}
	}
}

// AbstractWindCharge.tick: a charge thirty blocks above the build limit
// bursts where it is.
func TestWindChargeBurstsAboveTheBuildLimit(t *testing.T) {
	h, players, _ := breezeRig(t)
	top := h.worldFor(0).Ceiling() - 1
	a := h.launchProjectileIn(players, entityWindCharge, 0, 0.5, float64(top+31)+0.5, 0.5, 0, 0.5, 0)
	h.tick.Add(1)
	h.updateArrows(players)
	if h.arrows[a.eid] != nil {
		t.Fatal("a wind charge thirty blocks over the build limit kept climbing")
	}
}

// ShulkerBullet has no age; it is discarded on peaceful (checkDespawn).
func TestShulkerBulletHasNoAge(t *testing.T) {
	h, players, _ := breezeRig(t)
	h.rules.Difficulty = diffNormal
	sh := h.spawnMob(players, entityShulker, 0.5, 180, -6)
	a := agedProjectile(h, players, entityShulkerBullet, sh.eid)
	h.tick.Add(1)
	h.updateArrows(players)
	if h.arrows[a.eid] == nil {
		t.Fatal("a shulker bullet expired of old age; vanilla's bullets have none")
	}
	h.rules.Difficulty = diffPeaceful
	h.tick.Add(1)
	h.updateArrows(players)
	if h.arrows[a.eid] != nil {
		t.Fatal("a shulker bullet survived the switch to peaceful")
	}
}

// Thrown litter keeps its transient lifetime.
func TestSnowballKeepsItsLifetime(t *testing.T) {
	h, players, pl := breezeRig(t)
	a := agedProjectile(h, players, entitySnowball, pl.p.eid)
	h.tick.Add(1)
	h.updateArrows(players)
	if h.arrows[a.eid] != nil {
		t.Fatal("an aged snowball did not expire")
	}
}

// AbstractHurtingProjectile.tick discards a projectile that has flown into
// a chunk that is not loaded (and never loads it).
func TestFireballEndsAtTheLoadedEdge(t *testing.T) {
	h, players, _ := breezeRig(t) // chunks −2..2 loaded
	a := h.launchProjectileIn(players, entityLargeFireball, 0, 0.5, 185, 0.5, 1.9, 0, 0)
	for i := 0; i < 60 && h.arrows[a.eid] != nil; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if h.arrows[a.eid] != nil {
		t.Fatalf("the fireball flew on out of the loaded world, at x=%.1f", a.x)
	}
	if h.world.Loaded(3, 0) {
		t.Fatal("the fireball's flight loaded chunks beyond the edge")
	}
}
