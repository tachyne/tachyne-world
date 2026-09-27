package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// columnPool is a pool at y=180 with a bubble column in its middle cell
// (0, 0) from y=176 up, over soul sand or magma, and air above.
func columnPool(t *testing.T, col uint32) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.world
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 175, z, worldgen.Stone)
			for y := 176; y <= 180; y++ {
				w.SetBlock(x, y, z, worldgen.WaterBase)
			}
			for y := 181; y <= 186; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	for y := 176; y <= 180; y++ {
		w.SetBlock(0, y, 0, col)
	}
	return h, players
}

// AbstractBoat over a whirlpool: the bubble timer starts at sixty and
// counts down while the boat stays over the column's top; at zero the
// riders are thrown out. Leaving the column resets it.
func TestBoatOverWhirlpoolEjects(t *testing.T) {
	h, players := columnPool(t, worldgen.BubbleColumnDrag)
	v := h.addVehicle(players, 0, entityByName["oak_boat"], 0.5, 180.4, 0.5, 0)
	rider := survPlayer(h)
	rider.p.eid = 301
	rider.x, rider.y, rider.z = 0.5, 180.5, 0.5
	players[rider.p.eid] = rider
	h.mountVehicle(players, rider, v)
	if h.boatController(v) != rider.p.eid {
		t.Fatal("the rider should be aboard")
	}
	h.updateVehicles(players)
	if v.bubbleTime != boatBubbleTime-1 {
		t.Fatalf("over the column's top the timer starts: %d", v.bubbleTime)
	}
	for i := 0; i < boatBubbleTime && rider.ridingEID != 0; i++ {
		h.updateVehicles(players)
	}
	if rider.ridingEID != 0 {
		t.Fatal("a whirlpool throws the rider out when the timer runs out")
	}
	v.x, v.z = 2.5, 2.5 // off the column
	h.updateVehicles(players)
	if v.bubbleTime != 0 {
		t.Fatalf("off the column the timer resets: %d", v.bubbleTime)
	}
}

// Over an updraft an empty boat is tossed up when the timer runs out
// (handleBubbleColumnEffect(false): dy = 0.6).
func TestBoatOverUpdraftIsTossed(t *testing.T) {
	h, players := columnPool(t, worldgen.BubbleColumnUp)
	v := h.addVehicle(players, 0, entityByName["oak_boat"], 0.5, 180.4, 0.5, 0)
	top := v.y
	for i := 0; i < boatBubbleTime+5; i++ {
		h.updateVehicles(players)
		top = max(top, v.y)
	}
	if top < 181 {
		t.Fatalf("an updraft tosses the boat when its timer runs out: top %.2f", top)
	}
}

// HoneyBlock.entityInside slides an item falling past a honey side: it
// comes down at the slide, not in free fall.
func TestItemSlidesDownHoneyWall(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 196, worldgen.Stone, worldgen.Air)
	shaft(w, 3, 0, 180, 196, worldgen.Stone, worldgen.Air)
	honey := worldgen.BlockBase("honey_block")
	for y := 180; y <= 196; y++ {
		w.SetBlock(1, y, 0, honey)
	}
	slide := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, 0.9, 195, 0.5, 0, 0, 0)
	free := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, 3.5, 195, 0.5, 0, 0, 0)
	for i := 0; i < 15; i++ {
		h.tickItems(players)
	}
	if slide.vy < -0.2 || slide.y < 190 {
		t.Fatalf("an item on a honey side should slide: y %.2f vy %.3f", slide.y, slide.vy)
	}
	if free.y > slide.y-1 {
		t.Fatalf("a free item should fall faster: free %.2f honey %.2f", free.y, slide.y)
	}
}

// Entity.onAboveBubbleColumn throws a swimmer in an updraft's top cell
// clear of the surface.
func TestSwimmerThrownFromColumnTop(t *testing.T) {
	h, players := columnPool(t, worldgen.BubbleColumnUp)
	cod := h.spawnMob(players, entityCod, 0.5, 180.3, 0.5)
	for i := 0; i < 10 && !cod.leaping; i++ {
		h.swimMove(cod, cod.x, cod.z, 0, 0)
	}
	if !cod.leaping || cod.leapVY <= columnTopUpStep {
		t.Fatalf("the top cell should launch the cod: leaping %v vy %.3f", cod.leaping, cod.leapVY)
	}
	top := cod.y
	for i := 0; i < 20 && cod.leaping; i++ {
		h.leapFlight(players, cod)
		top = max(top, cod.y)
	}
	if top <= 181 {
		t.Fatalf("the cod should clear the surface: top %.2f", top)
	}
	down, _ := columnPool(t, worldgen.BubbleColumnDrag)
	cod2 := down.spawnMob(players, entityCod, 0.5, 180.3, 0.5)
	cod2.vx, cod2.vy, cod2.vz = 0, 0, 0
	for i := 0; i < 8; i++ {
		down.swimMove(cod2, cod2.x, cod2.z, 0, 0)
	}
	if cod2.leaping || cod2.y >= 180 {
		t.Fatalf("a whirlpool's top pulls down: y %.3f", cod2.y)
	}
}
