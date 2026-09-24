package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// flyUntilGone runs the projectile tick until nothing is in the air.
func flyUntilGone(h *hub, players map[int32]*tracked, ticks int) {
	for i := 0; i < ticks && len(h.arrows) > 0; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
}

// A ghast's fireball hurts what it strikes but does not set it alight
// (LargeFireball.onHitEntity deals its 6 and nothing else).
func TestGhastFireballHitDoesNotIgnite(t *testing.T) {
	h, pl, players := killRig(t)
	h.rules.MobGriefing = false
	h.arrows = map[int32]*arrowEntity{}
	ghast := h.spawnMob(players, entityGhast, 0, 180, 10)
	ghast.ghastCharge = ghastChargeFire - 1
	h.ghastTick(players, ghast)
	if len(h.arrows) != 1 {
		t.Fatalf("the ghast did not fire: %d projectiles", len(h.arrows))
	}
	flyUntilGone(h, players, 150)
	if pl.health >= 20 {
		t.Fatalf("the fireball never struck the player (health %v)", pl.health)
	}
	if pl.fireSecs != 0 {
		t.Errorf("a ghast fireball set the player burning for %d s", pl.fireSecs)
	}
}

// A blaze's small fireball sets its target alight, and a blow that does not
// land (Fire Resistance turns fireball damage away) leaves the old fire as it was.
func TestSmallFireballIgnitesOnlyWhenItHurts(t *testing.T) {
	h, pl, players := killRig(t)
	h.arrows = map[int32]*arrowEntity{}
	blaze := h.spawnMob(players, entityBlaze, 0, 180, 6)
	a := h.launchProjectileIn(players, entitySmallFireball, 0, 0, 181, 3, 0, 0, -hurtingSpeed)
	a.shooter, a.dmg, a.fire = blaze.eid, blazeFireballDmg, true
	flyUntilGone(h, players, 100)
	if pl.health >= 20 || pl.fireSecs == 0 {
		t.Fatalf("the fireball should hurt and ignite: health %v, fire %d s", pl.health, pl.fireSecs)
	}

	h, pl, players = killRig(t)
	h.arrows = map[int32]*arrowEntity{}
	h.applyEffect(players, pl, effFireRes, 0, 60)
	pl.fireSecs = 2
	a = h.launchProjectileIn(players, entitySmallFireball, 0, 0, 181, 3, 0, 0, -hurtingSpeed)
	a.shooter, a.dmg, a.fire = blaze.eid, blazeFireballDmg, true
	flyUntilGone(h, players, 100)
	if pl.health < 20 {
		t.Fatalf("Fire Resistance should turn the fireball away (health %v)", pl.health)
	}
	if pl.fireSecs != 2 {
		t.Errorf("a blow that failed left fire %d s, want the old 2", pl.fireSecs)
	}
}

// A dispensed fire charge is an ownerless small fireball: it burns and hurts.
func TestDispensedFireChargeBurnsAndHurts(t *testing.T) {
	h, pl, players := killRig(t)
	pl.x, pl.z = 0.5, 0.5 // in line with the dispenser's mouth
	h.arrows = map[int32]*arrowEntity{}
	info, _ := worldgen.InfoForState(dispenserMin)
	state := worldgen.SetProperty(info, dispenserMin, "facing", "north") // toward −z, at the player
	pos := blockPos{0, 181, 4}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	b := &bin{slots: make([]invStack, 9)}
	b.slots[0] = invStack{item: itemFireCharge, count: 1}
	h.bins[simPos{blockPos: pos}] = b
	h.ejectFromBin(players, simPos{blockPos: pos}, state)
	if len(h.arrows) != 1 {
		t.Fatalf("the dispenser fired %d projectiles", len(h.arrows))
	}
	flyUntilGone(h, players, 100)
	if pl.health >= 20 || pl.fireSecs == 0 {
		t.Errorf("a dispensed fire charge should hurt and ignite: health %v, fire %d s", pl.health, pl.fireSecs)
	}
}

// A small fireball a player batted back sets the mob it strikes alight.
func TestSmallFireballIgnitesAMob(t *testing.T) {
	h, pl, players := killRig(t)
	h.arrows = map[int32]*arrowEntity{}
	z := h.spawnMob(players, entityZombie, 0, 180, 6)
	a := h.launchProjectileIn(players, entitySmallFireball, 0, 0, 181, 3, 0, 0, hurtingSpeed)
	a.shooter, a.dmg, a.fire, a.playerShot = pl.p.eid, blazeFireballDmg, true, true
	flyUntilGone(h, players, 100)
	if z.health >= z.maxHP() {
		t.Fatalf("the fireball never struck the zombie")
	}
	if z.fireSecs == 0 {
		t.Error("a small fireball did not set the zombie alight")
	}
}
