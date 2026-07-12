package server

import (
	"encoding/json"
	"fmt"
)

// The plugin bus is an OPTIONAL out-of-process extension point: external programs
// subscribe to game events and send commands, with no code loaded into the
// server, so plugins attach and detach live. The backend is NATS (see
// natsbus.go); the server runs perfectly with no bus at all (nopBus).
//
// bus is the seam between the hub and the backend. The hub only emits events;
// the backend decides delivery and feeds commands back through executeCommand.
type bus interface {
	publish(topic string, data any)
}

// nopBus is the disabled default, so the hub never has to nil-check.
type nopBus struct{}

func (nopBus) publish(string, any) {}

// executeCommand runs a plugin command against the world, returning "" on
// success. Everything routes through the hub so it runs on the authoritative
// goroutine. New commands go here — they're instantly available to any backend.
func executeCommand(h *hub, cmd string, args json.RawMessage) string {
	switch cmd {
	case "say":
		var a struct {
			Text string `json:"text"`
		}
		json.Unmarshal(args, &a)
		if a.Text == "" {
			return "say requires a text arg"
		}
		h.post(evChat{text: a.Text})
	case "settime":
		var a struct {
			Time uint64 `json:"time"`
		}
		json.Unmarshal(args, &a)
		h.post(evSetTime{t: a.Time}) // through the hub so the plugin TimeSetEvent fires
	case "setblock":
		var a struct {
			X     int    `json:"x"`
			Y     int    `json:"y"`
			Z     int    `json:"z"`
			State uint32 `json:"state"`
		}
		if json.Unmarshal(args, &a) != nil {
			return "setblock requires x,y,z,state"
		}
		h.post(evSetBlock{x: a.X, y: a.Y, z: a.Z, state: a.State})
	case "spawn":
		var a struct {
			Type     int     `json:"type"`
			X        float64 `json:"x"`
			Y        float64 `json:"y"` // 0 = snap to the surface
			Z        float64 `json:"z"`
			Behavior string  `json:"behavior"`
		}
		if json.Unmarshal(args, &a) != nil {
			return "spawn requires type,x,z[,y,behavior]"
		}
		if a.Behavior == "" {
			a.Behavior = "wander"
		}
		if _, ok := behaviors[a.Behavior]; !ok {
			return fmt.Sprintf("unknown behavior %q", a.Behavior)
		}
		h.post(evSpawnMob{etype: a.Type, x: a.X, y: a.Y, z: a.Z, behavior: a.Behavior})
	case "behavior":
		var a struct {
			EID      int32  `json:"eid"`
			Behavior string `json:"behavior"`
		}
		if json.Unmarshal(args, &a) != nil {
			return "behavior requires eid,behavior"
		}
		if _, ok := behaviors[a.Behavior]; !ok {
			return fmt.Sprintf("unknown behavior %q", a.Behavior)
		}
		h.post(evSetBehavior{eid: a.EID, behavior: a.Behavior})
	default:
		return fmt.Sprintf("unknown command %q", cmd)
	}
	return ""
}
