package server

import (
	"iter"
	"path/filepath"
	"slices"

	"github.com/tachyne/tachyne-world/internal/world"
)

// dimensions holds the worlds a server runs beyond the overworld, by
// dimension id (world.Dimensions). The overworld stays the owner's world
// field, which everything reaches directly; this is where the rest live, so
// "the world for id" and "every dimension" are a lookup and a loop over the
// table instead of a named field per dimension.
type dimensions struct {
	worlds []*world.World // by id; nil = not run here (the overworld slot is unused)
	// standIn answers for a known dimension that has no world of its own. Only
	// tests set it (newTestHub): one fixture world then serves a player in any
	// dimension, while has() still reports the dimension as absent, so the
	// systems that start when one exists (the dragon fight, portal travel)
	// stay off. Production registers every dimension and leaves it nil.
	standIn *world.World
}

// set registers w as dimension id's world (nil removes it).
func (d *dimensions) set(id int, w *world.World) {
	if world.Dimension(id) == nil || id == dimOverworld {
		panic("dimensions.set: not a registrable dimension")
	}
	for len(d.worlds) <= id {
		d.worlds = append(d.worlds, nil)
	}
	d.worlds[id] = w
}

// has reports whether dimension id has a world of its own.
func (d *dimensions) has(id int) bool {
	return id > dimOverworld && id < len(d.worlds) && d.worlds[id] != nil
}

// get is dimension id's world: its own, else the stand-in for a known id,
// else nil. An id outside the table is always nil.
func (d *dimensions) get(id int) *world.World {
	if d.has(id) {
		return d.worlds[id]
	}
	if world.Dimension(id) != nil {
		return d.standIn
	}
	return nil
}

// worldIn is the world for a dimension id given the owner's overworld: nil
// for a dimension this server does not run.
func (d *dimensions) worldIn(overworld *world.World, id int) *world.World {
	if id == dimOverworld {
		return overworld
	}
	return d.get(id)
}

// all yields every dimension with a world of its own, in id order, the
// overworld first. A world registered under two ids (a test aliasing the
// overworld) comes once, under its first.
func (d *dimensions) all(overworld *world.World) iter.Seq2[int, *world.World] {
	return func(yield func(int, *world.World) bool) {
		var seen []*world.World
		for i := range world.Dimensions {
			w := overworld
			if i != dimOverworld {
				if !d.has(i) {
					continue
				}
				w = d.worlds[i]
			}
			if w == nil || slices.Contains(seen, w) {
				continue
			}
			seen = append(seen, w)
			if !yield(i, w) {
				return
			}
		}
	}
}

// dimType is dimension id's descriptor (nil outside the table).
func dimType(id int) *world.DimensionType { return world.Dimension(id) }

// allDims ranges over the hub's worlds (dimensions.all).
func (h *hub) allDims() iter.Seq2[int, *world.World] { return h.dims.all(h.world) }

// allDims ranges over the server's worlds (dimensions.all).
func (s *Server) allDims() iter.Seq2[int, *world.World] { return s.dims.all(s.world) }

// dimWorld is the world for a dimension id (nil: not run here).
func (s *Server) dimWorld(id int) *world.World { return s.dims.worldIn(s.world, id) }

// hasDim reports whether the hub runs dimension id (the overworld always).
func (h *hub) hasDim(id int) bool { return id == dimOverworld || h.dims.has(id) }

// dimFile is where dimension id's edits persist: the -world file for the
// overworld, the table's file name beside it for the rest ("" when the
// server keeps no files).
func (s *Server) dimFile(id int) string {
	if s.WorldFile == "" || id == dimOverworld {
		return s.WorldFile
	}
	return filepath.Join(filepath.Dir(s.WorldFile), dimType(id).File)
}

// dropUnrunFurniture forgets saved paintings, item frames and armour stands
// in a dimension this hub does not run (a save from a server that ran more):
// nothing could see or touch them, and their world is nil.
func (h *hub) dropUnrunFurniture() {
	for eid, p := range h.paintings {
		if h.worldFor(p.dim) == nil {
			delete(h.paintings, eid)
		}
	}
	for eid, f := range h.itemFrames {
		if h.worldFor(f.dim) == nil {
			delete(h.itemFrames, eid)
		}
	}
	for eid, st := range h.armorStands {
		if h.worldFor(st.dim) == nil {
			delete(h.armorStands, eid)
		}
	}
	for eid, c := range h.cushions {
		if h.worldFor(c.dim) == nil {
			delete(h.cushions, eid)
		}
	}
}
