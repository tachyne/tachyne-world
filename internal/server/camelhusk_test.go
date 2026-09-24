package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// camelHuskFixture is an open stone pad at y=180 with a player on it.
func camelHuskFixture(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 186; y++ {
				h.world.SetBlock(x, y, z, 0)
			}
		}
	}
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 30.5, 180, 0.5
	return h, players, pl
}

// mountedHusk spawns natural husks until one rolls the camel husk.
func mountedHusk(t *testing.T, h *hub, players map[int32]*tracked) *mob {
	t.Helper()
	for i := 0; i < 400; i++ {
		before := map[int32]bool{}
		for id := range h.mobs {
			before[id] = true
		}
		h.spawnNatural(players, dimOverworld, catMonster, entityHusk, 0, 180, 0)
		for id, m := range h.mobs {
			if !before[id] && m.etype == entityHusk && m.mount != 0 {
				return m
			}
		}
		for id := range h.mobs {
			if !before[id] {
				delete(h.mobs, id)
			}
		}
	}
	t.Fatal("no husk rolled a camel husk in 400 natural spawns")
	return nil
}

// Husk.finalizeSpawn: a natural husk sometimes arrives driving a camel husk
// with an iron spear, a parched in the back seat.
func TestNaturalHuskRidesACamelHusk(t *testing.T) {
	h, players, _ := camelHuskFixture(t)
	husk := mountedHusk(t, h, players)
	camel := h.mobs[husk.mount]
	if camel == nil || camel.etype != entityCamelHusk {
		t.Fatalf("husk rides %+v, want a camel husk", camel)
	}
	if !husk.mountDrives || husk.held != itemIronSpear {
		t.Errorf("husk drives=%v held=%d, want the driver with an iron spear", husk.mountDrives, husk.held)
	}
	seats := camel.mobPassengers()
	if len(seats) != 2 || seats[0] != husk.eid {
		t.Fatalf("camel husk seats %v, want the husk in front and a second rider", seats)
	}
	p := h.mobs[seats[1]]
	if p == nil || p.etype != entityParched || p.mount != camel.eid || p.mountDrives {
		t.Fatalf("back seat %+v, want a parched along for the ride", p)
	}
	if got := husk.moveSpeed(); got != camel.moveSpeed() || got == husk.mobAttrs().Value(attr.MovementSpeed) {
		t.Errorf("driving husk moves at %v, want its camel husk's pace", got)
	}
	if h.chargeSpeedModifier(husk) != camelHuskCharge || h.chargeSpeedModifier(p) != camelHuskCharge {
		t.Error("riders of a camel husk charge at 4×")
	}
	if mobSpawnCategory(camel) != catMonster {
		t.Error("a camel husk counts as a monster")
	}
	// A husk on its own never rolls it: only NATURAL spawns do.
	if m := h.spawnHostileYIn(players, entityHusk, dimOverworld, 4.5, 180, 4.5); m == nil || m.mount != 0 {
		t.Error("a husk spawned outside the natural spawner mounted a camel husk")
	}
}

// No room for the camel husk's 1.7 × 2.375 box: no roll at all.
func TestCamelHuskNeedsRoom(t *testing.T) {
	h, _, _ := camelHuskFixture(t)
	if !h.camelHuskFits(dimOverworld, 0.5, 180, 0.5) {
		t.Fatal("an open pad has no room")
	}
	h.world.SetBlock(1, 182, 0, worldgen.Stone) // inside the box's width and height
	if h.camelHuskFits(dimOverworld, 0.5, 180, 0.5) {
		t.Error("a block at head height beside the husk still fits a camel husk")
	}
}

// With the husk dead, the parched moves to the front and takes the reins.
func TestParchedTakesTheReins(t *testing.T) {
	h, players, _ := camelHuskFixture(t)
	husk := mountedHusk(t, h, players)
	camel := h.mobs[husk.mount]
	parched := h.mobs[camel.mobRider2]
	h.despawnMob(players, husk)
	if seats := camel.mobPassengers(); len(seats) != 1 || seats[0] != parched.eid {
		t.Fatalf("seats %v after the husk died, want the parched alone in front", seats)
	}
	if !parched.mountDrives || parched.navMount != camel {
		t.Error("the parched left in front does not drive")
	}
}

// A reload seats the driver in front whatever order the riders come back in.
func TestCamelHuskRelinkKeepsSeats(t *testing.T) {
	h, players, _ := camelHuskFixture(t)
	camel := h.spawnSpecies(players, entityCamelHusk, dimOverworld, 0.5, 180, 0.5)
	husk := h.spawnHostileYIn(players, entityHusk, dimOverworld, 0.5, 180, 0.5)
	parched := h.spawnHostileYIn(players, entityParched, dimOverworld, 0.5, 180, 0.5)
	husk.savedMount, husk.mountDrives = 77, true
	parched.savedMount = 77
	h.relinkMounts(players, []*mob{parched, husk}, map[int32]*mob{77: camel})
	if camel.mobRider != husk.eid || camel.mobRider2 != parched.eid || !husk.mountDrives || parched.mountDrives {
		t.Errorf("relinked seats %v (husk %d drives=%v, parched %d)", camel.mobPassengers(), husk.eid, husk.mountDrives, parched.eid)
	}
}

// The camel husk's overrides of its Camel parent.
func TestCamelHuskOverrides(t *testing.T) {
	h, players, pl := camelHuskFixture(t)
	husk := mountedHusk(t, h, players)
	camel := h.mobs[husk.mount]
	if canBeLeashed(camel) {
		t.Error("a mob-controlled camel husk takes a lead")
	}
	if panicsAt(camel, dtPlayerAttack) {
		t.Error("a mob-controlled camel husk panics")
	}
	free := h.spawnSpecies(players, entityCamelHusk, dimOverworld, 5.5, 180, 5.5)
	if !canBeLeashed(free) || !panicsAt(free, dtPlayerAttack) {
		t.Error("a riderless camel husk can be led and bolts when struck")
	}
	if !isTemptItem(entityCamelHusk, itemByName["rabbit_foot"]) || isTemptItem(entityCamelHusk, itemByName["cactus"]) {
		t.Error("camel husks follow rabbit's feet, not cactus")
	}
	// A rabbit's foot heals a hurt one and never sets it in love.
	pl.x, pl.z = 6.5, 5.5
	free.health = 10
	giveHeld(t, pl, itemByName["rabbit_foot"], 4)
	if !h.interactMob(players, pl, free, false) {
		t.Fatal("a hurt camel husk refused a rabbit's foot")
	}
	if free.health != 12 || free.loveTicks > 0 {
		t.Errorf("after a rabbit's foot: health %v love %d, want 12 and no love", free.health, free.loveTicks)
	}
	if !free.persistent {
		t.Error("a camel husk a player interacted with still despawns")
	}
	if h.removeWhenFarAway(camel, 1e6, h.tick.Load()) != true {
		t.Error("an untouched camel husk should despawn far away")
	}
}
