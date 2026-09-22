package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The server list showed 0/100 with a player in game: the gateway answered
// from its own books, which hold only the clients on its protocol range.
// The roster has to come from the world, and this is what it says.
func TestStatusRosterCountsPlayers(t *testing.T) {
	h := newHub(world.New(1))
	s := &Server{world: h.world, hub: h}
	startHub(t, h)

	if got := s.statusRoster(); got.Online != 0 || got.Max != statusMaxPlayers {
		t.Fatalf("an empty world reports %d/%d, want 0/%d", got.Online, got.Max, statusMaxPlayers)
	}

	for _, name := range []string{"LegionZA", "wesley"} {
		p := newPlayer(h.allocEID(), name, [16]byte{1, 2, 3})
		sy := h.world.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		h.post(evJoin{p: p, x: p.x, y: p.y, z: p.z, gamemode: gmCreative})
		waitJoined(t, h, name)
	}

	got := s.statusRoster()
	if got.Online != 2 {
		t.Errorf("two players in game report %d online", got.Online)
	}
	if len(got.Sample) != 2 {
		t.Fatalf("the hover card lists %d of 2 players", len(got.Sample))
	}
	var names []string
	for _, pl := range got.Sample {
		names = append(names, pl.Name)
		// 8-4-4-4-12, as the status schema wants it.
		if parts := strings.Split(pl.ID, "-"); len(parts) != 5 || len(parts[0]) != 8 || len(parts[4]) != 12 {
			t.Errorf("%s carries id %q, want the dashed form", pl.Name, pl.ID)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "LegionZA") || !strings.Contains(joined, "wesley") {
		t.Errorf("the hover card lists %q, want both players", joined)
	}
}

// The sample is capped the way vanilla caps it, however many are on.
func TestStatusSampleCapped(t *testing.T) {
	h := newHub(world.New(1))
	s := &Server{world: h.world, hub: h}
	startHub(t, h)

	for i := 0; i < statusSampleLimit+5; i++ {
		p := newPlayer(h.allocEID(), string(rune('a'+i)), [16]byte{byte(i)})
		sy := h.world.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		h.post(evJoin{p: p, x: p.x, y: p.y, z: p.z, gamemode: gmCreative})
		waitJoined(t, h, p.name)
	}
	got := s.statusRoster()
	if got.Online != statusSampleLimit+5 {
		t.Errorf("%d online, want %d", got.Online, statusSampleLimit+5)
	}
	if len(got.Sample) != statusSampleLimit {
		t.Errorf("the hover card lists %d, want it capped at %d", len(got.Sample), statusSampleLimit)
	}
}
