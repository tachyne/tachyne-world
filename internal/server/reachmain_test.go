package server

import (
	"os"
	"testing"
)

// TestMain runs the package with the block reach check off: most fixtures
// click far from where their player stands (see blockReachChecked).
func TestMain(m *testing.M) {
	blockReachChecked = false
	os.Exit(m.Run())
}
