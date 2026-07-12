package server

import (
	"encoding/json"
	"testing"
)

// stubDaemonBus answers /daemon round trips with canned manager replies.
type stubDaemonBus struct {
	nopBus
	subject string
	payload []byte
	reply   string
}

func (b *stubDaemonBus) request(subject string, data any) (json.RawMessage, error) {
	b.subject = subject
	b.payload, _ = json.Marshal(data)
	return json.RawMessage(b.reply), nil
}

func TestDaemonCommand(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	stub := &stubDaemonBus{reply: `{"ok":true}`}
	h.bus = stub

	// Not an op: refused before any bus traffic.
	s.handleCommand(p, "daemon list")
	if !waitChatLine(p, "You don't have permission.") {
		t.Fatal("non-op /daemon not refused")
	}
	if stub.subject != "" {
		t.Fatal("non-op /daemon reached the bus")
	}

	s.Ops[p.name] = true

	// install: module@version + args forwarded.
	s.handleCommand(p, "daemon install github.com/x/mapd@v1.2.0 -addr :9000")
	if stub.subject != "mc.daemon.install" {
		t.Fatalf("subject %q", stub.subject)
	}
	var inst struct {
		Module  string   `json:"module"`
		Version string   `json:"version"`
		Args    []string `json:"args"`
	}
	json.Unmarshal(stub.payload, &inst)
	if inst.Module != "github.com/x/mapd" || inst.Version != "v1.2.0" ||
		len(inst.Args) != 2 || inst.Args[1] != ":9000" {
		t.Fatalf("install payload %s", stub.payload)
	}
	if !waitChatLine(p, "Daemon install: done.") {
		t.Fatal("install ack missing")
	}

	// list: rows formatted.
	stub.reply = `{"ok":true,"data":{"daemons":[{"name":"webmap","module":"github.com/x/mapd","status":"running","restarts":2}]}}`
	s.handleCommand(p, "daemon list")
	if stub.subject != "mc.daemon.list" {
		t.Fatalf("subject %q", stub.subject)
	}
	if !waitChatLine(p, "webmap — github.com/x/mapd@latest [running, 2 restarts]") {
		t.Fatal("list row missing")
	}

	// Manager error surfaces.
	stub.reply = `{"ok":false,"error":"no daemon \"ghost\""}`
	s.handleCommand(p, "daemon uninstall ghost")
	if !waitChatLine(p, `Daemon manager: no daemon "ghost"`) {
		t.Fatal("manager error not surfaced")
	}
}
