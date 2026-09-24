package server

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/anvil"
)

// /version (VersionCommand) and /stop (StopCommand).
//
// /version prints the version the engine is: its canonical game version
// (the ids, registries and world format it speaks), in vanilla's lines.
// They are system messages, so send_command_feedback does not hide them.
// build_time is when this engine was built (its VCS commit time).
//
// /stop is the engine's graceful shutdown: the same path SIGTERM takes —
// every store and dimension saved, then the process exits. In a
// Kubernetes pod the controller starts the engine again, so on the cluster
// /stop is a clean restart; players are disconnected either way.

const (
	versionSeries   = "main"
	versionPackRes  = "97.1"  // the 26.3 client's resource-pack format
	versionPackData = "121.0" // …and its data-pack format
)

// engineBuildTime is the commit time the binary was built from, or
// "unknown" for a build without VCS stamping (tests, go run).
func engineBuildTime() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.time" {
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					return t.UTC().Format(time.RFC3339)
				}
				return s.Value
			}
		}
	}
	return "unknown"
}

func (s *Server) cmdVersion(p *player, args []string) {
	if !s.isOp(p.name) { // dedicated server: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	for _, l := range []string{
		"Server version info:",
		"id = " + idSpaceVersion,
		"name = " + idSpaceVersion,
		fmt.Sprintf("data = %d", anvil.DataVersion),
		"series = " + versionSeries,
		fmt.Sprintf("protocol = %d (0x%x)", protocol.CanonicalProtocol, protocol.CanonicalProtocol),
		"build_time = " + engineBuildTime(),
		"pack_resource = " + versionPackRes,
		"pack_data = " + versionPackData,
		"stable = yes",
	} {
		p.tell(l)
	}
}

// stopDelay lets the "Stopping the server" line reach its reader before
// the process goes.
const stopDelay = 500 * time.Millisecond

func (s *Server) cmdStop(p *player, args []string) {
	if !s.isOp(p.name) { // LEVEL_OWNERS
		p.tell("You don't have permission.")
		return
	}
	if s.Stop == nil {
		p.tell("This server cannot be stopped from inside the game.")
		return
	}
	s.ok(p, "Stopping the server")
	stop := s.Stop
	go func() {
		time.Sleep(stopDelay)
		stop()
	}()
}
