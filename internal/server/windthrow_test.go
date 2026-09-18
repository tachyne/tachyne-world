package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Every hub event type must have a dispatch case in the hub loop — the
// ender-pearl throw was posted for months and never handled.
func TestEveryHubEventDispatched(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := map[string]bool{}
	cased := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					if x.Name.Name == "isHubEvent" && x.Recv != nil && len(x.Recv.List) == 1 {
						if id, ok := x.Recv.List[0].Type.(*ast.Ident); ok {
							events[id.Name] = true
						}
					}
				case *ast.CaseClause:
					for _, e := range x.List {
						if id, ok := e.(*ast.Ident); ok {
							cased[id.Name] = true
						}
					}
				}
				return true
			})
		}
	}
	if len(events) < 50 {
		t.Fatalf("found only %d hub event types", len(events))
	}
	for ev := range events {
		if !cased[ev] {
			t.Errorf("%s is posted but never dispatched", ev)
		}
	}
}

// A wind charge thrown by a player: one consumed, the gust in flight from
// the eyes along the look, and half a second before the next.
func TestThrowWindCharge(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	pl.p.setHotbarSlot(0, itemWindCharge)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemWindCharge, count: 3}
	players := map[int32]*tracked{1: pl}
	h.playersRef = players

	h.throwWindCharge(players, pl)
	if got := pl.inv.slots[pl.p.heldSlot()].count; got != 2 || len(h.arrows) != 1 {
		t.Fatalf("one charge consumed + in flight: count=%d arrows=%d", got, len(h.arrows))
	}
	for _, a := range h.arrows {
		if a.etype != entityWindCharge || a.knock <= 0 || !a.playerShot || a.shooter != pl.p.eid {
			t.Fatalf("not a player's wind charge: %+v", a)
		}
		if a.y < 81.5 || a.y > 81.7 {
			t.Errorf("thrown from the eyes, y=%v", a.y)
		}
	}
	if !h.onCooldown(pl, itemWindCharge) {
		t.Fatal("no cooldown after the throw")
	}
	h.throwWindCharge(players, pl)
	if got := pl.inv.slots[pl.p.heldSlot()].count; got != 2 {
		t.Fatalf("a throw on cooldown consumed a charge: count=%d", got)
	}
	h.tick.Store(h.tick.Load() + windChargeCooldown)
	h.throwWindCharge(players, pl)
	if got := pl.inv.slots[pl.p.heldSlot()].count; got != 1 {
		t.Fatalf("the cooldown out, the next throw should go: count=%d", got)
	}
}

// The pearl is gated by its second of cooldown the same way.
func TestPearlCooldown(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	pl.inv.slots[0] = invStack{item: itemEnderPearl, count: 3}
	players := map[int32]*tracked{1: pl}
	h.throwPearl(players, pl)
	h.throwPearl(players, pl)
	if pl.inv.slots[0].count != 2 || !h.onCooldown(pl, itemEnderPearl) {
		t.Fatalf("two throws in one tick should spend one pearl: count=%d", pl.inv.slots[0].count)
	}
	h.tick.Store(h.tick.Load() + pearlCooldown)
	h.throwPearl(players, pl)
	if pl.inv.slots[0].count != 1 {
		t.Fatalf("the cooldown out, the next pearl should fly: count=%d", pl.inv.slots[0].count)
	}
}
