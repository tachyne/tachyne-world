package worldgen

import (
	"testing"
)

// skipHeavy marks a test that is deterministic, single-goroutine and
// expensive (thousands of chunks or trials): it runs in the plain suite,
// and is skipped under -short — which is how the race job runs, since the
// race detector has nothing to find in it and makes it ten times slower.
func skipHeavy(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("heavy deterministic test: skipped under -short (the race run)")
	}
}
