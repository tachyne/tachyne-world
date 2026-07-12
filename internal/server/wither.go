package server

import (
	"math"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// The wither boss. Built the vanilla way — a T of soul sand topped with three
// wither-skeleton skulls — then charges for a few seconds (invulnerable, boss
// bar filling), detonates a spawn blast, and flies free shooting wither skulls
// (witherShoot, from the species table). A purple boss bar tracks its health
// for nearby players; killing it drops the nether star.

const (
	witherHealth      = 300
	witherSpawnCharge = 220 // ticks of invulnerable spawn charge (vanilla ~200)
	witherBlastPower  = 7   // spawn-explosion radius/power
)

var (
	blockSoulSand  = worldgen.BlockBase("soul_sand")                  // minecraft:soul_sand
	witherSkullMin = worldgen.BlockBase("wither_skeleton_skull")      // wither_skeleton_skull (floor) rotations…
	witherSkullMax = worldgen.BlockBase("wither_skeleton_skull") + 31 // …end of the floor-skull range
)

func isWitherSkull(s uint32) bool { return s >= witherSkullMin && s <= witherSkullMax }

// checkWitherBuild is called after any block placement. If the placed block is
// a wither skull that completes the soul-sand T with three skulls, it consumes
// the structure and spawns a charging wither. The skull can be any of the three.
func (h *hub) checkWitherBuild(players map[int32]*tracked, dim, px, py, pz int, state uint32) {
	if !isWitherSkull(state) {
		return
	}
	w := h.worldFor(dim)
	// The placed skull sits in the top layer. Try both axes (row along X, row
	// along Z) and all three positions the placed skull could occupy.
	for _, ax := range [][2]int{{1, 0}, {0, 1}} {
		for k := -1; k <= 1; k++ {
			cx, cz := px-k*ax[0], pz-k*ax[1] // candidate centre column
			if witherPatternAt(w, cx, py, cz, ax) {
				h.spawnWitherFrom(players, dim, cx, py, cz, ax)
				return
			}
		}
	}
}

// witherPatternAt checks the full structure centred on (cx, topY, cz): three
// skulls in the top row, three soul sand beneath them (the arms), and one more
// soul sand under the centre (the stem).
func witherPatternAt(w *world.World, cx, topY, cz int, ax [2]int) bool {
	for j := -1; j <= 1; j++ {
		x, z := cx+j*ax[0], cz+j*ax[1]
		if !isWitherSkull(w.At(x, topY, z)) || w.At(x, topY-1, z) != blockSoulSand {
			return false
		}
	}
	return w.At(cx, topY-2, cz) == blockSoulSand
}

// spawnWitherFrom clears the structure and spawns a charging wither at its base.
func (h *hub) spawnWitherFrom(players map[int32]*tracked, dim, cx, topY, cz int, ax [2]int) {
	for j := -1; j <= 1; j++ {
		x, z := cx+j*ax[0], cz+j*ax[1]
		h.setBlock(players, blockPos{x, topY, z}, worldgen.Air)     // skull
		h.setBlock(players, blockPos{x, topY - 1, z}, worldgen.Air) // arm
	}
	h.setBlock(players, blockPos{cx, topY - 2, cz}, worldgen.Air) // stem

	m := h.spawnSpecies(players, entityWither, dim, float64(cx)+0.5, float64(topY-2), float64(cz)+0.5)
	if m == nil {
		return
	}
	m.health = witherHealth
	m.spawnInvuln = witherSpawnCharge
	h.playSoundDim(players, dim, "minecraft:entity.wither.spawn", sndHostile, m.x, m.y, m.z, 4, 1)
}

// updateWithers runs the spawn-charge countdown + blast and drives every
// wither's boss bar. Called on the hub tick.
func (h *hub) updateWithers(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.etype != entityWither || m.dying > 0 {
			continue
		}
		if m.spawnInvuln > 0 {
			if m.spawnInvuln--; m.spawnInvuln == 0 {
				// Charge complete: level the terrain around it and roar free.
				h.explodeAt(players, m.x, m.y+1, m.z, witherBlastPower, witherBlastPower*4)
				h.playSoundDim(players, m.dim, "minecraft:entity.wither.spawn", sndHostile, m.x, m.y, m.z, 4, 1)
			}
		}
		h.updateBossBar(players, m, "Wither", witherHealth)
	}
}

// updateBossBar shows/updates/removes a mob's boss bar for players near it (in
// its dimension). Reusable for any boss; keyed per (player, boss).
func (h *hub) updateBossBar(players map[int32]*tracked, m *mob, title string, maxHP int) {
	const barRange = 40.0
	frac := float32(math.Max(0, float64(m.health))) / float32(maxHP)
	for _, t := range players {
		key := [2]int32{t.p.eid, m.eid}
		near := t.dim == m.dim && dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= barRange
		if near {
			if !h.bossSeen[key] {
				h.bossSeen[key] = true
				t.p.trySendEv(bossBarAdd(m.uuid, title, frac))
			} else {
				t.p.trySendEv(bossBarHealth(m.uuid, frac))
			}
		} else if h.bossSeen[key] {
			delete(h.bossSeen, key)
			t.p.trySendEv(bossBarRemove(m.uuid))
		}
	}
}

// clearBossBar removes a dead/despawned boss's bar from everyone who saw it.
func (h *hub) clearBossBar(players map[int32]*tracked, m *mob) {
	for _, t := range players {
		key := [2]int32{t.p.eid, m.eid}
		if h.bossSeen[key] {
			delete(h.bossSeen, key)
			t.p.trySendEv(bossBarRemove(m.uuid))
		}
	}
}
