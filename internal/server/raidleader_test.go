package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Raid.spawnGroup: each wave's first raider that can lead (in the raider
// types' order, so a vindicator before a pillager, never a witch or a
// ravager) carries the ominous banner on its head.
func TestRaidWaveHasABannerCaptain(t *testing.T) {
	skipHeavy(t) // real terrain under the wave
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	h.playersRef = players
	lx, lz := h.findLand(120, 120)
	center := blockPos{lx, h.world.SurfaceFeet(lx, lz), lz}
	h.rules.Difficulty = diffHard
	raidVillage(h, center)
	h.startRaid(players, center)
	r := h.raids[center]
	if r == nil {
		t.Fatal("no raid")
	}
	sawVindicatorLead := false
	for {
		byType := map[int]int{}
		var caps []*mob
		for eid := range r.alive {
			m := h.mobs[eid]
			if m == nil || m.raidWave != r.wave {
				continue
			}
			byType[m.etype]++
			if isCaptain(m) {
				caps = append(caps, m)
			}
			if m.gear[0].item != 0 && !isCaptain(m) {
				t.Errorf("wave %d: only the captain wears a banner", r.wave)
			}
		}
		want := 0
		for _, e := range raiderOrder {
			if byType[e] > 0 && canBeLeader(e) {
				want = e
				break
			}
		}
		switch {
		case want == 0 && len(caps) != 0:
			t.Errorf("wave %d: a captain with nobody able to lead", r.wave)
		case want != 0 && (len(caps) != 1 || caps[0].etype != want):
			t.Fatalf("wave %d (%v): want one captain of type %d, got %d", r.wave, byType, want, len(caps))
		case want == entityVindicator:
			sawVindicatorLead = true
		}
		for _, c := range caps {
			if !c.gearSure[0] {
				t.Error("the banner always drops (drop chance 2.0)")
			}
		}
		if r.wave >= r.numGroups {
			break
		}
		for eid := range r.alive {
			delete(h.mobs, eid)
			delete(r.alive, eid)
		}
		h.spawnWave(players, r)
	}
	if !sawVindicatorLead {
		t.Error("no wave was led by a vindicator")
	}
}

// A pillager captain dies with an ominous bottle and its banner; a
// vindicator captain drops only the banner (the bottle is in the
// pillager's loot table alone, and needs the banner on its head).
func TestCaptainDeathDrops(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(10, 10, 1)
	count := func() (bottles, banners int) {
		for id, it := range h.items {
			switch it.item {
			case itemOminousBottle:
				bottles++
			case itemByName["white_banner"]:
				banners++
			}
			delete(h.items, id)
		}
		return
	}
	for _, tc := range []struct {
		etype           int
		bottle, captain bool
	}{{entityPillager, true, true}, {entityVindicator, false, true}, {entityPillager, false, false}} {
		m := h.spawnHostileYIn(players, tc.etype, dimOverworld, 10.5, 200, 10.5)
		if tc.captain {
			h.makeCaptain(players, m)
		}
		h.killMob(players, m)
		h.despawnMob(players, m)
		bottles, banners := count()
		if (bottles == 1) != tc.bottle || (banners == 1) != tc.captain || bottles > 1 || banners > 1 {
			t.Errorf("type %d captain=%v: %d bottles, %d banners", tc.etype, tc.captain, bottles, banners)
		}
	}
}
