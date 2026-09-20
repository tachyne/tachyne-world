package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestParseTargetSpec(t *testing.T) {
	if s, ok := parseTargetSpec("Steve"); !ok || s.kind != 0 || s.name != "Steve" {
		t.Fatalf("a bare word is a player name: %+v", s)
	}
	s, ok := parseTargetSpec("@e[type=zombie,distance=..16,limit=3]")
	if !ok {
		t.Fatal("a predicate list should parse")
	}
	if s.kind != 'e' || s.etype != "zombie" || s.maxDist != 16 || s.limit != 3 || !s.nearest {
		t.Fatalf("predicates: %+v", s)
	}
	if s2, _ := parseTargetSpec("@a[distance=5..]"); s2.minDist != 5 || s2.maxDist != 0 {
		t.Fatalf("an open-ended range: %+v", s2)
	}
	if s3, _ := parseTargetSpec("@e[type=!player]"); s3.notEtype != "player" {
		t.Fatalf("a negated type: %+v", s3)
	}
	if _, ok := parseTargetSpec("@q"); ok {
		t.Error("@q is not a selector")
	}
}

func TestSelectPlayersAndMobs(t *testing.T) {
	h := newHub(world.New(1))
	me := testTracked()
	me.p.name = "Me"
	me.x, me.y, me.z = 0, 70, 0
	far := testTracked()
	far.p.name, far.p.eid = "Far", 2
	far.x, far.y, far.z = 40, 70, 0
	players := map[int32]*tracked{me.p.eid: me, far.p.eid: far}

	if got := h.commandTargets(players, me.p.eid, "@s"); len(got) != 1 || got[0] != me {
		t.Fatalf("@s is the caller: %v", got)
	}
	if got := h.commandTargets(players, me.p.eid, "@a"); len(got) != 2 {
		t.Fatalf("@a is everyone, got %d", len(got))
	}
	if got := h.commandTargets(players, me.p.eid, "@p"); len(got) != 1 || got[0] != me {
		t.Fatalf("@p is the nearest (the caller), got %v", got)
	}
	if got := h.commandTargets(players, me.p.eid, "@a[distance=..10]"); len(got) != 1 || got[0] != me {
		t.Fatalf("a distance predicate should drop the far player: %v", got)
	}
	if got := h.commandTargets(players, me.p.eid, "Far"); len(got) != 1 || got[0] != far {
		t.Fatalf("a literal name still works: %v", got)
	}
	// Mobs: @e[type=…] selects entities, never players.
	z := h.spawnMob(players, entityZombie, 3, 70, 0)
	z.hostile = true
	h.spawnMob(players, entityCow, 3, 70, 2)
	if got := h.commandMobs(players, me.p.eid, "@e[type=zombie]"); len(got) != 1 || got[0] != z {
		t.Fatalf("@e[type=zombie] should find the zombie, got %d", len(got))
	}
	if got := h.commandMobs(players, me.p.eid, "@e[type=zombie,distance=..1]"); len(got) != 0 {
		t.Fatalf("a tight distance should exclude it, got %d", len(got))
	}
	if got := h.commandTargets(players, me.p.eid, "@e[type=zombie]"); len(got) != 0 {
		t.Fatalf("@e[type=zombie] matches no players, got %d", len(got))
	}
}

func TestParsePositionRelativeAndLocal(t *testing.T) {
	x, y, z, ok := parsePosition([]string{"~", "~10", "~-5"}, 100, 64, 200, 0, 0)
	if !ok || x != 100 || y != 74 || z != 195 {
		t.Fatalf("~ coordinates: %v %v %v ok=%v", x, y, z, ok)
	}
	if _, _, _, ok := parsePosition([]string{"1", "2", "3"}, 0, 0, 0, 0, 0); !ok {
		t.Fatal("plain numbers should parse")
	}
	// Looking due south (yaw 0) with no pitch: ^ ^ ^5 is five blocks ahead.
	x, y, z, ok = parsePosition([]string{"^", "^", "^5"}, 0, 64, 0, 0, 0)
	if !ok || math.Abs(x) > 1e-9 || math.Abs(y-64) > 1e-9 || math.Abs(z-5) > 1e-9 {
		t.Fatalf("^ forward: %v %v %v ok=%v", x, y, z, ok)
	}
	// ^2 to the left of due south is +x… the basis must at least be orthogonal.
	x, _, z, _ = parsePosition([]string{"^2", "^", "^"}, 0, 64, 0, 0, 0)
	if math.Abs(math.Hypot(x, z)-2) > 1e-9 {
		t.Fatalf("^ sideways should be two blocks from the origin: %v %v", x, z)
	}
	if _, _, _, ok := parsePosition([]string{"^1", "~2", "3"}, 0, 0, 0, 0, 0); ok {
		t.Error("mixing ^ and ~ is refused, as vanilla refuses it")
	}
}
