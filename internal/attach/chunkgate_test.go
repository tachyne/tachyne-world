package attach

import (
	"net"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"

	proto "github.com/tachyne/tachyne-common/attach"
)

type stubRemote struct{}

func (stubRemote) EID() int32                                       { return 7 }
func (stubRemote) Spawn() (x, y, z float64)                         { return 0.5, 100, 0.5 }
func (stubRemote) Gamemode() int32                                  { return 3 }
func (stubRemote) Death() *proto.DeathPos                           { return nil }
func (stubRemote) Move(x, y, z float64, yaw, pitch float32, g bool) {}
func (stubRemote) Chat(string)                                      {}
func (stubRemote) Command(string)                                   {}
func (stubRemote) Action(any)                                       {}
func (stubRemote) Dig(proto.Dig)                                    {}
func (stubRemote) Place(proto.Place)                                {}
func (stubRemote) HeldSlot(int16)                                   {}
func (stubRemote) Leave()                                           {}

// A session whose player brings no chunk in of their own (a spectator while
// spectators_generate_chunks is off) is sent only what its gate says others
// hold loaded, and nothing is generated for the rest of its window.
func TestLoadedOnlySessionGetsNoNewChunks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	w := world.New(1)
	go Serve(ln, Config{
		World: w,
		Time:  func() int64 { return 6000 },
		Token: "secret",
		Join:  func(Identity, func(byte, []byte)) (Remote, error) { return stubRemote{}, nil },
		ChunkGate: func(Remote) func(dim, cx, cz int32) bool {
			return func(dim, cx, cz int32) bool { return cx == 0 && cz == 0 } // someone else holds 0,0
		},
	})
	c := hello(t, ln.Addr(), "secret")
	if typ, _, err := proto.ReadFrame(c); err != nil || typ != proto.MsgWelcome {
		t.Fatalf("want Welcome, got typ=%#x err=%v", typ, err)
	}
	proto.WriteJSON(c, proto.MsgWant, proto.Want{CX: 0, CZ: 0, Radius: 2})
	nonce := []byte{9, 9, 9, 9, 9, 9, 9, 9}
	var got [][2]int32
	sawPong := false
	for !sawPong {
		typ, payload, err := proto.ReadFrame(c)
		if err != nil {
			t.Fatal(err)
		}
		switch typ {
		case proto.MsgChunk:
			h, _, err := proto.DecodeChunk(payload)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, [2]int32{h.CX, h.CZ})
			if len(got) == 1 { // the pool has had its chance at the rest
				proto.WriteFrame(c, proto.MsgPing, nonce)
			}
		case proto.MsgPong:
			sawPong = true
		}
	}
	// Anything a broken gate had queued would still be building: give the
	// pool a moment and collect it.
	c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	for {
		typ, payload, err := proto.ReadFrame(c)
		if err != nil {
			break
		}
		if typ == proto.MsgChunk {
			h, _, _ := proto.DecodeChunk(payload)
			got = append(got, [2]int32{h.CX, h.CZ})
		}
	}
	if len(got) != 1 || got[0] != [2]int32{0, 0} {
		t.Fatalf("chunks sent %v, want only the held 0,0", got)
	}
}
