package server

import (
	"math"
	"strconv"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Placement rules vanilla writes as "for each of the player's nearest
// looking directions" — torches, lanterns, vines, cocoa — live here. They
// consult the look vector, not the clicked face: a torch clicked onto a wall
// while looking down goes on the floor if there is one, and a vine clicked
// onto the top of a block hangs from whichever wall the player faces.

// lookOrder is Direction.orderedByNearest: the six faces sorted by how
// closely the look vector points along each, in wire face order (0 down,
// 1 up, 2 north, 3 south, 4 west, 5 east). Ties fall the way vanilla's
// strict comparisons fall.
func lookOrder(yaw, pitch float32) [6]int32 {
	p := float64(pitch) * math.Pi / 180
	y := -float64(yaw) * math.Pi / 180
	ps, pc := math.Sin(p), math.Cos(p)
	ys, yc := math.Sin(y), math.Cos(y)
	xPos, yPos, zPos := ys > 0, ps < 0, yc > 0
	xYaw, yMag, zYaw := math.Abs(ys), math.Abs(ps), math.Abs(yc)
	xMag, zMag := xYaw*pc, zYaw*pc
	ax, ay, az := int32(4), int32(0), int32(2)
	if xPos {
		ax = 5
	}
	if yPos {
		ay = 1
	}
	if zPos {
		az = 3
	}
	var a, b, c int32
	switch {
	case xYaw > zYaw && yMag > xMag:
		a, b, c = ay, ax, az
	case xYaw > zYaw && zMag > yMag:
		a, b, c = ax, az, ay
	case xYaw > zYaw:
		a, b, c = ax, ay, az
	case yMag > zMag:
		a, b, c = ay, az, ax
	case xMag > yMag:
		a, b, c = az, ax, ay
	default:
		a, b, c = az, ay, ax
	}
	return [6]int32{a, b, c, oppositeDir(c), oppositeDir(b), oppositeDir(a)}
}

// oppositeDir flips a wire face index (down↔up, north↔south, west↔east).
func oppositeDir(d int32) int32 { return d ^ 1 }

// placeOrder is BlockPlaceContext.getNearestLookingDirections: the look
// order with the OPPOSITE of the clicked face moved to the FRONT — the face
// you highlighted is the one a torch, lever or button attaches to, whatever
// else the look vector happens to favour. Only a click that replaced the
// block it landed on (into tall grass, into snow) falls back to the plain
// look order, exactly as vanilla's replaceClicked does.
func placeOrder(face int32, replacingClicked bool, yaw, pitch float32) [6]int32 {
	order := lookOrder(yaw, pitch)
	if replacingClicked || face < 0 || face > 5 {
		return order
	}
	want := oppositeDir(face)
	at := -1
	for i, d := range order {
		if d == want {
			at = i
			break
		}
	}
	if at <= 0 {
		return order
	}
	copy(order[1:at+1], order[0:at])
	order[0] = want
	return order
}

// placeAlias maps items whose block carries a different name — the item
// table pairs items with same-named blocks only. Seeds go through
// cropForSeed (they need soil rules); these need no more than the alias.
var placeAlias = func() map[int32]uint32 {
	m := map[int32]uint32{}
	add := func(item, block string) {
		id := itemByName[item]
		if _, _, ok := worldgen.BlockRangeOK(block); id != 0 && ok {
			m[id] = worldgen.BlockID(block)
		}
	}
	add("redstone", "redstone_wire")
	add("cocoa_beans", "cocoa")
	add("glow_berries", "cave_vines")
	return m
}()

// standingWallVariant pairs a StandingAndWallBlockItem's floor block with
// its wall block (attachment DOWN): the torch family and the coral fans.
// Signs, banners and heads keep their own placers (16-way rotation).
var standingWallVariant = func() map[uint32]uint32 {
	m := map[uint32]uint32{}
	add := func(standing, wall string) {
		if _, _, ok := worldgen.BlockRangeOK(standing); !ok {
			return
		}
		if _, _, ok := worldgen.BlockRangeOK(wall); !ok {
			return
		}
		m[worldgen.BlockID(standing)] = worldgen.BlockID(wall)
	}
	add("torch", "wall_torch")
	add("soul_torch", "soul_wall_torch")
	add("redstone_torch", "redstone_wall_torch")
	add("copper_torch", "copper_wall_torch")
	for _, c := range []string{"tube", "brain", "bubble", "fire", "horn"} {
		add(c+"_coral_fan", c+"_coral_wall_fan")
		add("dead_"+c+"_coral_fan", "dead_"+c+"_coral_wall_fan")
	}
	return m
}()

// standingOrWallState is StandingAndWallBlockItem.getPlacementState for a
// DOWN attachment: the wall state is the first look-order side whose wall
// holds (WallTorchBlock.getStateForPlacement, facing away from that wall);
// then the look order decides — down onto a holding floor gives the standing
// block, a sideways entry gives that wall state, up is never an option.
func standingOrWallState(w *world.World, pos blockPos, standing, wall uint32, order [6]int32) (uint32, bool) {
	winfo, ok := worldgen.InfoForState(wall)
	if !ok {
		return 0, false
	}
	wallState, haveWall := uint32(0), false
	for _, d := range order {
		if d < 2 {
			continue
		}
		st := worldgen.SetProperty(winfo, wall, "facing", faceName(oppositeDir(d)))
		if supported(w, pos, st) {
			wallState, haveWall = st, true
			break
		}
	}
	for _, d := range order {
		switch {
		case d == 1:
			continue
		case d == 0:
			if supported(w, pos, standing) {
				return standing, true
			}
		case haveWall:
			return wallState, true
		}
	}
	return 0, false
}

// isHangable reports a lantern-like block: hangs from a ceiling or stands
// on a floor by its own "hanging" property (LanternBlock).
func isHangable(def uint32) bool {
	info, ok := worldgen.InfoForState(def)
	return ok && info.HasProperty("hanging") && worldgen.SupportFor(def) == worldgen.SupportHangable
}

// hangableState is LanternBlock.getStateForPlacement: the first vertical
// look-order direction whose attachment holds — up means hanging.
func hangableState(w *world.World, pos blockPos, def uint32, order [6]int32) (uint32, bool) {
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return 0, false
	}
	for _, d := range order {
		if d > 1 {
			continue
		}
		hanging := "false"
		if d == 1 {
			hanging = "true"
		}
		st := worldgen.SetProperty(info, def, "hanging", hanging)
		if supported(w, pos, st) {
			return st, true
		}
	}
	return 0, false
}

var cocoaStates = func() stateRange {
	lo, hi, _ := worldgen.BlockRangeOK("cocoa")
	return stateRange{lo, hi}
}()

func isCocoa(s uint32) bool { return cocoaStates.hi > 0 && inStates(s, cocoaStates) }

// cocoaSupports is #supports_cocoa: the jungle logs and woods.
var cocoaSupports = blockRange("jungle_log", "jungle_wood", "stripped_jungle_log", "stripped_jungle_wood")

// cocoaState is CocoaBlock.getStateForPlacement: facing the first horizontal
// look-order direction with a jungle log there (the pod faces its log).
func cocoaState(w *world.World, pos blockPos, def uint32, order [6]int32) (uint32, bool) {
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return 0, false
	}
	for _, d := range order {
		if d < 2 {
			continue
		}
		st := worldgen.SetProperty(info, def, "facing", faceName(d))
		if supported(w, pos, st) {
			return st, true
		}
	}
	return 0, false
}

// multifaceCanAttach is MultifaceBlock.canAttachTo / VineBlock.canSupportAtFace
// for the face toward wire direction d: the neighbour there holds a block;
// a vine's side face may instead hang from the vine above carrying that
// same face, and a vine never attaches downward.
func multifaceCanAttach(w *world.World, pos blockPos, d int32, vine bool) bool {
	if vine && d == 0 {
		return false
	}
	f := faceDirs[d]
	if holdsBlock(w.At(pos.x+f.d[0], pos.y+f.d[1], pos.z+f.d[2])) {
		return true
	}
	if vine && d >= 2 {
		above := w.At(pos.x, pos.y+1, pos.z)
		if isVineBlock(above) {
			if ai, ok := worldgen.InfoForState(above); ok {
				return worldgen.GetProperty(ai, above, f.prop) == "true"
			}
		}
	}
	return false
}

// multifaceFull reports every face of a multiface block set — the point at
// which vanilla stops letting the same item join it (canBeReplaced).
func multifaceFull(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	for _, f := range faceDirs {
		if info.HasProperty(f.prop) && worldgen.GetProperty(info, state, f.prop) != "true" {
			return false
		}
	}
	return true
}

// multifacePlacement is MultifaceBlock.getStateForPlacement (and VineBlock's):
// the first look-order face whose neighbour can hold it joins the existing
// block of the same kind at the target, or starts a fresh one. No holdable
// face means no placement — never a face out into the air.
func multifacePlacement(w *world.World, pos blockPos, def, existing uint32, order [6]int32) (uint32, bool) {
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return 0, false
	}
	// MultifaceBlock.getDefaultMultifaceState: a fresh one has NO face set.
	// worldgen.BlockBase is property index 0 throughout, and index 0 of a
	// boolean is true — so the "default" glow lichen, vine, sculk vein and
	// resin clump all arrive with every face already on, and adding one more
	// left a full cube of lichen with its unsupported sides hanging in the
	// air. Legion reported exactly that.
	state := multifaceCleared(info, def)
	joined := sameBlockFamily(existing, def)
	if joined {
		state = existing
	}
	vine := isVineBlock(def)
	for _, d := range order {
		f := faceDirs[d]
		if !info.HasProperty(f.prop) {
			continue
		}
		if joined && worldgen.GetProperty(info, state, f.prop) == "true" {
			continue
		}
		if multifaceCanAttach(w, pos, d, vine) {
			return worldgen.SetProperty(info, state, f.prop, "true"), true
		}
	}
	return 0, false
}

// faceAttachedState is FaceAttachedHorizontalDirectionalBlock.getStateForPlacement
// (levers, buttons, grindstones): the first look-order direction whose
// attachment holds — floor or ceiling with the block facing the way the
// player faces, or a wall with the block facing away from it.
func faceAttachedState(w *world.World, pos blockPos, def uint32, order [6]int32, yaw float32) (uint32, bool) {
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return 0, false
	}
	for _, d := range order {
		var st uint32
		switch d {
		case 0:
			st = worldgen.SetProperty(info, worldgen.SetProperty(info, def, "face", "floor"), "facing", playerFacing(yaw))
		case 1:
			st = worldgen.SetProperty(info, worldgen.SetProperty(info, def, "face", "ceiling"), "facing", playerFacing(yaw))
		default:
			st = worldgen.SetProperty(info, worldgen.SetProperty(info, def, "face", "wall"), "facing", faceName(oppositeDir(d)))
		}
		if supported(w, pos, st) {
			return st, true
		}
	}
	return 0, false
}

// isFaceAttached reports a block placed by its "face" property (levers,
// buttons, grindstones).
func isFaceAttached(def uint32) bool {
	info, ok := worldgen.InfoForState(def)
	return ok && info.HasProperty("face") && info.HasProperty("facing")
}

// hopperFacing is HopperBlock.getStateForPlacement: the spout points into
// the clicked block, or down when placed on a floor or ceiling.
func hopperFacing(dir int32) string {
	if dir < 2 {
		return "down"
	}
	return faceName(oppositeDir(dir))
}

// stackProperty names the counted property a block of the held item's kind
// stacks into (candles, sea pickles, turtle eggs, snow layers), with its cap.
func stackProperty(info worldgen.BlockInfo) (string, int) {
	for _, p := range []struct {
		name string
		max  int
	}{{"candles", 4}, {"pickles", 4}, {"eggs", 4}, {"layers", 8}} {
		if info.HasProperty(p.name) {
			return p.name, p.max
		}
	}
	return "", 0
}

// isSlab reports a slab state (type + waterlogged, no facing).
func isSlab(info worldgen.BlockInfo) bool {
	return info.HasProperty("type") && info.HasProperty("waterlogged") && !info.HasProperty("facing")
}

// blockReplaceableBy is the per-block canBeReplaced override that lets the
// held item's own block stack into a standing one: a slab doubles when the
// click lands on its open half, candles, sea pickles and turtle eggs count
// up (not while sneaking), snow piles a layer (from the top when clicked).
// `clicked` says the target IS the clicked block (replacingClickedOnBlock).
func blockReplaceableBy(target, def uint32, dir int32, cursorY float32, clicked, sneaking bool) bool {
	if !sameBlockFamily(target, def) {
		return false
	}
	info, ok := worldgen.InfoForState(target)
	if !ok {
		return false
	}
	if isSlab(info) {
		switch worldgen.GetProperty(info, target, "type") {
		case "double":
			return false
		case "bottom":
			return !clicked || dir == 1 || (cursorY > 0.5 && dir >= 2)
		default:
			return !clicked || dir == 0 || (cursorY <= 0.5 && dir >= 2)
		}
	}
	if name, max := stackProperty(info); name != "" {
		n := atoi(worldgen.GetProperty(info, target, name))
		if n >= max {
			return false
		}
		if name == "layers" {
			return !clicked || dir == 1
		}
		return !sneaking
	}
	return false
}

// stackedState is the state a held item's block leaves when it stacks into
// the standing one: a double slab (never waterlogged), or one more candle,
// pickle, egg or layer.
func stackedState(target uint32) (uint32, bool) {
	info, ok := worldgen.InfoForState(target)
	if !ok {
		return 0, false
	}
	if isSlab(info) {
		st := worldgen.SetProperty(info, target, "type", "double")
		return worldgen.SetProperty(info, st, "waterlogged", "false"), true
	}
	if name, max := stackProperty(info); name != "" {
		n := atoi(worldgen.GetProperty(info, target, name))
		if n >= max {
			return 0, false
		}
		return worldgen.SetProperty(info, target, name, strconv.Itoa(n+1)), true
	}
	return 0, false
}

// multifaceCleared turns every face of a multiface block off, which is what
// vanilla's default state is.
func multifaceCleared(info worldgen.BlockInfo, def uint32) uint32 {
	for _, f := range faceDirs {
		if info.HasProperty(f.prop) {
			def = worldgen.SetProperty(info, def, f.prop, "false")
		}
	}
	return def
}

// multifaceBase is the cleared default state of a multiface block by name —
// what vanilla's getDefaultMultifaceState gives, with every face off. Use it
// anywhere a multiface block is built from scratch: worldgen.BlockBase is
// property index 0, and index 0 of a boolean is TRUE, so the raw base is a
// full cube of lichen/vine/vein.
func multifaceBase(name string) uint32 {
	def := worldgen.BlockBase(name)
	info, ok := worldgen.InfoForState(def)
	if !ok {
		return def
	}
	return multifaceCleared(info, def)
}
