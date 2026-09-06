package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Smite and Bane of Arthropods bite one family each and nothing else.
func TestFamilyMeleeBonusPicksItsFamily(t *testing.T) {
	smiter := invStack{item: tDiamondSword, count: 1,
		ench: enchList{{id: enchSmite, lvl: 4}}}
	baner := invStack{item: tDiamondSword, count: 1,
		ench: enchList{{id: enchBaneOfArthropods, lvl: 4}}}

	for _, c := range []struct {
		weapon invStack
		etype  int
		want   float64
		what   string
	}{
		{smiter, entityZombie, 10, "smite IV vs a zombie"},
		{smiter, entityDrowned, 10, "smite IV vs a drowned"},
		{smiter, entityWither, 10, "smite IV vs the wither"},
		{smiter, entityPhantom, 10, "smite IV vs a phantom"},
		{smiter, entityCow, 0, "smite IV vs a cow"},
		{smiter, entitySpider, 0, "smite IV vs a spider"},
		{baner, entitySpider, 10, "bane IV vs a spider"},
		{baner, entityCaveSpider, 10, "bane IV vs a cave spider"},
		{baner, entitySilverfish, 10, "bane IV vs a silverfish"},
		{baner, entityBee, 10, "bane IV vs a bee"},
		{baner, entityZombie, 0, "bane IV vs a zombie"},
	} {
		if got := familyMeleeBonus(c.weapon, c.etype); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", c.what, got, c.want)
		}
	}
}

// Fire Aspect sets the target alight for four seconds a level.
func TestFireAspectIgnites(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	m := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}

	// No enchantment: nothing catches.
	h.applyFireAspect(players, pl, m)
	if m.fireSecs != 0 {
		t.Fatalf("plain sword lit a cow for %d s", m.fireSecs)
	}

	pl.inv.slots[pl.p.heldSlot()] = invStack{item: tDiamondSword, count: 1,
		ench: enchList{{id: enchFireAspect, lvl: 2}}}
	h.applyFireAspect(players, pl, m)
	if want := fireAspectSecsPerLvl * 2; m.fireSecs != want {
		t.Errorf("fire aspect II lit the cow for %d s, want %d", m.fireSecs, want)
	}
	if !m.burning {
		t.Error("the cow is alight but not rendered burning")
	}
}

// Thorns rolls per piece, so a full set retaliates far more often than one.
func TestThornsRetaliates(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl

	m := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.setMaxHP(100000)
	m.health = 100000

	// Unarmoured: never retaliates, however many hits land.
	for i := 0; i < 200; i++ {
		h.thornsRetaliate(players, pl, m)
	}
	if m.health != 100000 {
		t.Fatalf("bare player retaliated for %d damage", 100000-m.health)
	}

	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1,
			ench: enchList{{id: enchThorns, lvl: 3}}}
	}
	for i := 0; i < 200; i++ {
		h.thornsRetaliate(players, pl, m)
	}
	if m.health >= 100000 {
		t.Error("a full Thorns III set never hit back over 200 blows")
	}
}

// Fire Protection shortens how long you burn; the attribute is what carries it.
func TestFireProtectionShortensBurning(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.setBurning(players, pl, 10)
	if pl.fireSecs != 10 {
		t.Fatalf("unprotected burn %d s, want 10", pl.fireSecs)
	}

	pl.fireSecs = 0
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1,
			ench: enchList{{id: enchFireProtection, lvl: 1}}}
	}
	h.setBurning(players, pl, 10)
	// Four pieces x level 1 = −60%: 10 s becomes 4.
	if pl.fireSecs != 4 {
		t.Errorf("burn under Fire Protection I x4 is %d s, want 4", pl.fireSecs)
	}
}

// Respiration is an OXYGEN_BONUS modifier and slows the drowning clock.
func TestRespirationSlowsDrowning(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	if h.keepsAirThisTick(pl) {
		t.Fatal("a bare player kept a breath")
	}

	pl.armor[0] = invStack{item: itemByName["iron_helmet"], count: 1,
		ench: enchList{{id: enchRespiration, lvl: 3}}}
	pl.refreshEnchantAttrs()
	if got := pl.playerAttrs().Value(attr.OxygenBonus); got != 3 {
		t.Fatalf("oxygen bonus %v under Respiration III, want 3", got)
	}
	kept := 0
	for i := 0; i < 4000; i++ {
		if h.keepsAirThisTick(pl) {
			kept++
		}
	}
	// 1-in-(3+1) chance of LOSING a breath, so ~75% are kept.
	if kept < 2700 || kept > 3300 {
		t.Errorf("kept %d breaths of 4000 under Respiration III, want about 3000", kept)
	}
}

// Blast Protection braces you against the shove as well as the burn.
func TestBlastProtectionResistsTheShove(t *testing.T) {
	pl := testTracked()
	if got := pl.explosionKnockScale(); got != 1 {
		t.Fatalf("bare player takes %v of a blast shove, want all of it", got)
	}
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1,
			ench: enchList{{id: enchBlastProtection, lvl: 1}}}
	}
	pl.refreshEnchantAttrs()
	// 4 pieces x 0.15 = 0.6 resistance, so 40% of the shove gets through.
	if got := pl.explosionKnockScale(); math.Abs(got-0.4) > 1e-9 {
		t.Errorf("blast knockback scale %v, want 0.4", got)
	}
}

// Curse of Vanishing destroys the item instead of dropping it.
func TestVanishingCurseDestroysOnDeath(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl

	cursed := itemByName["iron_helmet"]
	plain := itemByName["iron_sword"]
	pl.armor[0] = invStack{item: cursed, count: 1,
		ench: enchList{{id: enchVanishingCurse, lvl: 1}}}
	pl.inv.slots[0] = invStack{item: plain, count: 1}

	h.dropInventory(players, pl)
	got := map[int32]bool{}
	for _, it := range h.items {
		got[it.item] = true
	}
	if got[cursed] {
		t.Error("a Curse of Vanishing helmet was dropped rather than destroyed")
	}
	if !got[plain] {
		t.Error("the uncursed sword should still have dropped")
	}
}

// Frost Walker freezes the surface it walks over — sources only, air above.
func TestFrostWalkerFreezesTheSurface(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	// A patch of still water, high above the terrain.
	w := h.worldFor(0)
	const wy = 180
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			w.SetBlock(dx, wy, dz, worldgen.WaterBase)
		}
	}
	pl.x, pl.y, pl.z, pl.onGround = 0.5, wy+1, 0.5, true

	// Without the enchantment nothing freezes.
	h.frostWalk(players, pl)
	if w.At(0, wy, 0) != worldgen.WaterBase {
		t.Fatal("water froze with no Frost Walker on")
	}

	pl.armor[3] = invStack{item: itemByName["iron_boots"], count: 1,
		ench: enchList{{id: enchFrostWalker, lvl: 1}}}
	h.frostWalk(players, pl)
	if got := w.At(0, wy, 0); got < frostedIceMin || got > frostedIceMax {
		t.Errorf("underfoot block %d, want frosted ice", got)
	}
	// Radius 3 at level 1: the disk reaches 3 out but not 4.
	if got := w.At(3, wy, 0); got < frostedIceMin || got > frostedIceMax {
		t.Errorf("block 3 out is %d, want frosted ice inside the radius", got)
	}
	if got := w.At(4, wy, 0); got != worldgen.WaterBase {
		t.Errorf("block 4 out is %d, want untouched water outside the radius", got)
	}
}

// …and the ice it leaves ages back to water, so a lake does not stay paved.
func TestFrostedIceAgesBackToWater(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	const wy = 180
	pos := blockPos{0, wy, 0}
	w.SetBlock(pos.x, pos.y, pos.z, frostedIceMax) // already at the last age

	// Lit, alone, and at the final age: the next tick that rolls turns it back.
	melted := false
	for i := 0; i < 200 && !melted; i++ {
		h.tickFrostedIce(players, 0, pos, w.Block(pos.x, pos.y, pos.z))
		melted = w.At(pos.x, pos.y, pos.z) == worldgen.WaterBase
	}
	if !melted {
		t.Error("frosted ice at its last age never went back to water")
	}
}

// Soul Speed only helps while you are actually standing on soul sand.
func TestSoulSpeedAppliesOnSoulBlocksOnly(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	w := h.worldFor(0)
	const fy = 180
	w.SetBlock(0, fy, 0, soulSandBase)
	w.SetBlock(2, fy, 0, worldgen.BlockBase("stone"))

	pl.armor[3] = invStack{item: itemByName["iron_boots"], count: 1,
		ench: enchList{{id: enchSoulSpeed, lvl: 2}}}

	pl.x, pl.y, pl.z, pl.onGround = 0.5, fy+1, 0.5, true
	h.refreshSoulSpeed(pl)
	if !pl.playerAttrs().Get(attr.MovementSpeed).HasModifier(soulSpeedSource) {
		t.Error("Soul Speed did nothing while standing on soul sand")
	}

	pl.x = 2.5
	h.refreshSoulSpeed(pl)
	if pl.playerAttrs().Get(attr.MovementSpeed).HasModifier(soulSpeedSource) {
		t.Error("Soul Speed still applied after stepping onto stone")
	}
}
