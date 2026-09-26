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
// repairs the saves once. A hedge is told from canopy by where it stands: a
// leaf in the edit layer with no trunk within six steps through connected
// leaves AND something a player built close by (worldgen.PlayerBuilt — a
// lantern, a wall, a fence) is a player's, and becomes persistent. Canopy
// left by a felled tree stands among nothing and keeps decaying, as
// vanilla's does.
//
// The first version of the repair (marker .leafpersist-1) had only the trunk
// test and made some eighteen thousand leaves persistent, most of them such
// canopy. A world it ran on gets the second pass (.leafpersist-2): a
// persistent edited leaf with no trunk in reach and no build close by goes
// back to decaying.

const (
	leafRepairMarker = ".leafpersist-1"
	leafUndoMarker   = ".leafpersist-2"
)

// repairPlacedLeaves runs the repair, or the undo of its first version, once.
func (s *Server) repairPlacedLeaves() {
	dir := filepath.Dir(s.WorldFile)
	ran := func(m string) bool { _, err := os.Stat(filepath.Join(dir, m)); return err == nil }
	if ran(leafRepairMarker) && ran(leafUndoMarker) {
		return
	}
	undo := ran(leafRepairMarker) // the first version ran here: take back its canopy
	total := 0
	for _, w := range []*world.World{s.world, s.nether, s.end} {
		if w == nil {
			continue
		}
		if undo {
			total += unpersistCanopy(w)
		} else {
			total += persistHedges(w)
		}
		if err := w.Save(); err != nil { // saved before the markers, or a crash would skip it
			log.Printf("leaf repair: saving: %v (will retry next boot)", err)
			return
		}
	}
	for _, m := range []string{leafRepairMarker, leafUndoMarker} {
		if err := writeAtomic(filepath.Join(dir, m), []byte("done\n")); err != nil {
			log.Printf("leaf repair: writing marker: %v", err)
		}
	}
	if undo {
		log.Printf("leaf repair: %d leaves with no build nearby returned to decaying", total)
	} else {
		log.Printf("leaf repair: %d placed leaves made persistent", total)
	}
}

// builtCells marks the 4-block cells holding something a player built; a
// leaf is "close to a build" when one of the 27 cells around its own does
// (a build within four to seven blocks).
func builtCells(w *world.World) map[[3]int]bool {
	cells := map[[3]int]bool{}
	w.ForEachEdit(func(x, y, z int, state uint32) {
		if worldgen.PlayerBuilt(state) {
			cells[[3]int{x >> 2, y >> 2, z >> 2}] = true
		}
	})
	return cells
}

func nearBuild(cells map[[3]int]bool, x, y, z int) bool {
	cx, cy, cz := x>>2, y>>2, z>>2
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if cells[[3]int{cx + dx, cy + dy, cz + dz}] {
					return true
				}
			}
		}
	}
	return false
}

type leafCell struct {
	x, y, z int
	state   uint32
}

// editedLeaves collects the edit layer's leaves of one persistence (collected
// first: At takes the lock ForEachEdit holds).
func editedLeaves(w *world.World, persistent bool) []leafCell {
	var out []leafCell
	w.ForEachEdit(func(x, y, z int, state uint32) {
		if _, _, p, ok := leafInfo(state); ok && p == persistent {
			out = append(out, leafCell{x, y, z, state})
		}
	})
	return out
}

// persistHedges makes every edited, non-persistent leaf with no trunk in
// reach and a build close by persistent, and reports how many it changed.
func persistHedges(w *world.World) int {
	cells := builtCells(w)
	n := 0
	for _, c := range editedLeaves(w, false) {
		if !nearBuild(cells, c.x, c.y, c.z) || leafChainDistance(w, c.x, c.y, c.z) < 7 {
			continue
		}
		if setLeafPersistent(w, c, true) {
			n++
		}
	}
	return n
}

// unpersistCanopy returns every edited, persistent leaf with no trunk in
// reach and no build close by to decaying.
func unpersistCanopy(w *world.World) int {
	cells := builtCells(w)
	n := 0
	for _, c := range editedLeaves(w, true) {
		if nearBuild(cells, c.x, c.y, c.z) || leafChainDistance(w, c.x, c.y, c.z) < 7 {
			continue
		}
		if setLeafPersistent(w, c, false) {
			n++
		}
	}
	return n
}

func setLeafPersistent(w *world.World, c leafCell, p bool) bool {
	info, ok := worldgen.InfoForState(c.state)
	if !ok {
		return false
	}
	v := "false"
	if p {
		v = "true"
	}
	w.SetBlock(c.x, c.y, c.z, worldgen.SetProperty(info, c.state, "persistent", v))
	return true
}
