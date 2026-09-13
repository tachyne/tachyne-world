package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestLightningIgnites: a bolt sets the player and the mob it hits alight
// for eight seconds (Entity.thunderHit) on top of its damage.
func TestLightningIgnites(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 100.5, 180, 100.5
	z := h.spawnMob(players, entityZombie, 101.5, 180, 100.5)
	pl.health = 20
	h.strikeLightning(players, 100.5, 180, 100.5, false)
	if pl.fireSecs != lightningFireSecs || pl.health >= 20 {
		t.Fatalf("player: fire %d health %v", pl.fireSecs, pl.health)
	}
	if z.fireSecs != lightningFireSecs {
		t.Fatalf("zombie: fire %d", z.fireSecs)
	}
}

// TestComparatorPotAndHeart: a decorated pot reads its one slot's fullness;
// a creaking heart reads its creaking's distance.
func TestComparatorPotAndHeart(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pot := simPos{blockPos: blockPos{0, 180, 0}}
	w.SetBlock(0, 180, 0, worldgen.BlockBase("decorated_pot"))
	if got := h.analogSignal(pot); got != 0 {
		t.Fatalf("an empty pot reads 0, got %d", got)
	}
	h.pots = map[simPos]invStack{pot: {item: itemByName["stick"], count: 32}}
	if got := h.analogSignal(pot); got != 8 { // 1 + floor(14 × 32/64)
		t.Fatalf("a half-full pot reads 8, got %d", got)
	}
	heart := blockPos{10, 180, 0}
	w.SetBlock(10, 180, 0, worldgen.CreakingHeartBase+4) // axis x, awake, natural
	if got := h.analogSignal(simPos{blockPos: heart}); got != 0 {
		t.Fatalf("a heart with no creaking out reads 0, got %d", got)
	}
	c := h.spawnMob(players, entityCreaking, 26.5, 180.5, 0.5) // 16 blocks off
	if h.hearts == nil {
		h.hearts = map[blockPos]*heartLink{}
	}
	h.hearts[heart] = &heartLink{pos: heart, creaking: c.eid}
	if got := h.analogSignal(simPos{blockPos: heart}); got != 8 { // 15 - floor(16/32 × 15)
		t.Fatalf("a creaking 16 blocks out reads 8, got %d", got)
	}
	// A copper golem statue reads its pose: standing 1 … star 4.
	statue := worldgen.BlockID("oxidized_copper_golem_statue")
	info, _ := worldgen.InfoForState(statue)
	w.SetBlock(20, 180, 0, worldgen.SetProperty(info, statue, "copper_golem_pose", "running"))
	if got := h.analogSignal(simPos{blockPos: blockPos{20, 180, 0}}); got != 3 {
		t.Fatalf("a running statue reads 3, got %d", got)
	}
	w.SetBlock(10, 180, 0, worldgen.CreakingHeartBase) // dormant
	if got := h.analogSignal(simPos{blockPos: heart}); got != 0 {
		t.Fatalf("a dormant heart reads 0, got %d", got)
	}
}
