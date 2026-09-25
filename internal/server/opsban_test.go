package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tachyne/tachyne-common/access"
)

// /ban and /op go to the access service: the ban is filed against the
// player's UUID, and the op role is granted.
func TestBanAndOpGoToAccess(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("X-Actor"))
		mu.Unlock()
		if r.URL.Path == "/v1/bans" && r.Method == http.MethodPost {
			if body["kind"] != "uuid" {
				t.Errorf("ban kind %v, want uuid for an online player", body["kind"])
			}
			json.NewEncoder(w).Encode(map[string]int64{"id": 1})
		}
	}))
	defer srv.Close()
	h, s, players, pl := tickCmdHub(t)
	s.Access = access.New(srv.URL, "", 0)
	go func() { // the hub answers onHub while the command waits on it
		for ev := range h.events {
			switch e := ev.(type) {
			case evHubCmd:
				e.fn(players)
			case evKick:
				h.onKick(players, e)
			}
		}
	}()
	s.handleCommand(pl.p, "op tester")
	s.handleCommand(pl.p, "ban tester griefing")
	mu.Lock()
	defer mu.Unlock()
	got := strings.Join(calls, "\n")
	if !strings.Contains(got, "POST /v1/principals/") || !strings.Contains(got, "POST /v1/bans tester") {
		t.Fatalf("access calls:\n%s", got)
	}
}
