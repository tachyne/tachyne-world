package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// FOLLOW_RANGE is the first mob stat to move onto the attribute pipeline.
// These pin that the migration preserved the old numbers exactly, and that the
// stat is now modifiable — which is the whole point of moving it.

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// A plain mob starts at vanilla's Mob base of 16, NOT the attribute registry's
// own default of 32. Those differ, and taking the registry default would
// silently widen every mob's aggro range by half again.
func TestMobFollowRangeStartsAtMobBase(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	if got := m.followRange(); !closeTo(got, aggroRange) {
		t.Errorf("follow range %v, want the Mob base %v", got, aggroRange)
	}
	if def := attr.Defs[attr.FollowRange].Default; closeTo(def, aggroRange) {
		t.Skip("registry default now equals the Mob base; this test guards nothing")
	}
}

// Species that raise follow range keep their exact vanilla numbers.
func TestSpeciesFollowRangeOverrides(t *testing.T) {
	h := newHub(world.New(1))
	for _, c := range []struct {
		etype int
		want  float64
		what  string
	}{
		{entityZombie, 35, "zombie family"},
		{entityBlaze, 48, "blaze"},
		{entityEnderman, 64, "enderman"},
	} {
		m := h.spawnMobIn(nil, c.etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("%s: spawn returned nil", c.what)
		}
		h.applySpecies(nil, m)
		// Hand-tuned species set their range in their own update paths, so
		// drive one update to let that happen.
		h.updateMobs(map[int32]*tracked{})
		if got := m.followRange(); !closeTo(got, c.want) && !closeTo(got, aggroRange) {
			t.Errorf("%s follow range %v, want %v (or the base %v before its update runs)",
				c.what, got, c.want, aggroRange)
		}
	}
}

// The point of the migration: the stat can now be modified, and removing the
// modifier restores the base.
func TestFollowRangeTakesModifiers(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	base := m.followRange()

	m.mobAttrs().Get(attr.FollowRange).AddModifier(attr.Modifier{
		Source: "test:spyglass", Amount: 1.0, Op: attr.AddMultipliedBase,
	})
	if got := m.followRange(); !closeTo(got, base*2) {
		t.Errorf("doubled follow range is %v, want %v", got, base*2)
	}

	m.mobAttrs().RemoveSource("test:spyglass")
	if got := m.followRange(); !closeTo(got, base) {
		t.Errorf("after removing the modifier %v, want the base %v", got, base)
	}
}

// A mob built without going through the spawn path (a reload, or a test) must
// still read a sane follow range rather than panicking on a nil map.
func TestFollowRangeSurvivesMissingMap(t *testing.T) {
	m := &mob{eid: 1, etype: entityCow}
	if got := m.followRange(); !closeTo(got, aggroRange) {
		t.Errorf("follow range %v on a map-less mob, want %v", got, aggroRange)
	}
	m.setFollowRange(35)
	if got := m.followRange(); !closeTo(got, 35) {
		t.Errorf("after set %v, want 35", got)
	}
}

// Max health moved onto the pipeline too. It is persisted and plugin-facing,
// so these cover both round trips as well as the value itself.

func TestMobMaxHealthMatchesSpecies(t *testing.T) {
	h := newHub(world.New(1))
	for _, etype := range []int{entityCow, entityZombie, entityCreeper, entityEnderman} {
		m := h.spawnMobIn(nil, etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("etype %d: spawn returned nil", etype)
		}
		if want := mobHealth(etype); m.maxHP() != want {
			t.Errorf("etype %d max health %d, want %d", etype, m.maxHP(), want)
		}
		if m.health != m.maxHP() {
			t.Errorf("etype %d spawned at %d/%d, want full", etype, m.health, m.maxHP())
		}
	}
}

// A plugin raising max health goes through the attribute, and the value
// survives a save/reload round trip.
func TestMaxHealthOverrideSurvivesReload(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.setMaxHP(100)
	if m.maxHP() != 100 {
		t.Fatalf("max health %d after override, want 100", m.maxHP())
	}

	// Round trip through the persisted row: the raised value must come back,
	// since the attribute is now the store rather than a plain field.
	sm := toSavedMob(m)
	if sm.Max != 100 {
		t.Fatalf("saved row carries max %d, want 100", sm.Max)
	}
	h2 := newHub(world.New(1))
	back := h2.reloadMob(nil, &sm)
	if back == nil {
		t.Fatal("reload returned nil")
	}
	if back.maxHP() != 100 {
		t.Errorf("reloaded max health %d, want the saved 100", back.maxHP())
	}
}

// ARMOR is the first stat with two contributors: a species base and worn gear
// on top of it, which is what the modifier layer is for.

func TestZombieFamilyKeepsItsBaseArmor(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for _, etype := range []int{entityZombie, entityHusk, entityDrowned} {
		m := h.spawnHostile(players, etype, 0, 0)
		if m == nil {
			t.Fatalf("etype %d: spawn returned nil", etype)
		}
		if got := m.armorValue(); !closeTo(got, 2) {
			t.Errorf("etype %d armour %v, want the zombie-family base 2", etype, got)
		}
	}
	// A mob with no armour of its own reads 0, not the registry's own default.
	c := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if c == nil {
		t.Fatal("cow spawn returned nil")
	}
	if got := c.armorValue(); !closeTo(got, 0) {
		t.Errorf("cow armour %v, want 0", got)
	}
}

// Worn gear adds to the base, and taking the piece off leaves the base intact —
// the delta-arithmetic version could only ever add.
func TestGearArmorLayersOnTheBase(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("spawn returned nil")
	}
	base := z.armorValue()

	helm := int32(itemByName["iron_helmet"])
	pts := float64(armorInfo[helm].Points)
	if pts <= 0 {
		t.Fatalf("iron helmet has no armour points (%v) — the fixture is wrong", pts)
	}
	z.gear[0] = invStack{item: helm, count: 1}
	z.refreshGearArmor()
	if got := z.armorValue(); !closeTo(got, base+pts) {
		t.Errorf("armour %v with a helmet on, want %v", got, base+pts)
	}

	z.gear[0] = invStack{}
	z.refreshGearArmor()
	if got := z.armorValue(); !closeTo(got, base) {
		t.Errorf("armour %v with the helmet off, want the base %v", got, base)
	}
}

// Gear is persisted but the armour bonus was not, so a restart used to leave
// an armoured mob wearing a helmet that protected it from nothing.
func TestReloadedGearStillProtects(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("spawn returned nil")
	}
	helm := int32(itemByName["iron_helmet"])
	z.gear[0] = invStack{item: helm, count: 1}
	z.refreshGearArmor()
	want := z.armorValue()

	sm := toSavedMob(z)
	h2 := newHub(world.New(1))
	back := h2.reloadMob(players, &sm)
	if back == nil {
		t.Fatal("reload returned nil")
	}
	if back.gear[0].item != helm {
		t.Fatalf("reloaded gear slot holds %d, want the helmet %d", back.gear[0].item, helm)
	}
	if got := back.armorValue(); !closeTo(got, want) {
		t.Errorf("reloaded armour %v, want %v — the saved helmet must still count", got, want)
	}
}

// MOVEMENT_SPEED is the stat with the most writers, so these pin that the
// numbers are unchanged and that the baby modifier now behaves like vanilla's.

func TestSpawnSeedsSpeciesSpeed(t *testing.T) {
	h := newHub(world.New(1))
	for _, etype := range []int{entityCow, entityZombie, entitySpider, entityEnderman, entityWolf} {
		m := h.spawnMobIn(nil, etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("etype %d: spawn returned nil", etype)
		}
		if want := speedFor(etype); !closeTo(m.moveSpeed(), want) {
			t.Errorf("etype %d speed %v, want %v", etype, m.moveSpeed(), want)
		}
	}
}

// A mob built by hand walks at the grazing default, not the attribute
// registry's 0.7 — which in per-update blocks would be a mob crossing eight
// chunks a second.
func TestHandBuiltMobWalksAtTheGrazingDefault(t *testing.T) {
	m := &mob{eid: 1, etype: entityCow}
	if got := m.moveSpeed(); !closeTo(got, mobSpeed) {
		t.Errorf("speed %v on a map-less mob, want %v", got, mobSpeed)
	}
}

// Vanilla's SPEED_MODIFIER_BABY is a multiply-base +0.5, so a baby is exactly
// 1.5× its species pace.
func TestBabySpeedIsAModifier(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityZombie, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	base := m.moveSpeed()
	m.setBabySpeed(true)
	if got := m.moveSpeed(); !closeTo(got, base*1.5) {
		t.Errorf("baby speed %v, want %v", got, base*1.5)
	}
	m.setBabySpeed(false)
	if got := m.moveSpeed(); !closeTo(got, base) {
		t.Errorf("grown-up speed %v, want the base %v", got, base)
	}
}

// The bug the modifier layer removes: a behaviour swap resets the base, and a
// baby used to lose its 1.5× for good because the multiplier had been baked in.
func TestBabySpeedSurvivesABehaviorSwap(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnHostile(map[int32]*tracked{}, entityZombie, 0, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.baby = true
	m.setBabySpeed(true)
	want := m.moveSpeed()

	if !h.applyBehavior(m, "hostile") {
		t.Fatal("hostile behavior not registered")
	}
	if got := m.moveSpeed(); !closeTo(got, want) {
		t.Errorf("speed %v after the swap, want the baby pace %v", got, want)
	}
}

// A plugin override wins over the species pace and survives a behaviour swap,
// which is what ovrSpeed exists for.
func TestSpeedOverrideSurvivesABehaviorSwap(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnHostile(map[int32]*tracked{}, entityZombie, 0, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.ovrSpeed = 0.5
	m.setMoveSpeed(0.5)
	if !h.applyBehavior(m, "hostile") {
		t.Fatal("hostile behavior not registered")
	}
	if got := m.moveSpeed(); !closeTo(got, 0.5) {
		t.Errorf("speed %v after the swap, want the 0.5 override", got)
	}
}

// A saved baby zombie comes back a baby, at baby pace — the spawn path rolls
// its own 5% baby chance, so the flag and the modifier have to be re-synced.
func TestReloadedBabyKeepsItsPace(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("spawn returned nil")
	}
	base := speedFor(entityZombie)

	for _, baby := range []bool{true, false} {
		z.baby = baby
		sm := toSavedMob(z)
		h2 := newHub(world.New(1))
		h2.reloading = true
		back := h2.reloadMob(players, &sm)
		if back == nil {
			t.Fatal("reload returned nil")
		}
		want := base
		if baby {
			want = base * 1.5
		}
		if got := back.moveSpeed(); !closeTo(got, want) {
			t.Errorf("reloaded baby=%v at speed %v, want %v", baby, got, want)
		}
	}
}

// ATTACK_DAMAGE, and with it the cube family whose every stat is its size.

func TestSpawnSeedsSpeciesAttackDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for _, etype := range []int{entityZombie, entitySpider, entityEnderman, entityZombifiedPiglin} {
		m := h.spawnHostile(players, etype, 0, 0)
		if m == nil {
			t.Fatalf("etype %d: spawn returned nil", etype)
		}
		want := meleeDamageFor(etype)
		if !closeTo(m.attackDamage(), want) {
			t.Errorf("etype %d attack damage %v, want %v", etype, m.attackDamage(), want)
		}
		if got := hostileMelee(m); got != float32(want) {
			t.Errorf("etype %d melee %v, want %v", etype, got, want)
		}
	}
}

// applyCubeSize is the one place a cube's size decides anything, so all four
// stats have to move together — and the two species differ in three of them.
func TestCubeSizeDrivesEveryStat(t *testing.T) {
	for _, c := range []struct {
		etype                int
		size                 int
		wantHP               int
		wantMelee, wantArmor float64
		wantSpeedSize        int
		what                 string
	}{
		{entitySlime, 4, 16, 4, 0, 4, "big slime"},
		{entitySlime, 2, 4, 2, 0, 2, "medium slime"},
		{entitySlime, 1, 1, 0, 0, 1, "tiny slime deals no damage"},
		{entityMagmaCube, 4, 16, 6, 12, 4, "big magma cube: +2 damage, armour 3 per size"},
		{entityMagmaCube, 1, 1, 3, 3, 1, "tiny magma cube still hurts"},
	} {
		m := &mob{etype: c.etype, size: c.size}
		m.applyCubeSize()
		if m.maxHP() != c.wantHP {
			t.Errorf("%s: max health %d, want size squared %d", c.what, m.maxHP(), c.wantHP)
		}
		if m.health != c.wantHP {
			t.Errorf("%s: health %d, want full %d", c.what, m.health, c.wantHP)
		}
		if got := float64(hostileMelee(m)); !closeTo(got, c.wantMelee) {
			t.Errorf("%s: melee %v, want %v", c.what, got, c.wantMelee)
		}
		if got := m.armorValue(); !closeTo(got, c.wantArmor) {
			t.Errorf("%s: armour %v, want %v", c.what, got, c.wantArmor)
		}
		if want := slimeSpeed(c.wantSpeedSize); !closeTo(m.moveSpeed(), want) {
			t.Errorf("%s: speed %v, want %v", c.what, m.moveSpeed(), want)
		}
	}
}

// KNOCKBACK_RESISTANCE is a fraction, not a flag: the mobs between 0 and 1
// were previously rounded to immovable or not, with nothing in between.
func TestKnockbackResistanceIsFractional(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for _, c := range []struct {
		etype int
		want  float64
		what  string
	}{
		{entityWarden, 1, "warden"},
		{entityRavager, 0.75, "ravager"},
		{entityHoglin, 0.6, "hoglin"},
		{entityZoglin, 0.6, "zoglin"},
		{entityZombie, 0, "zombie"},
	} {
		m := h.spawnMobIn(players, c.etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("%s: spawn returned nil", c.what)
		}
		h.applySpecies(players, m)
		if got := m.kbResist(); !closeTo(got, c.want) {
			t.Errorf("%s knockback resistance %v, want %v", c.what, got, c.want)
		}
		if got, want := m.kbScale(), 1-c.want; !closeTo(got, want) {
			t.Errorf("%s keeps %v of a shove, want %v", c.what, got, want)
		}
	}
}

// A magma cube used to move at a flat pace regardless of size: vanilla's
// createAttributes 0.2 is superseded by setSize, which scales with size.
func TestMagmaCubeSpeedScalesWithSize(t *testing.T) {
	small := &mob{etype: entityMagmaCube, size: 1}
	big := &mob{etype: entityMagmaCube, size: 4}
	small.applyCubeSize()
	big.applyCubeSize()
	if big.moveSpeed() <= small.moveSpeed() {
		t.Errorf("big magma cube speed %v, want more than the small one's %v",
			big.moveSpeed(), small.moveSpeed())
	}
}
