package server

import (
	"bytes"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"testing"

	"tachyne/internal/world"
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
	s := &Server{hub: newHub(world.New(1))}
	p := newPlayer(1, "tester", [16]byte{})

	s.handleCommand(p, "time night")
	if got := s.hub.dayTime.Load(); got != 13000 {
		t.Errorf("after /time night, dayTime = %d, want 13000", got)
	}
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
