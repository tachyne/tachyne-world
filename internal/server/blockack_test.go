package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A client tags every dig and place with a prediction sequence and keeps
// showing its own guess for those positions until the server acknowledges
// that sequence. The world records the highest one it has processed and the
// hub drains it once a tick — vanilla's ackBlockChangesUpTo.
func TestBlockPredictionSequenceIsRecorded(t *testing.T) {
	p := &player{}
	if _, ok := p.takeAck(); ok {
		t.Fatal("a player who has done nothing has nothing to acknowledge")
	}
	p.noteAck(0) // sequence 0 is a real sequence, not "nothing"
	if seq, ok := p.takeAck(); !ok || seq != 0 {
		t.Fatalf("sequence 0 should be pending, got %d (ok=%v)", seq, ok)
	}
	if _, ok := p.takeAck(); ok {
		t.Fatal("draining twice should find nothing the second time")
	}
	p.noteAck(9)
	p.noteAck(4) // out of order: vanilla keeps the maximum
	p.noteAck(11)
	if seq, ok := p.takeAck(); !ok || seq != 11 {
		t.Fatalf("the highest sequence should win, got %d (ok=%v)", seq, ok)
	}
}

// The sequence arrives on the wire body the dig and place handlers parse, so
// handling either action leaves it pending.
func TestDigAndPlaceRecordTheirSequence(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	x, y, z := 1, 50, 1
	s.world.SetBlock(x, y, z, worldgen.Stone)
	body := protocol.AppendVarInt(nil, digStartBreak)
	body = protocol.AppendPosition(body, x, y, z)
	body = append(body, byte(1))
	body = protocol.AppendVarInt(body, 42)
	s.handleDig(p, body)
	if seq, ok := p.takeAck(); !ok || seq != 42 {
		t.Fatalf("the dig's sequence should be pending, got %d (ok=%v)", seq, ok)
	}
}
