package attach

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"

	proto "github.com/tachyne/tachyne-common/attach"
)

func startWorld(t *testing.T) net.Addr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(1)
	go Serve(ln, Config{
		World: w,
		Time:  func() int64 { return 6000 },
		Token: "secret",
		Spawn: proto.Pos{X: 0.5, Y: w.SurfaceY(0, 0), Z: 0.5},
	})
	t.Cleanup(func() { ln.Close() })
	return ln.Addr()
}

func hello(t *testing.T, addr net.Addr, token string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(30 * time.Second))
	if err := proto.WriteJSON(c, proto.MsgHello, proto.Hello{
		Token: token, Gateway: "gw-java-770/0", Name: "wesley", UUID: "u-1", Edition: "java",
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUnauthorizedHelloRefused(t *testing.T) {
	addr := startWorld(t)
	c := hello(t, addr, "wrong")
	typ, payload, err := proto.ReadFrame(c)
	if err != nil || typ != proto.MsgBye {
		t.Fatalf("want Bye, got typ=%#x err=%v", typ, err)
	}
	var bye proto.Bye
	json.Unmarshal(payload, &bye)
	if bye.Reason == "" {
		t.Fatal("Bye should carry a reason")
	}
}

func TestSessionFlow(t *testing.T) {
	addr := startWorld(t)
	c := hello(t, addr, "secret")

	// Welcome with a sane spawn.
	typ, payload, err := proto.ReadFrame(c)
	if err != nil || typ != proto.MsgWelcome {
		t.Fatalf("want Welcome, got typ=%#x err=%v", typ, err)
	}
	var w proto.Welcome
	if err := json.Unmarshal(payload, &w); err != nil {
		t.Fatal(err)
	}
	if w.Sections != proto.Sections || w.MinY != proto.MinY || w.Time != 6000 {
		t.Fatalf("welcome: %+v", w)
	}
	if w.Spawn.Y <= float64(proto.MinY) || w.Spawn.Y > 320 {
		t.Fatalf("implausible spawn: %+v", w.Spawn)
	}

	// Want radius 1 → exactly 9 chunks, each decodable, spawn chunk present.
	if err := proto.WriteJSON(c, proto.MsgWant, proto.Want{CX: 0, CZ: 0, Radius: 1}); err != nil {
		t.Fatal(err)
	}
	got := map[[2]int32]bool{}
	var sawGround bool
	for len(got) < 9 {
		typ, payload, err := proto.ReadFrame(c)
		if err != nil {
			t.Fatalf("reading chunks (have %d): %v", len(got), err)
		}
		if typ != proto.MsgChunk {
			continue // Time frames may interleave
		}
		h, body, err := proto.DecodeChunk(payload)
		if err != nil {
			t.Fatal(err)
		}
		got[[2]int32{h.CX, h.CZ}] = true
		if len(h.Biomes) != proto.Sections {
			t.Fatalf("chunk %d,%d: %d biome names", h.CX, h.CZ, len(h.Biomes))
		}
		if h.CX == 0 && h.CZ == 0 {
			for _, s := range body.BlockStates {
				if s != worldgen.Air {
					sawGround = true
					break
				}
			}
		}
	}
	if !got[[2]int32{0, 0}] || !got[[2]int32{-1, 1}] {
		t.Fatalf("missing expected chunks: %v", got)
	}
	if !sawGround {
		t.Fatal("spawn chunk is all air")
	}

	// Re-Want must not resend (dedup): expect only ping traffic afterwards.
	proto.WriteJSON(c, proto.MsgWant, proto.Want{CX: 0, CZ: 0, Radius: 1})
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := proto.WriteFrame(c, proto.MsgPing, nonce); err != nil {
		t.Fatal(err)
	}
	for {
		typ, payload, err := proto.ReadFrame(c)
		if err != nil {
			t.Fatal(err)
		}
		if typ == proto.MsgChunk {
			t.Fatal("duplicate chunk after re-Want")
		}
		if typ == proto.MsgPong {
			if string(payload) != string(nonce) {
				t.Fatalf("pong payload %v", payload)
			}
			break
		}
	}

	// Clean goodbye.
	proto.WriteJSON(c, proto.MsgBye, proto.Bye{Reason: "test done"})
}

// mockRemote satisfies Remote for the welcome-ordering test.
type mockRemote struct{}

func (mockRemote) EID() int32                                              { return 7 }
func (mockRemote) Spawn() (float64, float64, float64)                      { return 0.5, 63, 0.5 }
func (mockRemote) Gamemode() int32                                         { return 0 }
func (mockRemote) Move(x, y, z float64, yaw, pitch float32, onGround bool) {}
func (mockRemote) Chat(string)                                             {}
func (mockRemote) Command(string)                                          {}
func (mockRemote) Action(any)                                              {}
func (mockRemote) Dig(proto.Dig)                                           {}
func (mockRemote) Place(proto.Place)                                       {}
func (mockRemote) HeldSlot(int16)                                          {}
func (mockRemote) Leave()                                                  {}

// TestWelcomeIsFirstFrame: a Join hook that emits synchronously (the engine
// sends the command tree + abilities at join) must NOT push its frames ahead
// of Welcome — gateways refuse a session whose first frame isn't Welcome.
// Regression: this was live-broken once the join-time raw emissions landed.
func TestWelcomeIsFirstFrame(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(1)
	go Serve(ln, Config{
		World: w,
		Time:  func() int64 { return 6000 },
		Token: "secret",
		Spawn: proto.Pos{X: 0.5, Y: w.SurfaceY(0, 0), Z: 0.5},
		Join: func(name string, uuid [16]byte, emit func(byte, []byte)) (Remote, error) {
			emit(proto.MsgCommandTree, []byte(`{"data":"AQID"}`)) // command tree
			emit(proto.MsgAbilities, []byte(`{"may_fly":true}`))  // abilities
			return mockRemote{}, nil
		},
	})
	t.Cleanup(func() { ln.Close() })

	c := hello(t, ln.Addr(), "secret")
	typ, _, err := proto.ReadFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	if typ != proto.MsgWelcome {
		t.Fatalf("first frame must be Welcome (0x%02x), got 0x%02x", proto.MsgWelcome, typ)
	}
	// The held-back join frames arrive right after, in emission order.
	for i, want := range []byte{proto.MsgCommandTree, proto.MsgAbilities} {
		typ, _, err := proto.ReadFrame(c)
		if err != nil {
			t.Fatal(err)
		}
		if typ != want {
			t.Fatalf("frame %d after Welcome: want 0x%02x, got 0x%02x", i, want, typ)
		}
	}
}

// TestNearestFirst pins the chunk-streaming order: the Want center chunk must
// be first (it releases the client's "Loading terrain" screen) and distance
// from the center must never decrease along the queue.
func TestNearestFirst(t *testing.T) {
	const r, cx, cz = 5, 100, -40
	var queue [][3]int32
	for x := int32(cx - r); x <= cx+r; x++ {
		for z := int32(cz - r); z <= cz+r; z++ {
			queue = append(queue, [3]int32{0, x, z})
		}
	}
	nearestFirst(queue, cx, cz)
	if queue[0] != [3]int32{0, cx, cz} {
		t.Fatalf("first queued chunk = %v, want the center {0,%d,%d}", queue[0], cx, cz)
	}
	prev := int64(-1)
	for i, cc := range queue {
		dx, dz := int64(cc[1]-cx), int64(cc[2]-cz)
		d := dx*dx + dz*dz
		if d < prev {
			t.Fatalf("queue[%d]=%v is nearer (d²=%d) than its predecessor (d²=%d)", i, cc, d, prev)
		}
		prev = d
	}
	if len(queue) != (2*r+1)*(2*r+1) {
		t.Fatalf("queue length %d, want %d", len(queue), (2*r+1)*(2*r+1))
	}
}
