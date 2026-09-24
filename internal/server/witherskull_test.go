package server

import "testing"

// WitherSkull.onHitEntity ends with the owner healing 5 when the skull KILLS
// what it hit. Without it a wither could not feed on a drawn-out fight, which
// made the boss meaningfully easier than vanilla's.

func skullHub(t *testing.T) (*hub, map[int32]*tracked, *mob) {
	t.Helper()
	h, players := pushWorld(t)
	h.world.ForceLoad(0, 0, 2) // projectiles fly only through loaded chunks
	w := putMob(t, h, players, entityWither, 0.5, 70, 0.5)
	w.health = 10 // wounded, with room to heal
	return h, players, w
}

// The kill heals the wither that fired the skull.
func TestAWitherHealsWhenItsSkullKills(t *testing.T) {
	h, players, w := skullHub(t)
	victim := putMob(t, h, players, entityCow, 5, 70, 0.5)
	victim.health = 1

	a := h.launchProjectile(players, entityWitherSkull, 4, 70, 0.5, 1, 0, 0)
	a.shooter, a.dmg = w.eid, 8
	before := w.health

	h.arrowHitsMob(players, a, victim.x, victim.y+0.5, victim.z)

	if victim.health > 0 {
		t.Fatal("the skull did not kill the cow, so there is no heal to check")
	}
	if got := w.health - before; got != witherSkullHealHP {
		t.Errorf("wither healed %d, want %d", got, witherSkullHealHP)
	}
}

// A skull that only WOUNDS heals nothing — vanilla runs the post-attack
// effects on that branch instead.
func TestAWoundingSkullHealsNothing(t *testing.T) {
	h, players, w := skullHub(t)
	victim := putMob(t, h, players, entityCow, 5, 70, 0.5)
	victim.health = 200 // survives comfortably

	a := h.launchProjectile(players, entityWitherSkull, 4, 70, 0.5, 1, 0, 0)
	a.shooter, a.dmg = w.eid, 8
	before := w.health

	h.arrowHitsMob(players, a, victim.x, victim.y+0.5, victim.z)
	if victim.health <= 0 {
		t.Fatal("the cow died; this test needs it to survive")
	}
	if w.health != before {
		t.Errorf("wither healed to %d off a kill it did not make", w.health)
	}
}

// Only a WITHER SKULL feeds its owner. An arrow kill heals nobody.
func TestOtherProjectilesDoNotFeedTheirShooter(t *testing.T) {
	h, players, w := skullHub(t)
	victim := putMob(t, h, players, entityCow, 5, 70, 0.5)
	victim.health = 1

	a := h.launchProjectile(players, entityArrow, 4, 70, 0.5, 1, 0, 0)
	a.shooter, a.dmg = w.eid, 8
	before := w.health

	h.arrowHitsMob(players, a, victim.x, victim.y+0.5, victim.z)
	if w.health != before {
		t.Errorf("an arrow kill healed its shooter to %d", w.health)
	}
}

// The heal cannot carry a wither past its maximum.
func TestTheHealIsCappedAtFullHealth(t *testing.T) {
	h, players, w := skullHub(t)
	w.health = w.maxHP() - 1
	victim := putMob(t, h, players, entityCow, 5, 70, 0.5)
	victim.health = 1

	a := h.launchProjectile(players, entityWitherSkull, 4, 70, 0.5, 1, 0, 0)
	a.shooter, a.dmg = w.eid, 8

	h.arrowHitsMob(players, a, victim.x, victim.y+0.5, victim.z)
	if w.health > w.maxHP() {
		t.Errorf("wither healed to %d, past its maximum of %d", w.health, w.maxHP())
	}
}

// An ownerless skull — the same case that makes it deal magic rather than
// wither_skull — has nobody to feed.
func TestAnOwnerlessSkullFeedsNobody(t *testing.T) {
	h, players, w := skullHub(t)
	victim := putMob(t, h, players, entityCow, 5, 70, 0.5)
	victim.health = 1

	a := h.launchProjectile(players, entityWitherSkull, 4, 70, 0.5, 1, 0, 0)
	a.dmg = 8 // no shooter
	before := w.health

	h.arrowHitsMob(players, a, victim.x, victim.y+0.5, victim.z)
	if w.health != before {
		t.Errorf("an ownerless skull healed a bystanding wither to %d", w.health)
	}
}

// WitherSkull.onHitEntity: Wither II for 10 s on normal and 40 s on hard,
// and nothing at all on easy — fired by a wither and flown to the hit.
func TestWitherSkullWitherByDifficulty(t *testing.T) {
	for _, c := range []struct {
		name  string
		diff  int
		ticks int
	}{
		{"easy", diffEasy, 0},
		{"normal", diffNormal, 200},
		{"hard", diffHard, 800},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, players, w := skullHub(t)
			h.rules.Difficulty = c.diff
			pl := survPlayer(h)
			pl.x, pl.y, pl.z = 6.5, 70, 0.5
			pl.health = 1000 // survive the skull and its blast
			players[pl.p.eid] = pl
			h.witherSkullAt(players, w, pl.x, pl.y+1, pl.z, false)
			for i := 0; i < 60 && len(h.arrows) > 0; i++ {
				h.tick.Add(1)
				h.updateArrows(players)
			}
			if len(h.arrows) != 0 {
				t.Fatal("the skull never struck")
			}
			if pl.health >= 1000 {
				t.Fatal("the skull missed the player")
			}
			e := pl.effects[effWither]
			if c.ticks == 0 {
				if e != nil {
					t.Fatalf("a skull on easy withered the player: %+v", *e)
				}
				return
			}
			if e == nil || e.amp != 1 || e.left != c.ticks {
				t.Fatalf("wither effect %+v, want Wither II for %d ticks", e, c.ticks)
			}
		})
	}
}
