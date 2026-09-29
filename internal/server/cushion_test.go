package server

import (
	"encoding/json"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

var (
	itemWhiteCushion = int32(itemByName["white_cushion"])
	itemRedCushion   = int32(itemByName["red_cushion"])
)

// cushionFixture is a survival player beside a stone block in the sky
// (force-loaded), holding red cushions.
func cushionFixture(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	h.world.SetBlock(2, 179, 2, worldgen.BlockBase("stone"))
	pl := survPlayer(h)
	pl.p.eid = 9000 // clear of the ids the hub hands out
	pl.x, pl.y, pl.z = 3.5, 180, 4.5
	pl.yaw = 100
	pl.p.setHotbarSlot(0, itemRedCushion)
	pl.inv.slots[0] = invStack{item: itemRedCushion, count: 3}
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	return h, players, pl
}

// placeOnStone clicks the top of the stone with the held cushion.
func placeOnStone(h *hub, players map[int32]*tracked, pl *tracked) *cushion {
	before := map[int32]bool{}
	for eid := range h.cushions {
		before[eid] = true
	}
	h.useOnEvent(players, evPlaceCushion{eid: pl.p.eid, x: 2, y: 179, z: 2, up: true, hitY: 180})
	for eid, c := range h.cushions {
		if !before[eid] {
			return c
		}
	}
	return nil
}

// CushionItem.useOn: a click on a top face places a cushion at the cell's
// centre and the height clicked, turned to the nearest quarter of the
// player's facing, and costs one; a second cushion will not go in the same
// spot, and a side face takes none.
func TestCushionPlacesOnATopFace(t *testing.T) {
	h, players, pl := cushionFixture(t)
	c := placeOnStone(h, players, pl)
	if c == nil {
		t.Fatal("no cushion was placed")
	}
	if c.x != 2.5 || c.y != 180 || c.z != 2.5 || c.yaw != 90 || c.item != itemRedCushion {
		t.Fatalf("cushion %+v, want (2.5,180,2.5) yaw 90, red", c)
	}
	if pl.inv.slots[0].count != 2 {
		t.Fatalf("placing should cost one cushion, %d left", pl.inv.slots[0].count)
	}
	if placeOnStone(h, players, pl) != nil || len(h.cushions) != 1 {
		t.Fatal("a second cushion went into the same spot")
	}
	h.useOnEvent(players, evPlaceCushion{eid: pl.p.eid, x: 2, y: 179, z: 2, up: false, hitY: 179.5})
	if len(h.cushions) != 1 {
		t.Fatal("a cushion went onto a side face")
	}
	// In mid-air there is nothing to rest on.
	h.useOnEvent(players, evPlaceCushion{eid: pl.p.eid, x: 6, y: 185, z: 6, up: true, replacing: true, hitY: 185})
	if len(h.cushions) != 1 {
		t.Fatal("a cushion was placed with nothing under it")
	}
}

// The tracker spawns it as a cushion (the 26.2 stand-in is the gateway's
// business) and holds back DATA_COLOR, which the gateways cannot carry yet.
func TestCushionIsTrackedAsAnEntity(t *testing.T) {
	h, players, pl := cushionFixture(t)
	c := placeOnStone(h, players, pl)
	drainEvs(pl.p)
	h.syncTracking(players)
	added := false
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.EntityAdd:
			if e.EID == c.eid && e.Type == int32(entityCushion) && e.Yaw == 90 {
				added = true
			}
		case attachproto.EntityMeta:
			if e.EID == c.eid && len(e.Meta) > 0 && e.Meta[0] == metaIndexCushionColor {
				t.Fatal("DATA_COLOR went out while it is gated")
			}
		}
	}
	if !added {
		t.Fatal("the cushion was never spawned for the viewer")
	}
}

// Cushion.interact: a click sits the player on it, unless they sneak or it
// is taken; shift stands them up on top of it.
func TestCushionSitAndGetUp(t *testing.T) {
	h, players, pl := cushionFixture(t)
	c := placeOnStone(h, players, pl)
	h.onInteractMob(players, pl, evInteractMob{eid: pl.p.eid, target: c.eid, sneak: true})
	if pl.ridingEID != 0 {
		t.Fatal("a sneaking click sat the player down")
	}
	drainEvs(pl.p)
	h.onInteractMob(players, pl, evInteractMob{eid: pl.p.eid, target: c.eid})
	if pl.ridingEID != c.eid || c.rider != pl.p.eid {
		t.Fatalf("the click should seat the player: riding %d rider %d", pl.ridingEID, c.rider)
	}
	sat := false
	for _, ev := range drainEvs(pl.p) {
		if s, ok := ev.(attachproto.Sound); ok && s.Name == "minecraft:entity.cushion.sit" {
			sat = true
		}
	}
	if !sat {
		t.Fatal("sitting down is silent")
	}
	other := survPlayer(h)
	other.p = newPlayer(9001, "other", [16]byte{3})
	other.x, other.y, other.z = 2.5, 180, 3.5
	players[other.p.eid] = other
	h.onInteractMob(players, other, evInteractMob{eid: other.p.eid, target: c.eid})
	if other.ridingEID != 0 {
		t.Fatal("two players on one cushion")
	}
	h.dismount(players, pl) // shift, through evInput/evDismount
	if pl.ridingEID != 0 || c.rider != 0 {
		t.Fatal("shift should stand the player up")
	}
	if pl.y != c.y+cushionHeight {
		t.Fatalf("the player should stand on top of the cushion, y %v", pl.y)
	}
}

// A blow breaks it and drops its item (with its name), but not for a
// creative player, and an adventure player cannot break it at all.
func TestCushionBreaksAndDrops(t *testing.T) {
	h, players, pl := cushionFixture(t)
	c := placeOnStone(h, players, pl)
	pl.gamemode = gmAdventure
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: c.eid})
	if h.cushions[c.eid] == nil {
		t.Fatal("an adventure player broke a cushion")
	}
	pl.gamemode = gmSurvival
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: c.eid})
	if h.cushions[c.eid] != nil {
		t.Fatal("a blow should break the cushion")
	}
	dropped := 0
	for _, it := range h.items {
		if it.item == itemRedCushion {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("breaking should drop the red cushion, dropped %d", dropped)
	}
	pl.gamemode = gmCreative
	c = placeOnStone(h, players, pl)
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: c.eid})
	if h.cushions[c.eid] != nil {
		t.Fatal("a creative blow should break it too")
	}
	for _, it := range h.items {
		if it.item == itemRedCushion {
			dropped--
		}
	}
	if dropped != 0 {
		t.Fatal("a creative player's break dropped the item")
	}
}

// Every hundred ticks it checks what holds it up: with the block gone it
// breaks and drops, and its sitter stands.
func TestCushionFallsWithItsBlock(t *testing.T) {
	h, players, pl := cushionFixture(t)
	c := placeOnStone(h, players, pl)
	h.onInteractMob(players, pl, evInteractMob{eid: pl.p.eid, target: c.eid})
	h.world.SetBlock(2, 179, 2, worldgen.Air)
	for i := 0; i < cushionCheckEvery; i++ {
		h.tickCushions(players)
	}
	if h.cushions[c.eid] == nil {
		t.Fatal("the cushion went before its check came round")
	}
	h.tickCushions(players)
	if h.cushions[c.eid] != nil {
		t.Fatal("a cushion with nothing under it should break on its check")
	}
	if pl.ridingEID != 0 {
		t.Fatal("the sitter is still seated on a broken cushion")
	}
	found := false
	for _, it := range h.items {
		found = found || it.item == itemRedCushion
	}
	if !found {
		t.Fatal("the broken cushion dropped nothing")
	}
}

// The store keeps a cushion's place, facing, colour and name; the colour
// is its item, which the id migration carries.
func TestCushionPersists(t *testing.T) {
	cs := newContainerStore("")
	cs.recordCushions(map[int32]*cushion{1: {eid: 1, dim: 1, x: 2.5, y: 64.5, z: -3.5, yaw: 180, item: itemWhiteCushion, name: "Seat"}})
	raw, err := json.Marshal(cs.m)
	if err != nil {
		t.Fatal(err)
	}
	back := newContainerStore("")
	if err := json.Unmarshal(raw, &back.m); err != nil {
		t.Fatal(err)
	}
	next := int32(100)
	loaded := back.loadCushions(func() int32 { next++; return next })
	if len(loaded) != 1 {
		t.Fatalf("loaded %d cushions", len(loaded))
	}
	for _, c := range loaded {
		if c.dim != 1 || c.x != 2.5 || c.y != 64.5 || c.z != -3.5 || c.yaw != 180 || c.item != itemWhiteCushion || c.name != "Seat" {
			t.Fatalf("round trip: %+v", c)
		}
	}
}
