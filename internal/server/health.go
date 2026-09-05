package server

import (
	"expvar"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Observability for the world pod. Until this existed the only health signal
// was "the attach port accepts TCP" — which a wedged hub still does, so a
// deadlocked tick loop passed its liveness probe indefinitely — and the only
// memory number was whatever kubectl top could see from outside. This file is
// the engine's own account of itself: a heartbeat the hub stamps every tick,
// a small tick-duration histogram, the container sizes that grow when
// something leaks, and pprof for when a number looks wrong.
//
// Everything is served on ONE opt-in listener (-health), meant to be bound to
// the pod and scraped inside the cluster, never exposed through the ingress.

const (
	// hubStaleAfter is how long without a completed tick counts as "wedged".
	// 100 missed ticks: far past any legitimate long tick, well inside the
	// time a player would notice.
	hubStaleAfter = 5 * time.Second

	// slowTickLog is the tick duration above which the hub logs what it was
	// carrying. A 20 TPS budget is 50 ms; a tick twice that is worth a line.
	slowTickLog = 100 * time.Millisecond
	// slowTickLogEvery rate-limits those lines so a sustained overload does
	// not turn the log into the problem.
	slowTickLogEvery = 5 * time.Second
)

// tickHist is a fixed ring of the most recent tick durations. Written by the
// hub goroutine, read by the health handler; the mutex is uncontended in
// practice (one write per tick, one read per scrape).
type tickHist struct {
	mu       sync.Mutex
	ring     [256]time.Duration
	n        int // total observed (for the ring position and a "warm" flag)
	max      time.Duration
	lastSlow time.Time
}

func (t *tickHist) observe(d time.Duration) {
	t.mu.Lock()
	t.ring[t.n%len(t.ring)] = d
	t.n++
	if d > t.max {
		t.max = d
	}
	t.mu.Unlock()
}

// percentile returns the p-th percentile (0..1) of the recorded window.
func (t *tickHist) percentile(p float64) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.n
	if n > len(t.ring) {
		n = len(t.ring)
	}
	if n == 0 {
		return 0
	}
	buf := make([]time.Duration, n)
	copy(buf, t.ring[:n])
	sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
	i := int(float64(n-1) * p)
	return buf[i]
}

// shouldLogSlow reports whether a slow tick may be logged now (rate-limited).
func (t *tickHist) shouldLogSlow(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now.Sub(t.lastSlow) < slowTickLogEvery {
		return false
	}
	t.lastSlow = now
	return true
}

// hubHealth is what the health endpoint reads. It is deliberately a tiny
// interface so the handler can be tested against a fake hub.
type hubHealth interface {
	lastTickAt() time.Time
}

func (h *hub) lastTickAt() time.Time { return time.Unix(0, h.lastTick.Load()) }

// healthHandler answers /healthz: 200 while the hub is ticking, 503 once it has
// been silent past hubStaleAfter. Before the first tick it reports 503 too —
// the readiness probe on the attach port covers "starting up"; liveness must
// not say "alive" about a loop that has never run.
func healthHandler(h hubHealth, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		last := h.lastTickAt()
		if last.IsZero() || last.UnixNano() == 0 {
			http.Error(w, "hub has not ticked yet", http.StatusServiceUnavailable)
			return
		}
		if age := now().Sub(last); age > hubStaleAfter {
			http.Error(w, fmt.Sprintf("hub stalled: last tick %s ago", age.Round(time.Millisecond)), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	}
}

// serveHealth runs the health/metrics/pprof listener. Non-fatal on failure:
// observability must never be the reason the game does not start.
func (s *Server) serveHealth(addr string) {
	h := s.hub
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler(h, time.Now))

	// expvar: stdlib, dependency-free, JSON at /debug/vars. Everything here
	// is a number that grows when something is wrong.
	stats := expvar.NewMap("tachyne")
	stats.Set("players", expvar.Func(func() any { return len(h.playersRef) }))
	stats.Set("mobs", expvar.Func(func() any { return len(h.mobs) }))
	stats.Set("items", expvar.Func(func() any { return len(h.items) }))
	stats.Set("pending_block_updates", expvar.Func(func() any { return len(h.pending) }))
	stats.Set("chunk_cache", expvar.Func(func() any { return s.world.CacheLen() }))
	stats.Set("light_cache", expvar.Func(func() any { return s.world.LightCacheLen() }))
	stats.Set("block_edits", expvar.Func(func() any { return s.world.EditCount() }))
	stats.Set("tick_ms_p50", expvar.Func(func() any { return h.tickStats.percentile(0.50).Seconds() * 1e3 }))
	stats.Set("tick_ms_p99", expvar.Func(func() any { return h.tickStats.percentile(0.99).Seconds() * 1e3 }))
	stats.Set("tick_age_ms", expvar.Func(func() any { return time.Since(h.lastTickAt()).Seconds() * 1e3 }))
	stats.Set("goroutines", expvar.Func(func() any { return runtime.NumGoroutine() }))
	stats.Set("chunk_cache_backend", expvar.Func(func() any { return s.cacheBackend.Load() }))
	stats.Set("bus_connected", expvar.Func(func() any { return s.busConnected.Load() }))
	mux.Handle("/debug/vars", expvar.Handler())

	// pprof on our own mux, not DefaultServeMux — nothing else in the binary
	// should be reachable here by accident.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("health/metrics/pprof on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("health listener on %s failed: %v (game continues without it)", addr, err)
	}
}

// noteTick is the hub's end-of-tick bookkeeping: the heartbeat the liveness
// probe reads, the duration histogram, and a rate-limited line for any tick
// that blew its budget — with the counts that usually explain why.
func (h *hub) noteTick(started time.Time, players map[int32]*tracked, due int) {
	now := time.Now()
	d := now.Sub(started)
	h.tickStats.observe(d)
	h.lastTick.Store(now.UnixNano())
	if d > slowTickLog && h.tickStats.shouldLogSlow(now) {
		log.Printf("slow tick: %s (players=%d mobs=%d items=%d block_updates=%d pending=%d)",
			d.Round(time.Millisecond), len(players), len(h.mobs), len(h.items), due, len(h.pending))
	}
}

// Keep the compiler honest about the atomic type used for the heartbeat.
var _ = atomic.Int64{}
