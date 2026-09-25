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
	h.jukeboxes[simPos{dim: dimOverworld, blockPos: blockPos{2, 150, 0}}] = &jukebox{disc: disc, started: 1, length: 1 << 40}
	h.allayStep(players, m)
	if m.dancing {
		t.Fatal("an overworld jukebox must not set a Nether allay dancing")
	}
	h.jukeboxes[simPos{dim: dimNether, blockPos: blockPos{2, 150, 0}}] = &jukebox{disc: disc, started: 1, length: 1 << 40}
	h.allayStep(players, m)
	if !m.dancing {
		t.Fatal("a Nether allay beside a playing jukebox dances")
	}
}
