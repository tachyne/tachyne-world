package server

import (
	"bytes"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestChatNBT(t *testing.T) {
	// Nameless root TAG_String (type 8) + u16 length + bytes.
	got := chatNBT("hi")
	want := []byte{0x08, 0x00, 0x02, 'h', 'i'}
	if !bytes.Equal(got, want) {
		t.Errorf("chatNBT = %v, want %v", got, want)
	}
}

func TestGamemodeMapping(t *testing.T) {
	cases := map[string]struct {
		mode int
		ab   attachproto.Abilities
	}{
		"survival":  {0, attachproto.Abilities{}},
		"creative":  {1, attachproto.Abilities{Invulnerable: true, MayFly: true, Creative: true}},
		"adventure": {2, attachproto.Abilities{}},
		"spectator": {3, attachproto.Abilities{Invulnerable: true, Flying: true, MayFly: true}},
		"1":         {1, attachproto.Abilities{Invulnerable: true, MayFly: true, Creative: true}},
	}
	for name, w := range cases {
		mode, ok := ParseGamemode(name)
		if !ok || mode != w.mode || abilitiesFor(mode) != w.ab {
			t.Errorf("%q -> (%d, %+v, %v), want (%d, %+v, true)", name, mode, abilitiesFor(mode), ok, w.mode, w.ab)
		}
	}
	if _, ok := ParseGamemode("nope"); ok {
		t.Error("ParseGamemode(nope) should not be ok")
	}
}

func TestCommandTime(t *testing.T) {
	s := &Server{hub: newHub(world.New(1)), Ops: map[string]bool{"tester": true}} // /time is for ops
	s.hub.rules.DoMobSpawning = false
	s.hub.rules.DoDaylight = false // hold the clock still so the poll target is exact
	startHub(t, s.hub)             // /time routes through the hub (plugin TimeSetEvent)
	p := newPlayer(1, "tester", [16]byte{})

	s.handleCommand(p, "time night")
	waitDayTime(t, s.hub, 13000)
	// The player should have received a confirmation system-chat packet.
	select {
	case pkt := <-p.out:
		if _, ok := pkt.ev.(attachproto.Chat); !ok {
			t.Errorf("got %T, want a Chat event", pkt.ev)
		}
	default:
		t.Error("no confirmation message sent")
	}
}

// /time is a gamemaster command, and takes vanilla's set/add forms.
func TestTimeCommandForms(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want int64
		ok   bool
	}{{[]string{"100"}, 100, true}, {[]string{"1d"}, 24000, true}, {[]string{"2s"}, 40, true},
		{[]string{"0.5d"}, 12000, true}, {[]string{"-20"}, -20, true}, {[]string{"x"}, 0, false}} {
		if got, ok := parseTimeTicks(tc.in); ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseTimeTicks(%v) = %d, %v", tc.in, got, ok)
		}
	}
	s := &Server{hub: newHub(world.New(1)), Ops: map[string]bool{}}
	p := newPlayer(1, "tester", [16]byte{})
	s.handleCommand(p, "time set 500")
	select {
	case ev := <-s.hub.events:
		t.Errorf("a non-op's /time reached the hub: %T", ev)
	default:
	}
}
