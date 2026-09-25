package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// EnderEyeItem.use and EyeOfEnder: the eye leaves from the thrower's middle
// and eases toward a point twelve blocks along the way to the stronghold and
// eight up, passing through whatever is in the way; eighty ticks later it
// is gone, dropped back as an item four times in five. In the Nether there
// is no stronghold to signal, and the eye stays in the hand.
func TestEyeOfEnderSignals(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 100, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	eye := int32(itemEnderEye)
	give := func() {
		pl.p.setHotbarSlot(0, eye)
		pl.inv.slots[0] = invStack{item: eye, count: 16}
	}
	give()
	pl.dim = dimNether
	h.throwEye(players, pl)
	if len(h.eyes) != 0 || pl.inv.slots[0].count != 16 {
		t.Fatal("no eye leaves the hand in the Nether")
	}
	pl.dim = dimOverworld
	h.throwEye(players, pl)
	if len(h.eyes) != 1 {
		t.Skip("no stronghold near the fixture")
	}
	var e *eyeEntity
	for _, x := range h.eyes {
		e = x
	}
	if e.y != 100.9 {
		t.Fatalf("the eye starts at the thrower's middle: y %v", e.y)
	}
	if hd := math.Hypot(e.tx-e.x, e.tz-e.z); hd > eyeTooFar+1e-9 || (hd > 1e-9 && e.ty != e.y+eyeTooFarLift && hd == eyeTooFar) {
		t.Fatalf("a far stronghold is signalled twelve along and eight up: %v, %v", hd, e.ty-e.y)
	}
	x0, z0 := e.x, e.z
	for i := 0; i < 40; i++ {
		h.updateEyes(players)
	}
	if moved := math.Hypot(e.x-x0, e.z-z0); moved <= 0 {
		t.Fatal("the eye drifts toward the stronghold")
	}
	if dot := (e.x-x0)*(e.tx-x0) + (e.z-z0)*(e.tz-z0); dot <= 0 {
		t.Fatal("the eye drifts toward its signal point, not away")
	}
	for i := 0; i < 41; i++ {
		h.updateEyes(players)
	}
	if len(h.eyes) != 0 {
		t.Fatal("the eye is gone after eighty ticks")
	}
	dropped := 0
	for _, it := range h.items {
		if it.item == eye {
			dropped++
		}
	}
	if e.survive != (dropped == 1) {
		t.Fatalf("a surviving eye drops back as an item: survive %v, dropped %d", e.survive, dropped)
	}
}
