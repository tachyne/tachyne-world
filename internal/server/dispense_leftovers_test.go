package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// DispenseItemBehavior's CARVED_PUMPKIN and WITHER_SKELETON_SKULL: the
// block is placed only where it completes a golem (canSpawnGolem) or stands
// on a wither's base (canSpawnMob); anywhere else it goes on the head of
// whoever stands there, and with nobody the dispenser fails and keeps it.
func TestDispensedHeadsOnlyFinishBuilds(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	state := eastDispenser(t)
	pos := blockPos{5, 70, 5}
	front := blockPos{6, 70, 5}
	load := func(st invStack) *invStack {
		w.SetBlock(pos.x, pos.y, pos.z, state)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = st
		h.bins[simPos{blockPos: pos}] = b
		return &b.slots[0]
	}
	clear := func() {
		for dy := -3; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(front.x, front.y+dy, front.z+dz, worldgen.Air)
			}
		}
		w.SetBlock(front.x, front.y-4, front.z, worldgen.Stone)
	}
	golems := func() int {
		n := 0
		for _, m := range h.mobs {
			if m.etype == entitySnowGolem {
				n++
			}
		}
		return n
	}

	onHub(t, h, func() {
		h.rules.Difficulty = diffNormal
		// A pumpkin over one snow block: no golem, so it stays.
		clear()
		w.SetBlock(front.x, front.y-1, front.z, snowBlockState)
		pk := load(invStack{item: itemCarvedPumpkin, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if w.At(front.x, front.y, front.z) != worldgen.Air || pk.count != 1 {
			t.Errorf("a pumpkin over half a snow golem is kept")
		}
		// Over two: the snow golem is built and the pumpkin spent.
		w.SetBlock(front.x, front.y-2, front.z, snowBlockState)
		pk = load(invStack{item: itemCarvedPumpkin, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if golems() != 1 || pk.count != 0 {
			t.Errorf("a pumpkin on a snow golem body builds it: %d golems, %d pumpkins left", golems(), pk.count)
		}

		// A skull in open air stays; on a wither base it is placed (the
		// other two skulls not there yet).
		clear()
		sk := load(invStack{item: itemWitherSkull, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if w.At(front.x, front.y, front.z) != worldgen.Air || sk.count != 1 {
			t.Errorf("a skull with no wither base under it is kept")
		}
		for dz := -1; dz <= 1; dz++ {
			w.SetBlock(front.x, front.y-1, front.z+dz, worldgen.SoulSand)
		}
		w.SetBlock(front.x, front.y-2, front.z, worldgen.SoulSand)
		sk = load(invStack{item: itemWitherSkull, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if !isWitherSkull(w.At(front.x, front.y, front.z)) || sk.count != 0 {
			t.Errorf("a skull on a wither base is placed")
		}
		// Peaceful: canSpawnMob is false, so the skull is kept.
		h.rules.Difficulty = diffPeaceful
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		sk = load(invStack{item: itemWitherSkull, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if w.At(front.x, front.y, front.z) != worldgen.Air || sk.count != 1 {
			t.Errorf("in peaceful the skull is kept")
		}
	})
}
