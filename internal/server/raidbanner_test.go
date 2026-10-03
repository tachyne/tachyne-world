package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// ObtainRaidLeaderBannerGoal: a raider of a wave whose captain is gone walks
// to a dropped ominous banner, puts it on and leads the wave; while the wave
// has a living captain, nobody goes for it.
func TestRaiderTakesUpADroppedBanner(t *testing.T) {
	run := func(withLeader bool) (*mob, *itemEntity, *hub) {
		h := newTestHub(world.New(1))
		h.world.ForceLoad(0, 0, 3)
		poiFloor(h, 0, 0, 24)
		pl := survPlayer(h)
		pl.x, pl.y, pl.z, pl.gamemode = 0.5, 180, -20.5, gmCreative
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		center := blockPos{0, 180, 0}
		r := &raid{center: center, uuid: raidUUID(center),
			alive: map[int32]bool{}, shown: map[int32]bool{}, numGroups: 3, wave: 1}
		h.raids[center] = r
		v := h.spawnHostileYIn(players, entityVindicator, 0, 0.5, 180, 0.5)
		v.raidCenter, v.raidWave = center, 1
		r.alive[v.eid] = true
		if withLeader {
			c := h.spawnHostileYIn(players, entityPillager, 0, -10.5, 180, -10.5)
			c.raidCenter, c.raidWave = center, 1
			r.alive[c.eid] = true
			h.makeCaptain(players, c)
		}
		it := h.spawnItemIn(players, 0, itemWhiteBanner, 1, 10.5, 180, 6.5)
		it.setFrom(ominousBanner())
		it.noPickupUntil = 0
		for i := 0; i < 300 && h.items[it.eid] != nil; i++ {
			h.tick.Add(mobMoveInterval)
			h.mobUpdate(players)
		}
		return v, it, h
	}
	v, it, h := run(false)
	if h.items[it.eid] != nil || !isCaptain(v) || !v.gearSure[0] {
		t.Fatalf("the raider never took up the banner: at (%.1f, %.1f), captain %v", v.x, v.z, isCaptain(v))
	}
	if l := h.raidWaveLeader(h.raidOf(v), 1); l != v {
		t.Fatal("the raider that took the banner leads its wave")
	}
	v, it, h = run(true)
	if h.items[it.eid] == nil || isCaptain(v) {
		t.Fatal("a wave with a living captain leaves the banner where it lies")
	}
}
