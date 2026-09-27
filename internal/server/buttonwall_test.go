package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestButtonTrapdoorWallSignal rebuilds one row of a player's lift call
// panel: a button on a block powers the trapdoor behind it; the trapdoor
// swings open against a straight run of walls, which joins its hinge side
// (WallBlock.connectsTo asks isFaceSturdy of that face, and an open
// trapdoor's hinge side is a full face). The wall turns into a T and raises
// its post, the posts below follow it down the column, and the observer at
// the foot sees the change and lights a lamp. The engine asked for an
// opaque full cube instead, so the wall never joined and the press went
// nowhere.
func TestButtonTrapdoorWallSignal(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 2, 182, 2 // the button; the build runs east of it
	wall := withProps(t, worldgen.BlockBase("diorite_wall"), map[string]string{"up": "false", "waterlogged": "false"})
	var (
		trap  = withProps(t, worldgen.BlockBase("poplar_trapdoor"), map[string]string{"facing": "west", "half": "top", "open": "false", "powered": "false", "waterlogged": "false"})
		btn   = withProps(t, worldgen.BlockBase("polished_blackstone_button"), map[string]string{"face": "wall", "facing": "west", "powered": "false"})
		obs   = withProps(t, worldgen.BlockBase("observer"), map[string]string{"facing": "up", "powered": "false"})
		quart = worldgen.BlockBase("quartz_bricks")
	)
	onHub(t, h, func() {
		for yy := y - 6; yy <= y+3; yy++ {
			for xx := x - 1; xx <= x+5; xx++ {
				for zz := z - 2; zz <= z+2; zz++ {
					w.SetBlock(xx, yy, zz, worldgen.Air)
				}
			}
		}
		w.SetBlock(x+1, y, z, quart)
		w.SetBlock(x, y, z, btn)
		w.SetBlock(x+2, y, z, trap)
		for yy := y; yy >= y-2; yy-- { // three walls high, three long north-south
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(x+3, yy, z+dz, wall)
			}
		}
		for yy := y; yy >= y-2; yy-- {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(x+3, yy, z+dz, connectStateAt(w, x+3, yy, z+dz, w.Block(x+3, yy, z+dz)))
			}
		}
		w.SetBlock(x+3, y-3, z, obs)     // watching the foot of the middle column
		w.SetBlock(x+3, y-4, z, lampOff) // lit by the observer's pulse
		w.SetBlock(x+3, y-5, z, worldgen.Stone)
		h.rescheduleRedstone() // the observer takes its first look, as on load
	})
	var foot uint32
	onHub(t, h, func() { foot = w.Block(x+3, y-2, z) })
	if boolProp(foot, "up") {
		t.Fatalf("setup: the middle of a straight wall run should have no post, got state %d", foot)
	}

	p.x, p.y, p.z = float64(x)-1.5, float64(y), float64(z)+0.5 // standing in front of the panel
	if !s.tryUseBlock(p, false, x, y, z, 1, 4, float32(x)+0.9, float32(y)+0.5, float32(z)+0.5) {
		t.Fatal("the click on the button was not used")
	}
	var joined, posted, lit bool
	deadline := time.Now().Add(hubTestWait)
	for i := 0; i < 40 && time.Now().Before(deadline); i++ {
		onHub(t, h, func() {
			top := w.Block(x+3, y, z)
			if info, ok := wallInfo(top); ok && worldgen.GetProperty(info, top, "west") != "none" {
				joined = true
			}
			if boolProp(w.Block(x+3, y-2, z), "up") {
				posted = true
			}
			if w.Block(x+3, y-4, z) == lampOn {
				lit = true
			}
		})
	}
	if !joined {
		t.Fatal("the wall did not join the open trapdoor's hinge side")
	}
	if !posted {
		t.Fatal("the post did not run down the wall column")
	}
	if !lit {
		t.Fatal("the observer at the foot of the column never fired")
	}
}
