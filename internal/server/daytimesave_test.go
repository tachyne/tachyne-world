package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The day clock is saved (level.dat's DayTime) and comes back on the next
// boot; it used to reset to sunrise with every restart.
func TestDayTimeSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	h := newHub(world.New(1))
	h.rulesPath = path
	h.dayTime.Store(123456)
	h.saveRules()

	h2 := newHub(world.New(1))
	h2.rulesPath = path
	h2.loadRules()
	if got := h2.dayTime.Load(); got != 123456 {
		t.Fatalf("day time after a restart: %d, want 123456", got)
	}
}
