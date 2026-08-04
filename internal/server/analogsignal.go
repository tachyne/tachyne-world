package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// What a comparator reads out of the block behind it.
//
// Containers are generic — `containerSignal` measures fullness for all of them
// at once — but vanilla gives a dozen blocks that are not containers their own
// reading, each a different rule: how much cake is left, how full a composter
// is, whether an end portal frame has its eye. Those all live here, in one
// place, so a comparator gains a reading by adding a case rather than by
// finding whichever redstone branch happens to run first.

var (
	respawnAnchorBase = worldgen.BlockBase("respawn_anchor")   // charges 0..4
	endPortalFrameBse = worldgen.BlockBase("end_portal_frame") // eye(2) x facing(4)
	detectorRailBase  = worldgen.BlockBase("detector_rail")    // powered(2) x shape(6) x waterlogged(2)
)

// analogSignal is everything a comparator can read at a position: the generic
// container fullness first, then the per-block readings. -1 means the block
// says nothing, which is what lets the caller fall through to its other cases.
func (h *hub) analogSignal(pos simPos) int {
	if sig := h.containerSignal(pos); sig >= 0 {
		return sig
	}
	w := h.worldFor(pos.dim)
	if w == nil {
		return -1
	}
	st := w.At(pos.x, pos.y, pos.z)
	if bites, ok := cakeBites(st); ok {
		return cakeSignal(bites)
	}
	if level, ok := composterLevel(st); ok {
		return level
	}
	if _, level, ok := cauldronOf(st); ok {
		return level // empty 0, water/snow 1-3, lava 3
	}
	if isBeeHome(st) {
		return honeyLevel(st)
	}
	if st >= respawnAnchorBase && st <= respawnAnchorBase+4 {
		// vanilla scales the charge to the signal range: floor(charge/4 * 15).
		return int(st-respawnAnchorBase) * 15 / 4
	}
	if st >= endPortalFrameBse && st < endPortalFrameBse+8 {
		if (st-endPortalFrameBse)/4 == 0 { // eye is the first property, true first
			return 15
		}
		return 0
	}
	if st >= detectorRailBase && st < detectorRailBase+24 {
		if (st-detectorRailBase)/12 == 0 { // powered, true first
			return 15
		}
		return 0
	}
	if isJukebox(st) {
		if jb := h.jukeboxes[pos]; jb != nil && jb.disc.item != 0 {
			return jukeboxSignal(jb.disc.item)
		}
		return 0
	}
	return -1
}
