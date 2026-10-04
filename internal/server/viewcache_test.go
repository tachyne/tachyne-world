package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The world's cache floor covers the view window the server streams, with
// the ring lighting reads around it.
func TestCacheFloorCoversTheViewWindow(t *testing.T) {
	if want := (2*viewRadius + 3) * (2*viewRadius + 3); world.MinCachedChunks < want {
		t.Errorf("world.MinCachedChunks = %d, want at least %d for view radius %d", world.MinCachedChunks, want, viewRadius)
	}
}
