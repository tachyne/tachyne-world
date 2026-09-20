package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A spawner block draws an empty cage until the client is told what is
// inside it. The frame names the mob and carries BaseSpawner's ranges.
func TestShowSpawnerNamesItsMob(t *testing.T) {
	h := newHub(world.New(1))
	t1 := watcher(1, 0, 10, 70, 10)
	t2 := watcher(2, 0, 400, 70, 400) // far away
	t3 := watcher(3, 1, 10, 70, 10)   // right there, wrong dimension
	players := map[int32]*tracked{1: t1, 2: t2, 3: t3}

	h.showSpawner(players, 0, blockPos{10, 70, 12}, entityBlaze)

	got := takeEvents(t1)
	if len(got) != 1 {
		t.Fatalf("the player beside it got %d frames, want 1", len(got))
	}
	e, ok := got[0].(attachproto.SpawnerData)
	if !ok {
		t.Fatalf("sent %T, want SpawnerData", got[0])
	}
	if e.Entity != "minecraft:blaze" {
		t.Errorf("entity %q, want minecraft:blaze", e.Entity)
	}
	if e.X != 10 || e.Y != 70 || e.Z != 12 {
		t.Errorf("position %d,%d,%d, want 10,70,12", e.X, e.Y, e.Z)
	}
	if e.PlayerRange != spawnerRange || e.SpawnCount != spawnerCount {
		t.Errorf("ranges %+v do not match the spawner's own", e)
	}
	if n := len(takeEvents(t2)); n != 0 {
		t.Errorf("a player far away got %d frames, want none", n)
	}
	if n := len(takeEvents(t3)); n != 0 {
		t.Errorf("a player in another dimension got %d frames, want none", n)
	}
}

// watcher is a tracked player whose outbound frames can be read back.
func watcher(eid int32, dim int, x, y, z float64) *tracked {
	return &tracked{p: newPlayer(eid, "p", [16]byte{}), dim: dim, x: x, y: y, z: z}
}

// takeEvents empties a player's outbound queue and hands back what was in it
// (drainEvents throws them away; drain only counts them).
func takeEvents(t *tracked) []any {
	var out []any
	for {
		select {
		case pk := <-t.p.out:
			out = append(out, pk.ev)
		default:
			return out
		}
	}
}

// An entity type with no name is not sent at all rather than sent empty: an
// empty SpawnData would tell the client to draw nothing, which is a
// different thing from "I do not know".
func TestShowSpawnerSkipsUnknownMobs(t *testing.T) {
	h := newHub(world.New(1))
	tr := watcher(1, 0, 10, 70, 10)
	h.showSpawner(map[int32]*tracked{1: tr}, 0, blockPos{10, 70, 12}, 99999)
	if n := len(takeEvents(tr)); n != 0 {
		t.Errorf("sent %d frames for an unknown mob, want none", n)
	}
}

func TestSpawnerEntityNameIsQualified(t *testing.T) {
	if got := spawnerEntityName(entityBlaze); got != "minecraft:blaze" {
		t.Errorf("got %q, want minecraft:blaze", got)
	}
	if got := spawnerEntityName(99999); got != "" {
		t.Errorf("an unknown type gave %q, want empty", got)
	}
}
