package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Drinking a potion through the eat-hold: use, hold for 32 ticks, and the
// effect lands and the bottle comes back.
func TestDrinkPotionThroughTheHold(t *testing.T) {
	for _, mode := range []int{gmSurvival, gmAdventure, gmCreative} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.gamemode = mode
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		st := potionStack(potSwiftness)
		pl.inv.slots[pl.p.heldSlot()] = st
		h.startEating(pl, pl.p.heldSlot())
		for i := 0; i < 40; i++ {
			h.tick.Add(1)
			h.updateEating(players)
		}
		if pl.effects[effSpeed] == nil {
			t.Errorf("mode %d: drank a swiftness potion, no Speed", mode)
		}
		got := pl.inv.slots[pl.p.heldSlot()]
		if mode == gmCreative && got.item != itemPotion {
			t.Errorf("creative: the potion was used up (%d)", got.item)
		}
		if mode != gmCreative && got.item != itemGlassBottle {
			t.Errorf("mode %d: no bottle back (%d)", mode, got.item)
		}
	}
}

// Adventure players eat as survival ones do; a creative player eats on a
// full bar and keeps the food.
func TestEatingInAdventureAndCreative(t *testing.T) {
	for _, mode := range []int{gmAdventure, gmCreative} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.gamemode = mode
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		bread := itemByName["bread"]
		pl.inv.slots[pl.p.heldSlot()] = invStack{item: bread, count: 2}
		pl.food = 10
		if mode == gmCreative {
			pl.food = maxFood
		}
		h.startEating(pl, pl.p.heldSlot())
		for i := 0; i < 40; i++ {
			h.tick.Add(1)
			h.updateEating(players)
		}
		got := pl.inv.slots[pl.p.heldSlot()].count
		if mode == gmAdventure && (got != 1 || pl.food <= 10) {
			t.Errorf("adventure: bread %d, food %d", got, pl.food)
		}
		if mode == gmCreative && got != 2 {
			t.Errorf("creative: ate bread and it was used up (%d left)", got)
		}
	}
}

// BundleItem.use + onUseTick, through the use path: holding a bundle
// tosses its contents out one at a time, the first at once, then one
// every other tick after the tenth.
func TestBundleEmptiesWhileHeld(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 200, 0.5
	id := h.newBundleID()
	h.bundles.set(id, []invStack{{item: itemByName["stone"], count: 1}, {item: itemByName["dirt"], count: 1}, {item: itemByName["sand"], count: 1}})
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["bundle"], count: 1, bundleID: id}
	h.startEating(pl, pl.p.heldSlot())
	h.updateEating(players) // the first tick of use
	if n := len(h.bundles.get(id)); n != 2 || len(h.items) != 1 {
		t.Fatalf("after the first tick: %d left in the bundle, %d on the ground", n, len(h.items))
	}
	for i := 0; i < 11; i++ {
		h.tick.Add(1)
		h.updateEating(players)
	}
	if n := len(h.bundles.get(id)); n != 2 {
		t.Errorf("tick 11: %d left, want 2 (the next goes at tick 12)", n)
	}
	for i := 0; i < 4; i++ {
		h.tick.Add(1)
		h.updateEating(players)
	}
	if n := len(h.bundles.get(id)); n != 0 || pl.eatingSlot != -1 {
		t.Errorf("the bundle did not empty and stop: %d left, using %d", n, pl.eatingSlot)
	}
}

// ThrownEgg.onHit: chicks hatch where the egg broke, in its dimension, and
// take the egg's variant; a brown or blue egg can be thrown at all.
func TestEggsHatchTheirOwnVariant(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.inv.slots[0] = invStack{item: itemBlueEgg, count: 4}
	h.throwProjectile(players, pl, itemBlueEgg)
	var thrown *arrowEntity
	for _, a := range h.arrows {
		thrown = a
	}
	if thrown == nil || !thrown.egg || thrown.eggItem != itemBlueEgg {
		t.Fatalf("a blue egg did not fly as an egg: %+v", thrown)
	}
	a := &arrowEntity{dim: dimNether, x: 10.5, y: 70, z: 10.5, egg: true, eggItem: itemBrownEgg}
	for i := 0; i < 400 && len(h.mobs) == 0; i++ {
		h.hatchEgg(players, a)
	}
	if len(h.mobs) == 0 {
		t.Fatal("no chick in 400 eggs")
	}
	for _, m := range h.mobs {
		if m.etype != entityChicken || !m.baby || m.dim != dimNether || m.variant != tempWarm || m.y != 70 {
			t.Errorf("hatched %+v, want a warm baby chicken at the egg in the Nether", m)
		}
	}
}

// saveToBucketTag / loadFromBucketTag: a blue axolotl comes out of its
// bucket blue, and the bucket survives a save.
func TestAxolotlBucketKeepsItsColour(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	ax := h.spawnMob(players, entityAxolotl, 0.5, 200, 0.5)
	ax.variant, ax.variantSet = axolotlBlue, true
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBucketH2O, count: 1}
	if !h.tryBucketMob(players, pl, ax) {
		t.Fatal("the axolotl was not scooped")
	}
	st := pl.inv.slots[pl.p.heldSlot()]
	if st.cube.variant != axolotlBlue+1 {
		t.Fatalf("the bucket carries variant %d", st.cube.variant)
	}
	if back := unpackStack(packStack(st)); back != st {
		t.Errorf("the bucket did not survive a save: %+v vs %+v", back, st)
	}
	h.releaseBucketMob(players, 0, st.item, st.cube, 3, 200, 3)
	for _, m := range h.mobs {
		if m.etype == entityAxolotl && (m.variant != axolotlBlue || !m.variantSet) {
			t.Errorf("the axolotl came back variant %d", m.variant)
		}
	}
}

// SimpleWaterloggedBlock.pickupBlock: an empty bucket drains a waterlogged
// slab and comes back full.
func TestBucketDrainsWaterloggedBlock(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	slab := withWaterlogged(withProp(worldgen.BlockBase("oak_slab"), "type", "bottom"), true)
	h.world.SetBlock(0, 199, 2, slab)
	pl.x, pl.y, pl.z = 0.5, 200, 0.5
	pl.yaw, pl.pitch = 0, 45 // looking down and ahead (+z)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBucket, count: 1}
	h.bucketFill(players, pl, int32(pl.p.heldSlot()))
	if isWaterlogged(h.world.At(0, 199, 2)) || pl.inv.slots[pl.p.heldSlot()].item != itemBucketH2O {
		t.Errorf("slab still wet: %v, bucket %d", isWaterlogged(h.world.At(0, 199, 2)), pl.inv.slots[pl.p.heldSlot()].item)
	}
}

// DolphinJumpGoal: a dolphin swimming along the surface of open water leaps
// out and comes back down into it.
func TestDolphinLeaps(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	for x := -2; x <= 12; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 195, z, worldgen.Stone)
			for y := 196; y <= 199; y++ {
				h.world.SetBlock(x, y, z, worldgen.WaterBase)
			}
			for y := 200; y <= 205; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	d := h.spawnMob(players, entityDolphin, 0.5, 199, 0.5)
	jumped := false
	for i := 0; i < 200 && !jumped; i++ {
		d.vx, d.vz = 0.1, 0
		jumped = h.dolphinJumpStart(players, d)
	}
	if !jumped {
		t.Fatal("a dolphin at the surface of open water never leapt")
	}
	peak := d.y
	for i := 0; i < 40 && d.leaping; i++ {
		h.leapFlight(players, d)
		peak = math.Max(peak, d.y)
	}
	if peak < 200 || d.leaping || !worldgen.HoldsWater(h.world.At(floorInt(d.x), floorInt(d.y), floorInt(d.z))) {
		t.Errorf("the leap peaked at %.2f and ended leaping=%v at y %.2f", peak, d.leaping, d.y)
	}
}

// FrogAi's Croak: an idle frog on land croaks for sixty ticks, holding
// still, then stands.
func TestFrogCroaks(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	f := h.spawnMob(players, entityFrog, 0.5, 200, 0.5)
	for i := 0; i < 100 && f.croakLeft == 0; i++ {
		h.frogIdleCroak(players, f)
	}
	if f.croakLeft != frogCroakTicks {
		t.Fatal("an idle frog never croaked")
	}
	n := 0
	for h.frogCroakStep(players, f) {
		n++
	}
	if f.croakLeft != 0 || n != frogCroakTicks/mobMoveInterval-1 {
		t.Errorf("the croak lasted %d updates (want %d)", n, frogCroakTicks/mobMoveInterval-1)
	}
}

// FollowPlayerRiddenEntityGoal: a dolphin near a boat a player is rowing
// swims after it, and stops when the boat stops.
func TestDolphinFollowsARowedBoat(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	d := h.spawnMob(players, entityDolphin, 0.5, 199, 0.5)
	boat := &vehicle{eid: h.allocEID(), etype: vehicleItems[itemByName["oak_boat"]], x: 3.5, y: 200, z: 0.5, rider: pl.p.eid}
	h.vehicles[boat.eid] = boat
	pl.x, pl.y, pl.z = boat.x, boat.y+0.6, boat.z
	boat.movedAt, boat.moveDX = h.tick.Load(), 0.3
	if !h.dolphinFollowBoat(players, d) || d.followBoat != boat.eid || d.vx <= 0 {
		t.Fatalf("the dolphin did not take after the boat (follow %d, vx %v)", d.followBoat, d.vx)
	}
	h.tick.Add(10) // the boat has stopped
	if h.dolphinFollowBoat(players, d) {
		t.Error("the dolphin kept following a boat that stopped")
	}
}
