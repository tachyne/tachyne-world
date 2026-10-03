package attach

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"

	proto "github.com/tachyne/tachyne-common/attach"
)

// dimRemote is a player in a dimension of its own.
type dimRemote struct {
	mockRemote
	dim int32
}

func (r dimRemote) Dim() int32 { return r.dim }

// The Welcome carries the world's configuration data (its dimension table)
// and the dimension the player is in.
func TestWelcomeCarriesConfigAndDim(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(1)
	table := []proto.DimensionInfo{
		{ID: 0, Key: "minecraft:overworld", Type: "minecraft:overworld", SkyLight: true, Clock: "minecraft:overworld"},
		{ID: 1, Key: "minecraft:the_nether", Type: "minecraft:the_nether"},
		{ID: 3, Key: "tachyne:moon", Type: "tachyne:moon", TypeData: json.RawMessage(`{"height":256}`)},
	}
	go Serve(ln, Config{
		World: w, Time: func() int64 { return 0 }, Token: "secret",
		ConfigData: func() *proto.ConfigData { return &proto.ConfigData{Dimensions: table} },
		Join: func(id Identity, emit func(byte, []byte)) (Remote, error) {
			return dimRemote{dim: 1}, nil
		},
	})
	t.Cleanup(func() { ln.Close() })
	c := hello(t, ln.Addr(), "secret")
	typ, payload, err := proto.ReadFrame(c)
	if err != nil || typ != proto.MsgWelcome {
		t.Fatalf("first frame %#x, %v", typ, err)
	}
	var wel proto.Welcome
	if err := json.Unmarshal(payload, &wel); err != nil {
		t.Fatal(err)
	}
	if wel.Dim != 1 {
		t.Errorf("Welcome.Dim = %d", wel.Dim)
	}
	if wel.Config == nil || len(wel.Config.Dimensions) != 3 {
		t.Fatalf("Welcome.Config = %+v", wel.Config)
	}
	moon := wel.Config.Dimensions[2]
	if moon.ID != 3 || moon.Key != "tachyne:moon" || string(moon.TypeData) != `{"height":256}` || moon.SkyLight {
		t.Errorf("custom dimension %+v", moon)
	}
	if ow := wel.Config.Dimensions[0]; !ow.SkyLight || ow.Clock != "minecraft:overworld" {
		t.Errorf("overworld %+v", ow)
	}
}

// reconfRemote answers a finished configuration the way the engine does:
// it moves the player, then sends MsgRejoin from the session's join state.
type reconfRemote struct {
	mockRemote
	dim  int32
	emit func(byte, []byte)
	view chan int32
}

func (r *reconfRemote) Dim() int32 { return r.dim }
func (r *reconfRemote) Configured(c proto.Configured, welcome func() proto.Welcome) {
	r.dim = 2 // placed again in the End
	b, _ := json.Marshal(proto.Rejoin{Welcome: welcome()})
	r.emit(proto.MsgRejoin, b)
	r.view <- c.View
}

// readUntil reads frames until one of type want arrives.
func readUntil(t *testing.T, c net.Conn, want byte) []byte {
	t.Helper()
	for {
		typ, payload, err := proto.ReadFrame(c)
		if err != nil {
			t.Fatalf("waiting for %#x: %v", want, err)
		}
		if typ == want {
			return payload
		}
	}
}

// A finished configuration (MsgConfigured) reaches the remote with the
// client's view distance and a join state read after the remote moved the
// player — and the session forgets the chunks it sent, since the client
// discarded its level: the same Want streams them again.
func TestConfiguredRejoinsAndResendsChunks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(1)
	rec := &reconfRemote{view: make(chan int32, 1)}
	go Serve(ln, Config{
		World: w, Time: func() int64 { return 0 }, Token: "secret",
		Join: func(id Identity, emit func(byte, []byte)) (Remote, error) {
			rec.emit = emit
			return rec, nil
		},
	})
	t.Cleanup(func() { ln.Close() })
	c := hello(t, ln.Addr(), "secret")
	readUntil(t, c, proto.MsgWelcome)
	proto.WriteJSON(c, proto.MsgWant, proto.Want{CX: 0, CZ: 0, Radius: 0})
	readUntil(t, c, proto.MsgChunk)

	proto.WriteJSON(c, proto.MsgConfigured, proto.Configured{View: 6})
	var rj proto.Rejoin
	if err := json.Unmarshal(readUntil(t, c, proto.MsgRejoin), &rj); err != nil {
		t.Fatal(err)
	}
	if rj.Welcome.EID != 7 || rj.Welcome.Dim != 2 || rj.Welcome.Config != nil {
		t.Errorf("rejoin welcome %+v", rj.Welcome)
	}
	if v := <-rec.view; v != 6 {
		t.Errorf("view %d", v)
	}
	proto.WriteJSON(c, proto.MsgWant, proto.Want{CX: 0, CZ: 0, Radius: 0})
	readUntil(t, c, proto.MsgChunk) // sent again: the session forgot it
}
