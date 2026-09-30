package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// heartState builds a creaking heart state from its parts (axis 0 x, 1 y,
// 2 z; heartUprooted/Dormant/Awake; natural).
func heartState(axis, st int, natural bool) uint32 {
	s := worldgen.CreakingHeartBase + uint32(axis*6+st*2)
	if !natural {
		s++
	}
	return s
}

// CreakingHeartBlock.getStateForPlacement: set between two logs lying along
// the clicked face's axis, a heart goes in rooted — dormant by day, awake by
// night — and one with no logs goes in uprooted. Either way it is not natural.
func TestPlacedCreakingHeartTakesRootBetweenItsLogs(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1350, 180, 1350
	clearAirBox(w, x, y, z, 3)
	upLog := worldgen.BlockBase("pale_oak_log") + 1 // axis y
	w.SetBlock(x, y-1, z, upLog)
	w.SetBlock(x, y+1, z, upLog)
	selectSlot(p, 0)
	p.setHotbarSlot(0, itemByName["creaking_heart"])

	h.dayTime.Store(1000) // morning
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got, want := w.Block(x, y, z), heartState(1, heartDormant, false); got != want {
		t.Fatalf("a heart set between its logs by day is %s, want %s", describeState(got), describeState(want))
	}

	w.SetBlock(x, y, z, worldgen.Air)
	h.dayTime.Store(15000) // night
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got, want := w.Block(x, y, z), heartState(1, heartAwake, false); got != want {
		t.Fatalf("a heart set between its logs at night is %s, want %s", describeState(got), describeState(want))
	}

	// Logs lying across it do not count.
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x, y+1, z, worldgen.BlockBase("pale_oak_log")) // axis x
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got, want := w.Block(x, y, z), heartState(1, heartUprooted, false); got != want {
		t.Fatalf("a heart under a crosswise log is %s, want uprooted %s", describeState(got), describeState(want))
	}
}

// CreakingHeartBlock.updateShape: an uprooted heart whose missing log is put
// back takes root on the tick after, not on its block entity's next check.
func TestUprootedHeartRootsWhenItsLogReturns(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	pos := blockPos{60, 180, 60}
	w.ForceLoad(pos.x, pos.z, 1)
	upLog := worldgen.BlockBase("pale_oak_log") + 1
	w.SetBlock(pos.x, pos.y-1, pos.z, upLog)
	w.SetBlock(pos.x, pos.y+1, pos.z, worldgen.Air)
	uprooted := heartState(1, heartUprooted, true)
	w.SetBlock(pos.x, pos.y, pos.z, uprooted)
	h.dayTime.Store(1000)

	h.tick.Store(10)
	w.SetBlock(pos.x, pos.y+1, pos.z, upLog)
	h.onBlock(players, evBlock{dim: dimOverworld, x: pos.x, y: pos.y + 1, z: pos.z, state: upLog, placed: true})
	if got := w.At(pos.x, pos.y, pos.z); got != uprooted {
		t.Fatalf("the heart changed before its tick: %s", describeState(got))
	}
	h.tick.Store(11)
	h.runUpdates(players, 11)
	if got, want := w.At(pos.x, pos.y, pos.z), heartState(1, heartDormant, true); got != want {
		t.Fatalf("the heart is %s a tick after its log came back, want %s", describeState(got), describeState(want))
	}
	if h.hearts[simPos{dim: dimOverworld, blockPos: pos}] == nil {
		t.Fatal("the rooted heart's block entity is not ticking")
	}
}

// CreakingHeartBlockEntity.updateCreakingState: cut out of its tree, a heart
// with a creaking out keeps its state (and its creaking); only one with none
// out uproots.
func TestHeartWithACreakingOutDoesNotUproot(t *testing.T) {
	h, pos, link := paleTrunk(t)
	players := map[int32]*tracked{}
	nightHub(h)
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.CreakingHeartAwake)
	m := h.spawnMob(players, entityCreaking, float64(pos.x)+3.5, float64(pos.y), float64(pos.z)+0.5)
	m.home, m.heartBound = pos, true
	link.creaking = m.eid
	h.world.SetBlock(pos.x, pos.y+1, pos.z, worldgen.Air)
	link.nextAt = 0
	h.updateHearts(players)
	if got := h.world.At(pos.x, pos.y, pos.z); got != worldgen.CreakingHeartAwake {
		t.Fatalf("a heart with its creaking out became %s", describeState(got))
	}
	if h.mobs[m.eid] == nil || link.creaking != m.eid {
		t.Fatal("the heart let its creaking go when its log was cut")
	}
}

// CreakingHeartBlock.onExplosionHit: a player's TNT that takes out a natural
// heart kills its creaking — blamed on that player — and pays the player the
// heart's 20-24 experience. A blast with no one behind it pays nothing.
// heartInTheOpen clears the ground round a paleTrunk heart, leaving only its
// trunk, so a blast beside it reaches it through air.
func heartInTheOpen(h *hub, pos blockPos) {
	h.world.ForceLoad(pos.x, pos.z, 1)
	for dx := -4; dx <= 4; dx++ {
		for dy := -4; dy <= 4; dy++ {
			for dz := -4; dz <= 4; dz++ {
				if dx != 0 || dz != 0 {
					h.world.SetBlock(pos.x+dx, pos.y+dy, pos.z+dz, worldgen.Air)
				}
			}
		}
	}
}

func TestBlownUpHeartKillsItsCreaking(t *testing.T) {
	h, pos, link := paleTrunk(t)
	heartInTheOpen(h, pos)
	nightHub(h)
	by := &tracked{p: newPlayer(12, "miner", [16]byte{}), gamemode: gmSurvival, x: float64(pos.x) + 20, y: float64(pos.y), z: float64(pos.z)}
	players := map[int32]*tracked{12: by}
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.CreakingHeartAwake) // natural
	m := h.spawnMob(players, entityCreaking, float64(pos.x)+10.5, float64(pos.y), float64(pos.z)+0.5)
	m.home, m.heartBound = pos, true
	link.creaking = m.eid
	for id := range h.orbs {
		delete(h.orbs, id)
	}
	h.explodeTyped(players, 0, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+1.5, 6, 6, blastTNT, dtExplosion, deathCause{},
		withBlastCause(12, false))
	if isCreakingHeartBlock(h.world.At(pos.x, pos.y, pos.z)) {
		t.Fatal("the blast did not take the heart")
	}
	if m.dying == 0 {
		t.Fatal("the heart's creaking did not die of the blast")
	}
	if m.hurtByPlayer != by.p.eid {
		t.Errorf("the creaking's death is blamed on %d, want the player who lit the TNT (%d)", m.hurtByPlayer, by.p.eid)
	}
	xp := 0
	for _, o := range h.orbs {
		xp += o.value * o.count
	}
	if xp < 20 {
		t.Errorf("the player's blast paid %d experience, want the heart's 20-24 (plus nothing less)", xp)
	}

	// No one behind the blast: the creaking still dies, no experience.
	h2, pos2, link2 := paleTrunk(t)
	heartInTheOpen(h2, pos2)
	nightHub(h2)
	players2 := map[int32]*tracked{}
	m2 := h2.spawnMob(players2, entityCreaking, float64(pos2.x)+10.5, float64(pos2.y), float64(pos2.z)+0.5)
	m2.home, m2.heartBound = pos2, true
	link2.creaking = m2.eid
	h2.explodeTyped(players2, 0, float64(pos2.x)+0.5, float64(pos2.y)+0.5, float64(pos2.z)+1.5, 6, 6, blastTNT, dtExplosion, deathCause{})
	if m2.dying == 0 || link2.creaking != 0 {
		t.Fatal("an unowned blast left the creaking bound to a heart that is gone")
	}
	if len(h2.orbs) != 0 {
		t.Error("a blast with no player behind it paid experience for the heart")
	}
}
