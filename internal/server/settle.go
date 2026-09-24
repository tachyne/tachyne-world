package server

// settleChunkEdits reconciles a chunk's saved cave growths with the ground
// they now stand on, when the chunk comes into range. The world stores only
// what changed; everything else is generated. When the generator changes — a
// new biome, a new cave shape — the untouched terrain regenerates while the
// edits stay, and a growth can be left where vanilla could never have kept
// it: a stalagmite that grew on a floor the new generator no longer lays
// (bug #27), a cave vine whose ceiling moved. Vanilla would have broken each
// the moment its support went (updateShape → canSurvive), so each gets the
// same knock-down a player's edit gives (dropUnsupported: a stalactite falls).
//
// Only the blocks that grow on their own in caves, where a regenerated cave
// is the one way to lose support unseen. A dry run over the live world's
// 1.3 million edits found them — and also snow, crops, lanterns, doors and
// torches that the support check would have knocked down, some of them
// players' builds: those are not this pass's to judge.
func (h *hub) settleChunkEdits(players map[int32]*tracked, dim int, cx, cz int32) {
	w := h.worldFor(dim)
	if w == nil {
		return
	}
	for _, e := range w.EditedBlocks(cx, cz) {
		if !isCaveGrowth(e.State) {
			continue
		}
		pos := blockPos{int(cx)*16 + e.LX, e.Y, int(cz)*16 + e.LZ}
		st := w.At(pos.x, pos.y, pos.z)
		if st != e.State || supported(w, pos, st) {
			continue
		}
		// dropUnsupported checks the neighbours of the cell it is given.
		h.dropUnsupported(players, dim, blockPos{pos.x, pos.y + 1, pos.z})
	}
}

// isCaveGrowth reports the blocks a cave grows by itself: pointed dripstone
// and cave vines.
func isCaveGrowth(st uint32) bool {
	if _, _, _, ok := dripstoneParts(st); ok {
		return true
	}
	return (st >= caveVinesLo && st <= caveVinesHi) || (st >= caveVinesPlantLo && st <= caveVinesPlantHi)
}
