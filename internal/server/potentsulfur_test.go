package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// geyserPool builds a 5×5 pool `water` deep in open air at y≈180: a stone
// basin whose floor is stone at y except the centre cell, which is left for
// the potent sulfur, with `below` under the centre. Returns the centre.
func geyserPool(h *hub, x, y, z, water int, below uint32) blockPos {
	w := h.worldFor(0)
	w.ForceLoad(x, z, 2)
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			for dy := -2; dy <= water+8; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			switch {
			case dx < -3 || dx > 3 || dz < -3 || dz > 3:
			case dx == -3 || dx == 3 || dz == -3 || dz == 3: // the basin's rim
				for dy := 0; dy <= water; dy++ {
					w.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
				}
			default:
				w.SetBlock(x+dx, y, z+dz, worldgen.Stone)
				for dy := 1; dy <= water; dy++ {
					w.SetBlock(x+dx, y+dy, z+dz, worldgen.WaterBase)
				}
			}
		}
	}
	w.SetBlock(x, y-1, z, below)
	return blockPos{x, y, z}
}

func psKindAt(h *hub, pos blockPos) int {
	st := h.worldFor(0).At(pos.x, pos.y, pos.z)
	if !isPotentSulfur(st) {
		return -1
	}
	return psKind(st)
}

// PotentSulfurBlock.validBlockState: water source above or it is DRY; lava
// source below CONTINUOUS, magma DORMANT (a running eruption kept), else WET.
func TestPotentSulfurValidState(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	pos := geyserPool(h, 2400, 180, 2400, 2, worldgen.Stone)
	anyState := psState(psDry)
	check := func(label string, st uint32, want int) {
		t.Helper()
		if got := potentSulfurValid(w, pos, st); psKind(got) != want {
			t.Errorf("%s: %s, want %s", label, psStateNames[psKind(got)], psStateNames[want])
		}
	}
	check("water above, stone below", anyState, psWet)
	w.SetBlock(pos.x, pos.y-1, pos.z, magmaBlockState)
	check("magma below", anyState, psDormant)
	check("magma below, mid-eruption", psState(psErupting), psErupting)
	check("magma below, was continuous", psState(psContinuous), psDormant)
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.LavaBase)
	check("lava source below", anyState, psContinuous)
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.LavaBase+2)
	check("flowing lava below", anyState, psWet)
	w.SetBlock(pos.x, pos.y+1, pos.z, worldgen.WaterBase+1)
	check("flowing water above", anyState, psDry)
	w.SetBlock(pos.x, pos.y+1, pos.z, worldgen.Air)
	check("air above", anyState, psDry)
	slab := worldgen.BlockBase("oak_slab")
	info, _ := worldgen.InfoForState(slab)
	w.SetBlock(pos.x, pos.y+1, pos.z, worldgen.SetProperty(info, slab, "waterlogged", "true"))
	check("waterlogged slab above", anyState, psWet)
}

// A neighbour's change re-derives the state through the shape update, and an
// eruption that starts that way sends the block event, the start sound and
// BLOCK_ACTIVATE; turning a continuous geyser periodic resets its countdown.
func TestPotentSulfurFollowsNeighbours(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := geyserPool(h, 2400, 180, 2400, 2, worldgen.Stone)
	pl.x, pl.y, pl.z = float64(pos.x)+2.5, float64(pos.y)+1, float64(pos.z)+0.5
	h.setBlockAt(players, 0, pos, potentSulfurValid(w, pos, psState(psDry)))
	if k := psKindAt(h, pos); k != psWet {
		t.Fatalf("placed wet in the pool: kind %d", k)
	}
	if h.vents[simPos{0, pos}] == nil {
		t.Fatal("the placed block entity is not registered")
	}
	below := blockPos{pos.x, pos.y - 1, pos.z}
	h.setBlockAt(players, 0, below, magmaBlockState)
	if k := psKindAt(h, pos); k != psDormant {
		t.Fatalf("magma put under it: kind %d, want dormant", k)
	}
	drainEvs(pl.p)
	h.vents[simPos{0, pos}].countdown = 7
	h.setBlockAt(players, 0, below, worldgen.LavaBase)
	if k := psKindAt(h, pos); k != psContinuous {
		t.Fatalf("lava under it: kind %d, want continuous", k)
	}
	var sawEvent, sawSound bool
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.BlockEvent:
			if e.X == int32(pos.x) && e.Y == int32(pos.y) && e.Z == int32(pos.z) && e.Action == 0 && e.Block == int32(potentSulfurReg) {
				sawEvent = true
			}
		case attachproto.Sound:
			if e.Name == "minecraft:block.potent_sulfur.geyser_continuous_eruption" {
				sawSound = true
			}
		}
	}
	if !sawEvent || !sawSound {
		t.Fatalf("a starting geyser sends its block event (%v) and start sound (%v)", sawEvent, sawSound)
	}
	h.setBlockAt(players, 0, below, magmaBlockState)
	if k := psKindAt(h, pos); k != psDormant {
		t.Fatalf("magma again: kind %d, want dormant", k)
	}
	if c := h.vents[simPos{0, pos}].countdown; c != -1 {
		t.Fatalf("continuous → dormant keeps countdown %d; validBlockState resets it", c)
	}
	h.setBlockAt(players, 0, blockPos{pos.x, pos.y + 1, pos.z}, worldgen.Air)
	if k := psKindAt(h, pos); k != psDry {
		t.Fatalf("water taken off it: kind %d, want dry", k)
	}
	h.setBlockAt(players, 0, pos, worldgen.Stone)
	if h.vents[simPos{0, pos}] != nil {
		t.Fatal("a mined vent stays registered")
	}

	// A live edit (a bucket) has no shape update: the scheduled recheck.
	h.setBlockAt(players, 0, pos, psState(psDry))
	h.setBlockLive(players, 0, pos.x, pos.y+1, pos.z, worldgen.WaterBase)
	runTicks(h, players, 1, 2)
	if k := psKindAt(h, pos); k != psDormant {
		t.Fatalf("water poured over it: kind %d, want dormant", k)
	}
}

// The item places in the state its surroundings give it.
func TestPotentSulfurPlacedState(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, magmaBlockState)
	w.SetBlock(x, y, z, worldgen.WaterBase)
	w.SetBlock(x, y+1, z, worldgen.WaterBase)
	holdItem(t, p, "potent_sulfur")
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	got := w.Block(x, y, z)
	if !isPotentSulfur(got) || psKind(got) != psDormant {
		t.Fatalf("placed over magma under water: state %d, want a dormant geyser", got)
	}
}

// The draws match Xoroshiro128++ and SplitMix64's published first outputs.
func TestXoroshiroVectors(t *testing.T) {
	if got := mixStafford13(xoroGolden); got != 0xE220A8397B1DCDAF {
		t.Fatalf("mixStafford13(golden) = %#x, want splitmix64's first output", got)
	}
	r := newXoroshiroPair(1, 2)
	if got := r.nextLong(); got != 393217 { // rotl(1+2, 17) + 1
		t.Fatalf("xoroshiro128++ from (1,2): %d, want 393217", got)
	}
	a, b := geyserPositional(42, blockPos{5, 70, -9}), geyserPositional(42, blockPos{5, 70, -9})
	for i := 0; i < 8; i++ {
		if a.nextIntBetween(15, 30) != b.nextIntBetween(15, 30) {
			t.Fatal("a geyser's stream is its position's: two draws differ")
		}
	}
	seen := map[int32]bool{}
	for x := 0; x < 400; x++ {
		v := geyserPositional(42, blockPos{x, 70, 0}).nextIntBetween(15, 30)
		if v < 15 || v > 30 {
			t.Fatalf("nextIntBetweenInclusive(15, 30) drew %d", v)
		}
		seen[v] = true
	}
	if len(seen) != 16 {
		t.Fatalf("400 geysers drew %d of the 16 waits", len(seen))
	}
}

// SERVER_WAITING_COUNTDOWN_TICKER: a dormant geyser under two blocks of
// water waits 10 + 15..30 seconds, erupts for 1 + 1..2 seconds (BLOCK_ACTIVATE
// on the way in, BLOCK_DEACTIVATE on the way out), and waits again.
func TestPotentSulfurEruptionCycle(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := geyserPool(h, 2400, 180, 2400, 2, magmaBlockState)
	pl.x, pl.y, pl.z = float64(pos.x)+30, float64(pos.y)+1, float64(pos.z)+0.5
	h.setBlockAt(players, 0, pos, potentSulfurValid(h.worldFor(0), pos, psState(psDry)))
	if psKindAt(h, pos) != psDormant {
		t.Fatal("fixture: not a dormant geyser")
	}
	r := geyserPositional(h.worldFor(0).Seed(), pos)
	wait := 10*(2-1) + int(r.nextIntBetween(15, 30))
	r = geyserPositional(h.worldFor(0).Seed(), pos)
	r.nextInt()
	erupt := 2 - 1 + int(r.nextIntBetween(1, 2))

	var eruptAt, dormantAt uint64
	for tick := uint64(20); tick <= 20*80 && dormantAt == 0; tick++ {
		h.tick.Store(tick)
		h.potentSulfurTick(players)
		switch k := psKindAt(h, pos); {
		case k == psErupting && eruptAt == 0:
			eruptAt = tick
		case k == psDormant && eruptAt != 0:
			dormantAt = tick
		}
	}
	if want := uint64(20 + (wait-1)*20); eruptAt != want {
		t.Fatalf("erupted at tick %d, want %d (a %d-second wait)", eruptAt, want, wait)
	}
	if want := eruptAt + 20 + uint64((erupt-1)*20); dormantAt != want {
		t.Fatalf("settled at tick %d, want %d (a %d-second eruption)", dormantAt, want, erupt)
	}
}

// SERVER_NAUSEA_EFFECT_TICKER: every ten ticks whatever breathes at a vent's
// surface — eyes in the open cell over source water, in sight of the vent —
// gets Nausea 80 ticks; someone on the dry rim does not.
func TestPotentSulfurNausea(t *testing.T) {
	h := newHub(world.New(1))
	swimmer := survPlayer(h)
	rim := survPlayer(h)
	rim.p.eid = 2
	players := map[int32]*tracked{swimmer.p.eid: swimmer, rim.p.eid: rim}
	pos := geyserPool(h, 2400, 180, 2400, 1, worldgen.Stone)
	h.setBlockAt(players, 0, pos, potentSulfurValid(h.worldFor(0), pos, psState(psDry)))
	if psKindAt(h, pos) != psWet {
		t.Fatal("fixture: not a wet vent")
	}
	// The surface is y=182: a swimmer's feet in the water at 181, eyes above it.
	swimmer.x, swimmer.y, swimmer.z = float64(pos.x)+1.5, float64(pos.y)+1, float64(pos.z)+0.5
	rim.x, rim.y, rim.z = float64(pos.x)+3.5, float64(pos.y)+2, float64(pos.z)+0.5
	cow := h.spawnMob(players, entityCow, float64(pos.x)-0.5, float64(pos.y)+1, float64(pos.z)+0.5)

	h.tick.Store(15)
	h.potentSulfurTick(players)
	if swimmer.effects[effNausea] != nil {
		t.Fatal("the gas is given every ten ticks, not on tick 15")
	}
	h.tick.Store(20)
	h.potentSulfurTick(players)
	e := swimmer.effects[effNausea]
	if e == nil || e.left != psNauseaTicks || e.amp != 0 || !e.ambient {
		t.Fatalf("the swimmer at the surface: %+v, want ambient Nausea I for 80 ticks", e)
	}
	if rim.effects[effNausea] != nil {
		t.Fatal("the rim is out of the gas's reach and not over water")
	}
	if cow.hasEffect(effNausea) == 0 {
		t.Fatal("a mob breathing at the surface gets Nausea too")
	}
	// Dry, the vent gives nothing.
	h.setBlockAt(players, 0, blockPos{pos.x, pos.y + 1, pos.z}, worldgen.Stone)
	delete(swimmer.effects, effNausea)
	h.tick.Store(30)
	h.potentSulfurTick(players)
	if swimmer.effects[effNausea] != nil {
		t.Fatal("a capped (dry) vent still gasses")
	}
}

// LAUNCH_ENTITY_TICKER: a continuous geyser lifts a dropped item and a mob
// out of the water and up its column; a player in the column is left to the
// client but is never read as floating.
func TestPotentSulfurLaunch(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := geyserPool(h, 2400, 180, 2400, 2, worldgen.LavaBase)
	h.setBlockAt(players, 0, pos, potentSulfurValid(h.worldFor(0), pos, psState(psDry)))
	if psKindAt(h, pos) != psContinuous {
		t.Fatal("fixture: not a continuous geyser")
	}
	surface := float64(pos.y + 3)
	it := h.spawnItem(players, int32(itemByName["stone"]), 1, float64(pos.x)+0.5, float64(pos.y)+1.2, float64(pos.z)+0.5)
	it.vx, it.vy, it.vz = 0, 0, 0
	m := h.spawnMob(players, entityCow, float64(pos.x)+0.5, float64(pos.y)+1, float64(pos.z)+0.5)
	tnt := &primedTNT{eid: h.allocEID(), x: float64(pos.x) + 0.5, y: float64(pos.y) + 4, z: float64(pos.z) + 0.5, fuse: 80}
	h.tnt = append(h.tnt, tnt)
	pl.x, pl.y, pl.z = float64(pos.x)+0.5, float64(pos.y)+6, float64(pos.z)+0.5
	pl.floatTicks = 70

	h.tick.Store(1)
	h.potentSulfurTick(players)
	if tnt.vy < psLaunchForce-1e-9 {
		t.Fatalf("primed TNT in the column: vy %v, want the 0.2 lift", tnt.vy)
	}
	if pl.floatTicks != 0 {
		t.Fatal("a player carried by the plume must not count as hovering")
	}
	if pl.x != float64(pos.x)+0.5 || pl.y != float64(pos.y)+6 {
		t.Fatal("the server moved the player; the client does that")
	}
	topItem, topMob := it.y, m.y
	checked := false
	for tick := uint64(2); tick <= 30; tick++ {
		h.tick.Store(tick)
		h.tickItems(players)
		h.potentSulfurTick(players)
		topItem, topMob = max(topItem, it.y), max(topMob, m.y)
		// The mob's own movement does not seat it back on the pool floor
		// mid-flight.
		if !checked && m.geyserFly && m.y > surface+1 {
			checked = true
			y := m.y
			m.rest, m.stroll = 0, 100 // walking, not idling (an idler skips its step)
			h.updateMobs(players)
			if m.y != y {
				t.Fatalf("updateMobs moved a lifted cow from %.2f to %.2f", y, m.y)
			}
		}
	}
	if !checked {
		t.Fatal("the cow was never airborne over the surface")
	}
	if topItem <= surface+1 {
		t.Fatalf("the item rose only to %.2f; the surface is %.0f", topItem, surface)
	}
	if topMob <= surface+1 {
		t.Fatalf("the cow rose only to %.2f; the surface is %.0f", topMob, surface)
	}
	if topMob > float64(pos.y+1+6*2)+5 {
		t.Fatalf("the cow flew to %.2f, far past the twelve-block column", topMob)
	}
	// Capped with stone, the geyser dries up and lets the cow come down.
	h.setBlockAt(players, 0, blockPos{pos.x, pos.y + 1, pos.z}, worldgen.Stone)
	for tick := uint64(31); tick <= 200 && m.geyserFly; tick++ {
		h.tick.Store(tick)
		h.potentSulfurTick(players)
	}
	if m.geyserFly {
		t.Fatal("the cow never came down")
	}
}

// The floating check leaves a plume rider alone: hovering over a geyser for
// longer than the float limit is not snapped down.
func TestPotentSulfurRiderNotFloating(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := geyserPool(h, 2400, 180, 2400, 4, worldgen.LavaBase)
	h.setBlockAt(players, 0, pos, potentSulfurValid(h.worldFor(0), pos, psState(psDry)))
	x, z := float64(pos.x)+0.5, float64(pos.z)+0.5
	pl.x, pl.y, pl.z = x, float64(pos.y)+6, z
	for tick := uint64(1); tick <= 2*floatLimit; tick++ {
		h.tick.Store(tick)
		h.potentSulfurTick(players)
		ny := pl.y + 0.2
		if tick%2 == 0 {
			ny = pl.y - 0.02
		}
		if !h.validateMove(pl, evMove{x: x, y: ny, z: z}) {
			t.Fatalf("tick %d: a move riding the plume was rejected", tick)
		}
		pl.y = ny
	}
}

// Vents in a chunk are found when it first loads: placed ones from the edits,
// generated ones from the generated base.
func TestPotentSulfurDiscovery(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	x, y, z := 2400, 180, 2400
	w.ForceLoad(x, z, 1)
	w.SetBlock(x, y, z, psState(psWet))
	cx, cz := int32(x>>4), int32(z>>4)
	h.discoverVents(0, w, cx, cz, w.EditedBlocks(cx, cz))
	if h.vents[simPos{0, blockPos{x, y, z}}] == nil {
		t.Fatal("a placed vent in a loading chunk is not registered")
	}
	lo, hi := worldgen.BlockRange("bedrock")
	if len(w.GeneratedStatesIn(cx, cz, lo, hi)) == 0 {
		t.Fatal("GeneratedStatesIn finds nothing of the bedrock floor")
	}
}
