package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The dragon respawn ceremony (EndDragonFight.respawnDragon +
// DragonRespawnAnimation): four end crystals set around the exit portal
// close it and aim their beams at the sky; the dragon growls; the beams
// sweep the obsidian pillars one by one, each going up in a blast; the
// beams return to the centre, the crystals detonate, and the dragon is
// back over the island with a crystal on every pillar.

const (
	respawnStart = iota
	respawnPreparing
	respawnPillars
	respawnDragon
	respawnEnd

	metaIndexCrystalBeam = 8    // EndCrystal DATA_BEAM_TARGET (optional block pos)
	worldEventDragonRoar = 3001 // levelEvent 3001: the dragon's growl
	respawnSkyY          = 128
)

type dragonRespawn struct {
	stage    int
	time     int
	crystals []*crystal // the four around the portal
	daisY    int
}

// crystalBeamMeta points (or clears) a crystal's beam.
func crystalBeamMeta(eid int32, target *blockPos) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCrystalBeam)
	b = protocol.AppendVarInt(b, metaTypeOptBlockPos)
	if target == nil {
		b = protocol.AppendBool(b, false)
	} else {
		b = protocol.AppendBool(b, true)
		b = protocol.AppendPosition(b, target.x, target.y, target.z)
	}
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) aimRespawnBeams(players map[int32]*tracked, r *dragonRespawn, target *blockPos) {
	for _, c := range r.crystals {
		h.toDimEv(players, 2, metaEv(crystalBeamMeta(c.eid, target)))
	}
}

func (h *hub) dragonRoar(players map[int32]*tracked) {
	h.toDimEv(players, 2, attachproto.WorldFX{Event: worldEventDragonRoar, X: 0, Y: respawnSkyY, Z: 0})
}

// startDragonRespawn is respawnDragon: the portal closes (its end-portal
// blocks turn to bedrock, spawnExitPortal(false)) and the ceremony begins.
func (h *hub) startDragonRespawn(players map[int32]*tracked, found []*crystal, daisY int) {
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if h.end.At(dx, daisY, dz) == worldgen.EndPortalBlock {
				h.setBlockIn(players, 2, blockPos{dx, daisY, dz}, worldgen.Bedrock)
			}
		}
	}
	h.dragonRespawn = &dragonRespawn{stage: respawnStart, crystals: found, daisY: daisY}
}

func (r *dragonRespawn) setStage(stage int) { r.stage, r.time = stage, 0 }

// tickDragonRespawn is the animation's tick.
func (h *hub) tickDragonRespawn(players map[int32]*tracked) {
	r := h.dragonRespawn
	if r == nil {
		return
	}
	for _, c := range r.crystals { // a respawn crystal destroyed aborts the ceremony (onCrystalDestroyed)
		if h.crystals[c.eid] == nil {
			h.aimRespawnBeams(players, r, nil)
			h.dragonRespawn = nil
			return
		}
	}
	n := r.time
	r.time++
	sky := blockPos{0, respawnSkyY, 0}
	switch r.stage {
	case respawnStart:
		h.aimRespawnBeams(players, r, &sky)
		r.setStage(respawnPreparing)
	case respawnPreparing:
		if n < 100 {
			if n == 0 || n == 50 || n == 51 || n == 52 || n >= 95 {
				h.dragonRoar(players)
			}
		} else {
			r.setStage(respawnPillars)
		}
	case respawnPillars:
		i := n / 40
		if i >= worldgen.EndPillars {
			if n%40 == 0 {
				r.setStage(respawnDragon)
			}
			return
		}
		px := int(math.Floor(worldgen.EndPillarRing * cosTurn(float64(i)/worldgen.EndPillars)))
		pz := int(math.Floor(worldgen.EndPillarRing * sinTurn(float64(i)/worldgen.EndPillars)))
		top := worldgen.EndPillarTop(i)
		switch n % 40 {
		case 0:
			t := blockPos{px, top + 1, pz}
			h.aimRespawnBeams(players, r, &t)
		case 39:
			// The spike goes up in a blast and is set again (Feature.END_SPIKE):
			// its old crystal, if any, goes with it.
			for eid, c := range h.crystals {
				if int(math.Floor(c.x)) == px && int(math.Floor(c.z)) == pz && c.y >= float64(top-1) {
					delete(h.crystals, eid)
					h.toDimEv(players, 2, entGone(eid))
				}
			}
			h.explodeIn(players, 2, float64(px)+0.5, float64(top), float64(pz)+0.5, 0, 0)
		}
	case respawnDragon:
		switch {
		case n >= 100:
			r.setStage(respawnEnd)
			h.aimRespawnBeams(players, r, nil)
			for _, c := range r.crystals {
				delete(h.crystals, c.eid)
				h.toDimEv(players, 2, entGone(c.eid))
				h.explodeIn(players, 2, c.x, c.y, c.z, 0, 0)
			}
			h.dragonRespawn = nil
			// setRespawnStage(END): dragonKilled = false, createNewDragon —
			// enterEnd stages the dragon and a crystal on every pillar.
			h.rules.DragonDefeated = false
			h.rules.DragonHealth = 0
			h.saveRules()
			h.enterEnd(players, nil)
			for _, t := range players {
				if t.dim == 2 {
					t.p.trySendEv(chatEv("The Ender Dragon stirs again."))
				}
			}
		case n >= 80:
			h.dragonRoar(players)
		case n == 0:
			h.aimRespawnBeams(players, r, &sky)
		case n < 5:
			h.dragonRoar(players)
		}
	}
}
