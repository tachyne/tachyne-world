package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The stats that were already on the attribute pipeline get their effects for
// free — that is the whole payoff of the migration, so pin it.
func TestMobEffectsRideTheAttributePipeline(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnHostile(players, entityZombie, 0, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	baseDmg, baseSpeed, baseHP := m.attackDamage(), m.moveSpeed(), m.maxHP()

	h.applyMobEffect(players, m, effStrength, 0, 30)
	if got := m.attackDamage(); !closeTo(got, baseDmg+3) {
		t.Errorf("strength I on a mob: attack damage %v, want %v", got, baseDmg+3)
	}
	h.applyMobEffect(players, m, effSpeed, 0, 30)
	if got := m.moveSpeed(); !closeTo(got, baseSpeed*1.2) {
		t.Errorf("speed I on a mob: %v, want %v", got, baseSpeed*1.2)
	}
	h.applyMobEffect(players, m, effHealthBoost, 0, 30)
	if got := m.maxHP(); got != baseHP+4 {
		t.Errorf("health boost I on a mob: max health %d, want %d", got, baseHP+4)
	}

	// …and they all lift together when the effects go.
	for _, id := range []int32{effStrength, effSpeed, effHealthBoost} {
		h.removeMobEffect(players, m, id)
	}
	if !closeTo(m.attackDamage(), baseDmg) || !closeTo(m.moveSpeed(), baseSpeed) || m.maxHP() != baseHP {
		t.Errorf("stats did not return to base: dmg %v speed %v hp %d",
			m.attackDamage(), m.moveSpeed(), m.maxHP())
	}
}

// Poison hurts a mob but never kills it; the undead ignore it entirely.
func TestMobPoisonStopsAtOneAndSparesTheUndead(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	cow := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if cow == nil {
		t.Fatal("spawn returned nil")
	}
	h.applyMobEffect(players, cow, effPoison, 0, 60)
	for i := 0; i < 20*60 && cow.health > 1; i++ {
		h.updateMobEffects(players)
	}
	if cow.health != 1 {
		t.Errorf("poisoned cow at %d health, want to be left on 1", cow.health)
	}
	if cow.dying > 0 {
		t.Error("poison killed the cow — it never should")
	}

	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("zombie spawn returned nil")
	}
	before := z.health
	h.applyMobEffect(players, z, effPoison, 0, 60)
	if z.hasEffect(effPoison) != 0 {
		t.Error("poison stuck to a zombie (#ignores_poison_and_regen)")
	}
	for i := 0; i < 200; i++ {
		h.updateMobEffects(players)
	}
	if z.health != before {
		t.Errorf("zombie lost %d health to poison", before-z.health)
	}
}

// Instant Health is backwards on the undead — it harms them, and Harming heals.
func TestInstantEffectsInvertOnTheUndead(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("spawn returned nil")
	}
	z.health = z.maxHP()
	h.applyMobEffect(players, z, effInstantHealth, 0, 0)
	if z.health >= z.maxHP() {
		t.Errorf("healing a zombie left it at %d/%d — it should hurt", z.health, z.maxHP())
	}

	z.health = 1
	h.applyMobEffect(players, z, effInstantDamage, 0, 0)
	if z.health <= 1 {
		t.Errorf("harming a zombie left it at %d — it should heal", z.health)
	}

	// A living mob takes it the normal way round.
	cow := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if cow == nil {
		t.Fatal("cow spawn returned nil")
	}
	cow.health = 2
	h.applyMobEffect(players, cow, effInstantHealth, 0, 0)
	if cow.health <= 2 {
		t.Errorf("healing a cow left it at %d, want more", cow.health)
	}
}

// Resistance and Fire Resistance work on a mob the way they do on a player.
func TestMobResistanceAndFireResistance(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.setMaxHP(1000)
	m.health = 1000
	m.hurt(100)
	plain := 1000 - m.health

	m.health = 1000
	m.dmgFrac = 0
	h.applyMobEffect(players, m, effResistance, 1, 30) // −40%
	m.hurt(100)
	resisted := 1000 - m.health
	if resisted >= plain {
		t.Errorf("resistance II: took %d, want less than %d", resisted, plain)
	}

	m.ignite(10)
	if m.fireSecs == 0 {
		t.Fatal("the cow would not catch alight at all")
	}
	m.fireSecs = 0
	h.applyMobEffect(players, m, effFireRes, 0, 30)
	m.ignite(10)
	if m.fireSecs != 0 {
		t.Errorf("a fire-resistant mob caught alight for %d s", m.fireSecs)
	}
}

// A splash potion doses the mobs in its radius, not just the players.
func TestSplashPotionReachesMobs(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.x, m.y, m.z = 0.5, 70, 0.5

	effs := []potEffect{{effSlowness, 0, 60}}
	h.applyPotionAoEMob(players, m, effs, 1, 1)
	if m.hasEffect(effSlowness) == 0 {
		t.Error("a splash of slowness did nothing to the cow")
	}
}

// Enchanted armour a mob picked up now counts for something.
func TestMobGearEnchantmentsCount(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("spawn returned nil")
	}
	base := z.armorValue()

	// Diamond gear brings toughness with it, which used to be hardcoded to 0.
	diamond := itemByName["diamond_chestplate"]
	if armorInfo[diamond].Toughness <= 0 {
		t.Fatalf("diamond chestplate has no toughness in the table — fixture wrong")
	}
	z.gear[1] = invStack{item: diamond, count: 1,
		ench: [2]enchApply{{id: enchProtection, lvl: 4}, {id: enchRespiration, lvl: 3}}}
	z.refreshGearArmor()

	if z.armorValue() <= base {
		t.Errorf("armour %v with a chestplate on, want more than %v", z.armorValue(), base)
	}
	if z.armorToughness() <= 0 {
		t.Errorf("toughness %v with diamond gear on, want more than 0", z.armorToughness())
	}
	if got := z.mobAttrs().Value(attr.OxygenBonus); got != 3 {
		t.Errorf("oxygen bonus %v from Respiration III on mob gear, want 3", got)
	}
	// …and Protection IV on the piece really reduces a hit.
	if pts := protectionPoints(z.gear[:], dmgGeneric); pts != 4 {
		t.Errorf("protection points %d from one Protection IV piece, want 4", pts)
	}
}

// ATTACK_SPEED had no reader, so Haste and Mining Fatigue changed nothing.
func TestAttackSpeedChangesSwingRecovery(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	base := pl.attackPeriodTicks(20)
	if base != 20 {
		t.Fatalf("unmodified swing period %d, want the weapon's 20", base)
	}
	h.applyEffect(players, pl, effHaste, 1, 30) // +20%: recovers faster
	if hasted := pl.attackPeriodTicks(20); hasted >= base {
		t.Errorf("hasted swing period %d, want shorter than %d", hasted, base)
	}
	h.removeEffect(pl, effHaste)
	h.applyEffect(players, pl, effMiningFatigue, 1, 30) // −20%: drags out
	if tired := pl.attackPeriodTicks(20); tired <= base {
		t.Errorf("mining-fatigued swing period %d, want longer than %d", tired, base)
	}
}
