package server

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/nats-io/nats.go"
)

// natsBus is the bus backed by a standalone NATS server (tachyne is just a client).
// Events publish to subjects "mc.event.<topic>"; plugins send commands to
// "mc.cmd.<name>" with the JSON args as the payload (request-reply optional).
//
// This is OPTIONAL: the server runs fine with no bus (nopBus). NATS is only used
// when -nats points at a running nats-server, and it never blocks the game — if
// the broker is down, connecting just fails at startup and you run without it.
type natsBus struct {
	nc *nats.Conn
}

func newNatsBus(h *hub, url string) (*natsBus, error) {
	nc, err := nats.Connect(url,
		nats.Name("tachyne"),
		nats.MaxReconnects(-1), // keep trying to reconnect if the broker restarts
	)
	if err != nil {
		return nil, err
	}

	// Plugins send commands to mc.cmd.<name>; reply if they used
	// request-reply. Each message gets its own goroutine because queries
	// block briefly on the hub (runOnHub) — a slow query must not stall the
	// whole command stream.
	if _, err := nc.Subscribe("mc.cmd.>", func(msg *nats.Msg) {
		go func() {
			cmd := strings.TrimPrefix(msg.Subject, "mc.cmd.")
			data, errStr := executeCommand(h, cmd, msg.Data)
			if msg.Reply == "" {
				return
			}
			switch {
			case errStr != "":
				msg.Respond([]byte(fmt.Sprintf(`{"ok":false,"error":%q}`, errStr)))
			case data != nil:
				if body, err := json.Marshal(map[string]any{"ok": true, "data": data}); err == nil {
					msg.Respond(body)
				} else {
					msg.Respond([]byte(`{"ok":false,"error":"unmarshalable reply"}`))
				}
			default:
				msg.Respond([]byte(`{"ok":true}`))
			}
		}()
	}); err != nil {
		nc.Close()
		return nil, err
	}

	log.Printf("connected to NATS at %s (events: mc.event.*, commands: mc.cmd.*)", url)
	return &natsBus{nc: nc}, nil
}

// publish fans an event out on its NATS subject. Non-blocking (NATS buffers).
func (b *natsBus) publish(topic string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	b.nc.Publish("mc.event."+topic, payload)
}
