package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// nautilusSea floods a box of real water high above the terrain.
func nautilusSea(h *hub) {
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			for y := 180; y <= 192; y++ {
				h.world.SetBlock(x, y, z, worldgen.WaterBase)
			}
		}
	}
}

// NautilusAi: struck by a player in the water, a nautilus is ANGRY_AT them
// and its FIGHT activity outranks its panic — it charges back at 0.6 a tick
// and the blow lands, then it waits out its eighty-tick cooldown.
func TestNautilusChargesWhoeverHurtIt(t *testing.T) {
	h := newHub(world.New(1))
	nautilusSea(h)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 184, 0.5
	m := h.spawnSpecies(players, entityNautilus, 0, 0.5, 184, 0.5)
	if m == nil || !m.swims {
		t.Fatal("no swimming nautilus")
	}
	h.mobStruck(players, m, pl, dtPlayerAttack)
	if m.panic == 0 || m.nautAngryAt != pl.p.eid {
		t.Fatalf("a hurt nautilus both panics and grows angry: panic=%d angry=%d", m.panic, m.nautAngryAt)
	}
	h.updateMobs(players)
	if !m.charging || m.nautTarget != pl.p.eid {
		t.Fatalf("the fight should outrank the panic: charging=%v target=%d", m.charging, m.nautTarget)
	}
	if want := 0.6 * mobMoveInterval; math.Abs(m.chargeVX*mobMoveInterval-want) > 1e-9 || m.vx <= 0 {
		t.Fatalf("the charge runs at 0.6 a tick straight at the player: v=%.3f", m.chargeVX)
	}
	before := pl.health
	for i := 0; i < 10 && m.chargeCD == 0; i++ {
		h.updateMobs(players)
	}
	if pl.health >= before {
		t.Fatalf("the charge should land: health %v -> %v", before, pl.health)
	}
	if m.charging || m.nautTarget != 0 || m.chargeCD != nautilusChargeCooldown {
		t.Fatalf("after the blow the target drops and the cooldown starts: cd=%d", m.chargeCD)
	}
	// On the cooldown it does not charge again, though still angry.
	h.updateMobs(players)
	if m.charging {
		t.Fatal("charging again inside the eighty-tick cooldown")
	}
}

// A player holding #nautilus_food keeps the fight off (FIGHT wants no
// TEMPTING_PLAYER), and the nautilus swims up to them at 1.3 instead —
// tamed or not.
func TestNautilusTemptation(t *testing.T) {
	h := newHub(world.New(1))
	nautilusSea(h)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 188, 0.5
	cod := itemByName["cod"]
	pl.p.setHotbarSlot(0, cod)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: cod, count: 1}
	for _, c := range []struct {
		etype int
		tamed bool
		speed float64
	}{{entityNautilus, false, 1.3}, {entityNautilus, true, 1.3}, {entityZombieNautilus, false, 0.9}} {
		m := h.spawnSpecies(players, c.etype, 0, 0.5, 184, 0.5)
		m.tamed = c.tamed
		m.nautAngryAt, m.nautAngryUntil = pl.p.eid, h.tick.Load()+nautilusAngerTicks
		h.updateMobs(players)
		if m.charging {
			t.Errorf("etype %d: a tempting player keeps the fight off", c.etype)
		}
		if !m.tempted || m.x <= 0.5 || m.y <= 184 {
			t.Errorf("etype %d tamed=%v: not swimming after the cod (tempted=%v at %.2f,%.2f)", c.etype, c.tamed, m.tempted, m.x, m.y)
		}
		h.temptStep(players, m) // the steering itself, before the swim step damps its rise
		sp := math.Sqrt(m.vx*m.vx + m.vy*m.vy + m.vz*m.vz)
		if m.vy <= 0 || m.vx <= 0 || math.Abs(sp-m.moveSpeed()*c.speed) > 1e-9 {
			t.Errorf("etype %d tamed=%v: tempted=%v v=(%.3f %.3f) speed %.4f want %.4f",
				c.etype, c.tamed, m.tempted, m.vx, m.vy, sp, m.moveSpeed()*c.speed)
		}
		h.removeMob(players, m)
	}
	if temptStopFor(&mob{etype: entityNautilus}) != 3.5 || temptStopFor(&mob{etype: entityNautilus, baby: true}) != 2.5 {
		t.Error("FollowTemptation stops 3.5 blocks off, 2.5 for a baby")
	}
}

// The zombie nautilus is not a monster: it leaves a player alone until it
// is hurt, never panics, and then charges at 0.5.
func TestZombieNautilusFightsOnlyWhenAngered(t *testing.T) {
	h := newHub(world.New(1))
	nautilusSea(h)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 3.5, 184, 0.5
	m := h.spawnSpecies(players, entityZombieNautilus, 0, 0.5, 184, 0.5)
	if m.hostile {
		t.Fatal("a zombie nautilus is an animal, not a monster")
	}
	for i := 0; i < 40; i++ {
		h.updateMobs(players)
		h.updateHostiles(players)
	}
	if pl.health < 20 || m.charging {
		t.Fatalf("an unprovoked zombie nautilus attacked: health=%v", pl.health)
	}
	m.x, m.y, m.z = 0.5, 184, 0.5
	h.mobStruck(players, m, pl, dtPlayerAttack)
	if m.panic != 0 {
		t.Fatal("ZombieNautilusAi has no AnimalPanic")
	}
	h.updateMobs(players)
	if !m.charging || math.Abs(math.Hypot(m.chargeVX, m.chargeVZ)-0.5) > 1e-9 {
		t.Fatalf("a hurt zombie nautilus charges at 0.5: charging=%v v=%.3f", m.charging, math.Hypot(m.chargeVX, m.chargeVZ))
	}
	if !isTemptItem(entityZombieNautilus, itemByName["salmon_bucket"]) {
		t.Error("the zombie nautilus follows #nautilus_food")
	}
}

// Every 2400-3600 ticks, one time in two, a nautilus in the water goes for
// the nearest pufferfish it can see — never one out of the water, and never
// while tamed, a baby or ashore.
func TestNautilusHuntsPufferfish(t *testing.T) {
	h := newHub(world.New(1))
	nautilusSea(h)
	players := map[int32]*tracked{}
	m := h.spawnSpecies(players, entityNautilus, 0, 0.5, 184, 0.5)
	if m.nautTargetCD < nautilusTargetCDMin || m.nautTargetCD >= nautilusTargetCDMin+nautilusTargetCDSpan {
		t.Fatalf("a fresh nautilus waits 2400-3600 ticks first: %d", m.nautTargetCD)
	}
	if _, ok := h.nautilusFindTarget(players, m); ok {
		t.Fatal("found prey on its spawn cooldown")
	}
	ashore := h.spawnSpecies(players, entityPufferfish, 0, 3.5, 193, 0.5) // nearer, but above the sea
	puffer := h.spawnSpecies(players, entityPufferfish, 0, -10.5, 184, 0.5)
	found := false
	for i := 0; i < 64 && !found; i++ {
		m.nautTargetCD = 0
		q, ok := h.nautilusFindTarget(players, m)
		if ok {
			if q.o != puffer {
				t.Fatalf("went for %v, not the pufferfish in the water", q.o)
			}
			found = true
		} else if m.nautTargetCD < nautilusTargetCDMin {
			t.Fatal("a look restarts the cooldown whatever it finds")
		}
	}
	if !found || ashore == nil {
		t.Fatal("never went for the pufferfish")
	}
	m.tamed = true
	m.nautTargetCD = 0
	if _, ok := h.nautilusFindTarget(players, m); ok {
		t.Error("a tamed nautilus looks for no prey")
	}
}

// AnimalMakeLove at 0.4: two tamed, fed nautiluses swim together, and the
// calf is born in the water where they are, tamed to the same owner. A wild
// adult will not eat fish at all (it takes only the pufferfish that tames).
func TestNautilusBreeds(t *testing.T) {
	h := newHub(world.New(1))
	nautilusSea(h)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 170, -40.5
	cod := itemByName["cod"]
	pl.p.setHotbarSlot(0, cod)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: cod, count: 8}
	a := h.spawnSpecies(players, entityNautilus, 0, -3.5, 184, 0.5)
	b := h.spawnSpecies(players, entityNautilus, 0, 3.5, 186, 0.5)
	if h.feedAnimal(players, pl, a) || a.loveTicks != 0 {
		t.Fatal("a wild adult nautilus ate a cod")
	}
	for _, m := range []*mob{a, b} {
		m.tamed, m.owner, m.ownerUUID = true, pl.p.eid, pl.p.uuid
		if !h.feedAnimal(players, pl, m) || m.loveTicks == 0 {
			t.Fatal("a tamed adult should fall in love on a cod")
		}
	}
	h.updateMobs(players)
	if a.x <= -3.5 || b.x >= 3.5 {
		t.Fatalf("the pair should swim together: %.3f %.3f", a.x, b.x)
	}
	h.nautilusLoveStep(a)
	if sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz); a.vx <= 0 || math.Abs(sp-a.moveSpeed()*nautilusLoveSpeed) > 1e-9 {
		t.Fatalf("a courting nautilus swims to its mate at 0.4: v=%.4f want %.4f", sp, a.moveSpeed()*nautilusLoveSpeed)
	}
	n := len(h.mobs)
	for i := 0; i < 400 && len(h.mobs) == n; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if i%(survivalTickN/mobMoveInterval) == 0 {
			h.updateBreeding(players)
		}
	}
	var calf *mob
	for _, m := range h.mobs {
		if m.etype == entityNautilus && m.baby {
			calf = m
		}
	}
	if calf == nil {
		t.Fatal("no calf")
	}
	if !h.inWater(calf.dim, calf.x, calf.y, calf.z) || !calf.swims || !calf.tamed || calf.owner != pl.p.eid {
		t.Fatalf("the calf should be born tamed, in the water: y=%.1f swims=%v tamed=%v", calf.y, calf.swims, calf.tamed)
	}
}

// ChargeAttack.dealKnockBack: a zombie nautilus under Speed II shoves
// harder — the attribute's +40 % and speedBoostPower's flat +0.5 on top of
// the clamp — and Slowness takes it back off.
func TestNautilusChargeKnockbackSpeedBoost(t *testing.T) {
	shove := func(effect int32, amp int) float64 {
		h := newHub(world.New(1))
		nautilusSea(h)
		pl := survPlayer(h)
		pl.p.eid = 500
		pl.x, pl.y, pl.z = 0.5, 184, 0.5
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		m := h.spawnSpecies(players, entityZombieNautilus, 0, 0.5, 184, 0.5)
		if effect >= 0 {
			h.applyMobEffect(players, m, effect, amp, 200)
		}
		drainEvs(pl.p)
		if !h.nautilusChargeHit(players, m, m.x, m.y, m.z) {
			t.Fatal("the charge should hit the player it overlaps")
		}
		for _, ev := range drainEvs(pl.p) {
			if v, ok := ev.(attachproto.Velocity); ok && v.EID == pl.p.eid {
				return math.Hypot(v.VX, v.VZ)
			}
		}
		t.Fatal("no shove sent")
		return 0
	}
	if got := shove(-1, 0); math.Abs(got-1.1) > 1e-9 { // clamp(0.5 × 1.1) × 2
		t.Fatalf("plain charge shove %.4f, want 1.1", got)
	}
	if got := shove(effSpeed, 1); math.Abs(got-2.54) > 1e-9 { // (0.5 × 1.54 + 0.5) × 2
		t.Fatalf("Speed II charge shove %.4f, want 2.54", got)
	}
	if got := shove(effSlowness, 0); math.Abs(got-(0.5*1.1*0.85-0.25)*2) > 1e-9 {
		t.Fatalf("Slowness I charge shove %.4f", got)
	}
}
