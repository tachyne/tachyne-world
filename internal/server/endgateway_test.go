package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestDragonDeathBuildsTheGatewayRing(t *testing.T) {
	h, pl, players := endHub(t)
	pl.dim = 2
	h.dragonDefeated(players)
	for i := 0; i < endGatewayCount; i++ {
		p := endGatewayRingPos(i)
		if got := h.end.At(p.x, p.y, p.z); got != endGatewayState {
			t.Fatalf("gateway %d missing at %v: state %d", i, p, got)
		}
		if r := math.Hypot(float64(p.x), float64(p.z)); r < 95 || r > 97 {
			t.Fatalf("gateway %d off the ring: r=%.1f", i, r)
		}
		// The frame: bedrock above and below, air across the middle layer.
		if h.end.At(p.x, p.y+2, p.z) != worldgen.Bedrock {
			t.Fatalf("gateway %d has no cap", i)
		}
		if h.end.At(p.x+1, p.y, p.z) != worldgen.Air {
			t.Fatalf("gateway %d doorway is blocked", i)
		}
	}
}

func TestGatewayThrowsYouOutAndBringsYouBack(t *testing.T) {
	h, pl, players := endHub(t)
	pl.dim = 2
	h.dragonDefeated(players)

	g := endGatewayRingPos(0)
	pl.x, pl.y, pl.z = float64(g.x)+0.5, float64(g.y), float64(g.z)+0.5
	h.updateEndGateways(players)
	out := math.Hypot(pl.x, pl.z)
	if out < endGatewayCast {
		t.Fatalf("stepping into a gateway should throw you at least 1024 blocks out, got %.0f", out)
	}
	if h.end.At(int(pl.x), int(pl.y)-1, int(pl.z)) == worldgen.Air {
		t.Fatal("landed on nothing — the gateway must leave somewhere to stand")
	}
	// The cooldown holds you in place for a moment.
	before := pl.x
	h.updateEndGateways(players)
	if pl.x != before {
		t.Fatal("the gateway took the player again inside its cooldown")
	}

	// The gateway home sits beside the landing and returns you to the island.
	back := blockPos{x: int(math.Floor(pl.x)) + 2, y: int(pl.y), z: int(math.Floor(pl.z))}
	if h.end.At(back.x, back.y, back.z) != endGatewayState {
		t.Fatalf("no gateway home at %v", back)
	}
	pl.gatewayUntil = 0
	pl.x, pl.y, pl.z = float64(back.x)+0.5, float64(back.y), float64(back.z)+0.5
	h.updateEndGateways(players)
	if r := math.Hypot(pl.x, pl.z); r > 4 {
		t.Fatalf("the way home should land you on the main island, got r=%.0f", r)
	}
}
