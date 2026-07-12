package server

import (
	"encoding/json"
	"fmt"
	"time"
)

// The plugin bus is an OPTIONAL out-of-process extension point: external programs
// subscribe to game events and send commands, with no code loaded into the
// server, so plugins attach and detach live. The backend is NATS (see
// natsbus.go); the server runs perfectly with no bus at all (nopBus).
//
// bus is the seam between the hub and the backend. The hub only emits events;
// the backend decides delivery and feeds commands back through executeCommand.
// request is for engine-initiated round trips to other bus citizens (the
// /plugin command talking to the plugin manager).
type bus interface {
	publish(topic string, data any)
	request(subject string, data any) (json.RawMessage, error)
	// requestMany is scatter-gather: publish once, collect every reply
	// that arrives inside the window (fleet ops — one reply per shard
	// manager).
	requestMany(subject string, data any, window time.Duration) ([]json.RawMessage, error)
}

// nopBus is the disabled default, so the hub never has to nil-check.
type nopBus struct{}

func (nopBus) publish(string, any) {}
func (nopBus) request(string, any) (json.RawMessage, error) {
	return nil, fmt.Errorf("no bus configured (-nats)")
}
func (nopBus) requestMany(string, any, time.Duration) ([]json.RawMessage, error) {
	return nil, fmt.Errorf("no bus configured (-nats)")
}

// executeCommand runs a bus command against the world, returning optional
// reply data and "" on success. Everything routes through the hub so
// mutations run on the authoritative goroutine. New commands go here —
// they're instantly available to any backend. The v2 command/query handlers
// live in busv2.go.
func executeCommand(h *hub, cmd string, args json.RawMessage) (any, string) {
	switch cmd {
	case "say":
		var a struct {
			Text string `json:"text"`
		}
		json.Unmarshal(args, &a)
		if a.Text == "" {
			return nil, "say requires a text arg"
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
			return nil, "setblock requires x,y,z,state"
		}
		h.post(evSetBlock{x: a.X, y: a.Y, z: a.Z, state: a.State})
	case "spawn":
		return busCmdSpawn(h, args)
	case "behavior":
		var a struct {
			EID      int32  `json:"eid"`
			Behavior string `json:"behavior"`
		}
		if json.Unmarshal(args, &a) != nil {
			return nil, "behavior requires eid,behavior"
		}
		if _, ok := behaviors[a.Behavior]; !ok {
			return nil, fmt.Sprintf("unknown behavior %q", a.Behavior)
		}
		h.post(evSetBehavior{eid: a.EID, behavior: a.Behavior})

	// Facade-parity commands (busv2.go).
	case "weather":
		return busCmdWeather(h, args)
	case "gamerule":
		return busCmdGamerule(h, args)
	case "give":
		return busCmdGive(h, args)
	case "teleport":
		return busCmdTeleport(h, args)
	case "mobset":
		return busCmdMobSet(h, args)

	// Queries (request-reply).
	case "players":
		return busQueryPlayers(h)
	case "mobs":
		return busQueryMobs(h, args)
	case "block":
		return busQueryBlock(h, args)
	case "world":
		return busQueryWorld(h)

	default:
		return nil, fmt.Sprintf("unknown command %q", cmd)
	}
	return nil, ""
}
