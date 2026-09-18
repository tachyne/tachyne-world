//go:build race

package server

import "time"

func init() { hubTestWait = 30 * time.Second } // the detector makes the hub loop several times slower
