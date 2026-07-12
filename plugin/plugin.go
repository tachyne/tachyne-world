// Package plugin defines tachyne's in-process plugin API: typed game
// events with Bukkit-style priorities and cancellation, mutation handles
// over the live world, a tick scheduler, chat commands, and per-plugin
// configuration and storage.
//
// Plugins are ordinary Go packages compiled into the server binary. A
// plugin registers itself from init():
//
//	func init() { plugin.Register(&Greeter{}) }
//
// and the server binary selects plugins with blank imports (see
// cmd/server/plugins.go).
//
// # Threading contract
//
// The engine runs all game state on a single tick goroutine. Event
// handlers, command handlers, and scheduled tasks are always invoked on
// that goroutine, so they may use every Context API directly — but they
// must never block (no network calls, no channel waits, no sleeps). Code
// a plugin runs on its own goroutines may use only Scheduler;
// Scheduler.NextTick(fn) is the way back onto the tick goroutine.
package plugin

import (
	"fmt"
	"log"
	"sync"
)

// Plugin is the interface a plugin implements and passes to Register.
type Plugin interface {
	// Name is the stable plugin identifier (lowercase, [a-z0-9_-]).
	// It keys the plugin's data directory and log prefix.
	Name() string
	// Enable is called once at server startup, before the tick loop
	// starts. Register event listeners, commands, and tasks here. A
	// non-nil error aborts server startup.
	Enable(ctx Context) error
	// Disable is called on graceful shutdown, on the tick goroutine.
	Disable()
}

// Context is the API surface handed to a plugin at Enable. Every method
// except Scheduler is tick-goroutine-only after Enable returns.
type Context interface {
	// Server is the live game facade.
	Server() Server
	// Events is the dispatcher to register listeners on via On.
	Events() *Dispatcher
	// Scheduler runs functions on future ticks. Safe from any goroutine.
	Scheduler() Scheduler
	// RegisterCommand adds a chat command. It fails if the name or an
	// alias collides with a built-in or another plugin's command.
	RegisterCommand(cmd Command) error
	// Config unmarshals plugins/<name>/config.json into v. A missing
	// file leaves v untouched and returns nil, so v should arrive
	// pre-filled with defaults.
	Config(v any) error
	// Store is the plugin's persistent key-value store, saved to
	// plugins/<name>/data.json alongside world state.
	Store() KV
	// DataDir is the plugin's own directory (plugins/<name>).
	DataDir() string
	// Logger is prefixed with the plugin name.
	Logger() *log.Logger
}

var (
	regMu      sync.Mutex
	registered []Plugin
	regNames   = map[string]bool{}
)

// Register adds a plugin to the global registry. It is intended to be
// called from init() and panics on a nil plugin or duplicate name.
func Register(p Plugin) {
	if p == nil {
		panic("plugin: Register(nil)")
	}
	regMu.Lock()
	defer regMu.Unlock()
	name := p.Name()
	if name == "" {
		panic("plugin: Register with empty name")
	}
	if regNames[name] {
		panic(fmt.Sprintf("plugin: duplicate Register(%q)", name))
	}
	regNames[name] = true
	registered = append(registered, p)
}

// Registered returns the registered plugins in registration order.
func Registered() []Plugin {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Plugin, len(registered))
	copy(out, registered)
	return out
}
