package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Bonus spawns follow vanilla's per-difficulty table, an omen level above
// one brings a bonus wave, a raid survives a save/restore with its raiders,
// and a grateful villager throws a hero a gift.
func TestRaidBonusSpawnsAndPersistence(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs.json"))
	h.rules.Difficulty = diffEasy
	for i := 0; i < 50; i++ {
		if n := h.raidBonusSpawns(entityWitch, 3, false); n != 0 {
			t.Fatal("no witch bonus on Easy")
		}
		if n := h.raidBonusSpawns(entityRavager, 5, true); n != 0 {
			t.Fatal("no ravager bonus on Easy")
		}
		if n := h.raidBonusSpawns(entityPillager, 1, false); n < 0 || n > 1 {
			t.Fatalf("Easy pillager bonus %d", n)
		}
	}
	h.rules.Difficulty = diffHard
	seen := map[int]bool{}
	for i := 0; i < 200; i++ {
		n := h.raidBonusSpawns(entityVindicator, 1, false)
		if n < 0 || n > 2 {
			t.Fatalf("Hard vindicator bonus %d", n)
		}
		seen[n] = true
		if e := h.raidBonusSpawns(entityEvoker, 5, false); e != 0 {
			t.Fatal("evokers never get a bonus")
		}
	}
	if !seen[2] {
		t.Error("Hard should sometimes add two vindicators")
	}
	// A level-2 omen raid is not over after its last regular wave.
	h.rules.Difficulty = diffNormal
	center := blockPos{100, 64, 100}
	h.startRaidLevel(players, center, 2)
	r := h.raids[center]
	if r == nil || r.omenLevel != 2 {
		t.Fatal("raid should start at omen level 2")
	}
	r.wave = r.numGroups // pretend the regular waves are done
	// Persistence round-trip: raiders carry the centre, the raid rebuilds.
	m := h.spawnMob(players, entityPillager, 105, 64, 105)
	m.raidCenter = center
	r.alive[m.eid] = true
	h.mobstore.recordRaids(h.raids)
	sr := h.mobstore.raids()
	if len(sr) != 1 || sr[0].Omen != 2 || sr[0].Wave != r.numGroups {
		t.Fatalf("saved raid %+v", sr)
	}
	h2 := newHub(world.New(1))
	h2.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs2.json"))
	m2 := h2.spawnMob(players, entityPillager, 105, 64, 105)
	m2.raidCenter = center
	h2.restoreRaids(sr)
	r2 := h2.raids[center]
	if r2 == nil || !r2.alive[m2.eid] || r2.omenLevel != 2 || r2.wave != r.numGroups {
		t.Fatalf("restored raid %+v", r2)
	}
	if r2.uuid != r.uuid {
		t.Error("the boss bar identity must be stable across a restart")
	}
}

func TestVillagerGiftsHero(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	v := h.spawnMob(players, entityVillager, 10.5, 200, 10.5)
	h.initVillagerTrades(v, 0) // farmer
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = 12.5, 200, 10.5
	if _, _, ok := h.villagerGiftSteer(players, v); ok {
		t.Fatal("no gift without Hero of the Village")
	}
	h.applyEffect(players, pl, effHeroOfVillage, 0, 600)
	if _, _, ok := h.villagerGiftSteer(players, v); !ok {
		t.Fatal("a hero within five blocks gets a gift")
	}
	if len(h.items) == 0 {
		t.Error("the gift should be lying on the ground")
	}
	if v.giftAt <= h.tick.Load() {
		t.Error("the next gift waits")
	}
	if _, _, ok := h.villagerGiftSteer(players, v); ok {
		t.Error("no second gift before the timer")
	}
}
