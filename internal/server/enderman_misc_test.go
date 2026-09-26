package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// throwPotionDown launches a thrown potion straight down from four blocks
// above (x, z) and flies it until it breaks — the projectile path, not a
// direct splashPotion call.
func throwPotionDown(t *testing.T, h *hub, players map[int32]*tracked, shooter int32, x, z float64, kind int8, lingering bool) {
	t.Helper()
	a := h.launchProjectileIn(players, thrownPotionType(lingering), 0, x, 184, z, 0, -0.5, 0)
	a.shooter, a.splash, a.breaks, a.potion, a.lingering = shooter, true, true, kind, lingering
	a.playerShot = true
	for i := 0; i < 200 && h.arrows[a.eid] != nil; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if h.arrows[a.eid] != nil {
		t.Fatal("the potion never broke")
	}
}

// A bottle of water thrown at a blaze hurts it a point (and nothing it is
// not sensitive to), and puts out a burning zombie beside it.
func TestWaterSplashHurtsWaterSensitiveAndDouses(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 11.5, 180, 3.5
	players[pl.p.eid] = pl
	blaze := h.spawnHostileY(players, entityBlaze, 0.5, 180, 0.5)
	zombie := h.spawnHostileY(players, entityZombie, 2.5, 180, 0.5)
	zombie.fireSecs, zombie.burning = 8, true
	bhp, zhp := blaze.health, zombie.health
	throwPotionDown(t, h, players, pl.p.eid, 0.5, 0.5, potWater, false)
	if got := bhp - blaze.health; got != 1 {
		t.Errorf("the splash cost the blaze %v health, want 1", got)
	}
	if zombie.health != zhp {
		t.Errorf("the splash hurt a zombie: %v → %v", zhp, zombie.health)
	}
	if zombie.fireSecs != 0 || zombie.burning {
		t.Errorf("the burning zombie was not put out (fire %d, burning %v)", zombie.fireSecs, zombie.burning)
	}
}

// A lingering bottle of water does the same on the hit, and leaves no cloud.
func TestLingeringWaterHurtsOnTheHit(t *testing.T) {
	h, players := preyFixture(t)
	blaze := h.spawnHostileY(players, entityBlaze, 0.5, 180, 0.5)
	hp := blaze.health
	h.splashPotion(players, 0, 0.5, 180, 0.5, potWater, true)
	if got := hp - blaze.health; got != 1 {
		t.Errorf("a lingering water bottle cost the blaze %v health, want 1", got)
	}
	if len(h.clouds) != 0 {
		t.Errorf("water left %d clouds", len(h.clouds))
	}
	// Beyond four blocks it does nothing.
	far := h.spawnHostileY(players, entityBlaze, 8.5, 180, 0.5)
	fhp := far.health
	h.splashPotion(players, 0, 0.5, 180, 0.5, potWater, false)
	if far.health != fhp {
		t.Errorf("a blaze eight blocks off was hurt: %v → %v", fhp, far.health)
	}
}

// An enderman splashed with water takes the point and is gone at once
// (repeatedlyTryToTeleport); one splashed with Harming takes nothing and
// is gone just the same.
func TestPotionSplashTeleportsEnderman(t *testing.T) {
	h, players := preyFixture(t)
	for _, c := range []struct {
		kind int8
		hurt float64
	}{{potWater, 1}, {potHarming, 0}} {
		moved := 0
		for i := 0; i < 5; i++ {
			m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
			hp := m.health
			h.splashPotion(players, 0, 0.5, 180, 0.5, c.kind, false)
			if got := float64(hp - m.health); got != c.hurt {
				t.Errorf("potion %d cost the enderman %v health, want %v", c.kind, got, c.hurt)
			}
			if m.x != 0.5 || m.z != 0.5 {
				moved++
			}
			h.removeMob(players, m)
		}
		if moved < 4 {
			t.Errorf("potion %d: only %d of 5 endermen teleported", c.kind, moved)
		}
	}
}

// A thrown potion that strikes an enderman shatters on it: a potion of
// Slowness slows it where it stands rather than bouncing it away first.
func TestThrownPotionShattersOnEnderman(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 11.5, 180, 3.5
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
	throwPotionDown(t, h, players, pl.p.eid, 0.5, 0.5, potSlowness, false)
	if m.x != 0.5 || m.z != 0.5 {
		t.Errorf("the enderman teleported from a potion of Slowness to (%v, %v)", m.x, m.z)
	}
	if m.hasEffect(effSlowness) == 0 {
		t.Error("the enderman the bottle broke on is not slowed")
	}
}

// Enderman.teleport plays level event 2018 at the old block, carrying the
// packed difference to where it landed, after the entity event.
func TestEndermanTeleportTrailLevelEvent(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 11.5, 180, 3.5
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
	drainEvents(pl)
	if !h.endermanTeleportTo(players, m, 5.5, 180, -2.5) {
		t.Fatal("the teleport onto the stone floor failed")
	}
	var got []int32
	for _, fx := range drainFX(pl) {
		if fx.Event == worldEventEndermanTeleport {
			if fx.X != 0 || fx.Y != 180 || fx.Z != 0 {
				t.Errorf("trail at (%d,%d,%d), want the old block (0,180,0)", fx.X, fx.Y, fx.Z)
			}
			got = append(got, fx.Data)
		}
	}
	want := int32((5+127)<<16 | 127<<8 | (-3 + 127))
	if len(got) != 1 || got[0] != want {
		t.Errorf("trail events %v, want one with data %#x", got, want)
	}
}

// clampedPackDifferenceInPosition clamps each axis to ±127 before packing.
func TestClampedPackDifference(t *testing.T) {
	if got, want := clampedPackDifference(0, 0, 0, 300, -300, 5), int32(254<<16|0<<8|132); got != want {
		t.Errorf("pack = %#x, want %#x", got, want)
	}
}

// randomTeleport pulls a target beyond the world border back inside it: an
// enderman sent past the wall lands against it.
func TestEndermanTeleportClampsToBorder(t *testing.T) {
	h, players := preyFixture(t)
	h.border = defaultBorder()
	h.border.Size = 20 // the wall at ±10
	m := h.spawnHostileY(players, entityEnderman, 5.5, 180, 0.5)
	if !h.endermanTeleportTo(players, m, 50.5, 180, 0.5) {
		t.Fatal("a target past the wall should be clamped, not refused")
	}
	if m.x >= 10 || m.x < 9.99 {
		t.Errorf("landed at x=%v, want just inside the wall at 10", m.x)
	}
}

// #enderman_holdable comes from the tag: 26.3's golden dandelion is in it.
func TestEndermanHoldableFromTag(t *testing.T) {
	for _, n := range []string{"golden_dandelion", "poppy", "grass_block", "mud", "cactus_flower", "warped_nylium"} {
		if endermanHoldableDefault(worldgen.BlockID(n)) != worldgen.BlockID(n) {
			t.Errorf("%s is not holdable", n)
		}
	}
	if endermanHoldableDefault(worldgen.BlockID("sunflower")) != 0 {
		t.Error("a sunflower (not a small flower) is holdable")
	}
}

// The take ray clips block OUTLINES: grass or a torch between the enderman
// and the block stops it, though neither collides; a carpet below the ray's
// height does not.
func TestEndermanTakeRayClipsOutlines(t *testing.T) {
	h, players := preyFixture(t)
	m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
	h.world.SetBlock(3, 180, 0, worldgen.BlockID("dirt"))
	for _, c := range []struct {
		between string
		ok      bool
	}{{"air", true}, {"short_grass", false}, {"torch", false}, {"moss_carpet", true}, {"stone", false}} {
		h.world.SetBlock(1, 180, 0, worldgen.BlockID(c.between))
		h.world.SetBlock(2, 180, 0, worldgen.BlockID(c.between))
		if _, ok := h.endermanTakeable(m, 3, 180, 0); ok != c.ok {
			t.Errorf("with %s in the way, takeable = %v, want %v", c.between, ok, c.ok)
		}
	}
	// A holdable block that is not the first thing the ray meets is not
	// taken: dirt behind dirt.
	h.world.SetBlock(1, 180, 0, worldgen.Air)
	h.world.SetBlock(2, 180, 0, worldgen.BlockID("dirt"))
	if _, ok := h.endermanTakeable(m, 3, 180, 0); ok {
		t.Error("the enderman reached the dirt behind other dirt")
	}
	if _, ok := h.endermanTakeable(m, 2, 180, 0); !ok {
		t.Error("the enderman could not reach the nearer dirt")
	}
}

// A flower's outline is nudged per column the way the client draws it.
func TestOutlineNudgeMatchesOffsetFunction(t *testing.T) {
	poppy := worldgen.BlockID("poppy")
	for _, p := range [][2]int{{0, 0}, {5, -3}, {-17, 40}} {
		dx, dy, dz := outlineNudge(poppy, p[0], p[1])
		seed := mthGetSeed(int32(p[0]), 0, int32(p[1]))
		wx := (float64(float32(seed&15)/15) - 0.5) * 0.5
		if dx != clampF(wx, -0.25, 0.25) || dy != 0 || dz < -0.25 || dz > 0.25 {
			t.Errorf("poppy nudge at %v = (%v,%v,%v)", p, dx, dy, dz)
		}
	}
	if dx, dy, dz := outlineNudge(worldgen.BlockID("dirt"), 5, 5); dx != 0 || dy != 0 || dz != 0 {
		t.Error("dirt is nudged")
	}
}

// Sculk hears the take as BLOCK_DESTROY and the leave as BLOCK_PLACE.
func TestSculkHearsEndermanTakeAndLeave(t *testing.T) {
	f := sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		h.rules.MobGriefing = true
		m := h.spawnHostileY(players, entityEnderman, 8.5, 180, 4.5)
		h.world.SetBlock(7, 180, 4, worldgen.BlockID("dirt"))
		stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2)
		for i := 0; i < 100000 && m.carriedBlock == 0; i++ {
			h.endermanTakeBlock(players, m)
		}
		if m.carriedBlock == 0 {
			t.Fatal("the enderman never took the dirt")
		}
	})
	if f != freqBlockDestroy {
		t.Errorf("the sensor heard %d for a take, want BLOCK_DESTROY %d", f, freqBlockDestroy)
	}
	f = sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		m := h.spawnHostileY(players, entityEnderman, 8.5, 180, 4.5)
		m.carriedBlock = worldgen.BlockID("dirt")
		stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2)
		for i := 0; i < 500000 && m.carriedBlock != 0; i++ {
			h.endermanPlaceBlock(players, m)
		}
		if m.carriedBlock != 0 {
			t.Fatal("the enderman never set its dirt down")
		}
	})
	if f != freqBlockPlace {
		t.Errorf("the sensor heard %d for a leave, want BLOCK_PLACE %d", f, freqBlockPlace)
	}
}

// LeaveBlockGoal runs the carried block through updateFromNeighbourShapes:
// grass set down under snow is snowy.
func TestEndermanLeavesGrassSnowy(t *testing.T) {
	h, players := preyFixture(t)
	m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
	grass := worldgen.BlockID("grass_block")
	m.carriedBlock = grass
	snow := worldgen.BlockBase("snow")
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(dx, 180, dz, worldgen.Air)
			h.world.SetBlock(dx, 181, dz, snow)
		}
	}
	for i := 0; i < 500000 && m.carriedBlock != 0; i++ {
		h.endermanPlaceBlock(players, m)
	}
	if m.carriedBlock != 0 {
		t.Fatal("the enderman never set its grass down")
	}
	placed := 0
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if s := h.world.At(dx, 180, dz); inRanges2(s, blockRange("grass_block")) {
				placed++
				if !boolProp(s, "snowy") {
					t.Error("grass set down under snow is not snowy")
				}
			}
		}
	}
	if placed != 1 {
		t.Errorf("%d grass blocks placed, want 1", placed)
	}
}

// A carried flower "placed" where it cannot survive comes out of the shape
// update as air: the enderman's hands empty and nothing is set down.
func TestEndermanLosesFlowerOnStone(t *testing.T) {
	h, players := preyFixture(t)
	m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
	m.carriedBlock = worldgen.BlockID("poppy")
	for i := 0; i < 500000 && m.carriedBlock != 0; i++ {
		h.endermanPlaceBlock(players, m)
	}
	if m.carriedBlock != 0 {
		t.Fatal("the enderman kept its poppy")
	}
	for dx := -1; dx <= 1; dx++ {
		for dy := 0; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if s := h.world.At(dx, 180+dy, dz); s != worldgen.Air {
					t.Errorf("(%d,%d,%d) holds %d, want air", dx, 180+dy, dz, s)
				}
			}
		}
	}
}

// outline_gen.go covers the state registry the engine runs, no more.
func TestOutlineTableCoversRegistry(t *testing.T) {
	if _, ok := worldgen.StateName(outlineStateCount - 1); !ok {
		t.Errorf("state %d is not in the registry", outlineStateCount-1)
	}
	if _, ok := worldgen.StateName(outlineStateCount); ok {
		t.Errorf("state %d exists but the outline table stops before it", outlineStateCount)
	}
}
