package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /give with the rest of the item components the engine models, through the
// dispatcher: lore, unbreakable, can_break / can_place_on, trim, banner
// patterns, fireworks, a written book, a loaded crossbow and a bucketed
// mob's data; and ItemParser's refusals — an unknown component, a repeated
// one, and taking off one the engine does not model.
func TestGiveItemTagComponents(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, `give bob diamond_pickaxe[lore=["Line one",{text:"Line two"}],unbreakable={},can_break={blocks:"dandelion"},can_place_on=[{blocks:["stone","dirt"]}]]`)
	s.handleCommand(alice, `give bob iron_chestplate[trim={material:"minecraft:gold",pattern:"minecraft:coast"}]`)
	s.handleCommand(alice, `give bob white_banner[banner_patterns=[{pattern:"minecraft:stripe_top",color:"red"}]]`)
	s.handleCommand(alice, `give bob firework_rocket[fireworks={flight_duration:2b,explosions:[{shape:"star",colors:[I;16711680],has_trail:1b}]}]`)
	s.handleCommand(alice, `give bob written_book[written_book_content={title:"Tale",author:"Ann",pages:["one","two"]}]`)
	s.handleCommand(alice, `give bob crossbow[charged_projectiles=[{id:"minecraft:arrow",count:1}]]`)
	s.handleCommand(alice, `give bob axolotl_bucket[bucket_entity_data={Health:10f,Variant:2}]`)
	s.handleCommand(alice, `give bob diamond_sword[!damage]`)
	s.handleCommand(alice, `give bob stick[damage=5,!damage]`)
	s.handleCommand(alice, `give bob stick[nonsense=1]`)
	s.handleCommand(alice, `give bob stick[lore=["a"],lore=["b"]]`)
	s.handleCommand(alice, `give bob stick[!food]`)
	s.handleCommand(alice, `give bob stick[can_break={blocks:"stone",nbt:{}}]`)
	settle(t, h, logs, "C1")
	a := linesBetween(logs["alice"], "", "C1")
	for _, want := range []string{
		"Unknown item component 'minecraft:nonsense'",
		"Item component 'minecraft:lore' was repeated, but only one value can be specified",
		"Item component 'minecraft:damage' was repeated, but only one value can be specified",
		"The 'minecraft:food' component is not supported here",
		"The 'nbt' field of a 'minecraft:can_break' block predicate is not supported here",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		var bob *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" {
				bob = tr
			}
		}
		find := func(name string) invStack {
			for _, st := range bob.inv.slots {
				if st.item == itemByName[name] {
					return st
				}
			}
			t.Errorf("bob has no %s", name)
			return invStack{}
		}
		pick := find("diamond_pickaxe")
		if pick.tags.lore != "Line one\nLine two" || !pick.tags.unbreakable ||
			!advAllows(pick.tags.canBreak, worldgen.BlockBase("dandelion")) || advAllows(pick.tags.canBreak, worldgen.Stone) ||
			!advAllows(pick.tags.canPlace, worldgen.Stone) || !advAllows(pick.tags.canPlace, worldgen.Dirt) {
			t.Errorf("pickaxe tags %+v", pick.tags)
		}
		if _, wears := wearMax(pick); wears {
			t.Error("an unbreakable pickaxe still wears")
		}
		if got := unpackStack(packStack(pick)); got != pick {
			t.Errorf("the tags did not survive a save: %+v", got.tags)
		}
		if c := find("iron_chestplate"); c.trimMat != int8(registryIDs("minecraft:trim_material")["minecraft:gold"]+1) ||
			c.trimPat != int8(registryIDs("minecraft:trim_pattern")["minecraft:coast"]+1) {
			t.Errorf("trim %d %d", c.trimMat, c.trimPat)
		}
		if b := find("white_banner"); b.patCount() != 1 || int(b.pats[0].color) != dyeIndex("red") {
			t.Errorf("banner %+v", b.pats)
		}
		if r := find("firework_rocket"); r.flight != 2 || len(burstsOf(r)) != 1 || burstsOf(r)[0].Shape != burstStar ||
			!burstsOf(r)[0].Trail || burstsOf(r)[0].Colors[0] != 16711680 {
			t.Errorf("rocket flight %d bursts %+v", r.flight, burstsOf(r))
		}
		if bk, ok := h.books.get(find("written_book").bookID); !ok || bk.Title != "Tale" || bk.Author != "Ann" || len(bk.Pages) != 2 {
			t.Errorf("book %+v %v", bk, ok)
		}
		if x := find("crossbow"); x.load.item != itemByName["arrow"] || x.load.n != 1 {
			t.Errorf("crossbow load %+v", x.load)
		}
		if b := find("axolotl_bucket"); b.cube.health != 11 || b.cube.variant != 3 {
			t.Errorf("bucket %+v", b.cube)
		}
		if sw := find("diamond_sword"); sw.dmg != 0 {
			t.Errorf("!damage left %d damage", sw.dmg)
		}
		for _, st := range bob.inv.slots {
			if st.item == itemByName["stick"] {
				t.Errorf("a refused stick was given: %+v", st)
			}
		}
	})
}

// Adventure mode with can_break and can_place_on, through the dig and click
// entry points: the named block breaks and the named block takes a
// placement; anything else stays refused.
func TestAdventureCanBreakCanPlaceOn(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	st, msg := parseItemArg(`stone[can_break={blocks:"dandelion"},can_place_on={blocks:"stone"}]`)
	if msg != "" {
		t.Fatal(msg)
	}
	st.count = 16
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.inv.slots[0] = st
		h.sendSlot(tr, 0) // the hub mirrors the stack's tags to the session
	})
	selectSlot(p, 0)
	s.modes.set(p.name, gmAdventure)
	onHub(t, h, func() { h.playersRef[p.eid].gamemode = gmAdventure })

	dandelion := worldgen.BlockBase("dandelion")
	w.SetBlock(10, 80, 10, worldgen.Stone)
	w.SetBlock(10, 81, 10, dandelion)
	w.SetBlock(12, 80, 10, worldgen.Stone)
	w.SetBlock(12, 81, 10, worldgen.BlockBase("poppy"))
	s.handleDig(p, digBody(digStartBreak, 10, 81, 10))
	s.handleDig(p, digBody(digStartBreak, 12, 81, 10))
	if got := w.Block(10, 81, 10); got == dandelion {
		t.Error("can_break did not let adventure break a dandelion")
	}
	if got := w.Block(12, 81, 10); got != worldgen.BlockBase("poppy") {
		t.Error("adventure broke a poppy can_break does not name")
	}
	s.handleDig(p, digBody(digStartBreak, 10, 80, 10))
	s.handleDig(p, digBody(digFinishBreak, 10, 80, 10))
	if got := w.Block(10, 80, 10); got != worldgen.Stone {
		t.Error("adventure broke stone can_break does not name")
	}

	w.SetBlock(14, 80, 10, worldgen.Stone)
	w.SetBlock(14, 81, 10, worldgen.Air)
	w.SetBlock(16, 80, 10, worldgen.Dirt)
	w.SetBlock(16, 81, 10, worldgen.Air)
	s.handlePlace(p, placeBody(14, 80, 10, 1))
	s.handlePlace(p, placeBody(16, 80, 10, 1))
	if got := w.Block(14, 81, 10); got != worldgen.Stone {
		t.Errorf("can_place_on stone did not let adventure place on stone: %d", got)
	}
	if got := w.Block(16, 81, 10); got != worldgen.Air {
		t.Errorf("adventure placed on dirt, which can_place_on does not name: %d", got)
	}
}

// An item's own use on a block (ItemStack.useOn) is refused to adventure
// unless its can_place_on names the clicked block: a pig spawn egg with
// can_place_on stone hatches on stone and does nothing on dirt.
func TestAdventureCanPlaceOnItemUse(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	st, msg := parseItemArg(`pig_spawn_egg[can_place_on={blocks:"stone"}]`)
	if msg != "" {
		t.Fatal(msg)
	}
	st.count = 4
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.inv.slots[0] = st
		h.sendSlot(tr, 0)
	})
	selectSlot(p, 0)
	s.modes.set(p.name, gmAdventure)
	onHub(t, h, func() { h.playersRef[p.eid].gamemode = gmAdventure })

	w.SetBlock(3, 80, 3, worldgen.Dirt)
	w.SetBlock(3, 81, 3, worldgen.Air)
	w.SetBlock(3, 82, 3, worldgen.Air)
	var before int
	onHub(t, h, func() { before = len(h.mobs) })
	s.handlePlace(p, placeBody(3, 80, 3, 1))
	var n int
	onHub(t, h, func() { n = len(h.mobs) })
	if n != before {
		t.Fatalf("adventure hatched an egg on dirt, which can_place_on does not name: %d → %d mobs", before, n)
	}

	w.SetBlock(5, 80, 3, worldgen.Stone)
	w.SetBlock(5, 81, 3, worldgen.Air)
	w.SetBlock(5, 82, 3, worldgen.Air)
	s.handlePlace(p, placeBody(5, 80, 3, 1))
	onHub(t, h, func() { n = len(h.mobs) })
	if n != before+1 {
		t.Errorf("can_place_on stone did not let adventure hatch an egg on stone: %d → %d mobs", before, n)
	}
}
