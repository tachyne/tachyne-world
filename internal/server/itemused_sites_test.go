package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// minecraft:used beyond placement, mining and melee: each use below goes in
// the way a client sends it (use_item, use_item_on, release_use_item) and
// through the live hub, and counts once, as the vanilla item or block that
// handles it awards Stats.ITEM_USED. The rig's player is in creative, which
// counts the same as survival.
func TestItemUsedAtUseSites(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	r := &remotePlayer{s: s, p: p, gm: -1, emit: func(byte, []byte) {}}
	stop := make(chan struct{}) // nobody reads the session's queue here: keep it moving
	defer close(stop)
	go func() {
		for {
			select {
			case <-p.out:
			case <-stop:
				return
			}
		}
	}()
	var tr *tracked
	hold := func(st invStack) {
		onHub(t, h, func() {
			tr = h.playersRef[p.eid]
			tr.inv.slots[0] = st
			tr.p.setHeldSlot(0)
			h.sendHandSlot(tr, 0)
		})
		p.setHotbarSlot(0, st.item)
	}
	want := func(what string, item int32, n int32) {
		t.Helper()
		onHub(t, h, func() {}) // the posted events have run
		onHub(t, h, func() {
			if got := usedStat(tr, item); got != n {
				t.Errorf("%s: minecraft:used %d, want %d", what, got, n)
			}
		})
	}
	y := int(p.y)

	// Throwables and use()-items: a use_item frame each.
	for _, tc := range []struct {
		name string
		item int32
	}{
		{"snowball", itemSnowball},
		{"ender_pearl", itemEnderPearl},
		{"egg", itemEgg},
		{"experience_bottle", itemXPBottle},
		{"spyglass", itemSpyglass},
		{"wind_charge", int32(itemWindCharge)},
		{"goat_horn", itemGoatHorn},
		{"writable_book", itemWritableBook},
		// FoodOnAStickItem.use counts the wave that boosts nothing.
		{"carrot_on_a_stick", itemCarrotOnStick},
	} {
		hold(invStack{item: tc.item, count: 4})
		r.Action(attachproto.UseItem{Hand: 0})
		want(tc.name, tc.item, 1)
	}

	// A bow counts when the arrow is loosed, not when it is drawn.
	hold(invStack{item: itemBow, count: 1})
	onHub(t, h, func() { tr.inv.slots[9] = invStack{item: itemArrowAmmo, count: 8} })
	r.Action(attachproto.UseItem{Hand: 0})
	want("bow drawn", itemBow, 0)
	onHub(t, h, func() { tr.drawingAt = h.tick.Load() - 40 })
	s.handleDig(p, digBody(digReleaseUse, 0, 0, 0))
	want("bow loosed", itemBow, 1)

	// Buckets: poured on the ground, then scooped back up from below.
	hold(invStack{item: itemBucketH2O, count: 1})
	s.handlePlace(p, placeBody(3, y-1, 0, 1))
	want("water_bucket poured", itemBucketH2O, 1)
	hold(invStack{item: itemBucket, count: 1})
	onHub(t, h, func() {
		h.world.SetBlock(0, y, 0, worldgen.WaterBase) // the player's own cell
		tr.pitch = 90                                 // looking straight down into it
	})
	r.Action(attachproto.UseItem{Hand: 0})
	want("bucket filled", itemBucket, 1)

	// ItemStack.useOn: a hoe tilling in creative, an armour stand set down.
	hoe := itemByName["diamond_hoe"]
	onHub(t, h, func() {
		h.world.SetBlock(-3, y-1, 0, worldgen.BlockBase("dirt"))
		h.world.SetBlock(-3, y, 0, worldgen.Air)
	})
	hold(invStack{item: hoe, count: 1})
	s.handlePlace(p, placeBody(-3, y-1, 0, 1))
	want("hoe tilled", hoe, 1)
	onHub(t, h, func() {
		h.world.SetBlock(0, y-1, 3, worldgen.Stone)
		h.world.SetBlock(0, y, 3, worldgen.Air)
		h.world.SetBlock(0, y+1, 3, worldgen.Air)
	})
	hold(invStack{item: itemArmorStand, count: 1})
	s.handlePlace(p, placeBody(0, y-1, 3, 1))
	want("armor_stand placed", itemArmorStand, 1)

	// Cauldrons: a water bottle poured is USE_CAULDRON (not FILL_CAULDRON)
	// and a use of the potion.
	onHub(t, h, func() { h.world.SetBlock(5, y, 5, cauldronState) })
	hold(potionStack(potWater))
	s.handlePlace(p, placeBody(5, y, 5, 1))
	want("potion into a cauldron", itemPotion, 1)
	onHub(t, h, func() {
		if tr.stats[statKey{attachproto.StatCustom, customStatID["use_cauldron"]}] != 1 ||
			tr.stats[statKey{attachproto.StatCustom, customStatID["fill_cauldron"]}] != 0 {
			t.Error("a poured water bottle is use_cauldron, not fill_cauldron")
		}
	})
}
