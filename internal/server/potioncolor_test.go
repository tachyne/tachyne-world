package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// The mix is PotionContents.getColorOptional: each channel averaged over the
// effects, weighted by level. A single-effect potion is simply that effect's
// registered colour, and level does not change it — Swiftness II is the same
// cyan as Swiftness.
func TestPotionColorMixesEffectColors(t *testing.T) {
	for _, tc := range []struct {
		kind int8
		want int32
		name string
	}{
		{potSwiftness, 0x33EBFF, "swiftness"},
		{potStrongSwiftness, 0x33EBFF, "swiftness II"},
		{potHealing, 0xF82423, "healing"},
		{potPoison, 0x87A363, "poison"},
		{potTurtleMaster, 0x8D82E6, "turtle master"}, // slowness IV + resistance III
		{potWater, potionColorBase, "water bottle"},
		{potAwkward, potionColorBase, "awkward"},
	} {
		if got := potionColor(tc.kind); got != tc.want {
			t.Errorf("%s: colour %06X, want %06X", tc.name, got, tc.want)
		}
	}
}

// Every effect the engine can name has a colour: a gap would make any potion
// carrying it mix wrong and drift silently.
func TestEveryEffectHasAColor(t *testing.T) {
	for name, id := range effectNames {
		if _, ok := effectColorByName[name]; !ok {
			t.Errorf("effect %q (%d) has no registered colour", name, id)
		}
	}
	if len(effectColorByID) != len(effectNames) {
		t.Errorf("colour-by-id has %d entries, effectNames %d", len(effectColorByID), len(effectNames))
	}
}

// A potion stack's components must be exactly what the gateway's slot copier
// expects: it walks them to find where each one ends, so a payload it cannot
// parse loses every component after it too.
func TestPotionStackComponentsWalkable(t *testing.T) {
	for _, kind := range []int8{potWater, potSwiftness, potTurtleMaster, potStrongHarming} {
		st := potionStack(kind)
		body := appendStack(nil, st)
		item, count, ok := protocol.ReadSlot770(bytes.NewReader(body))
		if !ok {
			t.Fatalf("kind %d: the slot copier rejected the stack", kind)
		}
		if item != st.item || count != 1 {
			t.Errorf("kind %d: got item %d count %d, want %d 1", kind, item, count, st.item)
		}
	}
}

// …and they carry the brew's real effects, which is what colours the liquid
// and fills the tooltip on the client.
func TestPotionComponentCarriesEffects(t *testing.T) {
	// No display name on this one: potion_contents is then the stack's only
	// component, so the walk below needs no other component's shape.
	st := invStack{item: itemPotion, count: 1, potion: potTurtleMaster}
	r := bytes.NewReader(appendStack(nil, st))
	protocol.ReadVarInt(r) // count
	protocol.ReadVarInt(r) // item
	add, _ := protocol.ReadVarInt(r)
	protocol.ReadVarInt(r) // remove count
	var found bool
	for i := int32(0); i < add; i++ {
		cid, err := protocol.ReadVarInt(r)
		if err != nil {
			t.Fatal(err)
		}
		if cid != componentPotionContents {
			t.Fatalf("component %d, want potion_contents (%d)", cid, componentPotionContents)
		}
		found = true
		if b, _ := r.ReadByte(); b != 0 {
			t.Error("a potion holder rode along; the potion registry is not translated")
		}
		if b, _ := r.ReadByte(); b != 0 {
			t.Error("a custom colour rode along; the client should mix the effects itself")
		}
		n, _ := protocol.ReadVarInt(r)
		want := potionEffects(potTurtleMaster)
		if int(n) != len(want) {
			t.Fatalf("%d effects on the wire, want %d", n, len(want))
		}
		for j, e := range want {
			id, _ := protocol.ReadVarInt(r)
			amp, _ := protocol.ReadVarInt(r)
			ticks, _ := protocol.ReadVarInt(r)
			if id != e.id+1 {
				t.Errorf("effect %d: holder %d, want %d", j, id, e.id+1)
			}
			if amp != int32(e.amp) {
				t.Errorf("effect %d: amplifier %d, want %d", j, amp, e.amp)
			}
			if ticks != int32(e.secs*20) {
				t.Errorf("effect %d: %d ticks, want %d", j, ticks, e.secs*20)
			}
			var flags [4]byte
			r.Read(flags[:])
		}
		break
	}
	if !found {
		t.Fatal("the potion stack carries no potion_contents component")
	}
}

// An instant potion splashes with the other particle of the pair, the way
// vanilla picks between 2002 and 2007.
func TestPotionIsInstant(t *testing.T) {
	for _, kind := range []int8{potHealing, potStrongHealing, potHarming} {
		if !potionIsInstant(kind) {
			t.Errorf("kind %d should be instant", kind)
		}
	}
	for _, kind := range []int8{potSwiftness, potPoison, potWater, potRegen} {
		if potionIsInstant(kind) {
			t.Errorf("kind %d should not be instant", kind)
		}
	}
}

// A suspicious stew and a worked-on tool carry their components too, and
// both must walk cleanly — a payload the copier cannot parse takes every
// component after it down with it.
func TestStewAndRepairCostComponents(t *testing.T) {
	for _, st := range []invStack{
		{item: itemSuspiciousStew, count: 1, stew: 1},
		{item: itemSuspiciousStew, count: 1, stew: int8(len(stewEffects))},
		{item: itemByName["diamond_pickaxe"], count: 1, repairCost: 7},
		{item: itemSuspiciousStew, count: 1, stew: 99}, // orphaned code: no component
	} {
		body := appendStack(nil, st)
		if _, _, ok := protocol.ReadSlot770(bytes.NewReader(body)); !ok {
			t.Fatalf("%+v: the slot copier rejected the stack", st)
		}
	}
	// The effect and its duration are the flower's own row.
	want := stewEffects[0]
	r := bytes.NewReader(appendStack(nil, invStack{item: itemSuspiciousStew, count: 1, stew: 1}))
	for i := 0; i < 4; i++ {
		protocol.ReadVarInt(r)
	}
	if cid, _ := protocol.ReadVarInt(r); cid != componentStewEffects {
		t.Fatalf("component %d, want suspicious_stew_effects (%d)", cid, componentStewEffects)
	}
	n, _ := protocol.ReadVarInt(r)
	id, _ := protocol.ReadVarInt(r)
	dur, _ := protocol.ReadVarInt(r)
	if n != 1 || id != want.effect+1 || dur != int32(want.secs*20) {
		t.Errorf("stew carries %d×(%d, %d ticks), want 1×(%d, %d)", n, id, dur, want.effect+1, int32(want.secs*20))
	}
}

// A shulker box picked up full carries its contents to the client, which is
// what puts them in the item's tooltip. The list is positional, so all 27
// slots ride — and the whole thing has to walk cleanly, because the copier
// recurses into every nested stack.
func TestShulkerBoxCarriesItsContents(t *testing.T) {
	h := newHub(world.New(1))
	pos := simPos{blockPos: blockPos{3, 70, 3}}
	c := &chest{}
	c.slots[0] = invStack{item: itemByName["diamond"], count: 5}
	c.slots[26] = invStack{item: itemPotion, count: 1, potion: potSwiftness, name: potionName(potSwiftness, itemPotion)}
	h.chests[pos] = c
	boxID := h.stowShulkerBox(pos)
	if boxID == 0 {
		t.Fatal("a box with contents should mint an id")
	}

	st := invStack{item: itemByName["shulker_box"], count: 1, boxID: boxID}
	body := appendStack(nil, st)
	if _, _, ok := protocol.ReadSlot770(bytes.NewReader(body)); !ok {
		t.Fatal("the slot copier rejected the box")
	}

	r := bytes.NewReader(body)
	for i := 0; i < 4; i++ {
		protocol.ReadVarInt(r)
	}
	if cid, _ := protocol.ReadVarInt(r); cid != componentContainer {
		t.Fatalf("component %d, want container (%d)", cid, componentContainer)
	}
	n, _ := protocol.ReadVarInt(r)
	if n != 27 {
		t.Errorf("%d slots on the wire, want all 27", n)
	}
	// An empty box says nothing: there is no component to send.
	empty := appendStack(nil, invStack{item: itemByName["shulker_box"], count: 1})
	re := bytes.NewReader(empty)
	protocol.ReadVarInt(re)
	protocol.ReadVarInt(re)
	if add, _ := protocol.ReadVarInt(re); add != 0 {
		t.Errorf("an empty box carries %d components, want none", add)
	}
}
