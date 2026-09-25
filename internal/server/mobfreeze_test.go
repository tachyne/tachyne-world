package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	api "github.com/tachyne/tachyne-world/plugin/attribute"
)

// shaft builds a one-cell column at (x, z): a floor block at y0-1, stone
// walls all round from y0 to y1, and the cells between filled with fill.
func shaft(w *world.World, x, z, y0, y1 int, floor, fill uint32) {
	w.ForceLoad(x, z, 1)
	w.SetBlock(x, y0-1, z, floor)
	for y := y0; y <= y1; y++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx != 0 || dz != 0 {
					w.SetBlock(x+dx, y, z+dz, worldgen.Stone)
				}
			}
		}
		w.SetBlock(x, y, z, fill)
	}
}

func mobBlocksHub() (*hub, map[int32]*tracked) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	return h, players
}

// runMobs drives the mob update the way the hub loop does: every
// mobMoveInterval ticks.
func runMobs(h *hub, players map[int32]*tracked, n int, each func()) {
	for i := 0; i < n; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if each != nil {
			each()
		}
	}
}

// A zombie standing in powder snow freezes over 140 ticks, is slowed by the
// frost, and once frozen through takes freeze damage; leather armour, a
// stray and a snow golem are spared; out of the snow it thaws.
func TestMobsFreezeInPowderSnow(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 183, worldgen.Stone, worldgen.Air)
	shaft(w, 10, 0, 180, 183, worldgen.Stone, worldgen.Air)
	shaft(w, 20, 0, 180, 183, worldgen.Stone, worldgen.Air)
	for _, x := range []int{0, 10, 20} {
		w.SetBlock(x, 180, 0, powderSnowBlock)
		w.SetBlock(x, 181, 0, powderSnowBlock)
	}
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	leather := h.spawnMob(players, entityZombie, 10.5, 180, 0.5)
	leather.gear[0] = invStack{item: int32(itemByName["leather_helmet"]), count: 1}
	stray := h.spawnMob(players, entityStray, 20.5, 180, 0.5)
	hp := z.health

	runMobs(h, players, freezeTicks/mobMoveInterval, nil)
	if z.ticksFrozen != freezeTicks {
		t.Fatalf("a zombie in powder snow is frozen %d, want %d", z.ticksFrozen, freezeTicks)
	}
	if !z.mobAttrs().Get(api.MovementSpeed).HasModifier(frostSpeedSource) {
		t.Error("a frozen zombie is not slowed")
	}
	if leather.ticksFrozen != 0 || stray.ticksFrozen != 0 {
		t.Errorf("leather armour and strays do not freeze: %d %d", leather.ticksFrozen, stray.ticksFrozen)
	}
	runMobs(h, players, freezeHurtEvery/mobMoveInterval, nil)
	if z.health >= hp {
		t.Fatalf("a fully frozen zombie took no freeze damage (health %d)", z.health)
	}
	if leather.health != mobHealth(entityZombie) || stray.health != mobHealth(entityStray) {
		t.Error("an unfrozen mob was hurt")
	}

	w.SetBlock(0, 180, 0, worldgen.Air)
	w.SetBlock(0, 181, 0, worldgen.Air)
	runMobs(h, players, 5, nil)
	if z.ticksFrozen != freezeTicks-5*2*mobMoveInterval {
		t.Errorf("out of the snow it thaws two a tick: %d", z.ticksFrozen)
	}
}

// The rabbit, the fox, the endermite and the silverfish — and anything in
// leather boots — walk on powder snow; everything else sinks into it.
func TestSnowWalkersStandOnPowderSnow(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	for _, x := range []int{0, 10, 20} {
		shaft(w, x, 0, 180, 184, worldgen.Stone, worldgen.Air)
		w.SetBlock(x, 180, 0, powderSnowBlock)
		w.SetBlock(x, 181, 0, powderSnowBlock)
	}
	rabbit := h.spawnMob(players, entityRabbit, 0.5, 182, 0.5)
	booted := h.spawnMob(players, entityZombie, 10.5, 182, 0.5)
	booted.gear[3] = invStack{item: int32(itemByName["leather_boots"]), count: 1}
	plain := h.spawnMob(players, entityZombie, 20.5, 182, 0.5)
	runMobs(h, players, 10, nil)
	if rabbit.y != 182 || booted.y != 182 {
		t.Errorf("snow walkers should stand on the snow: rabbit y=%v, booted zombie y=%v", rabbit.y, booted.y)
	}
	if plain.y != 180 {
		t.Errorf("a plain zombie sinks through the snow: y=%v", plain.y)
	}
}

// A burning mob in powder snow is put out, and melts the snow it is in.
func TestBurningMobMeltsPowderSnow(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 183, worldgen.Stone, worldgen.Air)
	w.SetBlock(0, 180, 0, powderSnowBlock)
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	z.fireSecs, z.burning = 8, true
	runMobs(h, players, 1, nil)
	if z.fireSecs != 0 || z.burning {
		t.Errorf("the snow did not put the zombie out: fireSecs %d burning %v", z.fireSecs, z.burning)
	}
	if w.At(0, 180, 0) == powderSnowBlock {
		t.Error("a burning mob melts the powder snow it stands in")
	}
}
