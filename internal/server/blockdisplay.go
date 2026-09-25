package server

import (
	"math/rand"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// What three block entities show (their update tags): a vault's display item,
// the mob turning inside a trial spawner, and the item peeking out of a
// half-brushed suspicious block.

func blockDisplayEv(pos blockPos, kind int32, name string, count int32) attachproto.BlockDisplay {
	return attachproto.BlockDisplay{Pos: [3]int32{int32(pos.x), int32(pos.y), int32(pos.z)}, Kind: kind, Name: name, Count: count}
}

// vaultDisplay is VaultBlockEntity.cycleDisplayItemFromLootTable: while the
// vault is active, every twenty ticks it shows a random item its table could
// give; otherwise it shows nothing.
func (h *hub) vaultDisplay(players map[int32]*tracked, v *vaultRecord, now uint64, entered bool) {
	switch {
	case v.state == vaultActive && (entered || now%20 == 0):
		name, count := "", int32(0)
		if tbl, ok := lootForChest(vaultLootFor(v.ominous)); ok {
			ctx := &lootCtx{rng: h.rng.Intn, randf: h.rng.Float64}
			var got []invStack
			for _, st := range h.evalChestStacks(tbl, ctx, 0) {
				if st.item != 0 && st.count > 0 {
					got = append(got, st)
				}
			}
			if len(got) > 0 {
				st := got[h.rng.Intn(len(got))]
				name, count = "minecraft:"+itemNameOf[st.item], int32(st.count)
			}
		}
		h.toNearbyEv(players, 0, float64(v.pos.x), float64(v.pos.z), blockDisplayEv(v.pos, attachproto.DisplayVault, name, count))
	case v.state == vaultInactive && entered:
		h.toNearbyEv(players, 0, float64(v.pos.x), float64(v.pos.z), blockDisplayEv(v.pos, attachproto.DisplayVault, "", 0))
	}
}

// trialDisplay is TrialSpawnerStateData.getUpdateTag: the mob it spawns
// (spawn_data), which the client turns inside the cage.
func (h *hub) trialDisplay(players map[int32]*tracked, ts *trialSpawner) {
	etype, ok := trialSpawnerMobs[ts.kind]
	if !ok {
		return
	}
	h.toNearbyEv(players, ts.dim, float64(ts.pos.x), float64(ts.pos.z),
		blockDisplayEv(ts.pos, attachproto.DisplayTrialSpawner, "minecraft:"+entityNameByID[etype], 0))
}

// brushDisplay is the brushed block's update tag: the item the loot will give
// (rolled from the same seed the finished brush uses) and the face it is
// coming out of.
func (h *hub) brushDisplay(players map[int32]*tracked, dim int, pos blockPos, b *brushing) {
	name, ok := h.brushLootTable(pos)
	if !ok {
		return
	}
	tbl, found := lootForChest(name)
	if !found {
		return
	}
	r := rand.New(rand.NewSource(chestSeed(h.world.Seed(), pos, name)))
	ctx := &lootCtx{rng: r.Intn, randf: r.Float64}
	for _, s := range h.evalChestStacks(tbl, ctx, 0) {
		if s.item != 0 && s.count > 0 {
			ev := blockDisplayEv(pos, attachproto.DisplayBrushable, "minecraft:"+itemNameOf[s.item], int32(s.count))
			ev.HitDir = int32(b.face) + 1
			h.toNearbyEv(players, dim, float64(pos.x), float64(pos.z), ev)
			return
		}
	}
}
