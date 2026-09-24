package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Natural piglins follow Piglin.finalizeSpawn: a fifth are babies holding
// nothing, and a grown one carries a crossbow half the time, else a golden
// sword or (one in ten of those) a golden spear. They used to all be grown
// and all carry a golden sword.
func TestNaturalPiglinSpawnRolls(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	const n = 2000
	babies, crossbows, swords, spears := 0, 0, 0, 0
	for i := 0; i < n; i++ {
		h.spawnNatural(players, dimNether, catMonster, entityPiglin, i%40, 90, i/40)
	}
	count := 0
	for _, m := range h.mobs {
		if m.etype != entityPiglin {
			continue
		}
		count++
		switch {
		case m.baby:
			babies++
			if m.held != 0 || m.wearsAnything() {
				t.Fatalf("a baby piglin spawns empty-handed and bare, got held=%d gear=%v", m.held, m.gear)
			}
		case m.held == itemCrossbow:
			crossbows++
		case m.held == itemGoldSword:
			swords++
		case m.held == itemGoldSpear:
			spears++
		default:
			t.Fatalf("an adult piglin holds %d", m.held)
		}
	}
	if count < n*9/10 {
		t.Fatalf("only %d of %d piglins spawned", count, n)
	}
	adults := count - babies
	frac := func(a, b int) float64 { return float64(a) / float64(b) }
	if f := frac(babies, count); f < 0.16 || f > 0.24 {
		t.Errorf("babies %.3f of spawns, want ~0.2", f)
	}
	if f := frac(crossbows, adults); f < 0.45 || f > 0.55 {
		t.Errorf("crossbows %.3f of adults, want ~0.5", f)
	}
	if f := frac(spears, swords+spears); f < 0.06 || f > 0.14 {
		t.Errorf("spears %.3f of the melee weapons, want ~0.1", f)
	}
}

// A baby piglin shows as one on its own flag (index 17, not the ageable
// 16) and walks a fifth faster.
func TestPiglinBabyFlagAndSpeed(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityPiglin, 0.5, 70, 0.5)
	adult := m.moveSpeed()
	h.setPiglinBaby(players, m, true)
	if got, want := m.moveSpeed(), adult*1.2; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("baby piglin speed %v, want %v", got, want)
	}
	b := mobBabyMeta(m, true)
	want := boolMeta(m.eid, metaIndexPiglinBaby, true)
	if string(b) != string(want) {
		t.Errorf("baby piglin metadata %x, want %x", b, want)
	}
}

// A bastion's piglins are template mobs: they hold what their piece says
// (a crossbow or a golden sword) and none is a baby.
func TestBastionPiglinsHoldTheirPieceWeapon(t *testing.T) {
	h := newHub(world.New(3))
	players := map[int32]*tracked{}
	h.playersRef = players
	g := h.worldFor(dimNether).Gen()
	var b = g.BastionIn(0, 0)
	for cx := -30; cx < 30 && !b.Exists; cx++ {
		for cz := -30; cz < 30 && !b.Exists; cz++ {
			b = g.BastionIn(cx*448+8, cz*448+8)
		}
	}
	if !b.Exists {
		t.Fatal("no bastion found")
	}
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.dim = dimNether
	pl.x, pl.y, pl.z = float64(b.X), float64(b.Y), float64(b.Z)
	h.populateBastions(players)
	piglins := 0
	for _, m := range h.mobs {
		if m.etype != entityPiglin {
			continue
		}
		piglins++
		if m.baby {
			t.Error("a bastion piglin is never a baby")
		}
		if m.held != itemCrossbow && m.held != itemGoldSword {
			t.Errorf("a bastion piglin holds %d, want its piece's crossbow or golden sword", m.held)
		}
	}
	if piglins == 0 {
		t.Fatal("the bastion seeded no piglins")
	}
}

// A crossbow piglin shoots its target from range instead of meleeing.
func TestCrossbowPiglinShoots(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	pg := h.spawnMob(players, entityPiglin, 0.5, 180, 0.5)
	h.applySpecies(players, pg)
	pg.baby, pg.held = false, itemCrossbow
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	hp := pl.health
	for i := 0; i < 400 && len(h.arrows) == 0; i++ {
		h.tick.Add(1)
		h.updateMobs(players)
		pl.x, pl.y, pl.z = 6.5, 180, 0.5
	}
	if len(h.arrows) == 0 {
		t.Fatal("a crossbow piglin never shot")
	}
	if pl.health != hp {
		t.Errorf("the crossbow piglin should not melee: health %v → %v", hp, pl.health)
	}
}
