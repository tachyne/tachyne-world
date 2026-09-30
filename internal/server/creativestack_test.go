package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// Every stack stackComponents can write reads back through creativeStack to
// the same stack — or, where a store holds the contents (books, bundles,
// boxes), to a stack that writes the very same components.
func TestCreativeStackRoundTrip(t *testing.T) {
	h := newTestHub(world.New(1))
	burst := fireworkBurst{Shape: burstLargeBall, Colors: []int32{0xB3312C, 0x3B511A}, Fade: []int32{0xF0F0F0}, Trail: true}
	star := fireworkBurst{Shape: burstCreeper, Colors: []int32{0x41CD34}, Twinkle: true}
	book := h.books.create(savedBook{Title: "Notes", Author: "alice", Gen: 1, Pages: []string{"one", "two"}})
	quill := h.books.create(savedBook{Pages: []string{"draft"}})
	bundle := h.bundles.mint()
	h.bundles.set(bundle, []invStack{{item: itemByName["diamond"], count: 3},
		{item: itemByName["iron_sword"], count: 1, dmg: 4, name: "Poker"}})
	box := h.boxes.mint()
	var boxed chest
	boxed.slots[0] = invStack{item: itemByName["stone"], count: 64}
	boxed.slots[13] = invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchMending, lvl: 1}}}
	h.boxes.set(box, boxed)

	canBreak, _ := parseAdvPredicate("minecraft:can_break", []any{
		map[string]any{"blocks": []any{"stone", "dirt"}},
		map[string]any{"blocks": "#minecraft:logs", "state": map[string]any{"axis": "y", "age": map[string]any{"max": int64(3)}}},
	})
	canPlace, _ := parseAdvPredicate("minecraft:can_place_on", map[string]any{"state": map[string]any{"lit": true}})

	cases := []struct {
		name  string
		st    invStack
		exact bool // the decoded stack is field-for-field the same
	}{
		{"enchanted named sword", invStack{item: itemByName["diamond_sword"], count: 1, dmg: 10,
			ench: enchList{{id: enchSharpness, lvl: 5}, {id: enchUnbreaking, lvl: 3}}, name: "Excalibur", repairCost: 3}, true},
		{"enchanted book", invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchMending, lvl: 1}}}, true},
		{"potion of swiftness", invStack{item: itemPotion, count: 1, potion: potSwiftness, name: potionName(potSwiftness, itemPotion)}, true},
		{"awkward potion", invStack{item: itemPotion, count: 1, potion: potAwkward, name: potionName(potAwkward, itemPotion)}, true},
		{"splash of strong healing", invStack{item: itemSplashPotion, count: 1, potion: potStrongHealing, name: potionName(potStrongHealing, itemSplashPotion)}, true},
		{"ominous bottle", invStack{item: itemOminousBottle, count: 1, potion: 3}, true},
		{"dyed leather", invStack{item: itemByName["leather_chestplate"], count: 1, color: 0xA06540}, true},
		{"patterned banner", invStack{item: itemByName["red_banner"], count: 1,
			pats: [6]bannerLayer{{patPlus1: 3, color: 4}, {patPlus1: 9, color: 15}}}, true},
		{"decorated shield", invStack{item: itemByName["shield"], count: 1, shieldBase: 5,
			pats: [6]bannerLayer{{patPlus1: 2, color: 0}}}, true},
		{"trimmed chestplate", invStack{item: itemByName["diamond_chestplate"], count: 1, trimMat: 2, trimPat: 4}, true},
		{"goat horn", invStack{item: itemGoatHorn, count: 1, instrument: 5}, true},
		{"rocket", invStack{item: itemFireworkRocket, count: 3, flight: 2, starID: internBursts([]fireworkBurst{burst, star})}, true},
		{"firework star", invStack{item: itemFireworkStar, count: 1, starID: internBursts([]fireworkBurst{star})}, true},
		{"suspicious stew", invStack{item: itemByName["suspicious_stew"], count: 1, stew: 1}, true},
		{"decorated pot", invStack{item: itemDecoratedPot, count: 1,
			sherds: potSherds{itemByName["angler_pottery_sherd"], 0, 0, itemByName["skull_pottery_sherd"]}}, true},
		{"lodestone compass", invStack{item: itemCompass, count: 1,
			lode: lodeTracker{has: true, target: true, x: 10, y: -40, z: -2000, dim: dimNether}}, true},
		{"filled map", invStack{item: itemByName["filled_map"], count: 1, mapID: 7}, true},
		{"loaded crossbow", invStack{item: itemCrossbow, count: 1, load: xbowLoad{item: itemArrow, n: 3}}, true},
		{"ominous banner", invStack{item: itemWhiteBanner, count: 1, ominous: true}, true},
		{"lore", invStack{item: itemByName["stick"], count: 1, tags: itemTags{lore: "first line\nsecond line"}}, true},
		{"salmon bucket", invStack{item: itemByName["salmon_bucket"], count: 1, cube: cubeContent{variant: 2, health: 4}}, true},
		{"tropical fish bucket", invStack{item: itemByName["tropical_fish_bucket"], count: 1,
			cube: cubeContent{variant: (1 | 3<<16 | 2<<24) + 1, health: 3}}, true},
		{"axolotl bucket", invStack{item: itemByName["axolotl_bucket"], count: 1, cube: cubeContent{variant: 3, health: 15, age: -100}}, true},
		{"statue pose", invStack{item: itemByName["copper_golem_statue"], count: 1, golemPose: 2}, true},
		{"player head", invStack{item: itemPlayerHead, count: 1, profile: profileString(testProfile())}, true},
		{"name-only head", invStack{item: itemPlayerHead, count: 1, profile: profileString(gameProfile{name: "jeb_"})}, true},
		{"adventure pickaxe", invStack{item: itemByName["diamond_pickaxe"], count: 1,
			tags: itemTags{unbreakable: true, canBreak: canBreak, canPlace: canPlace}}, true},
		{"written book", invStack{item: itemWrittenBook, count: 1, bookID: book}, false},
		{"book and quill", invStack{item: itemWritableBook, count: 1, bookID: quill}, false},
		{"bundle", invStack{item: itemByName["bundle"], count: 1, bundleID: bundle}, false},
		{"shulker box", invStack{item: itemByName["shulker_box"], count: 1, boxID: box}, false},
	}
	for _, c := range cases {
		comps := stackComponents(c.st)
		got := h.creativeStack(c.st.item, c.st.count, comps)
		if again := stackComponents(got); !bytes.Equal(again, comps) {
			t.Errorf("%s: components differ after the round trip:\n got %x\nwant %x", c.name, again, comps)
		}
		if c.exact && got != c.st {
			t.Errorf("%s: read back %+v, want %+v", c.name, got, c.st)
		}
	}
}

// A potion picked from the creative menu carries the potion holder rather
// than effects: it reads as that potion, labelled as a brewed one is.
func TestCreativePotionHolder(t *testing.T) {
	h := newTestHub(world.New(1))
	// potion_contents: holder present (strong_healing = 25), no colour, no
	// effects, no custom name.
	pc := []byte{1}
	pc = protocol.AppendVarInt(pc, 25)
	pc = append(pc, 0)
	pc = protocol.AppendVarInt(pc, 0)
	pc = append(pc, 0)
	patch := protocol.AppendVarInt(protocol.AppendVarInt(nil, 1), 0)
	patch = protocol.AppendVarInt(patch, componentPotionContents)
	patch = append(patch, pc...)
	got := h.creativeStack(itemSplashPotion, 1, patch)
	if got.potion != potStrongHealing || got.name != potionName(potStrongHealing, itemSplashPotion) {
		t.Fatalf("picked potion = kind %d named %q, want strong healing", got.potion, got.name)
	}
}

// An armor stand's entity_data (its flags) comes through as the stand tags
// it places with.
func TestCreativeArmorStandEntityData(t *testing.T) {
	h := newTestHub(world.New(1))
	tag := protocol.NBTRoot()
	tag = protocol.NBTString(tag, "id", "minecraft:armor_stand")
	tag = protocol.NBTByte(tag, "ShowArms", 1)
	tag = protocol.NBTByte(tag, "Small", 1)
	tag = protocol.NBTEnd(tag)
	patch := protocol.AppendVarInt(protocol.AppendVarInt(nil, 1), 0)
	patch = protocol.AppendVarInt(patch, componentEntityData)
	patch = append(patch, tag...)
	got := h.creativeStack(itemArmorStand, 1, patch)
	want, _ := standTagsFromEntityData(map[string]any{"id": "minecraft:armor_stand", "ShowArms": int64(1), "Small": int64(1)})
	if got.standTags == "" || got.standTags != want {
		t.Fatalf("stand tags = %q, want %q", got.standTags, want)
	}
}

// Through the entry path: a creative client's set_creative_mode_slot with a
// component patch puts the whole stack in the slot, not the bare item.
func TestCreativeSlotKeepsComponents(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative)}
	w.ForceLoad(0, 0, 1)
	startHub(t, h)
	p := newPlayer(h.allocEID(), "maker", [16]byte{7})
	sy := w.SurfaceY(0, 0)
	h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
	waitJoined(t, h, "maker")

	book := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSharpness, lvl: 4}}}
	potion := invStack{item: itemPotion, count: 1, potion: potLongSwiftness, name: potionName(potLongSwiftness, itemPotion)}
	s.applyCreativeSlot(p, 36, book.item, book.count, "", stackComponents(book))
	s.applyCreativeSlot(p, 37, potion.item, potion.count, "", stackComponents(potion))
	got := make(chan [2]invStack, 1)
	s.onHub(func(players map[int32]*tracked) {
		if tr := players[p.eid]; tr != nil {
			got <- [2]invStack{tr.inv.slots[0], tr.inv.slots[1]}
			return
		}
		got <- [2]invStack{}
	})
	select {
	case sl := <-got:
		if sl[0] != book {
			t.Errorf("hotbar 0 = %+v, want the enchanted book %+v", sl[0], book)
		}
		if sl[1] != potion {
			t.Errorf("hotbar 1 = %+v, want the potion %+v", sl[1], potion)
		}
	case <-time.After(hubTestWait):
		t.Fatal("the hub never answered")
	}
}
