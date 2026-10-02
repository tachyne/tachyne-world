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
