package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func evokerSetup(t *testing.T) (*hub, *mob, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 181, 0.5
	players[pl.p.eid] = pl
	// Solid ground under the whole fight so fangs have something to stand on.
	for x := -40; x <= 40; x++ {
		for z := -40; z <= 40; z++ {
			h.world.SetBlock(x, 180, z, worldgen.BlockBase("stone"))
		}
	}
	m := h.spawnMobIn(players, entityEvoker, 0, 10, 181, 0)
	if m == nil {
		t.Fatal("evoker spawn returned nil")
	}
	m.hostile = true
	return h, m, pl, players
}

// The whole point of an evoker: it casts. It never did.
func TestEvokerCastsFangs(t *testing.T) {
	h, m, _, players := evokerSetup(t)
	m.fangNextAt, m.vexNextAt = 0, ^uint64(0) // fangs only

	h.evokerCast(players, m)
	if len(h.fangs) == 0 {
		t.Fatal("the evoker cast no fangs")
	}
	// At range vanilla lays a LINE of 16 walking toward the target.
	if len(h.fangs) != 16 {
		t.Errorf("ranged cast laid %d fangs, want 16", len(h.fangs))
	}
	// Each is delayed one tick more than the last, so the strike travels.
	if h.fangs[0].delay >= h.fangs[15].delay {
		t.Error("ranged fangs should be progressively delayed along the line")
	}
	// …and it respects its cooldown.
	n := len(h.fangs)
	h.evokerCast(players, m)
	if len(h.fangs) != n {
		t.Error("the evoker cast again without waiting out its interval")
	}
}

// Close in, the pattern is two rings rather than a line.
func TestEvokerRingsWhenClose(t *testing.T) {
	h, m, pl, players := evokerSetup(t)
	m.x, m.z = pl.x+1, pl.z // well inside vanilla's 3-block threshold
	m.fangNextAt, m.vexNextAt = 0, ^uint64(0)

	h.evokerCast(players, m)
	if len(h.fangs) != 13 { // 5 inner + 8 outer
		t.Errorf("close cast laid %d fangs, want 13", len(h.fangs))
	}
}

// A fang bites once, for its full magic damage, and then sinks.
func TestFangBitesOnceThenSinks(t *testing.T) {
	h, m, pl, players := evokerSetup(t)
	f := &evokerFang{eid: h.allocEID(), dim: 0, x: pl.x, y: pl.y, z: pl.z,
		delay: 1, life: 1 + fangLife, owner: m.eid}
	h.fangs = append(h.fangs, f)

	for i := 0; i < fangStrikeAfter+4; i++ {
		h.updateFangs(players)
	}
	if pl.health != 20-fangDamage {
		t.Errorf("the fang dealt %v damage, want %v", 20-pl.health, fangDamage)
	}
	// It must not keep biting every tick it stands there.
	pl.health = 20
	for i := 0; i < 10; i++ {
		h.updateFangs(players)
	}
	if pl.health != 20 {
		t.Errorf("the fang bit a second time for %v", 20-pl.health)
	}
	// And it eventually sinks.
	for i := 0; i < fangLife+8; i++ {
		h.updateFangs(players)
	}
	if len(h.fangs) != 0 {
		t.Errorf("%d fangs still standing after their life ran out", len(h.fangs))
	}
}

// Armour is no defence — the fangs' damage type bypasses it, which is what
// makes an evoker dangerous to a kitted player.
func TestFangsBypassArmour(t *testing.T) {
	h, m, pl, players := evokerSetup(t)
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["diamond_chestplate"], count: 1}
	}
	f := &evokerFang{eid: h.allocEID(), dim: 0, x: pl.x, y: pl.y, z: pl.z,
		delay: 0, life: fangLife, owner: m.eid}
	h.fangs = append(h.fangs, f)
	for i := 0; i < fangStrikeAfter+4; i++ {
		h.updateFangs(players)
	}
	if got := 20 - pl.health; got != fangDamage {
		t.Errorf("full diamond took %v from a fang, want the full %v", got, fangDamage)
	}
}

// The vex summon: three of them, and they expire on their own.
func TestEvokerSummonsVexesThatExpire(t *testing.T) {
	h, m, _, players := evokerSetup(t)
	h.summonVexes(players, m)

	n := 0
	var one *mob
	for _, v := range h.mobs {
		if v.etype == entityVex {
			n++
			one = v
		}
	}
	if n != vexSummonCount {
		t.Fatalf("summoned %d vexes, want %d", n, vexSummonCount)
	}
	if one.vexLife <= 0 {
		t.Error("a summoned vex has no limited life and would linger forever")
	}
	// Run its clock out.
	one.vexLife = 1
	h.updateVexLife(players)
	for _, v := range h.mobs {
		if v == one {
			t.Error("an expired vex is still in the world")
		}
	}
}
