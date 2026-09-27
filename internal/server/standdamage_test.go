package server

import (
	"bytes"
	"path/filepath"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func standFixture(t *testing.T) (*hub, map[int32]*tracked, *armorStand) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			h.world.SetBlock(x, 180, z, worldgen.Air)
			h.world.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	players := map[int32]*tracked{}
	h.playersRef = players
	st := &armorStand{eid: h.allocEID(), x: 0.5, y: 180, z: 0.5}
	st.equip[5] = invStack{item: int32(itemByName["iron_helmet"]), count: 1}
	h.armorStands[st.eid] = st
	return h, players, st
}

func countItem(h *hub, item int32) int {
	n := 0
	for _, it := range h.items {
		if it.item == item {
			n += it.count
		}
	}
	return n
}

// ArmorStand.hurtServer: a blast breaks it and drops its gear but not the
// stand; an arrow breaks it outright, stand and all; an adventure-mode
// punch does nothing.
func TestArmorStandBlastAndArrow(t *testing.T) {
	h, players, st := standFixture(t)
	h.explodeHurt(players, 0, 0.5, 181, 2.5, 2, dtExplosion, deathCause{})
	if h.armorStands[st.eid] != nil {
		t.Fatal("a blast breaks the stand")
	}
	if countItem(h, int32(itemByName["iron_helmet"])) != 1 || countItem(h, itemArmorStand) != 0 {
		t.Fatal("a blast drops the gear, not the stand")
	}
	h2, players2, st2 := standFixture(t)
	a := &arrowEntity{etype: entityArrow, dim: 0, shooter: 0}
	if !h2.arrowHitsStand(players2, a, 0.5, 181, 0.5) || h2.armorStands[st2.eid] != nil {
		t.Fatal("an arrow breaks the stand at once")
	}
	if countItem(h2, itemArmorStand) != 1 {
		t.Fatal("an arrow's break drops the stand itself")
	}
	h3, players3, st3 := standFixture(t)
	pl := survPlayer(h3)
	pl.x, pl.y, pl.z = 1.5, 180, 0.5 // within reach
	pl.gamemode = gmAdventure
	h3.hitStand(players3, pl, st3)
	h3.tick.Add(1)
	h3.hitStand(players3, pl, st3)
	if h3.armorStands[st3.eid] == nil {
		t.Fatal("an adventure-mode player cannot break a stand")
	}
}

// Fire lights a stand and the burn eats it away: four of its twenty health
// a second, gone once half a point is left. Lava lights it too, but only
// burns it once it is out of the lava.
func TestArmorStandBurns(t *testing.T) {
	h, players, st := standFixture(t)
	h.world.SetBlock(0, 180, 0, worldgen.BlockBase("fire"))
	for i := 0; i < 20*8 && h.armorStands[st.eid] != nil; i++ {
		h.tick.Add(1)
		h.tickStands(players)
	}
	if h.armorStands[st.eid] != nil {
		t.Fatalf("a stand in fire burns away (hurt %.1f, fire %d)", st.hurt, st.fire)
	}
	h2, players2, st2 := standFixture(t)
	h2.world.SetBlock(0, 180, 0, worldgen.LavaBase)
	for i := 0; i < 20*3; i++ {
		h2.tick.Add(1)
		h2.tickStands(players2)
	}
	if h2.armorStands[st2.eid] == nil || st2.hurt != 0 || st2.fire == 0 {
		t.Fatal("in the lava a stand is lit but not burnt")
	}
	h2.world.SetBlock(0, 180, 0, worldgen.Air)
	for i := 0; i < 20*6 && h2.armorStands[st2.eid] != nil; i++ {
		h2.tick.Add(1)
		h2.tickStands(players2)
	}
	if h2.armorStands[st2.eid] != nil {
		t.Fatal("out of the lava, still burning, it burns away")
	}
}

// A damaged, burning stand is still damaged and burning after a restart
// (LivingEntity's Health, Entity's Fire), and a player who joins while it
// burns sees the flames.
func TestArmorStandHealthAndFireSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "containers.json")
	cs := newContainerStore(path)
	st := &armorStand{eid: 1, x: 1.5, y: 64, z: 2.5, hurt: 7.5, fire: 140}
	cs.recordStands(map[int32]*armorStand{1: st})
	cs.flush()

	next := int32(100)
	var got *armorStand
	for _, l := range newContainerStore(path).loadStands(func() int32 { next++; return next }) {
		got = l
	}
	if got == nil || got.hurt != 7.5 || got.fire != 140 {
		t.Fatalf("after a restart: %+v, want hurt 7.5 and fire 140", got)
	}

	h := newTestHub(world.New(1))
	h.armorStands[got.eid] = got
	pl := survPlayer(h)
	drain(pl.p)
	h.sendStandsTo(pl)
	want := metaEv(standMeta(got)) // the flames (shared flag 0x01) ride with the stand's flags
	burning := false
	for _, f := range frames(pl.p) {
		if m, ok := f.(attachproto.EntityMeta); ok && m.EID == want.EID && bytes.Equal(m.Meta, want.Meta) {
			burning = true
		}
	}
	if !burning {
		t.Error("a player joining beside a burning stand must see it burn")
	}
}

// A portal trip gives the client a new, empty level: the stands (and the
// other fixed furniture) of the dimension it arrives in are sent again.
func TestStandsReappearAfterAPortalTrip(t *testing.T) {
	h := dimHub()
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	st := &armorStand{eid: h.allocEID(), x: 3.5, y: 70, z: 3.5}
	h.armorStands[st.eid] = st

	h.onDimSwitch(players, pl, evDim{eid: pl.p.eid, dim: dimNether, x: 0, y: 70, z: 0})
	h.onDimSwitch(players, pl, evDim{eid: pl.p.eid, dim: dimOverworld, x: 0, y: 70, z: 0})
	if fs := frames(pl.p); !sawAdd(fs, st.eid) {
		t.Fatal("back in the overworld, the armour stand must be spawned again")
	}
}

// A summoned stand takes the Small, ShowArms, NoBasePlate, Marker and
// Invisible tags: they reach the client (DATA_CLIENT_FLAGS and the shared
// invisible flag), survive a restart, and an invisible stand or a marker
// shrugs off blows, arrows and blasts (hurtServer) while /kill still works.
func TestArmorStandSummonFlags(t *testing.T) {
	h, players, _ := standFixture(t)
	summon := func(nbt map[string]any) *armorStand {
		before := map[int32]bool{}
		for id := range h.armorStands {
			before[id] = true
		}
		h.summonAt(players, evSummon{etype: entityByName["armor_stand"], x: 1.5, y: 180, z: 1.5, nbt: nbt})
		for id, st := range h.armorStands {
			if !before[id] {
				return st
			}
		}
		t.Fatal("no stand summoned")
		return nil
	}
	small := summon(map[string]any{"Small": true, "ShowArms": int64(1), "NoBasePlate": true})
	if !small.small || !small.arms || !small.noBasePlate || small.marker || small.invisible {
		t.Fatalf("flags from the NBT: %+v", small)
	}
	if f := small.clientFlags(); f != standFlagSmall|standFlagShowArms|standFlagNoBasePlate {
		t.Fatalf("DATA_CLIENT_FLAGS %#x", f)
	}
	if meta := standMeta(small); meta[len(meta)-2] != standFlagSmall|standFlagShowArms|standFlagNoBasePlate {
		t.Fatalf("the client flags are the last field: % x", meta)
	}
	// Small: half the box; an arrow at the full stand's chest height misses.
	a := &arrowEntity{etype: entityArrow, dim: 0}
	if h.arrowHitsStand(players, a, 1.5, 181.6, 1.5) {
		t.Fatal("an arrow over a small stand's head should miss it")
	}
	// Armed: a held item goes to the main hand.
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 2.5, 180, 1.5
	players[pl.p.eid] = pl
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: int32(itemByName["stick"]), count: 1}
	h.interactStand(players, pl, small)
	if small.equip[attachproto.EquipMainHand].item != int32(itemByName["stick"]) {
		t.Fatal("an armed stand takes a held item in its hand")
	}

	for _, tag := range []string{"Marker", "Invisible"} {
		st := summon(map[string]any{tag: true})
		h.hitStand(players, pl, st)
		h.tick.Add(1)
		h.hitStand(players, pl, st)
		h.arrowHitsStand(players, &arrowEntity{etype: entityArrow, dim: 0}, 1.5, 180.5, 1.5)
		h.standHurt(players, st, dtExplosion, nil, false)
		if h.armorStands[st.eid] == nil {
			t.Fatalf("a %s stand takes no blows, arrows or blasts", tag)
		}
		h.standHurt(players, st, dtGenericKill, nil, false)
		if h.armorStands[st.eid] != nil {
			t.Fatalf("/kill removes a %s stand", tag)
		}
	}

	cs := newContainerStore("")
	cs.recordStands(map[int32]*armorStand{small.eid: small})
	for _, l := range cs.loadStands(func() int32 { return 999 }) {
		if !l.small || !l.arms || !l.noBasePlate {
			t.Fatalf("the flags did not survive a save: %+v", l)
		}
	}
}
