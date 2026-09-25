package server

import (
	"math/rand"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Shape updates: a block whose STATE follows a neighbour (vanilla's
// Block.updateShape), for the families that have no path of their own —
// connectors, walls and stairs are re-wired by connect.go, multiface blocks,
// scaffolding and chorus by dropUnsupported. Each runs when the block beside
// it changes, however that change was written, and only for the side the
// change came from, as vanilla hands updateShape one direction at a time.

// shapeKind is which updateShape a state answers to.
type shapeKind uint8

const (
	shapeNone         shapeKind = iota
	shapeSnowy                  // SnowyBlock: grass, podzol, mycelium
	shapeGate                   // FenceGateBlock: in_wall
	shapeAttached               // AttachedStemBlock: back to a grown stem without its fruit
	shapeMushroom               // HugeMushroomBlock: faces against more of itself
	shapeCopperChest            // CopperChestBlock: one half follows the other's weathering and wax
	shapePotentSulfur           // PotentSulfurBlock: dry, wet or a geyser by the water above and the block below
	shapeCampfire               // CampfireBlock: a signal fire over a hay bale (isSmokeSource)
	shapeBell                   // BellBlock: between two walls, or on one when the other goes
	shapeConnect                // fences, panes, bars, walls, stairs, tripwire: their connections
	shapeSpeleothem             // pointed dripstone, sulfur spikes: thickness along the column
	shapeGrowingBody            // GrowingPlantBodyBlock: the top of a cut kelp or vine becomes its head
	shapeMossCarpet             // MossyCarpetBlock: its sides follow the walls beside it
	shapeBamboo                 // BambooSaplingBlock / BambooStalkBlock: bamboo growing above
	shapeBigDripleaf            // BigDripleafBlock: a leaf with another leaf on it becomes stem
	shapeHangingMoss            // HangingMossBlock: the tip is the strand's last cell
)

// shapeKinds maps every state of the families above to its kind — one map
// probe per neighbour on the setBlockAt path, whatever the block.
var shapeKinds = func() map[uint32]shapeKind {
	out := map[uint32]shapeKind{}
	add := func(name string, k shapeKind) {
		lo, hi, ok := worldgen.BlockRangeOK(name)
		if !ok {
			return
		}
		for s := lo; s <= hi; s++ {
			out[s] = k
		}
	}
	for _, n := range []string{"grass_block", "podzol", "mycelium"} {
		add(n, shapeSnowy)
	}
	for _, n := range []string{"attached_pumpkin_stem", "attached_melon_stem"} {
		add(n, shapeAttached)
	}
	for _, n := range []string{"brown_mushroom_block", "red_mushroom_block", "mushroom_stem"} {
		add(n, shapeMushroom)
	}
	add("potent_sulfur", shapePotentSulfur)
	for _, n := range []string{"kelp_plant", "twisting_vines_plant", "weeping_vines_plant", "cave_vines_plant"} {
		add(n, shapeGrowingBody)
	}
	add("pale_moss_carpet", shapeMossCarpet)
	add("bamboo_sapling", shapeBamboo)
	add("bamboo", shapeBamboo)
	add("big_dripleaf", shapeBigDripleaf)
	add("pale_hanging_moss", shapeHangingMoss)
	add("bell", shapeBell)
	add("campfire", shapeCampfire)
	add("soul_campfire", shapeCampfire)
	for _, n := range worldgen.AllBlockNames() {
		if lo, _, ok := worldgen.BlockRangeOK(n); ok {
			if info, ok := worldgen.InfoForState(lo); ok {
				_, stair := stairInfo(lo)
				if worldgen.IsHorizontalConnector(info) || worldgen.IsWallConnector(info) || stair {
					add(n, shapeConnect)
				}
				if worldgen.SupportFor(lo) == worldgen.SupportSpeleothem {
					add(n, shapeSpeleothem)
				}
			}
		}
		if strings.HasSuffix(n, "_fence_gate") {
			add(n, shapeGate)
		}
		if strings.HasSuffix(n, "copper_chest") {
			add(n, shapeCopperChest)
		}
	}
	return out
}()

// snowTagStates is #snow: what makes the ground under it snowy.
var snowTagStates = blockRange("snow", "snow_block", "powder_snow")

// shapeUpdated is updateShape for the block st at n, told that the cell one
// step along d changed. It returns the state the block should now hold
// (st itself when nothing follows) and whether st is one of these families.
func shapeUpdated(w *world.World, n blockPos, st uint32, d [3]int) (uint32, bool) {
	k := shapeKinds[st]
	if k == shapeNone {
		return st, false
	}
	nb := w.At(n.x+d[0], n.y+d[1], n.z+d[2])
	if k == shapeGrowingBody {
		// GrowingPlantBodyBlock.updateShape: the cell ahead in the growth
		// direction is no longer this plant (the top of a kelp stalk was
		// cut, a vine's lower end broken off) — the body there turns into
		// the plant's head at a fresh random age, so the plant grows on from
		// it (updateHeadAfterConvertedFromBody: cave vines keep their berries).
		g, ok := growingPlantOfBody(st)
		if !ok || d != [3]int{0, g.dy, 0} || sameGrowingPlant(st, nb) {
			return st, true
		}
		berries := false
		if bi, ok := worldgen.InfoForState(st); ok && bi.HasProperty("berries") {
			berries = worldgen.GetProperty(bi, st, "berries") == "true"
		}
		return g.headAt(rand.Intn(growingPlantMaxAge), berries), true
	}
	if k == shapeBamboo && inStates(st, bambooSapStates) {
		// BambooSaplingBlock.updateShape (the shoot has no properties, so it
		// is answered before the layout lookup): bamboo growing or set on a
		// shoot turns the shoot into a stalk.
		if d == [3]int{0, 1, 0} && inStates(nb, bambooStates) {
			return worldgen.BlockID("bamboo"), true
		}
		return st, true
	}
	info, ok := worldgen.InfoForState(st)
	if !ok {
		return st, false
	}
	switch k {
	case shapeSpeleothem:
		// SpeleothemBlock.updateShape: a change above or below re-reads the
		// thickness (a support lost is the sweep's to drop).
		if d[1] == 0 {
			return st, true
		}
		tip := worldgen.GetProperty(info, st, "vertical_direction")
		merge := worldgen.GetProperty(info, st, "thickness") == "tip_merge"
		return worldgen.SetProperty(info, st, "thickness", speleothemThickness(w, n, st, tip, merge)), true
	case shapeConnect:
		// FenceBlock / IronBarsBlock / WallBlock / StairBlock / TripWireBlock
		// .updateShape: any neighbour change, whoever made it, re-reads the
		// connections — not only a player's edit.
		return connectStateAt(w, n.x, n.y, n.z, st), true
	case shapeBell:
		// BellBlock.updateShape, along the facing's axis: a double-wall bell
		// that loses one wall hangs on from the other; a single-wall bell that
		// gains a wall on its free side is held from both.
		if d[1] != 0 {
			return st, true
		}
		facing := worldgen.GetProperty(info, st, "facing")
		fx, fz := facingDelta(facing)
		if (fx != 0) != (d[0] != 0) {
			return st, true // across the axis
		}
		switch worldgen.GetProperty(info, st, "attachment") {
		case "double_wall":
			if !holdsBlock(nb) { // the wall the other way stays: face it
				ns := worldgen.SetProperty(info, st, "attachment", "single_wall")
				return worldgen.SetProperty(info, ns, "facing", facingFromDelta(-d[0], -d[2])), true
			}
		case "single_wall":
			if d[0] == -fx && d[2] == -fz && holdsBlock(nb) {
				return worldgen.SetProperty(info, st, "attachment", "double_wall"), true
			}
		}
		return st, true
	case shapeCampfire:
		// CampfireBlock.updateShape: the block below decides signal_fire.
		if d != [3]int{0, -1, 0} {
			return st, true
		}
		return worldgen.SetProperty(info, st, "signal_fire", strconv.FormatBool(isHay(nb))), true
	case shapeSnowy:
		// SnowyBlock.updateShape: only the block above counts.
		if d != [3]int{0, 1, 0} {
			return st, true
		}
		v := "false"
		if inRanges2(nb, snowTagStates) {
			v = "true"
		}
		return worldgen.SetProperty(info, st, "snowy", v), true
	case shapeGate:
		// FenceGateBlock.updateShape: along the axis the gate spans (its
		// facing turned clockwise), a wall on either side sinks it.
		if d[1] != 0 || facingAxisX(worldgen.GetProperty(info, st, "facing")) == (d[0] != 0) {
			return st, true
		}
		_, a := wallInfo(nb)
		_, b := wallInfo(w.At(n.x-d[0], n.y, n.z-d[2]))
		v := "false"
		if a || b {
			v = "true"
		}
		return worldgen.SetProperty(info, st, "in_wall", v), true
	case shapeAttached:
		// AttachedStemBlock.updateShape: the side it faces lost its fruit —
		// the stem lets go and stands fully grown again, ready to fruit anew.
		dx, dz := facingDelta(worldgen.GetProperty(info, st, "facing"))
		if d != [3]int{dx, 0, dz} {
			return st, true
		}
		stem, fruit := pumpkinStemBase, pumpkinBlock
		if st >= attachedMelonBase && st <= attachedMelonBase+3 {
			stem, fruit = melonStemBase, melonBlock
		}
		if nb == fruit {
			return st, true
		}
		return stem + 7, true
	case shapeMushroom:
		return mushroomFaceOnNeighbor(info, st, nb, d), true
	case shapeCopperChest:
		// CopperChestBlock.updateShape: when the half it is paired with (on
		// its connected side) becomes another copper chest — it weathered,
		// was waxed or was scraped — this half turns into that block too,
		// keeping its own facing and type.
		ps, pw, _, pok := copperChestOf(nb)
		_, _, so, sok := copperChestOf(st)
		facing, ctype := chestFacingType(st)
		dir, paired := connectedDir(facing, ctype)
		if !pok || !sok || !paired {
			return st, true
		}
		if dx, dz := facingDelta(dir); d != [3]int{dx, 0, dz} {
			return st, true
		}
		i := ps
		if pw {
			i += 4
		}
		return copperChestBases[i] + so, true
	case shapeMossCarpet:
		// MossyCarpetBlock.updateShape → getUpdatedState(createSides=false):
		// a side keeps what it had while a wall is there to hold it, drops
		// to none when the wall goes, and turns tall or low with the layer
		// above. An upper layer left with no side at all is the sweep's to
		// remove (supported: hasFaces).
		return mossCarpetUpdated(w, n, st, false), true
	case shapeBamboo:
		// BambooStalkBlock.updateShape: a stalk takes the next AGE when the
		// one above is older than it.
		if d != [3]int{0, 1, 0} || !inStates(nb, bambooStates) {
			return st, true
		}
		ni, ok := worldgen.InfoForState(nb)
		if ok && atoi(worldgen.GetProperty(ni, nb, "age")) > atoi(worldgen.GetProperty(info, st, "age")) {
			return worldgen.SetProperty(info, st, "age", "1"), true // cycle(AGE) from 0: AGE runs 0..1
		}
		return st, true
	case shapeBigDripleaf:
		// BigDripleafBlock.updateShape: a leaf with another leaf set on top
		// becomes stem, keeping its facing and its water (withPropertiesOf).
		if d != [3]int{0, 1, 0} || !isBigDripleaf(nb) {
			return st, true
		}
		stem := worldgen.BlockID("big_dripleaf_stem")
		si, ok := worldgen.InfoForState(stem)
		if !ok {
			return st, true
		}
		stem = worldgen.SetProperty(si, stem, "facing", worldgen.GetProperty(info, st, "facing"))
		return worldgen.SetProperty(si, stem, "waterlogged", worldgen.GetProperty(info, st, "waterlogged")), true
	case shapeHangingMoss:
		// HangingMossBlock.updateShape: TIP is "no more moss below".
		if d != [3]int{0, -1, 0} {
			return st, true
		}
		return worldgen.SetProperty(info, st, "tip", strconv.FormatBool(!inRange(nb, hangingMossRng))), true
	case shapePotentSulfur:
		// PotentSulfurBlock.updateShape re-derives the whole state from any
		// side; only the cells above and below can change it.
		return potentSulfurValid(w, n, st), true
	}
	return st, true
}

// mushroomFaceOnNeighbor is HugeMushroomBlock.updateShape: a face turned to
// more of the same block closes. It never opens again — a face that loses its
// neighbour keeps the state it had, as vanilla's does.
func mushroomFaceOnNeighbor(info worldgen.BlockInfo, st, nb uint32, d [3]int) uint32 {
	if !sameBlockFamily(st, nb) {
		return st
	}
	var face string
	switch d {
	case [3]int{0, 1, 0}:
		face = "up"
	case [3]int{0, -1, 0}:
		face = "down"
	case [3]int{0, 0, -1}:
		face = "north"
	case [3]int{0, 0, 1}:
		face = "south"
	case [3]int{-1, 0, 0}:
		face = "west"
	case [3]int{1, 0, 0}:
		face = "east"
	default:
		return st
	}
	return worldgen.SetProperty(info, st, face, "false")
}
