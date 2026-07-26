package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A thrown bottle o' enchanting shatters into orbs worth 3-11.
func TestXPBottleShattersIntoExperience(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.inv.slots[0] = invStack{item: itemXPBottle, count: 2}
	pl.p.held = 0

	h.throwXPBottle(players, pl)
	if pl.inv.slots[0].count != 1 {
		t.Fatalf("throwing should cost one bottle, %d left", pl.inv.slots[0].count)
	}
	var a *arrowEntity
	for _, x := range h.arrows {
		a = x
	}
	if a == nil || !a.xpBottle || !a.breaks {
		t.Fatalf("no shattering bottle in the air: %+v", a)
	}

	before := len(h.orbs)
	h.breakXPBottle(players, a)
	if len(h.orbs) != before+1 {
		t.Fatal("a broken bottle awarded no experience")
	}
	for _, o := range h.orbs {
		if o.value < xpBottleBase || o.value > xpBottleBase+2*(xpBottleRoll-1) {
			t.Fatalf("payout %d outside 3-11", o.value)
		}
	}
}

// A goat horn sounds once, then holds for its full seven seconds.
func TestGoatHornCooldown(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.inv.slots[0] = invStack{item: itemGoatHorn, count: 1, instrument: 3}
	pl.p.held = 0

	h.tootHorn(players, pl)
	if !h.onCooldown(pl, itemGoatHorn) {
		t.Fatal("blowing a horn should put it on cooldown")
	}
	if got := pl.cooldowns[itemGoatHorn]; got != uint64(hornCooldown) {
		t.Fatalf("cooldown %d ticks, want %d", got, hornCooldown)
	}
	// It stays held until the cooldown runs out.
	h.tick.Store(uint64(hornCooldown) - 1)
	if !h.onCooldown(pl, itemGoatHorn) {
		t.Fatal("the horn freed up early")
	}
	h.tick.Store(uint64(hornCooldown))
	if h.onCooldown(pl, itemGoatHorn) {
		t.Fatal("the horn never freed up")
	}
}

// Frogspawn lands on the surface of a water source, not against a face.
func TestFrogspawnPlacesOnWaterSurface(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)
	// A pool two blocks ahead, its surface one below the player's eyes.
	for x := -1; x <= 3; x++ {
		w.SetBlock(x, 180, 0, worldgen.WaterBase)
		w.SetBlock(x, 181, 0, worldgen.Air)
	}
	pl.x, pl.y, pl.z = 0.5, 179.5, 0.5 // eyes at 181, looking down the +x ray
	pl.yaw, pl.pitch = -90, 20
	pl.inv.slots[0] = invStack{item: itemFrogspawn, count: 2}
	pl.p.held = 0

	h.placeFrogspawn(players, pl)
	placed := false
	for x := 0; x <= 3; x++ {
		if w.At(x, 181, 0) == frogspawnBlock {
			placed = true
		}
	}
	if !placed {
		t.Fatal("frogspawn never landed on the water")
	}
	if pl.inv.slots[0].count != 1 {
		t.Fatalf("placing should cost one, %d left", pl.inv.slots[0].count)
	}
}

// A rocket used while gliding attaches to the player and boosts them; used
// with both feet on the ground it does nothing.
func TestFireworkBoostsOnlyAGlider(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.inv.slots[0] = invStack{item: itemFireworkRocket, count: 4}
	pl.p.held = 0

	pl.onGround = true
	h.useFirework(players, pl)
	if len(h.rockets) != 0 {
		t.Fatal("a rocket fired from the hand while standing")
	}

	pl.onGround = false
	pl.armor[1] = invStack{item: itemElytra, count: 1}
	h.useFirework(players, pl)
	if len(h.rockets) != 1 {
		t.Fatalf("gliding should launch a rocket, have %d", len(h.rockets))
	}
	var r *rocketEntity
	for _, x := range h.rockets {
		r = x
	}
	if r.attached != pl.p.eid {
		t.Fatalf("the rocket should ride its glider, attached=%d", r.attached)
	}
	if pl.inv.slots[0].count != 3 {
		t.Fatalf("firing should cost one rocket, %d left", pl.inv.slots[0].count)
	}

	// It follows the player while they glide, and pops when they stop.
	h.updateRockets(players)
	if len(h.rockets) != 1 {
		t.Fatal("the rocket died while its glider was still flying")
	}
	pl.onGround = true
	h.updateRockets(players)
	if len(h.rockets) != 0 {
		t.Fatal("the rocket should pop once the glide ends")
	}
}

// A loose rocket climbs and pops on its own.
func TestLooseRocketClimbsAndPops(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	r := h.spawnRocket(players, 0, 0.5, 200, 0.5, 0)
	y0 := r.y
	for i := 0; i < r.lifetime+2; i++ {
		h.updateRockets(players)
	}
	if len(h.rockets) != 0 {
		t.Fatal("a rocket outlived its fuse")
	}
	if r.y <= y0 {
		t.Fatalf("a rocket should climb: %v → %v", y0, r.y)
	}
}

// A spyglass holds a scope until it is released, and drops it after the
// twenty seconds vanilla allows.
func TestSpyglassScopeStartsAndEnds(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.inv.slots[0] = invStack{item: itemSpyglass, count: 1}
	pl.p.setHotbarSlot(0, itemSpyglass)
	pl.p.held = 0

	h.raiseSpyglass(players, pl)
	if pl.scopeUntil == 0 {
		t.Fatal("using a spyglass should start a scope")
	}
	h.lowerSpyglass(players, pl)
	if pl.scopeUntil != 0 {
		t.Fatal("releasing should end the scope")
	}

	h.raiseSpyglass(players, pl)
	h.tick.Store(spyglassUseTicks - 1)
	h.expireSpyglass(players)
	if pl.scopeUntil == 0 {
		t.Fatal("the scope dropped early")
	}
	h.tick.Store(spyglassUseTicks)
	h.expireSpyglass(players)
	if pl.scopeUntil != 0 {
		t.Fatal("the scope outlived its duration")
	}
}
