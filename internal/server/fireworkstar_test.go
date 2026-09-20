package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

func starGrid(t *testing.T, items ...string) []invStack {
	t.Helper()
	grid := make([]invStack, 9)
	for i, n := range items {
		id := int32(itemByName[n])
		if id == 0 {
			t.Fatalf("no item %q", n)
		}
		grid[i] = invStack{item: id, count: 1}
	}
	return grid
}

// FireworkStarRecipe: gunpowder plus dyes, with at most one shape item, one
// glowstone dust and one diamond. The dyes give the burst its colours — and
// the value is the dye's FIREWORK colour, not its text colour.
func TestFireworkStarRecipe(t *testing.T) {
	h := newHub(world.New(1))
	res, ok := h.fireworkStarMatch(starGrid(t, "gunpowder", "red_dye", "blue_dye"))
	if !ok {
		t.Fatal("gunpowder and two dyes should make a star")
	}
	if res.item != itemFireworkStar || res.count != 1 {
		t.Fatalf("made %+v, want one firework star", res)
	}
	b := burstsOf(res)
	if len(b) != 1 {
		t.Fatalf("%d bursts, want 1", len(b))
	}
	if b[0].Shape != burstSmallBall {
		t.Errorf("shape %d, want small ball", b[0].Shape)
	}
	want := []int32{dyeFireworkColor[dyeColorOf[int32(itemByName["red_dye"])]],
		dyeFireworkColor[dyeColorOf[int32(itemByName["blue_dye"])]]}
	if len(b[0].Colors) != 2 || b[0].Colors[0] != want[0] || b[0].Colors[1] != want[1] {
		t.Errorf("colours %v, want %v", b[0].Colors, want)
	}
	if b[0].Trail || b[0].Twinkle {
		t.Error("a plain star should neither trail nor twinkle")
	}

	// The modifiers: shape, trail, twinkle.
	res, ok = h.fireworkStarMatch(starGrid(t, "gunpowder", "lime_dye", "fire_charge", "diamond", "glowstone_dust"))
	if !ok {
		t.Fatal("the modifiers should still make a star")
	}
	b = burstsOf(res)
	if b[0].Shape != burstLargeBall || !b[0].Trail || !b[0].Twinkle {
		t.Errorf("got shape %d trail=%v twinkle=%v, want large ball with both",
			b[0].Shape, b[0].Trail, b[0].Twinkle)
	}
	if got := burstsOf(mustStar(t, h, "gunpowder", "cyan_dye", "creeper_head"))[0].Shape; got != burstCreeper {
		t.Errorf("a head gives shape %d, want creeper", got)
	}

	// …and what is refused.
	for _, bad := range [][]string{
		{"gunpowder"},                         // no dye
		{"red_dye", "blue_dye"},               // no gunpowder
		{"gunpowder", "red_dye", "gunpowder"}, // two gunpowder
		{"gunpowder", "red_dye", "fire_charge", "feather"} /* two shapes */, {"gunpowder", "red_dye", "stick"},
	} {
		if _, ok := h.fireworkStarMatch(starGrid(t, bad...)); ok {
			t.Errorf("%v should not make a star", bad)
		}
	}
}

func mustStar(t *testing.T, h *hub, items ...string) invStack {
	t.Helper()
	res, ok := h.fireworkStarMatch(starGrid(t, items...))
	if !ok {
		t.Fatalf("%v made no star", items)
	}
	return res
}

// FireworkStarFadeRecipe: a finished star plus dyes gains the colours it
// fades to, and keeps everything else it had.
func TestFireworkStarFadeRecipe(t *testing.T) {
	h := newHub(world.New(1))
	star := mustStar(t, h, "gunpowder", "red_dye", "fire_charge", "diamond")
	grid := make([]invStack, 9)
	grid[0] = star
	grid[1] = invStack{item: int32(itemByName["white_dye"]), count: 1}
	res, ok := h.fireworkStarFadeMatch(grid)
	if !ok {
		t.Fatal("a star and a dye should set the fade")
	}
	b := burstsOf(res)[0]
	if len(b.Fade) != 1 || b.Fade[0] != dyeFireworkColor[0] {
		t.Errorf("fade %v, want white", b.Fade)
	}
	if b.Shape != burstLargeBall || !b.Trail || len(b.Colors) != 1 {
		t.Errorf("the star lost something: %+v", b)
	}
	// A star on its own, or two stars, is not the fade recipe.
	if _, ok := h.fireworkStarFadeMatch([]invStack{star}); ok {
		t.Error("a lone star should not match")
	}
}

// A rocket carries its stars' bursts, and no more than seven.
func TestRocketCarriesStars(t *testing.T) {
	h := newHub(world.New(1))
	star := mustStar(t, h, "gunpowder", "red_dye")
	grid := make([]invStack, 9)
	grid[0] = invStack{item: itemPaper, count: 1}
	grid[1] = invStack{item: itemGunpowder, count: 1}
	grid[2], grid[3] = star, star
	res, ok := h.fireworkRocketMatch(grid)
	if !ok {
		t.Fatal("paper, gunpowder and stars should make rockets")
	}
	if n := len(burstsOf(res)); n != 2 {
		t.Fatalf("%d bursts on the rocket, want 2", n)
	}
	if res.flight != 1 {
		t.Errorf("flight %d, want 1", res.flight)
	}

	// Seven stars is the most a grid can hold beside the paper and the
	// gunpowder, and all seven ride.
	big := make([]invStack, 9)
	big[0] = invStack{item: itemPaper, count: 1}
	big[1] = invStack{item: itemGunpowder, count: 1}
	for i := 2; i < 9; i++ {
		big[i] = star
	}
	res, ok = h.fireworkRocketMatch(big)
	if !ok {
		t.Fatal("a full grid of stars should still make rockets")
	}
	if n := len(burstsOf(res)); n != maxRocketBursts {
		t.Errorf("%d bursts, want %d", n, maxRocketBursts)
	}
}

// The burst reaches the client on both items, and the whole stack still walks
// through the slot copier — colours are fixed-width ints inside it, which is
// the part a walker gets wrong.
func TestFireworkComponentsOnTheWire(t *testing.T) {
	h := newHub(world.New(1))
	star := mustStar(t, h, "gunpowder", "red_dye", "blue_dye", "fire_charge", "glowstone_dust")
	for _, st := range []invStack{star, {item: itemFireworkRocket, count: 1, flight: 2, starID: star.starID}} {
		body := appendStack(nil, st)
		if _, _, ok := protocol.ReadSlot770(bytes.NewReader(body)); !ok {
			t.Fatalf("the slot copier rejected %+v", st)
		}
	}
	r := bytes.NewReader(appendStack(nil, star))
	for i := 0; i < 4; i++ {
		protocol.ReadVarInt(r)
	}
	if cid, _ := protocol.ReadVarInt(r); cid != componentFireworkStar {
		t.Fatalf("component %d, want firework_explosion (%d)", cid, componentFireworkStar)
	}
	shape, _ := protocol.ReadVarInt(r)
	n, _ := protocol.ReadVarInt(r)
	if shape != burstLargeBall || n != 2 {
		t.Errorf("shape %d with %d colours, want large ball with 2", shape, n)
	}
}

// Asking for the same star twice reuses its id: the result PREVIEW runs on
// every grid change, and minting there would fill the save with orphans.
func TestIdenticalStarsShareAnId(t *testing.T) {
	h := newHub(world.New(1))
	a := mustStar(t, h, "gunpowder", "red_dye")
	b := mustStar(t, h, "gunpowder", "red_dye")
	if a.starID != b.starID || a.starID == 0 {
		t.Fatalf("ids %d and %d, want one shared non-zero id", a.starID, b.starID)
	}
	c := mustStar(t, h, "gunpowder", "blue_dye")
	if c.starID == a.starID {
		t.Error("a different star should get a different id")
	}
	// …and the bursts survive a save.
	if got := unpackStack(packStack(a)); got.starID != a.starID {
		t.Errorf("starID %d did not survive a save (got %d)", a.starID, got.starID)
	}
}

// The rocket ENTITY carries the stack it was fired from, which is what the
// client draws the burst from when it pops. A rocket full of stars used to go
// off as nothing, because the entity was told only where it was.
func TestRocketEntityCarriesItsStack(t *testing.T) {
	h := newHub(world.New(1))
	star := mustStar(t, h, "gunpowder", "red_dye", "fire_charge")
	grid := make([]invStack, 9)
	grid[0] = invStack{item: itemPaper, count: 1}
	grid[1] = invStack{item: itemGunpowder, count: 1}
	grid[2] = star
	rocket, ok := h.fireworkRocketMatch(grid)
	if !ok {
		t.Fatal("no rocket")
	}
	tr := &tracked{p: newPlayer(1, "p", [16]byte{})}
	players := map[int32]*tracked{1: tr}
	drainEvents(tr)

	h.spawnRocket(players, 0, 0.5, 200, 0.5, 0, rocket)

	var meta *attachproto.EntityMeta
	for _, ev := range takeEvents(tr) {
		if m, ok := ev.(attachproto.EntityMeta); ok {
			meta = &m
		}
	}
	if meta == nil {
		t.Fatal("the rocket was spawned without its stack")
	}
	// Index 8, the ITEM_STACK serializer — the same pair a dropped item uses,
	// and the same on 1.21.11 and 26.x (Entity has eight synced fields in
	// both, so nothing shifts).
	if meta.Meta[0] != itemMetaIndexStack {
		t.Errorf("metadata index %d, want %d", meta.Meta[0], itemMetaIndexStack)
	}
	// …and what rides in it is the rocket with its burst.
	if !bytes.Contains(meta.Meta, []byte{byte(componentFireworks)}) {
		t.Error("the stack on the entity carries no fireworks component")
	}
}
