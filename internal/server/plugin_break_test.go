package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/world"
	"tachyne/internal/worldgen"
	"tachyne/plugin"
)

// digBody/placeBody encode serverbound action bodies exactly the way
// remote.Dig/remote.Place do, so these tests exercise the real gateway path.
func digBody(status int32, x, y, z int) []byte {
	b := protocol.AppendVarInt(nil, status)
	b = protocol.AppendPosition(b, x, y, z)
	b = append(b, byte(1)) // face
	return protocol.AppendVarInt(b, 0)
}

func placeBody(x, y, z int, face int32) []byte {
	b := protocol.AppendVarInt(nil, 0) // hand
	b = protocol.AppendPosition(b, x, y, z)
	b = protocol.AppendVarInt(b, face)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendBool(b, false)
	b = protocol.AppendBool(b, false) // world border hit
	return protocol.AppendVarInt(b, 0)
}

// breakPlaceServer builds a creative-mode Server around a live hub, with one
// joined player, mirroring the way remote sessions drive the handlers.
func breakPlaceServer(t *testing.T) (*Server, *hub, *player) {
	t.Helper()
	w := world.New(1)
	h := newHub(w)
	h.rules.DoMobSpawning = false
	h.plugHost = &pluginHost{h: h, cmds: map[string]*plugin.Command{}}
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative), Ops: map[string]bool{}}
	h.plugHost.s = s
	go h.run()

	p := newPlayer(h.allocEID(), "digger", [16]byte{1})
	sy := w.SurfaceY(0, 0)
	p.x, p.y, p.z = 0.5, sy, 0.5
	h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
	waitJoined(t, h, "digger")
	return s, h, p
}

func TestPluginBlockBreakCancel(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world

	// Protect everything below sea level — the classic region-protection hook.
	events := make(chan *plugin.BlockBreakEvent, 4)
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.BlockBreakEvent) {
		events <- e
		if e.Y < 60 {
			e.SetCancelled(true)
		}
	})

	// A real block near the player, below the protection line.
	x, y, z := 1, 50, 1
	w.SetBlock(x, y, z, worldgen.Stone)
	before := w.Block(x, y, z)

	s.handleDig(p, digBody(digStartBreak, x, y, z)) // creative: breaks on Start
	e := <-events
	if e.Name != "digger" || e.X != x || e.Y != y || e.Z != z || e.State != before {
		t.Fatalf("break event %+v", e)
	}
	if got := w.Block(x, y, z); got != before {
		t.Fatalf("cancelled break still removed the block (state %d → %d)", before, got)
	}
	// The digger must have been sent the reverting BlockSet.
	sawRevert := waitBlockSet(t, p, x, y, z, before)
	if !sawRevert {
		t.Fatal("no reverting BlockSet reached the digger")
	}

	// Above the line: the break goes through.
	x2, y2, z2 := 1, 80, 1
	w.SetBlock(x2, y2, z2, worldgen.Stone)
	s.handleDig(p, digBody(digStartBreak, x2, y2, z2))
	<-events
	if got := w.Block(x2, y2, z2); got != worldgen.Air {
		t.Fatalf("allowed break did not remove the block (state %d)", got)
	}
}

func TestPluginBlockPlaceCancelAndMutate(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world

	glowstone, _ := (srvFacade{h.plugHost}).ItemByName("glowstone")
	glowState, _ := protocol.BlockForItem(glowstone)
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.BlockPlaceEvent) {
		if e.Y < 60 {
			e.SetCancelled(true) // protected depth
			return
		}
		e.State = glowState // a midas plugin: everything placed becomes glowstone
	})

	// Give the player a stone block to place.
	stoneItem, _ := (srvFacade{h.plugHost}).ItemByName("stone")
	p.setHotbarSlot(0, stoneItem)

	// Cancelled placement: click the top of a block below the line.
	bx, by, bz := 2, 40, 2
	w.SetBlock(bx, by, bz, worldgen.Stone)
	s.handlePlace(p, placeBody(bx, by, bz, 1)) // face 1 = top → target (2,41,2)
	if got := w.Block(bx, by+1, bz); got != worldgen.Air {
		t.Fatalf("cancelled place still set state %d", got)
	}

	// Mutated placement above the line: stone in hand, glowstone in the world.
	bx2, by2, bz2 := 2, 80, 2
	w.SetBlock(bx2, by2, bz2, worldgen.Stone)
	s.handlePlace(p, placeBody(bx2, by2, bz2, 1))
	if got := w.Block(bx2, by2+1, bz2); got != glowState {
		t.Fatalf("mutated place set state %d, want glowstone %d", got, glowState)
	}
}

func TestPluginCommands(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	ctx := &pluginCtx{host: h.plugHost, name: "cmdtest", dir: t.TempDir()}

	ran := make(chan []string, 4)
	if err := ctx.RegisterCommand(plugin.Command{
		Name: "boom", Aliases: []string{"bm"}, Usage: "<word>", Help: "test command",
		Run: func(c plugin.CommandContext) {
			ran <- c.Args()
			c.Reply("boom says " + strings.Join(c.Args(), " "))
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ctx.RegisterCommand(plugin.Command{
		Name: "adminonly", OpOnly: true, Run: func(c plugin.CommandContext) { ran <- nil },
	}); err != nil {
		t.Fatal(err)
	}
	// Collisions rejected: a built-in, and an already-taken plugin name.
	if err := ctx.RegisterCommand(plugin.Command{Name: "give", Run: func(plugin.CommandContext) {}}); err == nil {
		t.Fatal("registering over built-in /give must fail")
	}
	if err := ctx.RegisterCommand(plugin.Command{Name: "bm", Run: func(plugin.CommandContext) {}}); err == nil {
		t.Fatal("registering over a taken alias must fail")
	}

	// A command event listener that rewrites /ping into /boom.
	plugin.On(h.plugins, plugin.Normal, false, func(e *plugin.PlayerCommandEvent) {
		if e.Line == "ping" {
			e.Line = "boom rewritten"
		}
		if e.Line == "forbidden" {
			e.SetCancelled(true)
		}
	})

	s.handleCommand(p, "boom hello world")
	if args := <-ran; len(args) != 2 || args[0] != "hello" {
		t.Fatalf("boom args %v", args)
	}
	if !waitChatLine(p, "boom says hello world") {
		t.Fatal("Reply never reached the sender")
	}

	s.handleCommand(p, "bm alias") // alias routes to the same command
	if args := <-ran; len(args) != 1 || args[0] != "alias" {
		t.Fatalf("alias args %v", args)
	}

	s.handleCommand(p, "adminonly") // digger is not an op
	if !waitChatLine(p, "You don't have permission.") {
		t.Fatal("OpOnly rejection missing")
	}
	select {
	case <-ran:
		t.Fatal("OpOnly command ran for a non-op")
	default:
	}
	s.Ops[p.name] = true
	s.handleCommand(p, "adminonly")
	<-ran // now it runs

	s.handleCommand(p, "ping") // rewritten to "boom rewritten"
	if args := <-ran; len(args) != 1 || args[0] != "rewritten" {
		t.Fatalf("rewrite args %v", args)
	}

	s.handleCommand(p, "forbidden") // cancelled: neither plugin nor legacy runs
	if waitChatLine(p, "Unknown command") {
		t.Fatal("cancelled command still hit the legacy switch")
	}
}

// waitChatLine drains queued packets looking for an exact chat line.
func waitChatLine(p *player, want string) bool {
	for i := 0; i < 4096; i++ {
		select {
		case pkt := <-p.out:
			if c, ok := pkt.ev.(attachproto.Chat); ok && c.Text == want {
				return true
			}
		default:
			return false
		}
	}
	return false
}

// waitBlockSet drains the player's queue looking for a BlockSet of the given
// cell+state (sent synchronously by the handler, so no deadline needed).
func waitBlockSet(t *testing.T, p *player, x, y, z int, state uint32) bool {
	t.Helper()
	for i := 0; i < 4096; i++ {
		select {
		case pkt := <-p.out:
			if bs, ok := pkt.ev.(attachproto.BlockSet); ok &&
				bs.X == x && bs.Y == y && bs.Z == z && bs.State == state {
				return true
			}
		default:
			return false
		}
	}
	return false
}
