package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// MobEffectInstance's hidden stack on a mob: a strong short Speed over a
// weak long one shows the strong one, keeps the weak one beneath it, and
// falls back to it when the strong one runs out.
func TestMobHiddenEffectStack(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	h.addMobEffect(players, m, effSpeed, activeEffect{amp: 0, left: 600})
	if !h.addMobEffect(players, m, effSpeed, activeEffect{amp: 2, left: 40}) {
		t.Fatal("a stronger Speed did not take")
	}
	e := m.effects[effSpeed]
	if e.amp != 2 || e.hidden == nil || e.hidden.amp != 0 {
		t.Fatalf("showing amp %d, hidden %+v", e.amp, e.hidden)
	}
	for i := 0; i < 45; i++ {
		h.updateMobEffects(players)
	}
	if e := m.effects[effSpeed]; e == nil || e.amp != 0 || e.left < 500 {
		t.Fatalf("after the strong one ran out: %+v, want the hidden Speed I back", e)
	}
	// A weaker, longer one arriving second goes underneath rather than away.
	m2 := h.spawnMob(players, entityZombie, 3.5, 180, 0.5)
	h.addMobEffect(players, m2, effStrength, activeEffect{amp: 1, left: 40})
	h.addMobEffect(players, m2, effStrength, activeEffect{amp: 0, left: 600})
	if e := m2.effects[effStrength]; e.amp != 1 || e.hidden == nil || e.hidden.left != 600 {
		t.Fatalf("the weaker longer Strength was dropped: %+v", e)
	}
}

// A player's hidden stack survives a relog.
func TestPlayerHiddenEffectSaved(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.applyEffectTicks(players, pl, effSpeed, 0, 6000)
	h.applyEffectTicks(players, pl, effSpeed, 2, 200)
	path := filepath.Join(t.TempDir(), "inv.json")
	newInvStore(path).save("tester", pl)
	back := testTracked()
	newInvStore(path).loadInto(back, "tester")
	e := back.effects[effSpeed]
	if e == nil || e.amp != 2 || e.hidden == nil || e.hidden.amp != 0 || e.hidden.left != 6000 {
		t.Fatalf("restored %+v, want Speed III over a hidden Speed I", e)
	}
}
