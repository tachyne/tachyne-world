package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Eyeblossoms and nether portals — the two random-tick one-offs left after the
// plant, melting and geology passes.
//
// An eyeblossom is the pale garden's clock: it opens through the night and
// shuts at first light. A nether portal quietly breeds zombified piglins, at a
// rate set by the difficulty, which is why a portal left standing in the
// Nether becomes a piglin farm.

var (
	openEyeblossom   = worldgen.BlockBase("open_eyeblossom")
	closedEyeblossom = worldgen.BlockBase("closed_eyeblossom")
	netherPortalBase = worldgen.BlockBase("nether_portal")
	netherPortalMax  = func() uint32 { _, hi, _ := worldgen.BlockRangeOK("nether_portal"); return hi }()
)

// portalPiglinOdds is vanilla's roll: nextInt(2000) < difficulty id, so easy
// breeds slowly and hard several times as often.
const portalPiglinOdds = 2000

// tickEyeblossom opens the flower at night and closes it at dawn.
func (h *hub) tickEyeblossom(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	var want uint32
	switch state {
	case openEyeblossom, closedEyeblossom:
		if h.nightNow() {
			want = openEyeblossom
		} else {
			want = closedEyeblossom
		}
	default:
		return false
	}
	if want == state {
		return true
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, want)
	sound := "minecraft:block.eyeblossom.close_long"
	if want == openEyeblossom {
		sound = "minecraft:block.eyeblossom.open_long"
	}
	h.playSoundDim(players, dim, sound, sndBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	return true
}

// nightNow reports whether the sun is down.
func (h *hub) nightNow() bool {
	dt := h.dayTime.Load() % dayLengthTicks
	return dt >= 13000 && dt < 23000
}

// tickNetherPortal breeds zombified piglins out of a standing portal.
func (h *hub) tickNetherPortal(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if netherPortalMax == 0 || state < netherPortalBase || state > netherPortalMax {
		return false
	}
	if dim != 1 || h.rules.Difficulty == diffPeaceful || !h.rules.DoMobSpawning {
		return true
	}
	if h.rng.Intn(portalPiglinOdds) >= h.rules.Difficulty {
		return true
	}
	// Walk to the foot of the portal and stand the piglin on the block below.
	fy := y
	for fy > worldgen.MinY+1 {
		st := h.worldFor(dim).At(x, fy-1, z)
		if st < netherPortalBase || st > netherPortalMax {
			break
		}
		fy--
	}
	if !worldgen.IsSolidFull(h.worldFor(dim).At(x, fy-1, z)) {
		return true
	}
	h.spawnHostileY(players, entityZombifiedPiglin, float64(x)+0.5, float64(fy), float64(z)+0.5)
	return true
}
