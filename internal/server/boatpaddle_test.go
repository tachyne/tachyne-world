package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Rowing (handlePaddleBoat → AbstractBoat.tick): the driver's paddle input
// becomes the boat's synced paddle state, and each paddle turning through
// its stroke plays the water paddle sound for everyone near — twice a turn
// of sixteen ticks, as vanilla's float test lets two ticks through. A
// passenger's input does nothing; letting go stops the sound.
func TestBoatPaddlesSoundAndSync(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.WaterBase)
		}
	}
	pl, watcher := survPlayer(h), survPlayer(h)
	watcher.p.eid = pl.p.eid + 1000
	players := map[int32]*tracked{pl.p.eid: pl, watcher.p.eid: watcher}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180.2, 0.5
	watcher.x, watcher.y, watcher.z = 3.5, 181, 3.5
	v := &vehicle{eid: h.allocEID(), etype: entityByName["oak_boat"], x: 0.5, y: 179.6, z: 0.5, rider: pl.p.eid}
	h.vehicles[v.eid] = v
	pl.ridingEID = v.eid

	row := func(who *tracked, l, r bool) {
		rp := &remotePlayer{s: &Server{hub: h}, p: who.p, gm: -1}
		rp.Action(attachproto.PaddleBoat{Left: l, Right: r})
		for len(h.events) > 0 {
			if ev, ok := (<-h.events).(evPaddleBoat); ok {
				h.paddleBoat(players, players[ev.eid], ev.left, ev.right)
			}
		}
	}
	strokes := func(ticks int) int {
		drainFXSounds(watcher)
		for i := 0; i < ticks; i++ {
			h.updateVehicles(players)
		}
		_, sounds := drainFXSounds(watcher)
		n := 0
		for _, s := range sounds {
			if s == "minecraft:entity.boat.paddle_water" {
				n++
			}
		}
		return n
	}

	row(watcher, true, true) // not aboard: ignored
	if v.paddle != [2]bool{} {
		t.Fatal("a player on the shore set the boat's paddles")
	}
	row(pl, true, true)
	if v.paddle != [2]bool{true, true} {
		t.Fatalf("the driver's paddles did not take: %v", v.paddle)
	}
	if n := strokes(16); n != 4 {
		t.Fatalf("a turn of both paddles played %d paddle sounds, want 4", n)
	}
	row(pl, false, false)
	if n := strokes(16); n != 0 {
		t.Fatalf("resting paddles played %d sounds", n)
	}
	row(pl, true, false)
	pl.ridingEID, v.rider = 0, 0 // the driver gets out
	h.updateVehicles(players)
	if v.paddle != [2]bool{} {
		t.Fatal("an empty boat kept rowing")
	}
}
