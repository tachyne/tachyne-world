package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The effects that are nothing but an attribute modifier now really are: the
// table IS the implementation, so these check the table drives the value.

func TestEffectModifiersFollowVanillaAmounts(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	for _, c := range []struct {
		id   int32
		amp  int
		read func() float64
		want float64
		what string
	}{
		{effHealthBoost, 0, func() float64 { return float64(pl.maxHP()) }, 24, "health boost I: +4 max health"},
		{effHealthBoost, 2, func() float64 { return float64(pl.maxHP()) }, 32, "health boost III: +12"},
		{effLuck, 0, pl.luck, 1, "luck I"},
		{effLuck, 4, pl.luck, 5, "luck V"},
		{effStrength, 0, func() float64 { return pl.playerAttrs().Value(attr.AttackDamage) }, 4, "strength I: fist 1 + 3"},
		{effSpeed, 0, pl.movementFactor, 1.2, "speed I: +20%"},
		{effSlowness, 0, pl.movementFactor, 0.85, "slowness I: -15%"},
	} {
		h.applyEffect(players, pl, c.id, c.amp, 30)
		if got := c.read(); !closeTo(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.what, got, c.want)
		}
		h.removeEffect(pl, c.id)
	}
}

// An effect's modifiers must go when it does, or a 30-second potion is
// permanent.
func TestEffectModifiersLiftOnExpiry(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.applyEffect(players, pl, effHealthBoost, 1, 30)
	if pl.maxHP() != 28 {
		t.Fatalf("max health %v under health boost II, want 28", pl.maxHP())
	}
	pl.health = 28

	h.removeEffect(pl, effHealthBoost)
	if pl.maxHP() != maxHealth {
		t.Errorf("max health %v after expiry, want %v", pl.maxHP(), maxHealth)
	}
	if pl.health > pl.maxHP() {
		t.Errorf("health %v left above the ceiling %v", pl.health, pl.maxHP())
	}
}

// Luck and Unluck are the same attribute pulling opposite ways.
func TestLuckAndUnluckCancel(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.applyEffect(players, pl, effLuck, 1, 30)   // +2
	h.applyEffect(players, pl, effUnluck, 1, 30) // -2
	if got := pl.luck(); !closeTo(got, 0) {
		t.Errorf("luck %v with both running, want 0", got)
	}
	h.removeEffect(pl, effUnluck)
	if got := pl.luck(); !closeTo(got, 2) {
		t.Errorf("luck %v with only Luck II, want 2", got)
	}
}

// Invisibility and Glowing share one metadata byte with the burning flag,
// which is why it has to be composed rather than written a bit at a time.
func TestPlayerEntityFlagsCompose(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	if got := playerEntityFlags(pl); got != 0 {
		t.Errorf("flags %#x on a plain player, want 0", got)
	}
	pl.fireSecs = 5
	h.applyEffect(players, pl, effInvisibility, 0, 30)
	h.applyEffect(players, pl, effGlowing, 0, 30)
	want := byte(entFlagOnFire | entFlagInvisible | entFlagGlowing)
	if got := playerEntityFlags(pl); got != want {
		t.Errorf("flags %#x, want %#x — burning, invisible and glowing at once", got, want)
	}
	h.removeEffect(pl, effInvisibility)
	if got := playerEntityFlags(pl); got != byte(entFlagOnFire|entFlagGlowing) {
		t.Errorf("flags %#x after invisibility lapsed, want the other two intact", got)
	}
}

// Conduit Power holds your breath the way Water Breathing does.
func TestConduitPowerStopsDrowning(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	if pl.breathesUnderwater() {
		t.Fatal("a plain player should not breathe underwater")
	}
	h.applyEffect(players, pl, effConduitPower, 0, 30)
	if !pl.breathesUnderwater() {
		t.Error("conduit power should suspend the drowning clock")
	}
}

// Saturation fills food and saturation together, and stops at the cap.
func TestSaturationFeeds(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.food, pl.saturation = 10, 0

	h.applyEffect(players, pl, effSaturation, 0, 30)
	if pl.food != 11 || pl.saturation != 2 {
		t.Errorf("food/saturation %d/%v after one application, want 11/2", pl.food, pl.saturation)
	}
	pl.food, pl.saturation = maxFood, float32(maxFood)
	h.feedSaturation(pl, 0) // already full — must not overflow
	if pl.food != maxFood || pl.saturation != float32(maxFood) {
		t.Errorf("food/saturation %d/%v when already full", pl.food, pl.saturation)
	}
}

// Bad Omen no longer drops a raid on your head the moment you reach a village,
// and with no village in range it does nothing at all.
func TestBadOmenNeedsAVillage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.applyEffect(players, pl, effBadOmen, 1, badOmenSecs)
	h.checkRaidTrigger(players, pl)
	if pl.hasEffect(effBadOmen) == 0 {
		t.Error("bad omen consumed with no village in range")
	}
	if pl.hasEffect(effRaidOmen) != 0 {
		t.Error("raid omen granted with no village in range")
	}
	if pl.raidOmenSet {
		t.Error("a raid position was remembered with no village in range")
	}
}

// The Raid Omen is a fuse: the raid lands where it was lit, when it burns out.
func TestRaidOmenFuseStartsTheRaid(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	pl.raidOmenPos, pl.raidOmenSet = blockPos{40, 70, 40}, true
	h.raidOmenExpired(players, pl)
	if pl.raidOmenSet {
		t.Error("the remembered position should be spent once the raid starts")
	}
	if len(h.raids) == 0 {
		t.Fatal("no raid started when the omen expired")
	}
	// Firing again must not stack a second raid on the same omen.
	h.raidOmenExpired(players, pl)
	if len(h.raids) != 1 {
		t.Errorf("%d raids after a spent omen fired twice, want 1", len(h.raids))
	}
}

// The ominous effects fire on death, not while they run.
func TestOozingSpillsSlimesOnDeath(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl

	before := len(h.mobs)
	h.applyEffect(players, pl, effOozing, 0, 30)
	if len(h.mobs) != before {
		t.Fatal("oozing spawned slimes just for being applied")
	}
	h.damageOf(players, pl, 100, dtGeneric)
	if !pl.dead {
		t.Fatal("100 damage should have killed the player")
	}
	slimes := 0
	for _, m := range h.mobs {
		if m.etype == entitySlime {
			slimes++
			if m.size != oozingSlimeSize {
				t.Errorf("oozing slime size %d, want %d", m.size, oozingSlimeSize)
			}
		}
	}
	if slimes != oozingSlimeCount {
		t.Errorf("%d slimes spilled, want %d", slimes, oozingSlimeCount)
	}
}

// Weaving leaves cobwebs where you fell — on ground, never in mid-air.
func TestWeavingStringsCobwebsOnSolidGround(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	// A small stone floor to die on, well above the terrain.
	stone, _ := worldgen.BlockRange("stone")
	w := h.worldFor(0)
	const fy = 180
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(dx, fy, dz, stone)
		}
	}
	pl.x, pl.y, pl.z = 0.5, fy+1, 0.5

	h.applyEffect(players, pl, effWeaving, 0, 30)
	h.weaveCobwebs(players, pl)

	web, _ := worldgen.BlockRange("cobweb")
	webs := 0
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				if w.At(dx, fy+dy, dz) == web {
					webs++
					if !worldgen.IsSolidFull(w.At(dx, fy+dy-1, dz)) {
						t.Errorf("cobweb at (%d,%d,%d) hangs with nothing under it", dx, fy+dy, dz)
					}
				}
			}
		}
	}
	if webs < 2 || webs > 3 {
		t.Errorf("%d cobwebs woven, want 2 or 3", webs)
	}
}

// Infested bursts silverfish out of you when you are HURT, not when you die.
func TestInfestedSpawnsSilverfishOnHurt(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl
	h.applyEffect(players, pl, effInfested, 0, 300)

	// A 10% roll per hit, so take a lot of small ones and heal back up.
	for i := 0; i < 400; i++ {
		h.damageOf(players, pl, 1, dtGeneric)
		pl.health, pl.dead = maxHealth, false
	}
	fish := 0
	for _, m := range h.mobs {
		if m.etype == entitySilverfish {
			fish++
		}
	}
	if fish == 0 {
		t.Error("400 hits while infested produced no silverfish at all")
	}
}
