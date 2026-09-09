package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// /locate structure names the nearest site in vanilla's words, refuses an
// unknown id, and is for operators only.
func TestCommandLocate(t *testing.T) {
	s := &Server{hub: newHub(world.New(1)), Ops: map[string]bool{"tester": true}}
	p := newPlayer(1, "tester", [16]byte{})
	g := s.hub.worldFor(p.dim).Gen()
	x, z, ok := g.LocateStructure("village", 0, 0, 100*16)
	if !ok {
		t.Skip("no village within 100 chunks of the origin for this seed")
	}
	p.x, p.z = float64(x)+10, float64(z)-10
	reply := func(cmd string) string {
		s.handleCommand(p, cmd)
		select {
		case pkt := <-p.out:
			if c, ok := pkt.ev.(attachproto.Chat); ok {
				return c.Text
			}
			t.Fatalf("got %T, want a Chat event", pkt.ev)
		default:
			t.Fatal("no reply sent")
		}
		return ""
	}
	got := reply("locate structure minecraft:village")
	if !strings.HasPrefix(got, "The nearest minecraft:village is at [") || !strings.HasSuffix(got, "(14 blocks away)") {
		t.Errorf("reply %q", got)
	}
	if got := reply("locate swamp_hut"); !strings.Contains(got, "no structure with type") {
		t.Errorf("unknown id reply %q", got)
	}
	if got := reply("locate structure end_city"); !strings.Contains(got, "Could not find") {
		t.Errorf("wrong-dimension reply %q", got)
	}
	s.Ops = nil
	if got := reply("locate structure village"); !strings.Contains(got, "permission") {
		t.Errorf("non-op reply %q", got)
	}
}
