package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A magma block burns whoever stands on it — unless they are fire-resistant or
// wearing Frost Walker boots.
func TestMagmaBlockBurns(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	w.SetBlock(0, 179, 0, magmaBlockState)

	pl.health = 20
	h.entityInsideTick(players)
	if pl.health >= 20 {
		t.Fatalf("standing on magma left health at %v", pl.health)
	}

	// Fire resistance spares you.
	pl.health = 20
	h.applyEffect(players, pl, effFireRes, 0, 60)
	h.entityInsideTick(players)
	if pl.health != 20 {
		t.Errorf("fire-resistant player took %v from magma", 20-pl.health)
	}
	h.removeEffect(pl, effFireRes)

	// So do Frost Walker boots.
	pl.health = 20
	pl.armor[3] = invStack{item: itemByName["iron_boots"], count: 1,
		ench: [2]enchApply{{id: enchFrostWalker, lvl: 1}}}
	h.entityInsideTick(players)
	if pl.health != 20 {
		t.Errorf("Frost Walker boots took %v from magma", 20-pl.health)
	}
}

// A berry bush only scratches while you move through it, and only once grown.
func TestBerryBushScratchesOnlyWhileMoving(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.contactX, pl.contactZ = pl.x, pl.z

	// Move WITHIN the bush's own block — stepping a whole block would just
	// take the player out of it.
	// A freshly planted bush is harmless whatever you do.
	w.SetBlock(0, 180, 0, berryBushMin)
	pl.health = 20
	pl.x += 0.2
	h.entityInsideTick(players)
	if pl.health != 20 {
		t.Errorf("an age-0 bush scratched for %v", 20-pl.health)
	}

	// A grown one scratches when you move…
	w.SetBlock(0, 180, 0, berryBushMax)
	pl.health = 20
	pl.x += 0.2
	h.entityInsideTick(players)
	if pl.health >= 20 {
		t.Error("moving through a grown bush did not scratch")
	}

	// …and not when you stand still.
	pl.health = 20
	h.entityInsideTick(players)
	if pl.health != 20 {
		t.Errorf("standing still in a bush cost %v", 20-pl.health)
	}
}

// A wither rose withers what stands in it, but never the undead.
func TestWitherRoseWithers(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	w.SetBlock(0, 180, 0, witherRoseMin)

	h.entityInsideTick(players)
	if pl.hasEffect(effWither) == 0 {
		t.Error("a wither rose did not wither the player")
	}

	// A zombie standing in one is unmoved.
	z := h.spawnHostile(players, entityZombie, 0, 0)
	if z == nil {
		t.Fatal("spawn returned nil")
	}
	z.x, z.y, z.z = 0.5, 180, 0.5
	h.entityInsideTick(players)
	if z.hasEffect(effWither) != 0 {
		t.Error("a wither rose withered a zombie")
	}
}

// Mobs meet these blocks too — that is what makes a hedge a defence.
func TestBerryBushHurtsMobsButNotFoxesOrBees(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	w.SetBlock(0, 180, 0, berryBushMax)

	cow := h.spawnMobIn(players, entityCow, 0, 0.5, 180, 0.5)
	fox := h.spawnMobIn(players, entityFox, 0, 0.5, 180, 0.5)
	if cow == nil || fox == nil {
		t.Fatal("spawn returned nil")
	}
	cow.x, cow.y, cow.z = 0.5, 180, 0.5
	fox.x, fox.y, fox.z = 0.5, 180, 0.5
	cowHP, foxHP := cow.health, fox.health

	h.entityInsideTick(players)
	if cow.health >= cowHP {
		t.Error("a cow in a berry bush was unharmed")
	}
	if fox.health != foxHP {
		t.Error("a fox was hurt by a berry bush — vanilla lets foxes through")
	}
}

// The block probe must look at the feet, the body and the floor.
func TestBlocksTouchingCoversFeetBodyAndFloor(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	stone, _ := worldgen.BlockRange("stone")
	w.SetBlock(0, 179, 0, stone)           // floor
	w.SetBlock(0, 180, 0, berryBushMax)    // feet
	w.SetBlock(0, 181, 0, magmaBlockState) // body

	var floors, others int
	h.blocksTouching(0, 0.5, 180, 0.5, func(s uint32, onFloor bool) {
		if onFloor {
			floors++
			if s != stone {
				t.Errorf("floor block %d, want stone %d", s, stone)
			}
		} else {
			others++
		}
	})
	if floors != 1 || others != 2 {
		t.Errorf("probed %d floor + %d other, want 1 + 2", floors, others)
	}
}
