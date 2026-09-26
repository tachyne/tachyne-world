package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// goneFor reports whether a removal frame for eid reached this player.
func goneFor(tr *tracked, eid int32) bool {
	for {
		select {
		case pkt := <-tr.p.out:
			if ev, ok := pkt.ev.(attachproto.EntityRemove); ok {
				for _, id := range ev.EIDs {
					if id == eid {
						return true
					}
				}
			}
		default:
			return false
		}
	}
}

// A mob despawning far from a player must still tell that player, or the
// client keeps what it last saw: a creature standing there that cannot be
// hit, because the server no longer has its id. The despawn happens at
// 128 blocks and the old broadcast was culled to six chunks, so the
// goodbye reached nobody at all.
func TestDespawnFarAwayStillTellsViewers(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0, 70, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players

	m := h.spawnMob(players, entityZombie, 400, 70, 400) // far beyond the despawn distance
	if m == nil {
		t.Fatal("no zombie")
	}
	m.hostile = true // as the natural spawner marks it; a monster despawns, a creature does not
	drainEvents(pl)
	h.despawnSweep(players)
	if _, live := h.mobs[m.eid]; live {
		t.Fatal("a zombie 400 blocks from the only player should despawn")
	}
	if !goneFor(pl, m.eid) {
		t.Fatal("the player was never told the zombie was gone — it stays on their screen as a ghost")
	}
	// And the interest-culled broadcast this replaced really would have
	// missed them, so the test above proves something.
	drainEvents(pl)
	h.toNearbyEv(players, 0, 400, 400, entGone(12345))
	if goneFor(pl, 12345) {
		t.Fatal("a broadcast culled to the interest radius should not reach 400 blocks")
	}
}

// The same for a chunk unloading its mobs five seconds after it leaves the
// player's view, which is the other way a creature leaves the world while
// everyone is too far to hear an interest-culled broadcast.
func TestChunkUnloadStillTellsViewers(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0, 70, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.mobstore = newMobStore("")

	cow := h.spawnMob(players, entityCow, 500, 70, 500)
	if cow == nil {
		t.Fatal("no cow")
	}
	c := mobChunkOf(cow)
	h.activeChunks = map[[3]int32]bool{c: true}
	h.chunkOutAt = map[[3]int32]uint64{c: 1}
	h.tick.Store(1 + mobUnloadGrace)
	drainEvents(pl)

	h.reconcileMobChunks(players, map[[3]int32]bool{}) // the chunk is out of range
	if _, live := h.mobs[cow.eid]; live {
		t.Fatal("the cow's chunk left range: its mobs should unload")
	}
	if !goneFor(pl, cow.eid) {
		t.Fatal("the player was never told the cow was gone — a ghost stays behind")
	}
}
