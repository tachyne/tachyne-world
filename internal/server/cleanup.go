package server

import (
	"log"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// One-time maintenance to clean up the debris a since-suppressed structure left
// behind: stranded mobs in the mob store and the block EDITS its simulation /
// interaction wrote to world.gob (crops that grew, doors villagers opened). Run
// behind -cleanup-village, then remove the flag.

type stRange struct{ lo, hi uint32 }

func brange(names ...string) []stRange {
	var out []stRange
	for _, n := range names {
		func() {
			defer func() { recover() }() // skip a name absent in this version
			lo, hi := worldgen.BlockRange(n)
			out = append(out, stRange{lo, hi})
		}()
	}
	return out
}

func inSt(s uint32, rs []stRange) bool {
	for _, r := range rs {
		if s >= r.lo && s <= r.hi {
			return true
		}
	}
	return false
}

// cleanupSpawnVillage clears the suppressed spawn village's stranded
// villagers/golems/cats and its crop/farm/door edits, WITHOUT touching a nearby
// castle: unambiguous farm blocks (a castle has no wheat) revert within a wide
// radius; doors/beds revert only when "stranded" (no adjacent wall — a walled
// castle door is left alone).
func (s *Server) cleanupSpawnVillage(wx, wz int) {
	mobs := s.hub.mobstore.removeNear(wx, wz, 160, entityVillager, entityIronGolem, entityCat)
	s.hub.mobstore.flush()

	farm := brange("wheat", "carrots", "potatoes", "beetroots", "melon", "pumpkin",
		"pumpkin_stem", "melon_stem", "attached_pumpkin_stem", "attached_melon_stem",
		"farmland", "dirt_path", "hay_block", "composter", "bell")
	stranded := brange(
		"oak_door", "spruce_door", "birch_door", "jungle_door", "acacia_door",
		"dark_oak_door", "mangrove_door", "cherry_door", "bamboo_door", "iron_door",
		"white_bed", "orange_bed", "magenta_bed", "light_blue_bed", "yellow_bed",
		"lime_bed", "pink_bed", "gray_bed", "light_gray_bed", "cyan_bed", "purple_bed",
		"blue_bed", "brown_bed", "green_bed", "red_bed", "black_bed",
		"ladder", "lantern", "torch", "wall_torch")

	reverted := 0
	for cx := int32((wx - 80) >> 4); cx <= int32((wx+80)>>4); cx++ {
		for cz := int32((wz - 80) >> 4); cz <= int32((wz+80)>>4); cz++ {
			for _, eb := range s.hub.world.EditedBlocks(cx, cz) {
				bx, bz := int(cx)*16+eb.LX, int(cz)*16+eb.LZ
				dx, dz := bx-wx, bz-wz
				d2 := dx*dx + dz*dz
				switch {
				case inSt(eb.State, farm) && d2 <= 80*80:
					s.hub.world.RevertEdit(bx, eb.Y, bz)
					reverted++
				case inSt(eb.State, stranded) && d2 <= 48*48 && s.strandedBlock(bx, eb.Y, bz):
					s.hub.world.RevertEdit(bx, eb.Y, bz)
					reverted++
				}
			}
		}
	}
	if err := s.hub.world.Save(); err != nil {
		log.Printf("cleanup: world save failed: %v", err)
	}
	log.Printf("cleanup: removed %d spawn-village mobs, reverted %d debris edits near (%d,%d)",
		mobs, reverted, wx, wz)
}

// strandedBlock reports whether a cell has no solid full block on its four
// horizontal sides — i.e. it is not embedded in a wall (a village-remnant door
// stands alone; a castle door does not).
func (s *Server) strandedBlock(x, y, z int) bool {
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if worldgen.IsSolidFull(s.hub.world.At(x+d[0], y, z+d[1])) {
			return false
		}
	}
	return true
}
