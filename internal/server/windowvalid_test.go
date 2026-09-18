package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func closeFrames(pl *tracked) []attachproto.WindowCloseServer {
	var out []attachproto.WindowCloseServer
	for {
		select {
		case pkt := <-pl.p.out:
			if c, ok := pkt.ev.(attachproto.WindowCloseServer); ok {
				out = append(out, c)
			}
		default:
			return out
		}
	}
}

// A dispenser's menu stays open while the player is near and the block
// stands; walking out of reach closes it from the server, and so does the
// block going.
func TestMenuClosesWhenInvalid(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	disp := worldgen.BlockID("dispenser")
	h.world.SetBlock(2, 70, 0, disp)
	h.openBin(pl, 2, 70, 0)
	if pl.winKind != winBin {
		t.Fatalf("no bin window: kind %d", pl.winKind)
	}
	id := pl.winID
	drainEvents(pl)
	h.validateWindows(players)
	if pl.winKind != winBin || len(closeFrames(pl)) != 0 {
		t.Fatal("a valid menu closed")
	}
	pl.x = 7.5 // eight and a half blocks is the edge: still in reach
	h.validateWindows(players)
	if pl.winKind != winBin {
		t.Fatal("a menu within reach closed")
	}
	pl.x = 12.5
	h.validateWindows(players)
	got := closeFrames(pl)
	if pl.winKind != winPlayer || len(got) != 1 || got[0].ID != int32(id) {
		t.Fatalf("out of reach: kind %d frames %+v want a close of window %d", pl.winKind, got, id)
	}
	pl.x = 0.5
	h.openBin(pl, 2, 70, 0)
	drainEvents(pl)
	h.world.SetBlock(2, 70, 0, worldgen.Air)
	h.validateWindows(players)
	if pl.winKind != winPlayer || len(closeFrames(pl)) != 1 {
		t.Fatal("the menu should close when its block goes")
	}
}

// A trade screen closes when the villager dies or the player walks past
// entity reach plus four of it.
func TestTradeClosesWhenInvalid(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	m := h.spawnMob(players, entityVillager, 2.5, 70, 0.5)
	h.initVillagerTrades(m, 0)
	h.openTrades(pl, m)
	if pl.winKind != winTrade || pl.tradeWith != m.eid {
		t.Fatalf("no trade screen: kind %d with %d", pl.winKind, pl.tradeWith)
	}
	drainEvents(pl)
	h.validateWindows(players)
	if pl.winKind != winTrade {
		t.Fatal("a valid trade screen closed")
	}
	pl.x = 12.5
	h.validateWindows(players)
	if pl.winKind != winPlayer || len(closeFrames(pl)) != 1 {
		t.Fatal("walking off should close the trade screen")
	}
	pl.x = 0.5
	h.openTrades(pl, m)
	drainEvents(pl)
	h.killMob(players, m)
	h.validateWindows(players)
	if pl.winKind != winPlayer || len(closeFrames(pl)) != 1 {
		t.Fatal("the villager dying should close the trade screen")
	}
}
