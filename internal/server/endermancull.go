package server

import (
	"log"
	"os"
	"path/filepath"
)

// Bug #45: endermen multiplied through the save/reload copies that
// mobchunks.go now prevents, and a saved copy cannot be told from its
// original (a saved mob has no lasting identity). Once, the overworld's saved
// endermen are cleared, and the natural spawner refills them at vanilla's
// rate. The store is copied aside first.

// endermanCullMarker records that the one-time cull has run.
const endermanCullMarker = ".cull-endermen-1"

// cullEndermenOnce removes every saved overworld enderman, once.
func (s *Server) cullEndermenOnce() {
	if s.MobFile == "" || s.hub.mobstore == nil {
		return
	}
	marker := filepath.Join(filepath.Dir(s.MobFile), endermanCullMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if _, err := os.Stat(s.MobFile); err == nil {
		if err := copyFile(s.MobFile, s.MobFile+".pre-enderman-cull"); err != nil {
			log.Printf("enderman cull: backing up %s: %v (skipped)", s.MobFile, err)
			return
		}
	}
	before, removed := s.hub.mobstore.removeSpecies(entityEnderman, dimOverworld)
	s.hub.mobstore.flush()
	if err := writeAtomic(marker, []byte("done\n")); err != nil {
		log.Printf("enderman cull: writing marker: %v", err)
	}
	log.Printf("enderman cull: removed %d saved overworld endermen of %d saved mobs", removed, before)
}

// removeSpecies drops every saved mob of one species in one dimension.
func (s *mobStore) removeSpecies(etype, dim int) (before, removed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, bucket := range s.m.Chunks {
		before += len(bucket)
		out := bucket[:0:0]
		for _, m := range bucket {
			if m.Etype == etype && m.Dim == dim {
				removed++
				continue
			}
			out = append(out, m)
		}
		if len(out) == 0 {
			delete(s.m.Chunks, key)
		} else {
			s.m.Chunks[key] = out
		}
	}
	return before, removed
}
