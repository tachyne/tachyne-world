package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// EndDragonFight.spawnNewGateway: one gateway per kill, from the shuffled
// ring, until all twenty stand.
func TestDragonDeathBuildsTheGatewayRing(t *testing.T) {
	h, pl, players := endHub(t)
	pl.dim = 2
	for kill := 1; kill <= endGatewayCount+1; kill++ {
		h.dragonDefeated(players)
		if want := min(kill, endGatewayCount); h.endGatewaysOpen() != want {
			t.Fatalf("after kill %d: %d gateways, want %d", kill, h.endGatewaysOpen(), want)
		}
	}
	for i := 0; i < endGatewayCount; i++ {
		p := endGatewayRingPos(i)
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

// firstGateway is the ring position the first kill opened.
func firstGateway(h *hub) blockPos {
	o := h.endGatewayOrder()
	return endGatewayRingPos(o[len(o)-1])
}

func TestGatewayThrowsYouOutAndBringsYouBack(t *testing.T) {
	h, pl, players := endHub(t)
	pl.dim = 2
	h.dragonDefeated(players)

	g := firstGateway(h)
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

// EndGatewayBlock.entityInside carries more than players: a mob, a dropped
// item and a thrown ender pearl all go through, and the pearl arrives at rest.
func TestGatewayCarriesMobsItemsAndPearls(t *testing.T) {
	h, pl, players := endHub(t)
	pl.dim = 2
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	h.dragonDefeated(players)
	g := firstGateway(h)
	gx, gy, gz := float64(g.x)+0.5, float64(g.y), float64(g.z)+0.5

	m := h.spawnMobIn(players, entityEnderman, 2, gx, gy, gz)
	if m == nil {
		t.Fatal("no enderman")
	}
	h.tick.Store(1)
	h.updateEndGateways(players)
	if math.Hypot(m.x, m.z) < endGatewayCast-16*16 {
		t.Fatalf("the enderman should have gone out to the islands, at r=%.0f", math.Hypot(m.x, m.z))
	}
	if m.portalCool == 0 {
		t.Fatal("a mob through a gateway serves the portal cooldown")
	}

	h.gatewayCool = map[simPos]uint64{}
	it := h.spawnItemIn(players, 2, itemEnderPearl, 1, gx, gy, gz)
	it.x, it.y, it.z = gx, gy, gz
	h.updateEndGateways(players)
	if math.Hypot(it.x, it.z) < endGatewayCast-16*16 {
		t.Fatalf("the item should have gone through, at r=%.0f", math.Hypot(it.x, it.z))
	}

	h.gatewayCool = map[simPos]uint64{}
	a := h.launchProjectileIn(players, entityPearlProj, 2, gx, gy+0.2, gz, 0.4, 0.1, 0.4)
	a.pearl, a.shooter = true, pl.p.eid
	h.updateEndGateways(players)
	if math.Hypot(a.x, a.z) < endGatewayCast-16*16 {
		t.Fatalf("the pearl should have gone through, at r=%.0f", math.Hypot(a.x, a.z))
	}
	if a.vx != 0 || a.vy != 0 || a.vz != 0 {
		t.Fatalf("a pearl comes out of a gateway at rest, got (%v,%v,%v)", a.vx, a.vy, a.vz)
	}
}

// portalTick: an idle gateway flashes its beam every 2400 ticks.
func TestGatewayAttentionBeam(t *testing.T) {
	h, pl, players := endHub(t)
	pl.dim = 2
	h.dragonDefeated(players)
	g := firstGateway(h)
	pl.x, pl.y, pl.z = float64(g.x)+3, float64(g.y), float64(g.z)
	h.tick.Store(endGatewayAttention)
	h.updateEndGateways(players)
	if h.gatewayCool[simPos{dim: 2, blockPos: g}] == 0 {
		t.Fatal("the gateway should have fired its attention beam")
	}
}
