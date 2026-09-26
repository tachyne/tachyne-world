package server

import (
	"log"
	"os"
	"path/filepath"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Villagers' door swings were saved as edits of both halves (report #48), and
// when a GenVersion laid the villages out afresh the houses moved and the
// doors the edits remembered stayed standing alone. SetBlock no longer keeps
// such a swing; this clears the ones already saved, once. A door edit goes
// when all three hold: generation has no door at either half, a village well
// is within loneDoorVillageRadius, and nothing solid stands beside either
// half (doorHeld) — a door in a player's wall, or far from any village, is left alone.
// The edits are reverted, so the cells read as generation made them.

const (
	loneDoorsMarker       = ".lonedoors-1"
	loneDoorVillageRadius = 80
)

// clearLoneDoors runs the clean-up once, on the overworld.
func (s *Server) clearLoneDoors() {
	dir := filepath.Dir(s.WorldFile)
	if _, err := os.Stat(filepath.Join(dir, loneDoorsMarker)); err == nil || s.world == nil {
		return
	}
	var wells [][3]int
	if s.hub != nil && s.hub.mobstore != nil {
		wells = s.hub.mobstore.villages()
	}
	doors := loneVillageDoors(s.world, wells)
	for _, d := range doors {
		for _, y := range []int{d.y, d.y + 1} {
			if st, ok := s.world.EditAt(d.x, y, d.z); ok && worldgen.IsDoor(st) {
				s.world.RevertEdit(d.x, y, d.z)
			}
		}
	}
	if err := s.world.Save(); err != nil { // saved before the marker, or a crash would skip it
		log.Printf("lone doors: saving: %v (will retry next boot)", err)
		return
	}
	if err := writeAtomic(filepath.Join(dir, loneDoorsMarker), []byte("done\n")); err != nil {
		log.Printf("lone doors: writing marker: %v", err)
	}
	log.Printf("lone doors: %d stand-alone door edits near villages reverted to generation", len(doors))
}

// loneVillageDoors lists, by lower half, the edited doors that generation
// does not have, that stand within loneDoorVillageRadius of a village well
// (a populated one from the mob store, or the one generation places in the
// village cells around), and that no wall holds on any side (doorHeld).
func loneVillageDoors(w *world.World, wells [][3]int) []blockPos {
	var edited []blockPos
	w.ForEachEdit(func(x, y, z int, state uint32) { // collected first: At takes the lock ForEachEdit holds
		if !worldgen.IsDoor(state) {
			return
		}
		if info, _ := worldgen.InfoForState(state); worldgen.GetProperty(info, state, "half") == "upper" {
			y--
		}
		edited = append(edited, blockPos{x, y, z})
	})
	gen := w.Gen()
	near := func(x, z int) bool {
		r2 := loneDoorVillageRadius * loneDoorVillageRadius
		for _, v := range wells {
			if dx, dz := v[0]-x, v[2]-z; dx*dx+dz*dz <= r2 {
				return true
			}
		}
		// Village cells are wider than the radius: the corners of the box
		// around the door reach every cell a well in range can lie in.
		for _, d := range [][2]int{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}} {
			v := gen.VillageIn(x+d[0]*loneDoorVillageRadius, z+d[1]*loneDoorVillageRadius)
			if dx, dz := v.X-x, v.Z-z; v.Exists && dx*dx+dz*dz <= r2 {
				return true
			}
		}
		return false
	}
	seen := map[blockPos]bool{}
	var out []blockPos
	for _, lo := range edited {
		if seen[lo] {
			continue
		}
		seen[lo] = true
		if worldgen.IsDoor(w.GeneratedAt(lo.x, lo.y, lo.z)) || worldgen.IsDoor(w.GeneratedAt(lo.x, lo.y+1, lo.z)) {
			continue
		}
		if !near(lo.x, lo.z) || doorHeld(w, lo) {
			continue
		}
		out = append(out, lo)
	}
	return out
}

// doorHeld reports a full solid block (worldgen.IsSolidFull: stone, planks,
// dirt, a log) beside either half of the door whose lower half is at p — the
// wall a door is set in. Leaves, fences and another door do not count: a
// remnant stands among them as often as not.
func doorHeld(w *world.World, p blockPos) bool {
	for _, y := range []int{p.y, p.y + 1} {
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if worldgen.IsSolidFull(w.At(p.x+d[0], y, p.z+d[1])) {
				return true
			}
		}
	}
	return false
}
