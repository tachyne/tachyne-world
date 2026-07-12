// tachyne-daemon is the go-get model applied to running plugins: give it a
// daemon plugin's module path and it pulls the source, builds it locally,
// boots it as its own process attached to the server's bus, and supervises
// it (restart with backoff, prefixed logs). The engine itself never loads or
// builds code — daemons live beside it, crash-isolated, hot add/remove.
//
// One-off:
//
//	tachyne-daemon run github.com/tachyne/tachyne-world/daemons/webmap [-- args...]
//	tachyne-daemon run ./daemons/webmap            (local directory dev loop)
//
// Managed (install/uninstall/reload while everything runs):
//
//	tachyne-daemon -config daemons.json
//
// loads the set from daemons.json, then listens on the bus for control:
//
//	mc.daemon.install   {"module": "...", "version": "v1.2.0", "args": [...]}
//	mc.daemon.uninstall {"name": "webmap"}
//	mc.daemon.restart   {"name": "webmap"}     (rebuilds — @latest hot-reloads)
//	mc.daemon.list
//
// all request-reply with the usual {"ok","data","error"} envelope, so they
// work from any bus client — including the engine's op-only /daemon chat
// command. Installs and uninstalls persist back to daemons.json.
//
// Daemons receive the bus address via NATS_URL. Building requires the Go
// toolchain; binaries are cached under the user cache dir.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
)

type daemonSpec struct {
	Module  string   `json:"module"`            // module path of a main package, or a local dir (./…)
	Version string   `json:"version,omitempty"` // module version (default latest)
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"` // extra KEY=VALUE pairs
}

func (s daemonSpec) name() string { return filepath.Base(s.Module) }

type managed struct {
	spec     daemonSpec
	quit     chan struct{}
	done     chan struct{}
	builtVer string     // module version baked into the binary (go version -m)
	mu       sync.Mutex // guards status/restarts (written by the supervise loop)
	status   string
	restarts int
}

type manager struct {
	mu        sync.Mutex
	daemons   map[string]*managed
	cache     string
	natsURL   string
	statePath string // persisted daemon set ("" = one-off run, no persistence)
	name      string // fleet identity (POD_NAME/hostname) for targeted ops
	reg       *registryClient
}

func main() {
	log.SetFlags(0)
	natsURL := flag.String("nats", "", "bus address handed to daemons as NATS_URL (default: current NATS_URL env, else nats://localhost:4222)")
	config := flag.String("config", "", "daemons.json holding the managed set (rewritten by install/uninstall)")
	cacheDir := flag.String("cache", defaultCache(), "built-binary cache directory")
	name := flag.String("name", "", "this manager's fleet name (default: POD_NAME, else hostname) — targeted ops address mc.daemon.at.<name>.<op>")
	registryURLs := flag.String("registry", "", "comma-separated tachyne plugin registry URLs (default: TACHYNE_REGISTRY env) for name resolution, search, and out-of-date checks")
	flag.Parse()

	url := *natsURL
	if url == "" {
		url = os.Getenv("NATS_URL")
	}
	if url == "" {
		url = "nats://localhost:4222"
	}
	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		log.Fatal(err)
	}
	mgrName := *name
	if mgrName == "" {
		mgrName = os.Getenv("POD_NAME")
	}
	if mgrName == "" {
		mgrName, _ = os.Hostname()
	}
	mgrName = strings.ReplaceAll(mgrName, ".", "-") // dots are NATS subject separators
	m := &manager{daemons: map[string]*managed{}, cache: *cacheDir, natsURL: url,
		name: mgrName, reg: newRegistryClient(*registryURLs)}

	var specs []daemonSpec
	switch {
	case *config != "":
		m.statePath = *config
		if raw, err := os.ReadFile(*config); err == nil {
			if err := json.Unmarshal(raw, &specs); err != nil {
				log.Fatalf("%s: %v", *config, err)
			}
		} else if !os.IsNotExist(err) { // absent file = start empty, install over the bus
			log.Fatal(err)
		}
	case flag.Arg(0) == "run" && flag.NArg() >= 2:
		spec := daemonSpec{Module: flag.Arg(1)}
		if i := strings.Index(spec.Module, "@"); i >= 0 && !strings.HasPrefix(spec.Module, ".") {
			spec.Version = spec.Module[i+1:]
			spec.Module = spec.Module[:i]
		}
		spec.Args = flag.Args()[2:]
		if len(spec.Args) > 0 && spec.Args[0] == "--" { // the conventional separator is ours, not the daemon's
			spec.Args = spec.Args[1:]
		}
		specs = []daemonSpec{spec}
	default:
		log.Fatal("usage: tachyne-daemon run <module[@version] | ./localdir> [-- args...]   or   tachyne-daemon -config daemons.json")
	}

	for _, s := range specs {
		if err := m.install(s); err != nil {
			log.Fatalf("%s: %v", s.Module, err)
		}
	}

	// Bus control plane: managed mode only (a one-off `run` stays plain).
	if m.statePath != "" {
		if nc, err := nats.Connect(url, nats.Name("tachyne-daemon"), nats.MaxReconnects(-1)); err != nil {
			log.Printf("bus control unavailable (%v) — running without install/uninstall", err)
		} else {
			defer nc.Close()
			if err := m.serveControl(nc); err != nil {
				log.Printf("bus control unavailable: %v", err)
			} else {
				log.Printf("manager %q: bus control on mc.daemon.<op> (fleet) and mc.daemon.at.%s.<op> (targeted)", m.name, m.name)
			}
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("stopping daemons…")
	m.mu.Lock()
	var ds []*managed
	for _, d := range m.daemons {
		close(d.quit)
		ds = append(ds, d)
	}
	m.mu.Unlock()
	for _, d := range ds {
		<-d.done
	}
}

// install builds the daemon and starts supervising it.
func (m *manager) install(s daemonSpec) error {
	name := s.name()
	m.mu.Lock()
	if _, exists := m.daemons[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("daemon %q already installed", name)
	}
	m.mu.Unlock()

	bin, err := build(m.cache, s)
	if err != nil {
		return err
	}
	d := &managed{spec: s, quit: make(chan struct{}), done: make(chan struct{}), status: "starting",
		builtVer: builtVersion(bin)}
	m.mu.Lock()
	m.daemons[name] = d
	m.mu.Unlock()
	go m.supervise(d, bin)
	m.persist()
	if !strings.HasPrefix(s.Module, ".") && !strings.HasPrefix(s.Module, "/") && m.reg.enabled() {
		go m.reg.pingInstalled(s.Module) // best-effort install count
	}
	return nil
}

// uninstall stops a daemon and forgets it.
func (m *manager) uninstall(name string) error {
	m.mu.Lock()
	d := m.daemons[name]
	delete(m.daemons, name)
	m.mu.Unlock()
	if d == nil {
		return fmt.Errorf("no daemon %q", name)
	}
	close(d.quit)
	<-d.done
	m.persist()
	return nil
}

// restart rebuilds (an unpinned daemon picks up its latest code — the
// hot-reload path) and boots fresh.
func (m *manager) restart(name string) error {
	m.mu.Lock()
	d := m.daemons[name]
	m.mu.Unlock()
	if d == nil {
		return fmt.Errorf("no daemon %q", name)
	}
	spec := d.spec
	if err := m.uninstall(name); err != nil {
		return err
	}
	return m.install(spec)
}

type listRow struct {
	Manager  string `json:"manager"`
	Name     string `json:"name"`
	Module   string `json:"module"`
	Version  string `json:"version,omitempty"` // pinned spec version
	Built    string `json:"built,omitempty"`   // module version actually running
	Latest   string `json:"latest,omitempty"`  // registry's newest ("" = unlisted/no registry)
	Outdated bool   `json:"outdated"`
	Status   string `json:"status"`
	Restarts int    `json:"restarts"`
}

func (m *manager) list() []listRow {
	m.mu.Lock()
	type snap struct {
		name string
		d    *managed
	}
	snaps := make([]snap, 0, len(m.daemons))
	for name, d := range m.daemons {
		snaps = append(snaps, snap{name, d})
	}
	m.mu.Unlock()
	rows := make([]listRow, 0, len(snaps))
	for _, s := range snaps {
		d := s.d
		d.mu.Lock()
		row := listRow{Manager: m.name, Name: s.name, Module: d.spec.Module,
			Version: d.spec.Version, Built: d.builtVer, Status: d.status, Restarts: d.restarts}
		d.mu.Unlock()
		if m.reg.enabled() && !strings.HasPrefix(row.Module, ".") && !strings.HasPrefix(row.Module, "/") {
			if latest := m.reg.latest(row.Module); latest != "" {
				row.Latest = latest
				// Out of date when the pin (or the built pseudo-version)
				// doesn't match the registry's newest.
				current := row.Version
				if current == "" {
					current = row.Built
				}
				row.Outdated = current != "" && current != latest &&
					!strings.Contains(current, strings.TrimPrefix(latest, "v"))
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// persist rewrites the managed set so a manager restart resumes it.
func (m *manager) persist() {
	if m.statePath == "" {
		return
	}
	m.mu.Lock()
	specs := make([]daemonSpec, 0, len(m.daemons))
	for _, d := range m.daemons {
		specs = append(specs, d.spec)
	}
	m.mu.Unlock()
	raw, err := json.MarshalIndent(specs, "", "  ")
	if err == nil {
		err = os.WriteFile(m.statePath, raw, 0o644)
	}
	if err != nil {
		log.Printf("persist %s: %v", m.statePath, err)
	}
}

// serveControl answers the fleet control plane. Plain mc.daemon.<op> is a
// BROADCAST — every manager on the bus executes it (fleet-wide install);
// mc.daemon.at.<name>.<op> targets one manager. Every reply carries this
// manager's name so scatter-gather callers can attribute answers. Handlers
// run in their own goroutines: installs compile and must not stall the
// subscription.
func (m *manager) serveControl(nc *nats.Conn) error {
	reply := func(msg *nats.Msg, data map[string]any, err error) {
		if msg.Reply == "" {
			return
		}
		if err != nil {
			body, _ := json.Marshal(map[string]any{"ok": false, "manager": m.name, "error": err.Error()})
			msg.Respond(body)
			return
		}
		if data == nil {
			data = map[string]any{}
		}
		data["ok"] = true
		data["manager"] = m.name
		if body, e := json.Marshal(data); e == nil {
			msg.Respond(body)
		}
	}
	_, err := nc.Subscribe("mc.daemon.>", func(msg *nats.Msg) {
		go func() {
			op := strings.TrimPrefix(msg.Subject, "mc.daemon.")
			if rest, targeted := strings.CutPrefix(op, "at."); targeted {
				target, opAt, ok := strings.Cut(rest, ".")
				if !ok {
					return
				}
				if target != m.name {
					return // addressed to another shard's manager
				}
				op = opAt
			}
			switch op {
			case "install":
				var s daemonSpec
				var req struct {
					daemonSpec
					ByName string `json:"name"`
				}
				if json.Unmarshal(msg.Data, &req) != nil || (req.Module == "" && req.ByName == "") {
					reply(msg, nil, fmt.Errorf("install requires module or name"))
					return
				}
				s = req.daemonSpec
				if s.Module == "" { // registry name → module path
					module, err := m.reg.resolve(req.ByName)
					if err != nil {
						reply(msg, nil, err)
						return
					}
					s.Module = module
				}
				log.Printf("[control] install %s@%s", s.Module, orLatest(s.Version))
				reply(msg, nil, m.install(s))
			case "uninstall", "restart", "upgrade":
				var a struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(msg.Data, &a) != nil || a.Name == "" {
					reply(msg, nil, fmt.Errorf("%s requires name", op))
					return
				}
				log.Printf("[control] %s %s", op, a.Name)
				if op == "uninstall" {
					reply(msg, nil, m.uninstall(a.Name))
				} else { // restart and upgrade are the same op: rebuild + boot
					reply(msg, nil, m.restart(a.Name))
				}
			case "list":
				reply(msg, map[string]any{"daemons": m.list()}, nil)
			case "info":
				var a struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(msg.Data, &a) != nil || a.Name == "" {
					reply(msg, nil, fmt.Errorf("info requires name"))
					return
				}
				l, err := m.reg.info(a.Name)
				if err != nil {
					reply(msg, nil, err)
					return
				}
				reply(msg, map[string]any{"plugins": []any{l}}, nil)
			case "rate":
				var a struct {
					Name  string `json:"name"`
					Stars int    `json:"stars"`
				}
				if json.Unmarshal(msg.Data, &a) != nil || a.Name == "" || a.Stars < 1 || a.Stars > 5 {
					reply(msg, nil, fmt.Errorf("rate requires name and stars 1..5"))
					return
				}
				module, err := m.reg.resolve(a.Name)
				if err != nil {
					reply(msg, nil, err)
					return
				}
				reply(msg, nil, m.reg.rate(module, a.Stars))
			case "search":
				var a struct {
					Q string `json:"q"`
				}
				json.Unmarshal(msg.Data, &a)
				if !m.reg.enabled() {
					reply(msg, nil, fmt.Errorf("no registry configured on manager %q", m.name))
					return
				}
				reply(msg, map[string]any{"plugins": m.reg.search(a.Q)}, nil)
			default:
				reply(msg, nil, fmt.Errorf("unknown daemon op %q", op))
			}
		}()
	})
	return err
}

// build fetches + compiles a daemon, returning the cached binary path.
// Module paths go through `go install module@version` (GOBIN pointed at a
// per-version cache slot); local directories build in place.
func build(cache string, s daemonSpec) (string, error) {
	name := s.name()

	if strings.HasPrefix(s.Module, ".") || strings.HasPrefix(s.Module, "/") {
		bin := filepath.Join(cache, "local-"+name)
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = s.Module
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("go build: %w", err)
		}
		return bin, nil
	}

	v := orLatest(s.Version)
	sum := sha256.Sum256([]byte(s.Module + "@" + v))
	slot := filepath.Join(cache, hex.EncodeToString(sum[:8]))
	bin := filepath.Join(slot, name)
	// A pinned version that's already built is reused; "latest" always
	// reinstalls so restart doubles as hot-reload.
	if v != "latest" {
		if _, err := os.Stat(bin); err == nil {
			return bin, nil
		}
	}
	if err := os.MkdirAll(slot, 0o755); err != nil {
		return "", err
	}
	log.Printf("[%s] go install %s@%s", name, s.Module, v)
	cmd := exec.Command("go", "install", s.Module+"@"+v)
	cmd.Env = append(os.Environ(), "GOBIN="+slot)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go install: %w", err)
	}
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("built, but no binary at %s (is %s a main package?)", bin, s.Module)
	}
	return bin, nil
}

// supervise runs the daemon, restarting on exit with exponential backoff
// (reset after a healthy minute), until its quit channel closes.
func (m *manager) supervise(d *managed, bin string) {
	defer close(d.done)
	name := d.spec.name()
	setStatus := func(s string) { d.mu.Lock(); d.status = s; d.mu.Unlock() }
	backoff := time.Second
	for {
		select {
		case <-d.quit:
			setStatus("stopped")
			return
		default:
		}
		cmd := exec.Command(bin, d.spec.Args...)
		cmd.Env = append(append(os.Environ(), "NATS_URL="+m.natsURL), d.spec.Env...)
		cmd.Stdout = prefixed(name)
		cmd.Stderr = prefixed(name)
		start := time.Now()
		log.Printf("[%s] starting", name)
		if err := cmd.Start(); err != nil {
			log.Printf("[%s] start: %v", name, err)
			setStatus("failed")
			return
		}
		setStatus("running")
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		var err error
		select {
		case err = <-done:
		case <-d.quit:
			cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			setStatus("stopped")
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second // it ran fine for a while — fresh slate
		}
		d.mu.Lock()
		d.restarts++
		d.mu.Unlock()
		setStatus("backoff")
		log.Printf("[%s] exited (%v) — restarting in %s", name, err, backoff)
		select {
		case <-time.After(backoff):
		case <-d.quit:
			setStatus("stopped")
			return
		}
		if backoff *= 2; backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

func orLatest(v string) string {
	if v == "" {
		return "latest"
	}
	return v
}

// prefixed returns a writer stamping each line with the daemon's name.
type lineWriter struct {
	prefix string
	buf    []byte
}

func prefixed(name string) *lineWriter { return &lineWriter{prefix: "[" + name + "] "} }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		os.Stderr.WriteString(w.prefix + string(w.buf[:i+1]))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func defaultCache() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "tachyne-daemon")
	}
	return ".tachyne-daemon-cache"
}
