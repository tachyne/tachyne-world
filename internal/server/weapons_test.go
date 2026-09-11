package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestMobWeaponEnchantments: a mob's enchanted weapon bites harder
// (Sharpness), sets you alight (Fire Aspect), and a skeleton's Power/Flame
// bow shoots harder, burning arrows.
func TestMobWeaponEnchantments(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	z := h.spawnMob(players, entityZombie, 1.2, 180, 0.5)
	z.held = itemIronSword
	plain := (hostileMelee(z) + mobHeldBonus(z) + mobSharpness(z)) * h.diffMult()
	z.heldEnch = enchList{{id: enchSharpness, lvl: 5}, {id: enchFireAspect, lvl: 2}}
	sharp := (hostileMelee(z) + mobHeldBonus(z) + mobSharpness(z)) * h.diffMult()
	if want := plain + 3*h.diffMult(); sharp != want {
		t.Fatalf("Sharpness V adds 3: plain %v sharp %v want %v", plain, sharp, want)
	}
	pl.health, pl.fireSecs = 20, 0
	z.attackCD = 0
	h.mobMelee(players, z)
	if pl.health >= 20 || pl.fireSecs != 8 {
		t.Fatalf("the bite should land and Fire Aspect II burn 8 s: health %v fire %d", pl.health, pl.fireSecs)
	}
	// Ranged.
	sk := h.spawnMob(players, entitySkeleton, 5.5, 180, 0.5)
	sk.held = itemBow
	sk.heldEnch = enchList{{id: enchPower, lvl: 3}, {id: enchFlame, lvl: 1}, {id: enchPunch, lvl: 1}}
	before := len(h.arrows)
	h.spawnArrow(players, sk, pl)
	if len(h.arrows) != before+1 {
		t.Fatal("no arrow")
	}
	var a *arrowEntity
	for _, ar := range h.arrows {
		if ar.shooter == sk.eid {
			a = ar
		}
	}
	if a == nil || a.dmg <= arrowDamage || !a.fire || a.punch != 1 {
		t.Fatalf("Power III / Flame / Punch arrow: %+v", a)
	}
	// The weapon survives a save and reload with its enchantments, and drops
	// enchanted when the mob dies.
	sm := toSavedMob(z)
	h2 := newHub(world.New(1))
	h2.reloading = true
	m2 := h2.reloadMob(players, &sm)
	if m2.held != itemIronSword || m2.heldStack().enchLvl(enchSharpness) != 5 {
		t.Fatalf("reloaded held %d ench %v", m2.held, m2.heldEnch)
	}
	z.spawnGear, z.gearDrop = false, 0 // picked-up gear drops in full
	z.dying = 1
	h.despawnMob(players, z)
	found := false
	for _, it := range h.items {
		if it.item == itemIronSword && it.stack().enchLvl(enchSharpness) == 5 {
			found = true
		}
	}
	if !found {
		t.Fatal("the enchanted sword should drop with its enchantments")
	}
}
