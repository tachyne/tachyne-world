package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func pandaWith(h *hub, players map[int32]*tracked, trait int32) *mob {
	m := h.spawnAnimal(players, entityPanda, 3, 3)
	m.variant = packPandaGenes(trait, trait)
	m.x, m.y, m.z = 3.5, 180, 3.5
	return m
}

// TestPandaPersonalities: a worried panda sits out thunder, a lazy one lies
// on its back and gets up again, a playful one rolls off a ledge along its
// facing, and the flags byte carries every state.
func TestPandaPersonalities(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.worldFor(0)
	for x := 0; x < 8; x++ {
		for z := 0; z < 8; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	// Worried.
	wo := pandaWith(h, players, pandaWorried)
	h.thundering = true
	if !h.pandaStep(players, wo) || wo.pandaFlags&pandaFlagSit == 0 {
		t.Fatal("a worried panda sits when it thunders")
	}
	h.thundering = false
	h.pandaStep(players, wo)
	if wo.pandaFlags&pandaFlagSit != 0 {
		t.Fatal("…and stands when the storm passes")
	}
	wo.dying = 1
	// Lazy: slow (setAttributes), lies down within a few thousand updates, then gets up.
	la := pandaWith(h, players, pandaLazy)
	h.applyPandaGenes(la)
	if got, want := la.moveSpeed(), pandaLazySpeed*attrToStep; math.Abs(got-want) > 1e-9 {
		t.Fatalf("a lazy panda walks at 0.07: %.4f want %.4f", got, want)
	}
	lay := false
	for i := 0; i < 5000 && !lay; i++ {
		h.pandaStep(players, la)
		lay = la.pandaFlags&pandaFlagOnBack != 0
	}
	if !lay {
		t.Fatal("a lazy panda should lie on its back at 1/200 a goal tick")
	}
	up := false
	for i := 0; i < 20000 && !up; i++ {
		h.pandaStep(players, la)
		up = la.pandaFlags&pandaFlagOnBack == 0
	}
	if !up || la.lieCD == 0 {
		t.Fatal("it gets up again, and waits before lying down once more")
	}
	la.dying = 1
	// Playful, facing a ledge: rolls at once, along its facing.
	pl := pandaWith(h, players, pandaPlayful)
	pl.yaw = 0 // facing +z
	w.SetBlock(3, 179, 4, worldgen.Air)
	if !h.pandaStep(players, pl) || pl.pandaFlags&pandaFlagRoll == 0 || pl.vz <= 0 {
		t.Fatalf("a playful panda rolls off the ledge ahead: flags %d vz %.3f", pl.pandaFlags, pl.vz)
	}
	w.SetBlock(3, 179, 4, worldgen.Stone) // the panda never actually moves here: fill the ledge so it does not roll again
	for i := 0; i < pandaRollTicks; i++ {
		h.pandaStep(players, pl)
	}
	if pl.pandaFlags&pandaFlagRoll != 0 {
		t.Fatal("the roll ends after 32 ticks")
	}
	// The flags ride the variant metadata.
	meta := variantMeta(pl)
	if len(meta) == 0 || meta[len(meta)-4] != metaIndexPandaFlags {
		t.Fatalf("panda metadata should end with the flags byte: %v", meta)
	}
}
