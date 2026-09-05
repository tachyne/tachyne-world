package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
)

type fakeHub struct{ last time.Time }

func (f fakeHub) lastTickAt() time.Time { return f.last }

// The liveness contract: a hub that ticked recently is alive; one that has
// gone quiet past hubStaleAfter is not — and Kubernetes must be told so with a
// non-2xx, because a wedged hub still accepts TCP on the attach port.
func TestHealthzReportsAStalledHub(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	cases := []struct {
		name string
		last time.Time
		want int
	}{
		{"ticked 50ms ago", now.Add(-50 * time.Millisecond), http.StatusOK},
		{"ticked 4s ago (long but alive)", now.Add(-4 * time.Second), http.StatusOK},
		{"silent for 6s", now.Add(-6 * time.Second), http.StatusServiceUnavailable},
		{"never ticked", time.Unix(0, 0), http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			healthHandler(fakeHub{c.last}, clock)(rec, httptest.NewRequest("GET", "/healthz", nil))
			if rec.Code != c.want {
				t.Errorf("status %d (%q), want %d", rec.Code, rec.Body.String(), c.want)
			}
		})
	}
}

// A running hub stamps the heartbeat every tick, so a live hub reads healthy.
func TestRunningHubStampsHeartbeat(t *testing.T) {
	h := newHub(world.New(1))
	if h.lastTick.Load() != 0 {
		t.Fatal("heartbeat set before the hub ran")
	}
	startHub(t, h)
	deadline := time.Now().Add(3 * time.Second)
	for h.lastTick.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if h.lastTick.Load() == 0 {
		t.Fatal("hub ran for 3s and never stamped a tick")
	}
	rec := httptest.NewRecorder()
	healthHandler(h, time.Now)(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("live hub reported %d: %s", rec.Code, rec.Body.String())
	}
	if h.tickStats.percentile(0.5) <= 0 {
		t.Error("tick histogram recorded nothing")
	}
}

func TestTickHistPercentiles(t *testing.T) {
	var th tickHist
	for i := 1; i <= 100; i++ {
		th.observe(time.Duration(i) * time.Millisecond)
	}
	if p := th.percentile(0.5); p < 49*time.Millisecond || p > 51*time.Millisecond {
		t.Errorf("p50 = %v, want ~50ms", p)
	}
	if p := th.percentile(0.99); p < 98*time.Millisecond {
		t.Errorf("p99 = %v, want ~99ms", p)
	}
	if th.max != 100*time.Millisecond {
		t.Errorf("max = %v", th.max)
	}
	// The ring wraps: 300 observations keep the most recent 256.
	for i := 0; i < 300; i++ {
		th.observe(time.Millisecond)
	}
	if p := th.percentile(0.99); p != time.Millisecond {
		t.Errorf("after wrap p99 = %v, want 1ms (old samples must age out)", p)
	}
}
