// tachyne-daemon is the go-get model applied to running plugins: give it a
// daemon plugin's module path and it pulls the source, builds it locally,
// boots it as its own process attached to the server's bus, and supervises
// it (restart with backoff, prefixed logs). The engine itself never loads or
// builds code — daemons live beside it, crash-isolated, hot add/remove.
//
// One-off:
//
//	tachyne-daemon run github.com/tachyne/tachyne-world/daemons/webmap [-- args...]
//	tachyne-daemon run github.com/you/yourdaemon@v1.2.0
//	tachyne-daemon run ./daemons/webmap            (local directory dev loop)
//
// A set, supervised together (daemons.json):
//
//	[
//	  {"module": "github.com/tachyne/tachyne-world/daemons/webmap", "args": ["-addr", ":8100"]},
//	  {"module": "github.com/you/yourdaemon", "version": "v1.2.0"}
//	]
//
//	tachyne-daemon -config daemons.json
//
// Daemons receive the bus address via NATS_URL (from --nats or the
// environment). Building requires the Go toolchain; binaries are cached
// under the user cache dir and rebuilt only when the version changes.
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
)

type daemonSpec struct {
	Module  string   `json:"module"`            // module path of a main package, or a local dir (./…)
	Version string   `json:"version,omitempty"` // module version (default latest)
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"` // extra KEY=VALUE pairs
}

func main() {
	log.SetFlags(0)
	natsURL := flag.String("nats", "", "bus address handed to daemons as NATS_URL (default: current NATS_URL env, else nats://localhost:4222)")
	config := flag.String("config", "", "daemons.json describing a set to supervise")
	cacheDir := flag.String("cache", defaultCache(), "built-binary cache directory")
	flag.Parse()

	url := *natsURL
	if url == "" {
		url = os.Getenv("NATS_URL")
	}
	if url == "" {
		url = "nats://localhost:4222"
	}

	var specs []daemonSpec
	switch {
	case *config != "":
		raw, err := os.ReadFile(*config)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.Unmarshal(raw, &specs); err != nil {
			log.Fatalf("%s: %v", *config, err)
		}
	case flag.Arg(0) == "run" && flag.NArg() >= 2:
		spec := daemonSpec{Module: flag.Arg(1)}
		if i := strings.Index(spec.Module, "@"); i >= 0 && !strings.HasPrefix(spec.Module, ".") {
			spec.Version = spec.Module[i+1:]
			spec.Module = spec.Module[:i]
		}
		spec.Args = flag.Args()[2:]
		specs = []daemonSpec{spec}
	default:
		log.Fatal("usage: tachyne-daemon run <module[@version] | ./localdir> [-- args...]   or   tachyne-daemon -config daemons.json")
	}

	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		log.Fatal(err)
	}

	// Build everything first so a typo fails fast, then supervise.
	bins := make([]string, len(specs))
	for i, s := range specs {
		bin, err := build(*cacheDir, s)
		if err != nil {
			log.Fatalf("%s: %v", s.Module, err)
		}
		bins[i] = bin
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	var wg sync.WaitGroup
	quit := make(chan struct{})
	for i, s := range specs {
		wg.Add(1)
		go func(s daemonSpec, bin string) {
			defer wg.Done()
			supervise(s, bin, url, quit)
		}(s, bins[i])
	}
	<-stop
	log.Print("stopping daemons…")
	close(quit)
	wg.Wait()
}

// build fetches + compiles a daemon, returning the cached binary path.
// Module paths go through `go install module@version` (GOBIN pointed at a
// per-version cache slot); local directories build in place.
func build(cache string, s daemonSpec) (string, error) {
	name := filepath.Base(s.Module)

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

	v := s.Version
	if v == "" {
		v = "latest"
	}
	sum := sha256.Sum256([]byte(s.Module + "@" + v))
	slot := filepath.Join(cache, hex.EncodeToString(sum[:8]))
	bin := filepath.Join(slot, name)
	// A pinned version that's already built is reused; "latest" always
	// reinstalls so a new release is picked up on restart.
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
// (reset after a healthy minute), until quit closes.
func supervise(s daemonSpec, bin, natsURL string, quit chan struct{}) {
	name := filepath.Base(s.Module)
	backoff := time.Second
	for {
		select {
		case <-quit:
			return
		default:
		}
		cmd := exec.Command(bin, s.Args...)
		cmd.Env = append(append(os.Environ(), "NATS_URL="+natsURL), s.Env...)
		cmd.Stdout = prefixed(name)
		cmd.Stderr = prefixed(name)
		start := time.Now()
		log.Printf("[%s] starting", name)
		err := cmd.Start()
		if err != nil {
			log.Printf("[%s] start: %v", name, err)
			return
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err = <-done:
		case <-quit:
			cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second // it ran fine for a while — fresh slate
		}
		log.Printf("[%s] exited (%v) — restarting in %s", name, err, backoff)
		select {
		case <-time.After(backoff):
		case <-quit:
			return
		}
		if backoff *= 2; backoff > time.Minute {
			backoff = time.Minute
		}
	}
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
