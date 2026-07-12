package server

import (
	"encoding/json"
	"testing"
	"time"
)

// stubDaemonBus answers /daemon round trips with canned manager replies —
// one reply per fake shard for scatter-gather ops.
type stubDaemonBus struct {
	nopBus
	subject string
	payload []byte
	replies []string // one JSON reply per fake manager
}

func (b *stubDaemonBus) request(subject string, data any) (json.RawMessage, error) {
	b.subject = subject
	b.payload, _ = json.Marshal(data)
	return json.RawMessage(b.replies[0]), nil
}

func (b *stubDaemonBus) requestMany(subject string, data any, _ time.Duration) ([]json.RawMessage, error) {
	b.subject = subject
	b.payload, _ = json.Marshal(data)
	out := make([]json.RawMessage, len(b.replies))
	for i, r := range b.replies {
		out[i] = json.RawMessage(r)
	}
	return out, nil
}

func TestDaemonFleetCommand(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	stub := &stubDaemonBus{replies: []string{`{"ok":true,"manager":"shard-0"}`}}
	h.bus = stub

	// Not an op: refused before any bus traffic.
	s.handleCommand(p, "plugin list")
	if !waitChatLine(p, "You don't have permission.") {
		t.Fatal("non-op /plugin not refused")
	}
	if stub.subject != "" {
		t.Fatal("non-op /plugin reached the bus")
	}
	s.Ops[p.name] = true

	// install by registry NAME → {"name": ...}; by module path → {"module": ...}.
	stub.replies = []string{`{"ok":true,"manager":"shard-0"}`, `{"ok":true,"manager":"shard-1"}`}
	s.handleCommand(p, "plugin install webmap")
	if stub.subject != "mc.plugin.install" {
		t.Fatalf("subject %q", stub.subject)
	}
	var inst map[string]any
	json.Unmarshal(stub.payload, &inst)
	if inst["name"] != "webmap" || inst["module"] != nil {
		t.Fatalf("install payload %s", stub.payload)
	}
	if !waitChatLine(p, "[shard-0] install: done") || !waitChatLine(p, "[shard-1] install: done") {
		t.Fatal("per-manager install acks missing")
	}
	s.handleCommand(p, "plugin install github.com/x/mapd@v1.2.0")
	json.Unmarshal(stub.payload, &inst)
	if inst["module"] != "github.com/x/mapd" || inst["version"] != "v1.2.0" {
		t.Fatalf("module install payload %s", stub.payload)
	}

	// Fleet list: manager sections + the OUTDATED flag + the summary hint.
	stub.replies = []string{
		`{"ok":true,"manager":"shard-0","daemons":[{"manager":"shard-0","name":"webmap","module":"github.com/x/mapd","built":"v1.0.0","latest":"v1.1.0","outdated":true,"status":"running","restarts":0}]}`,
		`{"ok":true,"manager":"shard-1","daemons":[{"manager":"shard-1","name":"webmap","module":"github.com/x/mapd","built":"v1.1.0","latest":"v1.1.0","status":"running","restarts":1}]}`,
	}
	s.handleCommand(p, "plugin list")
	if !waitChatLine(p, "[shard-0] webmap — github.com/x/mapd@v1.0.0 [running, 0 restarts] *** OUTDATED (latest v1.1.0)") {
		t.Fatal("outdated row missing")
	}
	if !waitChatLine(p, "[shard-1] webmap — github.com/x/mapd@v1.1.0 [running, 1 restarts]") {
		t.Fatal("current row missing")
	}
	if !waitChatLine(p, "1 of 2 daemons outdated — /plugin upgrade <name> rolls the fleet progressively.") {
		t.Fatal("stale summary missing")
	}

	// search formats registry rows.
	stub.replies = []string{`{"ok":true,"manager":"shard-0","plugins":[{"module":"github.com/x/mapd","name":"webmap","type":"daemon","description":"live map","latest":"v1.1.0","installs":7,"rating":4.5,"ratings":2}]}`}
	s.handleCommand(p, "plugin search map")
	if stub.subject != "mc.plugin.search" {
		t.Fatalf("subject %q", stub.subject)
	}
	if !waitChatLine(p, "webmap (daemon) v1.1.0 — live map [7 installs, 4.5★×2]") {
		t.Fatal("search row missing")
	}

	// info renders the registry card; rate confirms.
	stub.replies = []string{`{"ok":true,"manager":"shard-0","plugins":[{"module":"github.com/x/mapd","name":"webmap","type":"daemon","description":"live map","latest":"v1.1.0","installs":7,"rating":4.5,"ratings":2}]}`}
	s.handleCommand(p, "plugin info webmap")
	if stub.subject != "mc.plugin.info" {
		t.Fatalf("subject %q", stub.subject)
	}
	if !waitChatLine(p, "webmap (daemon) — live map") {
		t.Fatal("info card missing")
	}
	stub.replies = []string{`{"ok":true,"manager":"shard-0"}`}
	s.handleCommand(p, "plugin rate webmap 5")
	if stub.subject != "mc.plugin.rate" {
		t.Fatalf("subject %q", stub.subject)
	}
	var rate map[string]any
	json.Unmarshal(stub.payload, &rate)
	if rate["stars"].(float64) != 5 {
		t.Fatalf("rate payload %s", stub.payload)
	}
	if !waitChatLine(p, "Rated webmap 5★.") {
		t.Fatal("rate ack missing")
	}
	s.handleCommand(p, "plugin rate webmap 9")
	if !waitChatLine(p, "Usage: /plugin rate <name> <1-5>") {
		t.Fatal("bad stars not rejected")
	}

	// /plugin list opens with the compiled-in set.
	s.handleCommand(p, "plugin list")
	if !waitChatLine(p, "No plugins compiled into this server.") {
		t.Fatal("/plugin list compiled-in section missing")
	}

	// A manager error surfaces per shard.
	stub.replies = []string{`{"ok":false,"manager":"shard-1","error":"no daemon \"ghost\""}`}
	s.handleCommand(p, "plugin uninstall ghost")
	if !waitChatLine(p, `[shard-1] uninstall: no daemon "ghost"`) {
		t.Fatal("manager error not surfaced")
	}
}
