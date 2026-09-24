package server

import (
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
	for _, n := range worldgen.AllBlockNames() {
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
	info, ok := worldgen.InfoForState(st)
	if !ok {
		return st, false
	}
	nb := w.At(n.x+d[0], n.y+d[1], n.z+d[2])
	switch k {
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
