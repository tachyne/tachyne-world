package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// AllayDropItemOnBlockTrigger has no dimension in it: an allay that drops
// a cake on the note block it heard earns its player "Birthday Song" in
// the Nether too. Only an overworld delivery used to count.
func TestAllayNoteBlockDeliveryCountsInAnyDimension(t *testing.T) {
	h := newHub(world.New(1))
	nether := h.worldFor(dimNether)
	nether.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	pl.adv = advState{}
	pl.dim, pl.x, pl.y, pl.z = dimNether, 4.5, 150, 4.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	note := blockPos{0, 149, 0}
	nether.SetBlock(note.x, note.y, note.z, worldgen.BlockBase("note_block"))
	m := h.spawnSpecies(players, entityAllay, dimNether, 0.5, 150, 0.5)
	m.owner = pl.p.eid
	h.rules.MobGriefing = true
	m.held = itemByName["cake"] // the item it was given to look for
	m.carry = invStack{item: itemByName["cake"], count: 1}
	m.allayNote, m.allayNoteDim, m.allayNoteCD = note, dimNether, allayNoteCD
	h.allayStep(players, m)
	if m.carry.count != 0 {
		t.Fatal("the allay beside its note block should have dropped the cake")
	}
	if !hasCrit(pl, "minecraft:husbandry/allay_deliver_cake_to_note_block", "allay_deliver_cake_to_note_block") {
		t.Fatal("a cake dropped on a Nether note block earns Birthday Song")
	}
}

// An allay dances to a jukebox in its own dimension, whichever that is;
// one playing in another dimension at the same coordinates does not count.
// It hears the jukebox's JUKEBOX_PLAY, sent once a second while the song
// runs.
func TestAllayDancesInAnyDimension(t *testing.T) {
	h := newHub(world.New(1))
	nether := h.worldFor(dimNether)
	nether.ForceLoad(0, 0, 1)
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	pl.dim, pl.x, pl.y, pl.z = dimNether, 4.5, 150, 4.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnSpecies(players, entityAllay, dimNether, 0.5, 150, 0.5)
	disc := invStack{item: int32(itemByName["music_disc_cat"]), count: 1}
	h.tick.Store(100)
	h.world.SetBlock(2, 150, 0, jukeboxState(true))
	h.jukeboxes[simPos{dim: dimOverworld, blockPos: blockPos{2, 150, 0}}] = &jukebox{disc: disc, started: 100, length: 1 << 40}
	h.jukeboxTick(players)
	if m.dancing {
		t.Fatal("an overworld jukebox must not set a Nether allay dancing")
	}
	nether.SetBlock(2, 150, 0, jukeboxState(true))
	h.jukeboxes[simPos{dim: dimNether, blockPos: blockPos{2, 150, 0}}] = &jukebox{disc: disc, started: 100, length: 1 << 40}
	h.jukeboxTick(players)
	if !m.dancing {
		t.Fatal("a Nether allay beside a playing jukebox dances")
	}
}

// Allay.JukeboxListener hears JUKEBOX_PLAY within ten blocks, not the
// vibration listener's sixteen; the song's end stops the dance, and so does
// walking ten away or the jukebox being broken (shouldStopDancing).
func TestAllayJukeboxListener(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	jpos := blockPos{0, 180, 0}
	h.world.SetBlock(jpos.x, jpos.y, jpos.z, jukeboxState(true))
	disc := invStack{item: int32(itemByName["music_disc_cat"]), count: 1}
	h.tick.Store(100)
	jb := &jukebox{disc: disc, started: 100, length: 400}
	h.jukeboxes[simPos{dim: dimOverworld, blockPos: jpos}] = jb
	far := h.spawnSpecies(players, entityAllay, 0, 13.5, 180, 0.5)
	near := h.spawnSpecies(players, entityAllay, 0, 6.5, 180, 0.5)
	h.jukeboxTick(players)
	if far.dancing {
		t.Error("an allay thirteen blocks off is past JUKEBOX_PLAY's ten")
	}
	if !near.dancing {
		t.Fatal("an allay six blocks off hears the jukebox")
	}
	// The song ends: JUKEBOX_STOP_PLAY.
	h.tick.Store(500)
	h.jukeboxTick(players)
	if near.dancing {
		t.Fatal("the song's end stops the dance")
	}
	// Dancing again, it walks away: past ten the next check stops it.
	jb.started, jb.length = 500, 1<<30
	h.jukeboxTick(players)
	if !near.dancing {
		t.Fatal("a new song sets it dancing again")
	}
	near.x = 11.5
	for i := 0; i < 20; i++ {
		h.tick.Add(1)
		h.allayStep(players, near)
	}
	if near.dancing {
		t.Fatal("an allay eleven blocks from its jukebox stops dancing")
	}
	// Back beside it and dancing; the jukebox is broken.
	near.x = 3.5
	h.tick.Store(520)
	h.jukeboxTick(players)
	if !near.dancing {
		t.Fatal("back in range, it dances")
	}
	h.world.SetBlock(jpos.x, jpos.y, jpos.z, worldgen.Air)
	for i := 0; i < 20; i++ {
		h.tick.Add(1)
		h.allayStep(players, near)
	}
	if near.dancing {
		t.Fatal("with the jukebox gone it stops dancing")
	}
}
