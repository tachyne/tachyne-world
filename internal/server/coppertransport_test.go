package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// golemYard is a stone floor at y=179 with the air above cleared, a
// creative player standing by, and a copper golem at the middle.
func golemYard(t *testing.T) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	for x := -20; x <= 20; x++ {
		for z := -20; z <= 20; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 183; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.gamemode = 0.5, 180, -15.5, gmCreative
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnSpecies(players, entityCopperGolem, 0, 0.5, 180, 0.5)
	m.transportCD = 0
	return h, players, pl, m
}

// golemEvents drains what the player was sent: sounds by name, block
// events by position.
func golemEvents(pl *tracked) (sounds map[string]int, lids []attachproto.BlockEvent) {
	sounds = map[string]int{}
	for {
		select {
		case pkt := <-pl.p.out:
			switch ev := pkt.ev.(type) {
			case attachproto.Sound:
				sounds[ev.Name]++
			case attachproto.BlockEvent:
				lids = append(lids, ev)
			}
		default:
			return
		}
	}
}

func chestCount(c *chest, item int32) int {
	n := 0
	for _, st := range c.slots {
		if st.item == item {
			n += st.count
		}
	}
	return n
}

// TransportItemsBetweenContainers: the golem takes sixteen from the copper
// chest and carries them in its hand to the wooden chest, then comes back
// for the rest. At each chest the lid opens and the interaction sound
// plays; the lid shuts again after the sixty ticks.
func TestCopperGolemCarriesBetweenChests(t *testing.T) {
	h, players, pl, m := golemYard(t)
	src, dst := blockPos{6, 180, 0}, blockPos{-6, 180, 0}
	h.world.SetBlock(src.x, src.y, src.z, worldgen.BlockID("copper_chest"))
	h.world.SetBlock(dst.x, dst.y, dst.z, worldgen.BlockID("chest"))
	diamond := int32(itemByName["diamond"])
	sc := &chest{}
	sc.slots[3] = invStack{item: diamond, count: 20}
	h.chests[simPos{blockPos: src}] = sc
	sawOpen, sawHeld := false, false
	sounds := map[string]int{}
	var lids []attachproto.BlockEvent
	for i := 0; i < 1500; i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		if h.chestViewers(simPos{blockPos: src}, nil) == 1 {
			sawOpen = true
		}
		if m.held == diamond && m.heldCount == 16 {
			sawHeld = true
		}
		s, l := golemEvents(pl)
		for k, v := range s {
			sounds[k] += v
		}
		lids = append(lids, l...)
		if d := h.chests[simPos{blockPos: dst}]; d != nil && chestCount(d, diamond) == 20 {
			break
		}
	}
	d := h.chests[simPos{blockPos: dst}]
	if d == nil || chestCount(d, diamond) != 20 || chestCount(sc, diamond) != 0 || m.held != 0 {
		t.Fatalf("not all moved: src %d, dst %v, holding %d; golem at (%.1f, %.1f) phase %d target %v cd %d",
			chestCount(sc, diamond), d, m.held, m.x, m.z, m.cgPhase, m.cgTarget, m.transportCD)
	}
	if !sawOpen || !sawHeld {
		t.Errorf("the golem opened the copper chest %v, carried sixteen in its hand %v", sawOpen, sawHeld)
	}
	if sounds["minecraft:entity.copper_golem.no_item_get"] < 2 || sounds["minecraft:entity.copper_golem.item_drop"] < 2 {
		t.Errorf("interaction sounds: %v", sounds)
	}
	if sounds["minecraft:block.copper_chest.open"] < 2 || sounds["minecraft:block.chest.close"] < 2 {
		t.Errorf("chest sounds: %v", sounds)
	}
	opened, closed := false, false
	for _, e := range lids {
		if e.X == int32(src.x) && e.Z == int32(src.z) && e.Action == 1 {
			opened = opened || e.Param == 1
			closed = closed || (opened && e.Param == 0)
		}
	}
	if !opened || !closed {
		t.Errorf("the copper chest's lid event: opened %v, closed %v", opened, closed)
	}
	if len(h.golemChests) != 0 {
		t.Error("the golem let every chest go")
	}
}

// isAnotherMobInteractingWithTarget: while a player has the copper chest
// open the golem waits its turn a few blocks off, and takes nothing.
func TestCopperGolemQueuesForAnOpenChest(t *testing.T) {
	h, players, pl, m := golemYard(t)
	src := blockPos{6, 180, 0}
	h.world.SetBlock(src.x, src.y, src.z, worldgen.BlockID("copper_chest"))
	sc := &chest{}
	sc.slots[0] = invStack{item: int32(itemByName["diamond"]), count: 5}
	h.chests[simPos{blockPos: src}] = sc
	pl.winKind, pl.winPos = winChest, simPos{blockPos: src}
	queued := false
	for i := 0; i < 300; i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		queued = queued || m.cgPhase == cgQueuing
	}
	if !queued || m.held != 0 || sc.slots[0].count != 5 {
		t.Fatalf("queued %v, holding %d, chest %d", queued, m.held, sc.slots[0].count)
	}
	pl.winKind, pl.winPos = winPlayer, simPos{}
	for i := 0; i < 300 && m.held == 0; i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
	}
	if m.held == 0 {
		t.Fatal("once the chest is shut the golem takes its turn")
	}
}

// A chest full of other things will not take the load (PLACE_NO_ITEM): the
// golem remembers it and goes on to the next nearest chest.
func TestCopperGolemSkipsAFullChest(t *testing.T) {
	h, players, _, m := golemYard(t)
	full, spare := blockPos{-4, 180, 0}, blockPos{-12, 180, 0}
	h.world.SetBlock(full.x, full.y, full.z, worldgen.BlockID("chest"))
	h.world.SetBlock(spare.x, spare.y, spare.z, worldgen.BlockID("chest"))
	fc := &chest{}
	for i := range fc.slots {
		fc.slots[i] = invStack{item: int32(itemByName["dirt"]), count: 64}
	}
	h.chests[simPos{blockPos: full}] = fc
	iron := int32(itemByName["iron_ingot"])
	m.setHeld(invStack{item: iron, count: 7})
	m.gearSure[gearSlotHand] = true
	for i := 0; i < 1200 && m.held != 0; i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
	}
	sp := h.chests[simPos{blockPos: spare}]
	if m.held != 0 || sp == nil || chestCount(sp, iron) != 7 || chestCount(fc, iron) != 0 {
		t.Fatalf("holding %d; spare chest %v", m.held, sp)
	}
}

// With nothing to do the golem rests TRANSPORT_ITEMS_COOLDOWN_TICKS (140)
// and only then does it stroll a block or two about, or stand.
func TestCopperGolemIdlesOnItsCooldown(t *testing.T) {
	h, players, _, m := golemYard(t)
	h.tick.Add(mobMoveInterval)
	h.mobUpdate(players)
	if m.transportCD != cgIdleCooldown {
		t.Fatalf("no chest in reach: cooldown %d, want %d", m.transportCD, cgIdleCooldown)
	}
	walked := false
	var last *idleWalk
	for i := 0; i < 60; i++ {
		x0, z0 := m.x, m.z
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		if w := m.idleWalk; w != nil && !w.still && w != last {
			last, walked = w, true
			if d := dist3(w.x, 0, w.z, x0, 0, z0); d > 3.5 {
				t.Fatalf("RandomStroll.stroll(1, 2, 2) went %.1f away", d)
			}
		}
	}
	if !walked {
		t.Error("the idle RunOne never strolled")
	}
}

// TransportItemsBetweenContainers looks 32 blocks sideways and 8 up or
// down from the golem's block.
func TestCopperGolemSearchBox(t *testing.T) {
	for _, tc := range []struct {
		p  blockPos
		in bool
	}{{blockPos{32, 64, 0}, true}, {blockPos{33, 64, 0}, false}, {blockPos{0, 72, -32}, true}, {blockPos{0, 73, 0}, false}} {
		h := newTestHub(world.New(1))
		h.world.ForceLoad(0, 0, 4)
		m := &mob{x: 0.5, y: 64, z: 0.5, etype: entityCopperGolem}
		h.world.SetBlock(tc.p.x, tc.p.y, tc.p.z, worldgen.BlockID("copper_chest"))
		if _, got := h.cgFindTarget(m); got != tc.in {
			t.Errorf("%v in the search box: %v, want %v", tc.p, got, tc.in)
		}
	}
}

// CopperGolem.mobInteract: an empty hand takes what the golem is carrying,
// thrown toward the player.
func TestCopperGolemHandsOverItsLoad(t *testing.T) {
	h, players, pl, m := golemYard(t)
	m.setHeld(invStack{item: int32(itemByName["gold_ingot"]), count: 9})
	pl.x, pl.z = 2.5, 0.5
	if !h.copperGolemTakeItem(players, pl, m) || m.held != 0 {
		t.Fatal("an empty hand takes the golem's load")
	}
	found := false
	for _, it := range h.items {
		if it.item == int32(itemByName["gold_ingot"]) && it.count == 9 && it.vx > 0 {
			found = true
		}
	}
	if !found {
		t.Error("the load is thrown toward the player")
	}
}

// COPPER_GOLEM_STATE sits at index 17 on 26.2 and 26.3 alike.
func TestCopperGolemStateMeta(t *testing.T) {
	b := copperGolemStateMeta(5, cgStateDroppingItem)
	want := []byte{5, metaIndexCopperGolemState, metaTypeInt, cgStateDroppingItem, itemMetaEnd}
	if string(b) != string(want) {
		t.Fatalf("state meta %v, want %v", b, want)
	}
}
