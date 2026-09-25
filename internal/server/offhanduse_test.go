package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// offhandUseRig is a survival player holding a pickaxe in the main hand,
// with the use_item path from the client's frame to the hub handler.
func offhandUseRig(t *testing.T) (*hub, map[int32]*tracked, *tracked, *remotePlayer) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pick := invStack{item: itemByName["diamond_pickaxe"], count: 1}
	pl.inv.slots[0] = pick
	pl.p.setHotbarSlot(0, pick.item)
	return h, players, pl, &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
}

// useHand sends a use_item for one hand and runs what it posts, as the hub
// loop would.
func useHand(h *hub, players map[int32]*tracked, r *remotePlayer, hand int32) {
	r.Action(attachproto.UseItem{Hand: hand})
	for len(h.events) > 0 {
		ev := <-h.events
		if h.useItemEvent(players, ev) {
			continue
		}
		pl := players[r.p.eid]
		switch e := ev.(type) {
		case evThrowPotion:
			h.throwSplashPotion(players, pl, e.slot)
		case evFillBottle:
			h.fillBottle(players, pl, e.slot)
		case evBucketFill:
			h.bucketFill(players, pl, e.slot)
		}
	}
}

func setOffhand(pl *tracked, st invStack) {
	pl.offhand = st
	pl.p.setOffhand(st.item)
}

// Throwables and pearls go from the hand that used them: vanilla's
// Item.use gets the stack of the hand the client named. A pearl held only
// in the offhand used to be refused (the throw searched the main
// inventory), and a snowball thrown from the offhand came out of the
// first stack of snowballs in the inventory instead.
func TestOffhandThrowsTakeFromTheOffhand(t *testing.T) {
	h, players, pl, r := offhandUseRig(t)
	setOffhand(pl, invStack{item: itemEnderPearl, count: 2})
	useHand(h, players, r, handOffhand)
	if pl.offhand.count != 1 {
		t.Fatalf("the offhand pearl should be thrown: offhand %+v", pl.offhand)
	}
	if len(h.arrows) != 1 {
		t.Fatalf("one pearl in flight, have %d", len(h.arrows))
	}

	setOffhand(pl, invStack{item: itemSnowball, count: 3})
	pl.inv.slots[5] = invStack{item: itemSnowball, count: 16}
	useHand(h, players, r, handOffhand)
	if pl.offhand.count != 2 || pl.inv.slots[5].count != 16 {
		t.Fatalf("the snowball comes from the offhand: offhand %d, slot 5 %d", pl.offhand.count, pl.inv.slots[5].count)
	}

	setOffhand(pl, invStack{item: itemXPBottle, count: 1})
	useHand(h, players, r, handOffhand)
	if pl.offhand.count != 0 {
		t.Fatalf("the offhand bottle o' enchanting should be thrown: %+v", pl.offhand)
	}

	setOffhand(pl, potionStackIn(itemSplashPotion, potWater))
	useHand(h, players, r, handOffhand)
	if pl.offhand.count != 0 {
		t.Fatalf("the offhand splash potion should be thrown: %+v", pl.offhand)
	}

	// A main-hand use with nothing throwable there throws nothing.
	before := len(h.arrows)
	setOffhand(pl, invStack{item: itemSnowball, count: 3})
	r.Action(attachproto.UseItem{Hand: 0})
	for len(h.events) > 0 {
		h.useItemEvent(players, <-h.events)
	}
	if len(h.arrows) != before || pl.offhand.count != 3 {
		t.Fatalf("a main-hand pickaxe use threw something: arrows %d→%d, offhand %d", before, len(h.arrows), pl.offhand.count)
	}
}

// A bow in the offhand draws, shoots and wears there, with the arrow from
// the main hand (ProjectileWeaponItem.getHeldProjectile reads the offhand,
// then the main hand, before the inventory).
func TestOffhandBowDrawsAndWearsInTheOffhand(t *testing.T) {
	h, players, pl, r := offhandUseRig(t)
	arrows := invStack{item: itemArrowAmmo, count: 8}
	pl.inv.slots[0] = arrows
	pl.p.setHotbarSlot(0, arrows.item)
	pl.inv.slots[9] = invStack{item: itemArrowAmmo, count: 64}
	setOffhand(pl, invStack{item: itemBow, count: 1})
	h.tick.Store(100)
	useHand(h, players, r, handOffhand)
	if pl.drawingAt == 0 {
		t.Fatal("an offhand bow should draw")
	}
	h.tick.Store(100 + bowFullDraw)
	h.releaseDraw(players, pl)
	if len(h.arrows) != 1 {
		t.Fatalf("the full draw should loose one arrow, have %d", len(h.arrows))
	}
	if pl.inv.slots[0].count != 7 || pl.inv.slots[9].count != 64 {
		t.Fatalf("the arrow comes from the main hand: hand %d, slot 9 %d", pl.inv.slots[0].count, pl.inv.slots[9].count)
	}
	if pl.offhand.dmg != 1 {
		t.Fatalf("the offhand bow takes the shot's wear, dmg %d", pl.offhand.dmg)
	}
}

// A glass bottle held in the offhand fills from the water it looks at, and
// the last bottle turns into the water bottle in that hand
// (ItemUtils.createFilledResult).
func TestOffhandBottleFillsInTheOffhand(t *testing.T) {
	h, players, pl, r := offhandUseRig(t)
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 4; z++ {
			h.world.SetBlock(x, 178, z, worldgen.WaterBase)
		}
	}
	pl.pitch = 60 // looking down into the pool
	setOffhand(pl, invStack{item: itemGlassBottle, count: 1})
	useHand(h, players, r, handOffhand)
	if pl.offhand.item != itemPotion || pl.offhand.potion != potWater {
		t.Fatalf("the offhand bottle should fill in place: %+v", pl.offhand)
	}
}
