package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Raider.RaiderCelebration: a lost raid's raiders stop walking to the
// village, raise the IS_CELEBRATING flag, and jump now and then; when the
// raid is gone the flag comes down again.
func TestLostRaidRaidersCelebrate(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	h.playersRef = players
	center := blockPos{64, 200, 64}
	h.world.ForceLoad(center.x, center.z, 3)
	h.poiWorld(dimOverworld)
	stone := worldgen.BlockBase("stone")
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(100+dx, 199, 64+dz, stone)
		}
	}
	pl := testTracked()
	pl.gamemode = gmCreative // nobody for the raiders to fight
	pl.x, pl.y, pl.z = 104, 200, 64
	players[pl.p.eid] = pl
	m := h.spawnHostileYIn(players, entityPillager, dimOverworld, 100.5, 200, 64.5)
	m.raidCenter = center
	h.raids[center] = &raid{center: center, uuid: raidUUID(center), wave: 1, numGroups: 5,
		alive: map[int32]bool{m.eid: true}, shown: map[int32]bool{}}

	h.updateMobs(players)
	if m.celebrating {
		t.Fatal("an ongoing raid is nothing to celebrate")
	}
	h.updateRaids(players) // no village any more: the raid is lost
	if h.raids[center].lostLeft == 0 {
		t.Fatal("the raid should be lost")
	}
	pl.tracked = map[int32]bool{m.eid: true} // the player has the raider in view
	drainEvs(pl.p)
	x0, z0 := m.x, m.z
	jumped := false
	for i := 0; i < 200; i++ {
		h.updateMobs(players)
		jumped = jumped || m.leaping
	}
	if !m.celebrating {
		t.Fatal("a raider of a lost raid with nothing to fight celebrates")
	}
	if m.x != x0 || m.z != z0 {
		t.Errorf("a celebrating raider stays put, moved from (%v,%v) to (%v,%v)", x0, z0, m.x, m.z)
	}
	if !jumped {
		t.Error("a celebrating raider jumps now and then")
	}
	sawFlag := false
	for _, ev := range drainEvs(pl.p) {
		want := metaEv(boolMeta(m.eid, metaIndexRaiderCelebrating, true))
		if me, ok := ev.(attachproto.EntityMeta); ok && me.EID == m.eid && string(me.Meta) == string(want.Meta) {
			sawFlag = true
		}
	}
	if !sawFlag {
		t.Error("viewers are sent IS_CELEBRATING")
	}
	for i := 0; i < raidDefeatSecs; i++ {
		h.updateRaids(players)
	}
	h.updateMobs(players)
	if m.celebrating {
		t.Error("the celebration ends with the raid")
	}
}
