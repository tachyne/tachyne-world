package server

import (
	"math/rand"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Block.getStateForPlacement overrides that read the world around the target
// cell rather than the clicked face: the block a player's item becomes can
// depend on what is above, below or beside it. Each returns ok=false where
// vanilla returns null and the placement is refused.

// crafterPlacedState is CrafterBlock.getStateForPlacement: the front faces
// back along the nearest looking direction (toward the player), and the top
// is UP for a horizontal front; a front pointing down takes its top from the
// player's horizontal facing reversed, a front pointing up from the facing
// itself. TRIGGERED is left to the redstone update the placement raises.
func crafterPlacedState(def uint32, yaw, pitch float32) uint32 {
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return def
	}
	front := oppositeFace6(nearestLookingDirection(yaw, pitch))
	top := "up"
	switch front {
	case "down":
		top = oppositeFacing(playerFacing(yaw))
	case "up":
		top = playerFacing(yaw)
	}
	return worldgen.SetProperty(info, def, "orientation", front+"_"+top)
}

// bambooPlacedState is BambooStalkBlock.getStateForPlacement: never into a
// fluid; on a sapling a fresh age-0 stalk, on a stalk one matching its
// thickness (age), and on bare soil a sapling — unless a stalk hangs just
// above, which the new segment joins at that stalk's age. Soil that cannot
// carry bamboo is refused by the canSurvive check that follows.
func bambooPlacedState(w *world.World, pos blockPos, target uint32) (uint32, bool) {
	if worldgen.HoldsWater(target) || worldgen.IsLava(target) {
		return 0, false
	}
	below := w.At(pos.x, pos.y-1, pos.z)
	switch {
	case below == bambooSapling:
		return bambooState(0, bambooLeavesNone, 0), true
	case isBamboo(below):
		return bambooState(bambooAge(below), bambooLeavesNone, 0), true
	}
	if above := w.At(pos.x, pos.y+1, pos.z); isBamboo(above) {
		return bambooState(bambooAge(above), bambooLeavesNone, 0), true
	}
	return bambooSapling, true
}

// dirtPathPlacedState is DirtPathBlock.getStateForPlacement: a path that could
// not survive where it is placed — a solid block above it — goes down as
// plain dirt instead.
func dirtPathPlacedState(w *world.World, pos blockPos, def uint32) uint32 {
	if coversSoil(w.At(pos.x, pos.y+1, pos.z)) {
		return worldgen.Dirt
	}
	return def
}

// growingPlantPlacedState is GrowingPlantBlock.getStateForPlacement for kelp,
// weeping, twisting and cave vines: when the cell the plant grows into already
// holds this plant, the new block is a length of BODY; otherwise it is a head
// with a random AGE 0-24 (GrowingPlantHeadBlock). KelpBlock adds that kelp only
// goes into a full water source.
func growingPlantPlacedState(w *world.World, pos blockPos, g growingPlant, target uint32) (uint32, bool) {
	if g.intoWater && !worldgen.IsFluidSource(target, worldgen.WaterBase) {
		return 0, false
	}
	next := w.At(pos.x, pos.y+g.dy, pos.z)
	if (next >= g.headLo && next <= g.headHi) || sameBlockFamily(next, g.body) {
		return growingPlantBody(g, 0), true
	}
	return g.headAt(rand.Intn(growingPlantMaxAge), false), true
}

// growingPlantBody is the plant's body block in its default state, carrying
// the berries of the head it replaces (CaveVinesBlock.
// updateBodyAfterConvertedFromHead); head=0 means a fresh body.
func growingPlantBody(g growingPlant, head uint32) uint32 {
	body := g.body
	info, ok := worldgen.InfoForState(body)
	if !ok || !info.HasProperty("berries") {
		return body
	}
	berries := "false"
	if head != 0 {
		if hi, ok := worldgen.InfoForState(head); ok && worldgen.GetProperty(hi, head, "berries") == "true" {
			berries = "true"
		}
	}
	return worldgen.SetProperty(info, body, "berries", berries)
}

// growingPlantAnchorBody is GrowingPlantHeadBlock.updateShape as a placement
// sees it: the head the new block was set onto now has plant in its growth
// direction, so it becomes a length of body. ok=false leaves the anchor as it
// is.
func growingPlantAnchorBody(w *world.World, pos blockPos, placed uint32) (blockPos, uint32, bool) {
	g, ok := growingPlantOf(placed)
	if !ok {
		if g, ok = growingPlantOfBody(placed); !ok {
			return blockPos{}, 0, false
		}
	}
	at := blockPos{pos.x, pos.y - g.dy, pos.z}
	anchor := w.At(at.x, at.y, at.z)
	if anchor < g.headLo || anchor > g.headHi {
		return blockPos{}, 0, false
	}
	return at, growingPlantBody(g, anchor), true
}

// growingPlantOfBody finds the plant a body block belongs to.
func growingPlantOfBody(state uint32) (growingPlant, bool) {
	for _, g := range growingPlants {
		if sameBlockFamily(state, g.body) {
			return g, true
		}
	}
	return growingPlant{}, false
}

var hugeMushroomBlocks = [...]uint32{
	worldgen.BlockBase("brown_mushroom_block"),
	worldgen.BlockBase("red_mushroom_block"),
	worldgen.BlockBase("mushroom_stem"),
}

func isHugeMushroom(s uint32) bool {
	for _, b := range hugeMushroomBlocks {
		if sameBlockFamily(s, b) {
			return true
		}
	}
	return false
}

// hugeMushroomPlacedState is HugeMushroomBlock.getStateForPlacement: every
// face shows its skin except where a block of the same kind sits against it.
func hugeMushroomPlacedState(w *world.World, pos blockPos, def uint32) uint32 {
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return def
	}
	for _, f := range faceDirs {
		v := "true"
		if sameBlockFamily(w.At(pos.x+f.d[0], pos.y+f.d[1], pos.z+f.d[2]), def) {
			v = "false"
		}
		def = worldgen.SetProperty(info, def, f.prop, v)
	}
	return def
}

func isPaleMossCarpet(s uint32) bool { return inRange(s, paleCarpetRange) }

// paleCarpetSides is Direction.Plane.HORIZONTAL order, which the side walk in
// MossyCarpetBlock.getUpdatedState follows.
var paleCarpetSides = [4]struct {
	prop   string
	dx, dz int
}{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}}

// mossCarpetUpdated is MossyCarpetBlock.getUpdatedState: a side is LOW where
// the wall beside it can hold the carpet (a new or base layer creates it,
// anything else keeps what it had), TALL where the layer above grows on that
// side too, and never on an upper layer whose carpet below has no such side.
func mossCarpetUpdated(w *world.World, pos blockPos, state uint32, createSides bool) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	base := worldgen.GetProperty(info, state, "bottom") == "true"
	createSides = createSides || base
	above := w.At(pos.x, pos.y+1, pos.z)
	below := w.At(pos.x, pos.y-1, pos.z)
	for _, f := range paleCarpetSides {
		side := "none"
		if holdsBlock(w.At(pos.x+f.dx, pos.y, pos.z+f.dz)) {
			side = worldgen.GetProperty(info, state, f.prop)
			if createSides {
				side = "low"
			}
		}
		if side == "low" {
			if isPaleMossCarpet(above) && worldgen.GetProperty(info, above, f.prop) != "none" &&
				worldgen.GetProperty(info, above, "bottom") != "true" {
				side = "tall"
			}
			if !base && isPaleMossCarpet(below) && worldgen.GetProperty(info, below, f.prop) == "none" {
				side = "none"
			}
		}
		state = worldgen.SetProperty(info, state, f.prop, side)
	}
	return state
}

// mossCarpetTopper is MossyCarpetBlock.createTopperWithSideChance as
// setPlacedBy runs it: the cell above a placed carpet (replaceable, or an
// upper layer) gets a base-less layer on the sides the carpet below climbs,
// each kept on a coin flip. ok=false when no side survives or nothing
// changes.
func mossCarpetTopper(w *world.World, pos blockPos) (uint32, bool) {
	prev := w.At(pos.x, pos.y+1, pos.z)
	info, ok := worldgen.InfoForState(worldgen.BlockID("pale_moss_carpet"))
	if !ok {
		return 0, false
	}
	prevCarpet := isPaleMossCarpet(prev)
	if prevCarpet && worldgen.GetProperty(info, prev, "bottom") == "true" {
		return 0, false
	}
	if !prevCarpet && !worldgen.IsReplaceable(prev) {
		return 0, false
	}
	top := worldgen.SetProperty(info, worldgen.BlockID("pale_moss_carpet"), "bottom", "false")
	top = mossCarpetUpdated(w, blockPos{pos.x, pos.y + 1, pos.z}, top, true)
	any := false
	for _, f := range paleCarpetSides {
		if worldgen.GetProperty(info, top, f.prop) == "none" {
			continue
		}
		if rand.Intn(2) == 0 {
			top = worldgen.SetProperty(info, top, f.prop, "none")
			continue
		}
		any = true
	}
	if !any || top == prev {
		return 0, false
	}
	return top, true
}
