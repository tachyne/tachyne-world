package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// immediate_respawn, limited_crafting and reduced_debug_info reach live
// clients (game events 11/12, entity event 22/23) and the next login's
// flags; an operator's client learns its permission level (event 28).
func TestGameruleFlagsReachClients(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.applyRule(players, evSetRule{rule: "immediate_respawn", on: true})
	h.applyRule(players, evSetRule{rule: "limited_crafting", on: true})
	h.applyRule(players, evSetRule{rule: "reducedDebugInfo", on: true})
	var events []int32
	var statuses []int32
	for {
		select {
		case pkt := <-pl.p.out:
			switch e := pkt.ev.(type) {
			case attachproto.GameEvent:
				if e.Value == 1 {
					events = append(events, e.Event)
				}
			case attachproto.EntityStatus:
				statuses = append(statuses, e.Status)
			}
			continue
		default:
		}
		break
	}
	if len(events) != 2 || events[0] != 11 || events[1] != 12 {
		t.Fatalf("game events %v, want [11 12]", events)
	}
	if len(statuses) != 1 || statuses[0] != 22 {
		t.Fatalf("entity events %v, want [22]", statuses)
	}
	if f := h.loginFlags.Load(); f != loginNoRespawnScreen|loginLimitedCrafting|loginReducedDebug {
		t.Fatalf("login flags %b", f)
	}
	if v, ok := h.ruleValueText("reduced_debug_info"); !ok || v != "true" {
		t.Fatalf("reduced_debug_info reads %q %v", v, ok)
	}
	if opLevelEvent(5, true).Status != 28 || opLevelEvent(5, false).Status != 24 {
		t.Fatal("op level events are not 24 + level")
	}
}
