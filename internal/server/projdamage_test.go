package server

import "testing"

// Every projectile used to deal `arrow` damage, whatever it was. That decided
// the wrong things: whether armour absorbed it, which protection enchantment
// applied, what the death message said — and the fireball type was never dealt
// at all, so armour protected against a ghast's fireball exactly as if it were
// an arrow.

func TestEachProjectileDealsItsOwnDamageType(t *testing.T) {
	cases := []struct {
		name  string
		etype int
		want  dmgType
	}{
		{"an arrow", entityArrow, dtArrow},
		{"a trident", entityTrident, dtTrident},
		{"a snowball", entitySnowball, dtThrown},
		{"an egg", entityEggProj, dtThrown},
		{"a blaze's fireball", entitySmallFireball, dtFireball},
		{"a ghast's fireball", entityLargeFireball, dtFireball},
		{"a wither skull", entityWitherSkull, dtWitherSkull},
		{"a shulker bullet", entityShulkerBullet, dtMobProjectile},
		{"llama spit", entityLlamaSpit, dtSpit},
		{"a wind charge", entityWindCharge, dtWindCharge},
		{"an ender pearl", entityPearlProj, dtEnderPearl},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := projectileDamage(c.etype); got != c.want {
				t.Errorf("%s deals %q, want %q",
					c.name, got.name(), c.want.name())
			}
		})
	}
}

// DamageSources.fireball: no owner means UNATTRIBUTED_FIREBALL, which is a
// distinct type — a dispenser-fired fireball is nobody's kill.
func TestAnOwnerlessFireballIsUnattributed(t *testing.T) {
	owned := &arrowEntity{etype: entityLargeFireball, shooter: 42}
	loose := &arrowEntity{etype: entityLargeFireball}
	if got := projectileDamageOf(owned); got != dtFireball {
		t.Errorf("an owned fireball deals %q, want fireball", got.name())
	}
	if got := projectileDamageOf(loose); got != dtUnattributedFireball {
		t.Errorf("an ownerless fireball deals %q, want unattributed_fireball",
			got.name())
	}
}

// WitherSkull.onHitEntity: only a skull with a living owner deals witherSkull
// damage; an ownerless one deals plain magic, and for less.
func TestAnOwnerlessWitherSkullIsMagic(t *testing.T) {
	owned := &arrowEntity{etype: entityWitherSkull, shooter: 7}
	loose := &arrowEntity{etype: entityWitherSkull}
	if got := projectileDamageOf(owned); got != dtWitherSkull {
		t.Errorf("an owned skull deals %q, want wither_skull", got.name())
	}
	if got := projectileDamageOf(loose); got != dtMagic {
		t.Errorf("an ownerless skull deals %q, want magic", got.name())
	}
}

// The owner rule applies ONLY to those two. An ownerless arrow is still an
// arrow — it is the fireball and skull sources that branch, not every one.
func TestOtherProjectilesIgnoreTheOwner(t *testing.T) {
	for _, etype := range []int{entityArrow, entityTrident, entitySnowball, entityLlamaSpit} {
		with := projectileDamageOf(&arrowEntity{etype: etype, shooter: 3})
		without := projectileDamageOf(&arrowEntity{etype: etype})
		if with != without {
			t.Errorf("type %d deals %q with an owner and %q without",
				etype, with.name(), without.name())
		}
	}
}

// The whole point: armour and the protection enchantments key off the damage
// type, so a fireball and an arrow must not resolve to the same one.
func TestAFireballIsNotAnArrow(t *testing.T) {
	if projectileDamage(entityLargeFireball) == projectileDamage(entityArrow) {
		t.Error("a fireball and an arrow deal the same damage type")
	}
	if !dtFireball.has(tagIsProjectile) {
		t.Error("the fireball type is not tagged as a projectile")
	}
}

// A launched projectile remembers what it is, or the type lookup has nothing
// to work from.
func TestALaunchedProjectileRemembersItsType(t *testing.T) {
	h, players := pushWorld(t)
	a := h.launchProjectile(players, entitySmallFireball, 0, 70, 0, 1, 0, 0)
	if a == nil {
		t.Fatal("no projectile")
	}
	if a.etype != entitySmallFireball {
		t.Errorf("projectile etype %d, want %d", a.etype, entitySmallFireball)
	}
	if got := projectileDamageOf(a); got != dtUnattributedFireball {
		t.Errorf("deals %q, want unattributed_fireball for an unowned launch",
			got.name())
	}
}
