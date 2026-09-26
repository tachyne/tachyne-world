package server

import (
	"log"
	"os"
	"path/filepath"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Leaves a player placed before placement set PERSISTENT were saved as tree
// leaves (persistent=false), so a hedge with no trunk in reach rotted like
// the canopy of a felled tree (bugs #40, #43). Placement is right now; this
// repairs the saves once. A leaf in the edit layer — something changed it, so
// it is not untouched generation — that is not persistent and has no trunk
// within six steps through connected leaves is taken to be a player's, and
// becomes persistent. A leaf that is part of a living tree keeps decaying as
// vanilla's does. The cost of the rule: canopy left behind by a tree felled
// where nobody has been since stays up.

// leafRepairMarker records that the repair has run for this world directory.
const leafRepairMarker = ".leafpersist-1"

// repairPlacedLeaves runs the repair over every dimension, once.
func (s *Server) repairPlacedLeaves() {
	marker := filepath.Join(filepath.Dir(s.WorldFile), leafRepairMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	total := 0
	for _, w := range []*world.World{s.world, s.nether, s.end} {
		if w == nil {
			continue
		}
		total += persistOrphanedLeaves(w)
		if err := w.Save(); err != nil { // saved before the marker, or a crash would skip it
			log.Printf("leaf repair: saving: %v (will retry next boot)", err)
			return
		}
	}
	if err := writeAtomic(marker, []byte("done\n")); err != nil {
		log.Printf("leaf repair: writing marker: %v", err)
	}
	log.Printf("leaf repair: %d placed leaves made persistent", total)
}

// persistOrphanedLeaves makes every edited, non-persistent leaf with no trunk
// in reach persistent, and reports how many it changed.
func persistOrphanedLeaves(w *world.World) int {
	type cell struct {
		x, y, z int
		state   uint32
	}
	var leaves []cell
	w.ForEachEdit(func(x, y, z int, state uint32) { // collected first: At takes the same lock
		if _, _, persistent, ok := leafInfo(state); ok && !persistent {
			leaves = append(leaves, cell{x, y, z, state})
		}
	})
	n := 0
	for _, c := range leaves {
		if leafChainDistance(w, c.x, c.y, c.z) < 7 {
			continue // held by a trunk: a tree's leaf
		}
		info, ok := worldgen.InfoForState(c.state)
		if !ok {
			continue
		}
		w.SetBlock(c.x, c.y, c.z, worldgen.SetProperty(info, c.state, "persistent", "true"))
		n++
	}
	return n
}
