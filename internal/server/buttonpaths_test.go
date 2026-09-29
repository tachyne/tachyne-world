package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// useOnBody is a use-item-on packet for hand (0 main, 1 off) on (x,y,z).
func useOnBody(hand int32, x, y, z int, face int32) []byte {
	b := protocol.AppendVarInt(nil, hand)
	b = protocol.AppendPosition(b, x, y, z)
	b = protocol.AppendVarInt(b, face)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendBool(b, false)
	b = protocol.AppendBool(b, false)
	return protocol.AppendVarInt(b, 0)
}

// TestButtonPressPaths presses stone and wooden buttons on the floor, the
// ceiling and a wall through the click a client sends (ButtonBlock
// .useWithoutItem → press): the button powers the block it is on strongly,
// so a lamp on that block's far side lights; it clicks on (for everyone but
// the presser), stays down 20 ticks (stone) or 30 (wood), then clicks off
// and the lamp goes dark.
func TestButtonPressPaths(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	type mount struct {
		face, facing string
		d            [3]int // from the button to the block it is on
	}
	mounts := []mount{
		{"floor", "north", [3]int{0, -1, 0}},
		{"ceiling", "north", [3]int{0, 1, 0}},
		{"wall", "west", [3]int{1, 0, 0}},
	}
	x := 2
	for _, kind := range []struct {
		name  string
		ticks uint64
		sound string
	}{{"stone_button", 20, "stone_button"}, {"oak_button", 30, "wooden_button"}} {
		for _, m := range mounts {
			x += 4
			y, z := 180, 3
			btn := withProps(t, worldgen.BlockBase(kind.name), map[string]string{"face": m.face, "facing": m.facing, "powered": "false"})
			ax, ay, az := x+m.d[0], y+m.d[1], z+m.d[2]
			lx, ly, lz := ax+m.d[0], ay+m.d[1], az+m.d[2]
			onHub(t, h, func() {
				clearAirBox(w, x, y, z, 2)
				w.SetBlock(ax, ay, az, worldgen.Stone)
				w.SetBlock(lx, ly, lz, lampOff)
				w.SetBlock(x, y, z, btn)
			})
			drainOut(p)
			p.x, p.y, p.z = float64(x)-1.5, float64(y)-1, float64(z)+0.5
			s.handlePlace(p, useOnBody(0, x, y, z, 1))
			var pressedAt, releasedAt uint64
			var lit, darkAfter bool
			for i := 0; i < int(kind.ticks)+15; i++ {
				onHub(t, h, func() {
					now := h.tick.Load()
					b := w.Block(x, y, z)
					if boolProp(b, "powered") && pressedAt == 0 {
						pressedAt = now
					}
					if !boolProp(b, "powered") && pressedAt != 0 && releasedAt == 0 {
						releasedAt = now
					}
					if w.Block(lx, ly, lz) == lampOn && releasedAt == 0 {
						lit = true
					}
					darkAfter = releasedAt != 0 && w.Block(lx, ly, lz) == lampOff
				})
			}
			label := kind.name + " on the " + m.face
			if pressedAt == 0 {
				t.Errorf("%s: the click did not press it", label)
				continue
			}
			if !lit {
				t.Errorf("%s: the lamp beyond the block it is on never lit (no strong power)", label)
			}
			if held := releasedAt - pressedAt; releasedAt == 0 || held+1 < kind.ticks || held > kind.ticks+1 {
				t.Errorf("%s: stayed down %d ticks, want %d", label, held, kind.ticks)
			}
			if !darkAfter {
				t.Errorf("%s: the lamp stayed lit after the release", label)
			}
			on, off := 0, 0
			for done := false; !done; {
				select {
				case pkt := <-p.out:
					if ev, ok := pkt.ev.(attachproto.Sound); ok {
						switch ev.Name {
						case "minecraft:block." + kind.sound + ".click_on":
							on++
						case "minecraft:block." + kind.sound + ".click_off":
							off++
						}
					}
				default:
					done = true
				}
			}
			// ButtonBlock.playSound(player, …): the Java presser's client
			// played its own click-on; the release is everyone's.
			if on != 0 || off == 0 {
				t.Errorf("%s: the presser heard %d click-on and %d click-off sounds, want 0 and some", label, on, off)
			}
		}
	}
}

// TestButtonPowersPistons: a piston beside a pressed button extends (the
// button's own weak power), and so does one beside the block it is on (the
// strong power in that block).
func TestButtonPowersPistons(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 6, 180, 6
	btn := withProps(t, worldgen.BlockBase("stone_button"), map[string]string{"face": "wall", "facing": "west", "powered": "false"})
	piston := withProps(t, worldgen.BlockBase("piston"), map[string]string{"facing": "down", "extended": "false"})
	onHub(t, h, func() {
		clearAirBox(w, x, y, z, 3)
		w.SetBlock(x+1, y, z, worldgen.Stone) // the block the button is on
		w.SetBlock(x, y, z, btn)
		w.SetBlock(x, y-1, z, piston)   // beside the button
		w.SetBlock(x+1, y-1, z, piston) // beside the block, not the button
	})
	p.x, p.y, p.z = float64(x)-1.5, float64(y)-1, float64(z)+0.5
	s.handlePlace(p, useOnBody(0, x, y, z, 4))
	var direct, viaBlock bool
	for i := 0; i < 10; i++ {
		onHub(t, h, func() {
			direct = direct || boolProp(w.Block(x, y-1, z), "extended")
			viaBlock = viaBlock || boolProp(w.Block(x+1, y-1, z), "extended")
		})
	}
	if !direct {
		t.Error("a piston beside the button did not extend")
	}
	if !viaBlock {
		t.Error("a piston beside the block the button is on did not extend")
	}
}

// TestButtonClickHands: ServerPlayerGameMode.useItemOn — a player sneaking
// with anything in a hand does not use the block, and a block's own use
// (useWithoutItem) answers the main hand only; sneaking empty-handed still
// presses.
func TestButtonClickHands(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 6, 180, 10
	btn := withProps(t, worldgen.BlockBase("stone_button"), map[string]string{"face": "wall", "facing": "west", "powered": "false"})
	reset := func() {
		onHub(t, h, func() {
			clearAirBox(w, x, y, z, 1)
			w.SetBlock(x+1, y, z, worldgen.Stone)
			w.SetBlock(x, y, z, btn)
		})
	}
	pressed := func() bool {
		var on bool
		for i := 0; i < 3; i++ {
			onHub(t, h, func() { on = on || boolProp(w.Block(x, y, z), "powered") })
		}
		return on
	}
	p.x, p.y, p.z = float64(x)-1.5, float64(y)-1, float64(z)+0.5
	stick := itemByName["stick"]
	selectSlot(p, 0)

	reset()
	p.setHotbarSlot(0, stick)
	p.sneaking = true
	s.handlePlace(p, useOnBody(0, x, y, z, 4))
	if pressed() {
		t.Error("sneaking with a stick in hand pressed the button")
	}

	reset()
	p.setHotbarSlot(0, 0)
	s.handlePlace(p, useOnBody(0, x, y, z, 4))
	if !pressed() {
		t.Error("sneaking empty-handed should still press the button")
	}

	reset()
	p.sneaking = false
	s.handlePlace(p, useOnBody(1, x, y, z, 4))
	if pressed() {
		t.Error("an offhand click pressed the button (useWithoutItem is main hand only)")
	}

	reset()
	p.setHotbarSlot(0, stick)
	s.handlePlace(p, useOnBody(0, x, y, z, 4))
	if !pressed() {
		t.Error("a main-hand click holding a stick should press the button")
	}
}

// The server's click-on skips only a Java presser: a bystander hears it,
// and so does a Bedrock presser, whose client plays no click of its own.
func TestButtonClickSkipsOnlyTheJavaPresser(t *testing.T) {
	h := newTestHub(world.New(1))
	x, y, z := 0, 180, 0
	h.world.ForceLoad(x, z, 1)
	btn := withProps(t, worldgen.BlockBase("stone_button"), map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	heard := func(p *player) bool {
		for {
			select {
			case pkt := <-p.out:
				if ev, ok := pkt.ev.(attachproto.Sound); ok && ev.Name == "minecraft:block.stone_button.click_on" {
					return true
				}
			default:
				return false
			}
		}
	}
	for _, bedrock := range []bool{false, true} {
		presser, other := testTracked(), testTracked()
		other.p = newPlayer(2, "other", [16]byte{2})
		presser.p.bedrock = bedrock
		players := map[int32]*tracked{1: presser, 2: other}
		h.world.SetBlock(x, y, z, btn)
		h.pressButton(players, blockPos{x, y, z}, btn, presser)
		if got := heard(presser.p); got != bedrock {
			t.Errorf("bedrock %v: the presser heard the click %v", bedrock, got)
		}
		if !heard(other.p) {
			t.Errorf("bedrock %v: a bystander did not hear the click", bedrock)
		}
	}
}
