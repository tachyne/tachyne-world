package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The rest of the dispenser table: glowstone charges an anchor ahead, a
// carved pumpkin is placed facing the dispenser, a shulker box is placed
// with its facing, an XP bottle and a rocket fly, a chest goes onto a tamed
// llama, and a brush combs an armadillo for a scute.
func TestDispenseRemainingBehaviors(t *testing.T) {
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

	onHub(t, h, func() {
		// Glowstone charges an anchor in front, one level per shot.
		anchor := anchorWithCharge(worldgen.BlockBase("respawn_anchor"), 0)
		w.SetBlock(front.x, front.y, front.z, anchor)
		s := load(invStack{item: itemGlowstoneBlock, count: 2})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if c := anchorCharge(w.At(front.x, front.y, front.z)); c != 1 {
			t.Errorf("anchor charge after a glowstone = %d, want 1", c)
		}
		if s.count != 1 {
			t.Errorf("the glowstone should be spent: %d left", s.count)
		}

		// A carved pumpkin lands facing back west at the dispenser.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		w.SetBlock(front.x, front.y-1, front.z, worldgen.Stone) // no golem body below
		load(invStack{item: itemCarvedPumpkin, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		got := w.At(front.x, front.y, front.z)
		info, _ := worldgen.InfoForState(carvedPumpkinBase)
		if !isCarvedPumpkin(got) || worldgen.GetProperty(info, got, "facing") != "west" {
			t.Errorf("carved pumpkin state %d facing %q, want west", got, worldgen.GetProperty(info, got, "facing"))
		}

		// A shulker box opens east with ground below, up without.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		box := int32(itemByName["red_shulker_box"])
		load(invStack{item: box, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		got = w.At(front.x, front.y, front.z)
		binfo, _ := worldgen.InfoForState(got)
		if !isShulkerBox(got) || worldgen.GetProperty(binfo, got, "facing") != "up" {
			t.Errorf("shulker over ground: state %d facing %q, want up", got, worldgen.GetProperty(binfo, got, "facing"))
		}
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		w.SetBlock(front.x, front.y-1, front.z, worldgen.Air)
		load(invStack{item: box, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		got = w.At(front.x, front.y, front.z)
		if !isShulkerBox(got) || worldgen.GetProperty(binfo, got, "facing") != "east" {
			t.Errorf("shulker over air: state %d facing %q, want east", got, worldgen.GetProperty(binfo, got, "facing"))
		}
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		w.SetBlock(front.x, front.y-1, front.z, worldgen.Stone)

		// An XP bottle flies as a shattering projectile; a rocket takes off east.
		h.arrows = map[int32]*arrowEntity{}
		load(invStack{item: itemXPBottle, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if len(h.arrows) != 1 {
			t.Fatalf("xp bottle: %d projectiles, want 1", len(h.arrows))
		}
		for _, a := range h.arrows {
			if !a.xpBottle || !a.breaks {
				t.Error("the bottle should shatter as an xp bottle")
			}
		}
		load(invStack{item: itemFireworkRocket, count: 1})
		before := len(h.rockets)
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if len(h.rockets) != before+1 {
			t.Fatalf("firework: %d rockets, want %d", len(h.rockets), before+1)
		}
		for _, r := range h.rockets {
			if r.vx <= 0 {
				t.Errorf("the rocket should leave eastward: vx=%.2f", r.vx)
			}
		}

		// A chest goes onto a tamed llama standing in front.
		llama := h.spawnSpecies(h.playersRef, entityLlama, 0, float64(front.x)+0.5, float64(front.y), float64(front.z)+0.5)
		if llama == nil {
			t.Fatal("no llama")
		}
		llama.tamed = true
		s = load(invStack{item: int32(itemByName["chest"]), count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if !llama.chested || s.count != 0 {
			t.Errorf("the llama should take the chest: chested=%v left=%d", llama.chested, s.count)
		}
		h.despawnMob(h.playersRef, llama)

		// A brush combs an adult armadillo for a scute and wears by 16.
		dillo := h.spawnSpecies(h.playersRef, entityArmadillo, 0, float64(front.x)+0.5, float64(front.y), float64(front.z)+0.5)
		if dillo == nil {
			t.Fatal("no armadillo")
		}
		items := len(h.items)
		s = load(invStack{item: itemBrush, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if len(h.items) != items+1 || s.dmg != 16 {
			t.Errorf("brushing: items %d→%d, brush dmg %d (want +1 item, dmg 16)", items, len(h.items), s.dmg)
		}
		dillo.baby = true
		s = load(invStack{item: itemBrush, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if s.dmg != 0 {
			t.Error("a baby armadillo gives no scute and costs no wear")
		}
	})
}
