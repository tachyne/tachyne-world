package server

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/render770"
)

// lastTimeFrame is the last Time frame the player was sent.
func lastTimeFrame(t *testing.T, pl *tracked) attachproto.Time {
	t.Helper()
	var f attachproto.Time
	seen := false
	for _, ev := range drainEvs(pl.p) {
		if tf, ok := ev.(attachproto.Time); ok {
			f, seen = tf, true
		}
	}
	if !seen {
		t.Fatal("no Time frame was sent")
	}
	return f
}

// The clock sync is ServerClockManager's: every clock with its full total
// (the moon phase needs more than a day), partial tick and rate — 0 while it
// is paused or advance_time is off — sent at each change.
func TestClockSyncCarriesEveryClock(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	h.rules.DoDaylight = true
	h.dayTime.Store(5*dayLengthTicks + 6000) // the sixth day's noon: moon phase 5
	h.clocks[clockTheEnd].Total = 777
	drainEvs(pl.p)

	h.runTime(players, pl.p, []string{"pause"})
	f := lastTimeFrame(t, pl)
	if len(f.Clocks) != 2 {
		t.Fatalf("%d clocks, want the overworld's and the End's", len(f.Clocks))
	}
	ow, end := f.Clocks[0], f.Clocks[1]
	if ow.ID != attachproto.ClockOverworld || ow.Total != 5*dayLengthTicks+6000 || ow.Rate != 0 {
		t.Errorf("paused overworld clock %+v", ow)
	}
	if end.ID != attachproto.ClockTheEnd || end.Total != 777 || end.Rate != 1 {
		t.Errorf("End clock %+v", end)
	}

	h.runTime(players, pl.p, []string{"of", "the_end", "rate", "2.5"})
	if end := lastTimeFrame(t, pl).Clocks[1]; end.Rate != 2.5 {
		t.Errorf("re-rated End clock %+v", end)
	}

	h.applyRule(players, evSetRule{rule: "advance_time", on: false})
	for _, c := range lastTimeFrame(t, pl).Clocks {
		if c.Rate != 0 {
			t.Errorf("advance_time off, clock %d still runs at %v", c.ID, c.Rate)
		}
	}
}

// Through the gateway's rendering, a 26.3 client gets set_time with both
// clocks: game time, then the map of (clock id, VarLong total, partial, rate).
func TestClockSyncReachesTheClient(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	h.rules.DoDaylight = true
	h.dayTime.Store(200000)
	h.clocks[clockOverworld].Rate = 0.5
	h.clocks[clockOverworld].Partial = 0.5
	drainEvs(pl.p)
	h.broadcastTime(players)
	p := render770.Time(lastTimeFrame(t, pl))
	for _, v := range []int32{776, 777} {
		_, body, drop := protocol.TranslatorFor(v).Clientbound(protocol.StatePlay, p.ID, append([]byte(nil), p.Body...))
		if drop {
			t.Fatalf("v%d: set_time dropped", v)
		}
		r := bytes.NewReader(body[8:])
		if n, _ := protocol.ReadVarInt(r); n != 2 {
			t.Fatalf("v%d: %d clocks", v, n)
		}
		if id, _ := protocol.ReadVarInt(r); id != 0 {
			t.Fatalf("v%d: first clock %d", v, id)
		}
		// VarLong 200000, then 0.5 and 0.5.
		want := []byte{0xc0, 0x9a, 0x0c, 0x3f, 0, 0, 0, 0x3f, 0, 0, 0}
		got := make([]byte, len(want))
		r.Read(got)
		if !bytes.Equal(got, want) {
			t.Errorf("v%d overworld clock %x, want %x", v, got, want)
		}
		if id, _ := protocol.ReadVarInt(r); id != 1 {
			t.Fatalf("v%d: second clock %d", v, id)
		}
		rest := make([]byte, r.Len())
		r.Read(rest)
		if len(rest) != 9 || math.Float32frombits(binary.BigEndian.Uint32(rest[5:])) != 1 {
			t.Errorf("v%d End clock %x", v, rest)
		}
	}
}
