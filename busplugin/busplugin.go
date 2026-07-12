// Package busplugin is the daemon-plugin kit: everything an out-of-process
// tachyne plugin needs to attach to a running server over the NATS bus.
// A daemon plugin is an ordinary main package — usually a dozen lines:
//
//	func main() {
//		c, err := busplugin.ConnectEnv()
//		if err != nil { log.Fatal(err) }
//		defer c.Close()
//
//		busplugin.On(c, "player_join", func(e plugin.PlayerJoinEvent) {
//			c.Command("say", map[string]any{"text": "Welcome, " + e.Name + "!"})
//		})
//		select {}
//	}
//
// Event payloads are the plugin package's event structs (snake_case JSON on
// the wire); commands and queries are documented in docs/PLUGINS.md. Run a
// daemon by hand, or let cmd/tachyne-plugin-manager pull, build, and
// supervise it from its module path — the go-get model for running plugins.
//
// Daemons observe and command; they cannot veto actions inside the tick —
// that is what compiled-in plugins (tachyne-build) are for.
package busplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

// Conn is a bus connection.
type Conn struct{ nc *nats.Conn }

// Connect dials the bus at the given NATS URL.
func Connect(url string) (*Conn, error) {
	nc, err := nats.Connect(url,
		nats.Name("tachyne-daemon-plugin"),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, err
	}
	return &Conn{nc: nc}, nil
}

// ConnectEnv dials NATS_URL (the address the plugin manager injects),
// falling back to the local default.
func ConnectEnv() (*Conn, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	return Connect(url)
}

func (c *Conn) Close() { c.nc.Close() }

// On subscribes to mc.event.<event>, unmarshalling each payload into T —
// use the tachyne plugin package's event structs as T (their JSON tags are
// the wire shape). Returns an unsubscribe func.
func On[T any](c *Conn, event string, fn func(T)) (func(), error) {
	sub, err := c.nc.Subscribe("mc.event."+event, func(m *nats.Msg) {
		var v T
		if json.Unmarshal(m.Data, &v) == nil {
			fn(v)
		}
	})
	if err != nil {
		return nil, err
	}
	return func() { sub.Unsubscribe() }, nil
}

// OnRaw subscribes to mc.event.<event> and hands over the raw JSON —
// subscribe "mc.event.>" style patterns work too ("" means every event).
func (c *Conn) OnRaw(event string, fn func(event string, payload []byte)) (func(), error) {
	subject := "mc.event.>"
	if event != "" {
		subject = "mc.event." + event
	}
	sub, err := c.nc.Subscribe(subject, func(m *nats.Msg) {
		fn(m.Subject[len("mc.event."):], m.Data)
	})
	if err != nil {
		return nil, err
	}
	return func() { sub.Unsubscribe() }, nil
}

// Announce tells the server's operators something worth knowing — call it
// once at startup with whatever an op needs to use the plugin (the webmap
// announces its URL). Online ops see it in chat; the plugin manager
// remembers the latest note per daemon so /plugin shows it later too.
func (c *Conn) Announce(pluginName, text string) error {
	return c.Command("announce", map[string]any{"name": pluginName, "text": text})
}

// Command sends a fire-and-forget command (mc.cmd.<name>).
func (c *Conn) Command(name string, args any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return c.nc.Publish("mc.cmd."+name, raw)
}

// reply is the server's request-reply envelope.
type reply struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error"`
	Data  json.RawMessage `json:"data"`
}

// Request sends a command/query and unmarshals the reply's data into out
// (out may be nil for commands without reply data).
func (c *Conn) Request(name string, args any, out any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	msg, err := c.nc.Request("mc.cmd."+name, raw, 5*time.Second)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var r reply
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return fmt.Errorf("%s: bad reply: %w", name, err)
	}
	if !r.OK {
		return fmt.Errorf("%s: %s", name, r.Error)
	}
	if out != nil && r.Data != nil {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}
