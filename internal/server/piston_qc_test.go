package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestPistonQuasiConnectivity(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	info, ok := worldgen.InfoForState(pistonMin)
	if !ok || !info.HasProperty("facing") {
		t.Fatalf("piston has no facing info: %+v", info.Props)
	}
	east := setBoolProp(worldgen.SetProperty(info, pistonMin, "facing", "east"), "extended", false)

	onHub(t, h, func() {
		p := blockPos{0, 70, 0}
		w.SetBlock(p.x, p.y, p.z, east) // faces +x
		w.SetBlock(1, 70, 0, worldgen.Air)
		w.SetBlock(0, 71, 0, worldgen.Air) // the block directly above, empty

		// No power adjacent to the piston → stays retracted.
		h.updatePiston(h.playersRef, p, w.At(p.x, p.y, p.z))
		if boolProp(w.At(p.x, p.y, p.z), "extended") {
			t.Fatal("piston extended with no power")
		}

		// Power the cell ABOVE the block-above (quasi region) → QC extends it,
		// even though no direct neighbour of the piston is powered.
		w.SetBlock(0, 72, 0, redstoneBlock)
		h.updatePiston(h.playersRef, p, w.At(p.x, p.y, p.z))
		if !boolProp(w.At(p.x, p.y, p.z), "extended") {
			t.Fatal("piston did not extend via quasi-connectivity")
		}
		if !isPistonHead(w.At(1, 70, 0)) {
			t.Error("piston head not placed in front after QC extend")
		}
	})
}
