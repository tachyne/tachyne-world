package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/render770"
	"github.com/tachyne/tachyne-world/internal/world"
)

// slotOnClient renders st as set_slot and walks it through the gateway's
// translation to a client version, returning a reader at the slot's
// component entries (past count, item, added and removed counts) and the
// number added. A slot the copier refused would keep canonical component
// ids, which the id checks in the tests catch.
func slotOnClient(t *testing.T, st invStack, version int32) (*bytes.Reader, int32, int32) {
	t.Helper()
	p := render770.WindowSlot(attachproto.WindowSlot{ID: 0, StateID: 1, Slot: 36, Item: stackEv(st)})
	_, body, drop := protocol.TranslatorFor(version).Clientbound(protocol.StatePlay, p.ID, p.Body)
	if drop {
		t.Fatalf("v%d: set_slot dropped", version)
	}
	r := bytes.NewReader(body)
	protocol.ReadVarInt(r) // window
	protocol.ReadVarInt(r) // state id
	r.ReadByte()           // slot (i16)
	r.ReadByte()
	protocol.ReadVarInt(r) // count
	item, _ := protocol.ReadVarInt(r)
	added, _ := protocol.ReadVarInt(r)
	protocol.ReadVarInt(r) // removed
	return r, item, added
}

// The ominous banner reaches the client with vanilla's component set: the
// eight layers (so it is not a white banner any more), the layer list
// hidden, its name and uncommon rarity. 26.2 numbers banner_patterns 72
// and 26.3 numbers it 74; tooltip_display, item_name and rarity are 18, 9
// and 12 on both.
func TestOminousBannerComponentsReachTheClient(t *testing.T) {
	st := ominousBanner()
	if _, _, ok := protocol.ReadSlot770(bytes.NewReader(appendStack(nil, st))); !ok {
		t.Fatal("the slot copier rejected the ominous banner")
	}
	for v, banner := range map[int32]int32{776: 72, 777: 74} {
		r, _, added := slotOnClient(t, st, v)
		if added != 4 {
			t.Fatalf("v%d: %d components, want 4", v, added)
		}
		if cid, _ := protocol.ReadVarInt(r); cid != banner {
			t.Fatalf("v%d: first component %d, want banner_patterns %d", v, cid, banner)
		}
		if n, _ := protocol.ReadVarInt(r); n != 8 {
			t.Fatalf("v%d: %d layers, want 8", v, n)
		}
		first, _ := protocol.ReadVarInt(r)
		dye, _ := protocol.ReadVarInt(r)
		if first != int32(bannerPatternIDs["minecraft:rhombus"])+1 || dye != 9 { // rhombus, cyan
			t.Errorf("v%d: first layer (%d, %d), want the cyan rhombus", v, first, dye)
		}
		for i := 1; i < 8; i++ {
			protocol.ReadVarInt(r)
			protocol.ReadVarInt(r)
		}
		if cid, _ := protocol.ReadVarInt(r); cid != 18 {
			t.Fatalf("v%d: tooltip_display id %d, want 18", v, cid)
		}
		hide, _ := r.ReadByte()
		n, _ := protocol.ReadVarInt(r)
		hidden, _ := protocol.ReadVarInt(r)
		if hide != 0 || n != 1 || hidden != banner {
			t.Errorf("v%d: tooltip_display (%d, %d×%d), want the layer list hidden", v, hide, n, hidden)
		}
		if cid, _ := protocol.ReadVarInt(r); cid != 9 {
			t.Fatalf("v%d: item_name id %d, want 9", v, cid)
		}
		if err := protocol.SkipNetworkNBT(r); err != nil {
			t.Fatalf("v%d: item_name: %v", v, err)
		}
		if cid, _ := protocol.ReadVarInt(r); cid != 12 {
			t.Fatalf("v%d: rarity id %d, want 12", v, cid)
		}
		if rarity, _ := protocol.ReadVarInt(r); rarity != rarityUncommon {
			t.Errorf("v%d: rarity %d, want uncommon", v, rarity)
		}
	}
	if !bytes.Contains(appendStack(nil, st), []byte("block.minecraft.ominous_banner")) {
		t.Error("the banner is not named")
	}
}

// The load lives on the crossbow: a second crossbow does not fire the first
// one's bolt, and the first is still loaded when it comes back to hand.
func TestCrossbowLoadStaysWithItsCrossbow(t *testing.T) {
	h, pl, players := xbowSetup()
	h.useXbow(players, pl)
	h.tick.Add(xbowBaseCharge)
	h.finishXbowCharge(players, pl)
	if pl.inv.slots[0].load.n != 1 || pl.inv.slots[0].load.item != itemArrowAmmo {
		t.Fatalf("the crossbow's load is %+v, want one arrow", pl.inv.slots[0].load)
	}

	pl.inv.slots[2] = invStack{item: itemCrossbow, count: 1}
	pl.p.setHeldSlot(2)
	h.useXbow(players, pl)
	if len(h.arrows) != 0 {
		t.Fatal("an empty crossbow fired the other crossbow's bolt")
	}
	if pl.xbowAt == 0 {
		t.Fatal("an empty crossbow should begin charging")
	}
	pl.xbowAt = 0

	pl.p.setHeldSlot(0)
	h.useXbow(players, pl)
	if len(h.arrows) != 1 {
		t.Fatalf("the loaded crossbow loosed %d bolts, want 1", len(h.arrows))
	}
	if pl.inv.slots[0].load != (xbowLoad{}) {
		t.Errorf("the crossbow is still loaded after firing: %+v", pl.inv.slots[0].load)
	}
}

// A loaded crossbow sends charged_projectiles — Multishot's three stacks,
// each a 26.x item template (item before count) — so the client draws it
// loaded and lists the projectile. A tipped arrow keeps its potion.
func TestLoadedCrossbowSendsChargedProjectiles(t *testing.T) {
	ammo := invStack{item: itemTippedArrow, count: 1, potion: potSwiftness}
	st := invStack{item: itemCrossbow, count: 1, load: loadOf(ammo, 3)}
	if _, _, ok := protocol.ReadSlot770(bytes.NewReader(appendStack(nil, st))); !ok {
		t.Fatal("the slot copier rejected the loaded crossbow")
	}
	for v, charged := range map[int32]int32{776: 49, 777: 51} {
		r, _, added := slotOnClient(t, st, v)
		if added != 1 {
			t.Fatalf("v%d: %d components, want 1", v, added)
		}
		if cid, _ := protocol.ReadVarInt(r); cid != charged {
			t.Fatalf("v%d: component %d, want charged_projectiles %d", v, cid, charged)
		}
		if n, _ := protocol.ReadVarInt(r); n != 3 {
			t.Fatalf("v%d: %d projectiles, want 3", v, n)
		}
		item, _ := protocol.ReadVarInt(r)
		count, _ := protocol.ReadVarInt(r)
		if item != protocol.RemapID(protocol.RegItem, v, itemTippedArrow) || count != 1 {
			t.Errorf("v%d: first projectile (item %d, count %d), want one tipped arrow", v, item, count)
		}
		if added, _ := protocol.ReadVarInt(r); added != 1 {
			t.Errorf("v%d: the tipped arrow carries %d components, want its potion", v, added)
		}
	}
}

// The load is saved with the stack: a restart keeps a crossbow loaded, and a
// loaded rocket keeps its flight and bursts.
func TestCrossbowLoadPersists(t *testing.T) {
	for _, l := range []xbowLoad{
		{item: itemArrowAmmo, n: 1},
		{item: itemTippedArrow, n: 3, potion: potSwiftness},
		{item: itemFireworkRocket, n: 1, flight: 3, starID: 12},
	} {
		st := invStack{item: itemCrossbow, count: 1, dmg: 4, load: l}
		if got := unpackStack(packStack(st)); got != st {
			t.Errorf("round trip %+v, want %+v", got.load, l)
		}
	}
	if got := unpackStack(packStack(invStack{item: itemCrossbow, count: 1})); got.load != (xbowLoad{}) {
		t.Errorf("an empty crossbow came back loaded: %+v", got.load)
	}
}

// A scooped tropical fish's bucket carries its pattern and colours, which is
// what the bucket's tooltip shows, and bucket_entity_data with its Health.
func TestTropicalFishBucketCarriesItsVariant(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 30, 70, 30)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	fish := h.spawnSpecies(players, entityTropicalFish, 0, 30.9, 70, 30.9)
	fish.variant, fish.variantSet = packTropicalVariant(tropicalPatterns[1], 11, 7), true
	slot := pl.p.heldSlot()
	pl.inv.slots[slot] = invStack{item: itemBucketH2O, count: 1}
	if !h.tryBucketMob(players, pl, fish) {
		t.Fatal("the fish was not scooped")
	}
	st := pl.inv.slots[slot]
	if _, _, ok := protocol.ReadSlot770(bytes.NewReader(appendStack(nil, st))); !ok {
		t.Fatal("the slot copier rejected the bucket")
	}
	pattern := tropicalPatterns[1].packed() & 0xffff
	for v, ids := range map[int32][4]int32{776: {89, 90, 91, 59}, 777: {95, 96, 97, 61}} {
		r, _, added := slotOnClient(t, st, v)
		if added != 4 {
			t.Fatalf("v%d: %d components, want 4", v, added)
		}
		for i, want := range []int32{pattern, 11, 7} {
			cid, _ := protocol.ReadVarInt(r)
			val, _ := protocol.ReadVarInt(r)
			if cid != ids[i] || val != want {
				t.Errorf("v%d: component %d = %d, want %d = %d", v, cid, val, ids[i], want)
			}
		}
		if cid, _ := protocol.ReadVarInt(r); cid != ids[3] {
			t.Fatalf("v%d: component %d, want bucket_entity_data %d", v, cid, ids[3])
		}
		rest := make([]byte, r.Len())
		r.Read(rest)
		if !bytes.Contains(rest, []byte("Health")) {
			t.Errorf("v%d: bucket_entity_data has no Health", v)
		}
	}
}

// A salmon's size and an axolotl's colour ride their buckets too.
func TestSalmonAndAxolotlBucketsCarryTheirVariant(t *testing.T) {
	for _, tc := range []struct {
		etype   int
		variant int32
		id777   int32
	}{{entitySalmon, salmonLarge, 93}, {entityAxolotl, axolotlBlue, 111}} {
		st := invStack{item: mobBucketBySpecies[tc.etype].item, count: 1, cube: cubeContent{variant: tc.variant + 1, health: 4}}
		r, _, added := slotOnClient(t, st, 777)
		if added != 2 {
			t.Fatalf("species %d: %d components, want 2", tc.etype, added)
		}
		cid, _ := protocol.ReadVarInt(r)
		val, _ := protocol.ReadVarInt(r)
		if cid != tc.id777 || val != tc.variant {
			t.Errorf("species %d: component %d = %d, want %d = %d", tc.etype, cid, val, tc.id777, tc.variant)
		}
	}
}
