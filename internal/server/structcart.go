package server

import (
	"encoding/binary"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Mineshaft chest minecarts. Vanilla's mineshaft corridor lays a rail and
// adds a chest minecart entity carrying chests/abandoned_mineshaft where
// it rolls a chest (MineShaftCorridor.createChest). worldgen lays the
// rail; the hub adds the cart the first time the chunk holding it is
// seeded — the same once-per-chunk-ever gate the generation herds use — and
// the cart then persists like any other vehicle. Its loot stays an unrolled
// table until something first reaches inside, as a structure chest's does.

// seedChunkCarts adds the chest minecarts the mineshafts rolled in a chunk.
func (h *hub) seedChunkCarts(players map[int32]*tracked, c [2]int32) {
	bx, bz := int(c[0])*16, int(c[1])*16
	if !h.ownedBlock(bx, bz) {
		return
	}
	g := h.world.Gen()
	for _, m := range g.MineshaftsNear(bx+8, bz+8) {
		for _, p := range g.MineshaftCarts(m) {
			if int32(p[0]>>4) != c[0] || int32(p[2]>>4) != c[1] {
				continue
			}
			h.spawnStructureCart(players, dimOverworld, p[0], p[1], p[2], worldgen.MineshaftCartTable)
		}
	}
}

// unpackCartLoot rolls a structure cart's loot table into its slots the
// first time anything reaches inside (opened, broken, drained by a hopper),
// and names the table it rolled ("" when there was none left to roll).
func (h *hub) unpackCartLoot(v *vehicle) string {
	if v.loot == "" || v.chest == nil {
		return ""
	}
	name := v.loot
	v.loot = ""
	h.fillSlots(v.chest.slots[:], name, v.lootPos)
	return name
}

// spawnStructureCart places a chest minecart that carries an unrolled loot
// table on the rail at a cell. Returns it, or nil when no rail stands there
// (the corridor found the cell blocked or floorless and laid none).
func (h *hub) spawnStructureCart(players map[int32]*tracked, dim, bx, by, bz int, table string) *vehicle {
	w := h.worldFor(dim)
	if w == nil || !isAnyRail(w.At(bx, by, bz)) {
		return nil
	}
	x, y, z := float64(bx)+0.5, float64(by)+0.1, float64(bz)+0.5
	v := &vehicle{eid: h.allocEID(), dim: dim, etype: entityChestMinecart, x: x, y: y, z: z, sx: x, sy: y, sz: z}
	binary.BigEndian.PutUint32(v.uuid[12:], uint32(v.eid))
	initCartKind(v)
	v.loot, v.lootPos = table, blockPos{bx, by, bz}
	h.vehicles[v.eid] = v
	h.toNearbyEv(players, dim, x, z, entAdd(v.eid, v.etype, v.uuid, x, y, z, 0, 0))
	return v
}
