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
	if out := math.Hypot(pl.x, pl.z); out < endGatewayCast-16*16 {
		t.Fatalf("stepping into a gateway should throw you out to the islands, got r=%.0f", out)
	}
	if !worldgen.IsFullCube(h.end.At(int(math.Floor(pl.x)), int(pl.y)-1, int(math.Floor(pl.z)))) {
		t.Fatal("landed on nothing — the gateway must leave somewhere to stand")
	}
	// The cooldown holds you in place for a moment.
	before := pl.x
	h.updateEndGateways(players)
	if pl.x != before {
		t.Fatal("the gateway took the player again inside its cooldown")
	}

	// The far gateway hangs over the island and the two are a pair.
	far, _, ok := h.gatewayExitOf(g)
	if !ok || h.end.At(far.x, far.y, far.z) != endGatewayState {
		t.Fatalf("no far gateway recorded/built for %v (got %v ok=%v)", g, far, ok)
	}
	if back, _, ok := h.gatewayExitOf(far); !ok || back != g {
		t.Fatalf("the far gateway leads to %v, want the ring gateway %v", back, g)
	}
	if math.Abs(pl.x-float64(far.x)) > 6 || math.Abs(pl.z-float64(far.z)) > 6 {
		t.Fatalf("landed at %.0f,%.0f, not beside the far gateway %v", pl.x, pl.z, far)
	}
	// Through it again, you come out at the ring gateway you left by, not
	// at the centre of the main island.
	h.gatewayCool = map[simPos]uint64{}
	pl.x, pl.y, pl.z = float64(far.x)+0.5, float64(far.y), float64(far.z)+0.5
	h.updateEndGateways(players)
	if math.Abs(pl.x-float64(g.x)) > 6 || math.Abs(pl.z-float64(g.z)) > 6 {
		t.Fatalf("the way home landed at %.0f,%.0f, want beside the ring gateway %v", pl.x, pl.z, g)
	}
	// A second trip out goes to the same far gateway: nothing new is built.
	h.gatewayCool = map[simPos]uint64{}
	n := len(h.rules.EndGateways)
	pl.x, pl.y, pl.z = float64(g.x)+0.5, float64(g.y), float64(g.z)+0.5
	h.updateEndGateways(players)
	if len(h.rules.EndGateways) != n || math.Abs(pl.x-float64(far.x)) > 6 {
		t.Fatalf("second trip: %d links (was %d), landed %.0f,%.0f", len(h.rules.EndGateways), n, pl.x, pl.z)
	}
}
